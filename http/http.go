// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

// Package http is datakit's HTTP server
package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"

	// nolint:gosec
	_ "net/http/pprof"
	"os"
	"runtime"
	"time"

	"github.com/GuanceCloud/cliutils"
	"github.com/GuanceCloud/cliutils/logger"
	uhttp "github.com/GuanceCloud/cliutils/network/http"
	"github.com/GuanceCloud/timeout"
	"github.com/gin-gonic/gin"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/git"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/dataway"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

var (
	l              = logger.DefaultSLogger("http")
	ginLog         string
	log            string
	ginReleaseMode = true

	enablePprof = false
	pprofListen = ":6060"

	pprofSrv *http.Server

	enableRequestLogger = false

	uptime = time.Now()

	dw        dataway.DataWay
	apiConfig = &APIConfig{}
	dcaConfig *DCAConfig

	ginRotate = 32 // MB

	g = datakit.G("http")

	DcaToken = ""

	semReload          *cliutils.Sem // [http server](the normal one, not dca nor pprof) reload signal
	semReloadCompleted *cliutils.Sem // [http server](the normal one, not dca nor pprof) reload completed signal
)

type Option struct {
	Log       string
	GinLog    string
	GinRotate int
	APIConfig *APIConfig
	DataWay   dataway.DataWay
	DCAConfig *DCAConfig

	GinReleaseMode bool

	PProf       bool
	PProfListen string
}

type APIConfig struct {
	RUMOriginIPHeader string   `toml:"rum_origin_ip_header"`
	Listen            string   `toml:"listen"`
	Disable404Page    bool     `toml:"disable_404page"`
	RUMAppIDWhiteList []string `toml:"rum_app_id_white_list"`
	PublicAPIs        []string `toml:"public_apis"`
	RequestRateLimit  float64  `toml:"request_rate_limit,omitzero"`

	Timeout             string `toml:"timeout"`
	CloseIdleConnection bool   `toml:"close_idle_connection"`
	timeoutDuration     time.Duration
}

func Start(o *Option) {
	l = logger.SLogger("http")

	ginLog = o.GinLog
	log = o.Log

	enablePprof = o.PProf
	if o.PProfListen != "" {
		pprofListen = o.PProfListen
	}

	ginReleaseMode = o.GinReleaseMode
	ginRotate = o.GinRotate

	apiConfig = o.APIConfig
	if apiConfig.RequestRateLimit > 0.0 {
		l.Infof("set request limit to %f", apiConfig.RequestRateLimit)
		reqLimiter = setupLimiter(apiConfig.RequestRateLimit)
	} else {
		l.Infof("set request limit not set: %f", apiConfig.RequestRateLimit)
	}

	apiConfig.timeoutDuration = 30 * time.Second
	switch apiConfig.Timeout {
	case "":
	default:
		du, err := time.ParseDuration(apiConfig.Timeout)
		if err == nil {
			apiConfig.timeoutDuration = du
		}
	}

	dw = o.DataWay
	dcaConfig = o.DCAConfig

	// start HTTP server
	g.Go(func(ctx context.Context) error {
		HTTPStart()
		l.Info("http goroutine exit")
		return nil
	})

	// DCA server require dataway
	if dcaConfig.Enable {
		if dw == nil {
			l.Warn("Ignore to start DCA server because dataway is not set!")
		} else {
			g.Go(func(ctx context.Context) error {
				dcaHTTPStart()
				l.Info("DCA http goroutine exit")
				return nil
			})
		}
	}

	// start pprof if enabled
	if enablePprof {
		pprofSrv = &http.Server{
			Addr: pprofListen,
		}

		g.Go(func(ctx context.Context) error {
			tryStartServer(pprofSrv, true, semReload, semReloadCompleted)
			l.Info("pprof server exit")
			return nil
		})
	}
}

type welcome struct {
	Version string
	BuildAt string
	Uptime  string
	OS      string
	Arch    string
}

func SetAPIConfig(c *APIConfig) {
	apiConfig = c
}

func page404(c *gin.Context) {
	w := &welcome{
		Version: datakit.Version,
		BuildAt: git.BuildAt,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}

	c.Writer.Header().Set("Content-Type", "text/html")
	t := template.New(``)
	t, err := t.Parse(welcomeMsgTemplate)
	if err != nil {
		l.Error("parse welcome msg failed: %s", err.Error())
		uhttp.HttpErr(c, err)
		return
	}

	buf := &bytes.Buffer{}
	w.Uptime = fmt.Sprintf("%v", time.Since(uptime))
	if err := t.Execute(buf, w); err != nil {
		l.Error("build html failed: %s", err.Error())
		uhttp.HttpErr(c, err)
		return
	}

	c.String(http.StatusNotFound, buf.String())
}

func setupGinLogger() (gl io.Writer) {
	gin.DisableConsoleColor()

	// set gin logger
	l.Infof("set gin log to %s", ginLog)
	if ginLog == "stdout" {
		gl = os.Stdout
	} else {
		gl = &lumberjack.Logger{
			Filename:   ginLog,
			MaxSize:    ginRotate, // MB
			MaxBackups: 5,
			MaxAge:     30, // day
		}
	}

	return
}

func setVersionInfo(c *gin.Context) {
	c.Header("X-DataKit", fmt.Sprintf("%s/%s", datakit.Version, git.BuildAt))
}

func timeoutResponse(c *gin.Context) {
	c.String(http.StatusRequestTimeout, fmt.Sprintf("timeout(%s)", apiConfig.timeoutDuration))
}

func dkHTTPTimeout() gin.HandlerFunc {
	return timeout.New(
		timeout.WithTimeout(apiConfig.timeoutDuration),
		timeout.WithHandler(func(c *gin.Context) {
			c.Next()
		}),
		timeout.WithResponse(timeoutResponse),
	)
}

func setupRouter() *gin.Engine {
	if ginReleaseMode {
		l.Info("set gin in release mode")
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// use whitelist config
	if len(apiConfig.PublicAPIs) != 0 {
		router.Use(getAPIWhiteListMiddleware())
	}

	router.Use(setVersionInfo)

	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Formatter: uhttp.GinLogFormmatter,
		Output:    setupGinLogger(),
	}))

	router.Use(gin.Recovery())
	router.Use(uhttp.CORSMiddleware)
	router.Use(dkHTTPTimeout())

	if !apiConfig.Disable404Page {
		router.NoRoute(page404)
	}

	if enableRequestLogger {
		router.Use(uhttp.RequestLoggerMiddleware)
	}

	applyHTTPRoute(router)

	router.GET("/stats", rawHTTPWraper(reqLimiter, apiGetDatakitStats))
	router.GET("/stats/:type", rawHTTPWraper(reqLimiter, apiGetDatakitStatsByType))
	// router.GET("/stats/input", rawHTTPWraper(reqLimiter, apiGetInputStats))
	router.GET("/monitor", apiGetDatakitMonitor)
	router.GET("/restart", apiRestart)

	router.GET("/v1/workspace", apiWorkspace)
	router.GET("/v1/ping", rawHTTPWraper(reqLimiter, apiPing))
	router.POST("/v1/lasterror", apiGetDatakitLastError)

	router.POST("/v1/write/:category", rawHTTPWraper(reqLimiter, apiWrite, &apiWriteImpl{}))

	router.POST("/v1/query/raw", apiQueryRaw)
	router.POST("/v1/object/labels", apiCreateOrUpdateObjectLabel)
	router.DELETE("/v1/object/labels", apiDeleteObjectLabel)

	router.POST("/v1/pipeline/debug", rawHTTPWraper(reqLimiter, apiPipelineDebugHandler))
	router.POST("/v1/dialtesting/debug", rawHTTPWraper(reqLimiter, apiDebugDialtestingHandler))
	return router
}

func getAPIWhiteListMiddleware() gin.HandlerFunc {
	publicAPITable := make(map[string]struct{}, len(apiConfig.PublicAPIs))
	for _, apiPath := range apiConfig.PublicAPIs {
		apiPath = strings.TrimSpace(apiPath)
		if len(apiPath) > 0 && apiPath[0] != '/' {
			apiPath = "/" + apiPath
		}
		publicAPITable[apiPath] = struct{}{}
	}

	return func(c *gin.Context) {
		cliIP := net.ParseIP(c.ClientIP())
		if _, ok := publicAPITable[c.Request.URL.Path]; !ok && !cliIP.IsLoopback() {
			uhttp.HttpErr(c, uhttp.Errorf(ErrPublicAccessDisabled,
				"api %s disabled from IP %s, only loopback(localhost) allowed",
				c.Request.URL.Path, cliIP.String()))
			c.Abort()
			return
		}
		c.Next()
	}
}

func HTTPStart() {
	refreshRebootSem()
	l.Debugf("HTTP bind addr:%s", apiConfig.Listen)
	srv := &http.Server{
		Addr:    apiConfig.Listen,
		Handler: setupRouter(),
	}

	if apiConfig.CloseIdleConnection {
		srv.ReadTimeout = apiConfig.timeoutDuration
	}

	g.Go(func(ctx context.Context) error {
		l.Info("start HTTP metric goroutine")
		metrics()
		return nil
	})

	g.Go(func(ctx context.Context) error {
		tryStartServer(srv, true, semReload, semReloadCompleted)
		l.Info("http server exit")
		return nil
	})

	l.Debug("http server started")

	stopFunc := func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			l.Errorf("Failed of http server shutdown, err: %s", err.Error())
		} else {
			l.Info("http server shutdown ok")
		}

		if enablePprof {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := pprofSrv.Shutdown(ctx); err != nil {
				l.Error(err)
			}
			l.Infof("pprof stopped")
		}
	}

	for {
		select {
		case <-datakit.Exit.Wait():
			stopFunc()
			return
		case <-semReload.Wait():
			l.Info("[HttpServer] reload detected")
			stopFunc()
			if semReloadCompleted != nil {
				l.Debug("[HttpServer] before reload completed")
				semReloadCompleted.Close()
				l.Debug("[HttpServer] after reload completed")
			}
			return
		}
	}
}

func refreshRebootSem() {
	semReload = cliutils.NewSem()
	semReloadCompleted = cliutils.NewSem()
}

func ReloadTheNormalServer() {
	if semReload != nil {
		semReload.Close()

		// wait stop completed
		if semReloadCompleted != nil {
			l.Debug("[HttpServer] check wait")

			<-semReloadCompleted.Wait()
			l.Info("[HttpServer] reload stopped")

			// start HTTP server
			g.Go(func(ctx context.Context) error {
				HTTPStart()
				l.Info("http goroutine exit")
				return nil
			})

			return
		}
	}
}

func tryStartServer(srv *http.Server, canReload bool, semReload, semReloadCompleted *cliutils.Sem) {
	retryCnt := 0

	for {
		select {
		case <-datakit.Exit.Wait():
			l.Info("tryStartServer exit")
			return
		default:
			if canReload && semReload != nil {
				select {
				case <-semReload.Wait():
					l.Info("tryStartServer reload detected")

					if semReloadCompleted != nil {
						semReloadCompleted.Close()
					}
					return
				default:
				}
			}
		}

		if portInUse(srv.Addr) {
			l.Warnf("start server at %s ,Port is already used", srv.Addr)
		} else {
			break
		}
		time.Sleep(time.Second)
	}

	listener, err := initListener(srv.Addr)
	if err != nil {
		l.Errorf("initListener failed: %v", err)
		return
	}

	closeListener := func() {
		if listener != nil {
			err = listener.Close()
			if err != nil {
				l.Warnf("listener.Close failed: %v", err)
			}
		}
	}

	defer closeListener()

	for {
		l.Infof("try start server at %s(retrying %d)...", srv.Addr, retryCnt)
		if err = srv.Serve(listener); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				l.Warnf("start server at %s failed: %s, retrying(%d)...", srv.Addr, err.Error(), retryCnt)
				retryCnt++
			} else {
				l.Debugf("server(%s) stopped on: %s", srv.Addr, err.Error())
				closeListener()
				break
			}

			// retry
			time.Sleep(time.Second)
		}
	}
}

func portInUse(addr string) bool {
	timeout := time.Millisecond * 100
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	defer conn.Close() //nolint:errcheck
	return true
}

func checkToken(r *http.Request) error {
	if dw == nil {
		return ErrInvalidToken
	}
	localTokens := dw.GetTokens()
	if len(localTokens) == 0 {
		return ErrInvalidToken
	}

	tkn := r.URL.Query().Get("token")

	if tkn == "" || tkn != localTokens[0] {
		return ErrInvalidToken
	}

	return nil
}

func initListener(lsn string) (net.Listener, error) {
	var (
		listener net.Listener
		err      error
	)

	if filepath.IsAbs(lsn) {
		if err = os.RemoveAll(lsn); err != nil {
			return nil, fmt.Errorf("os.RemoveAll: %w", err)
		}

		if listener, err = net.Listen("unix", lsn); err != nil {
			return nil, fmt.Errorf(`net.Listen("unix"): %w`, err)
		}
		return listener, nil
	}

	// netip.ParseAddrPort can't parse `localhost', see:
	//  https://pkg.go.dev/net/netip#ParseAddrPort
	if strings.Contains(lsn, "localhost") {
		lsn = strings.ReplaceAll(lsn, "localhost", "127.0.0.1")
	}

	// ipv6 or ipv6
	if addrPort, err := netip.ParseAddrPort(lsn); err != nil {
		return nil, fmt.Errorf("netip.ParseAddrPort: %w", err)
	} else {
		switch {
		case addrPort.Addr().Is6():
			listener, err = net.Listen("tcp6", lsn)
			if err != nil {
				return nil, fmt.Errorf("net.Listen(tcp6): %w", err)
			}
		default: // Is4:
			listener, err = net.Listen("tcp4", lsn)
			if err != nil {
				return nil, fmt.Errorf("net.Listen(tcp4): %w", err)
			}
		}
	}

	return listener, nil
}
