// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package config

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"gitlab.jiagouyun.com/cloudcare-tools/datakit"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/sinkfuncs"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/dataway"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/parser"
)

func (c *Config) loadSinkEnvs() error {
	sinkMetric := datakit.GetEnv("ENV_SINK_M")
	sinkNetwork := datakit.GetEnv("ENV_SINK_N")
	sinkKeyEvent := datakit.GetEnv("ENV_SINK_K")
	sinkObject := datakit.GetEnv("ENV_SINK_O")
	sinkCustomObject := datakit.GetEnv("ENV_SINK_CO")
	sinkLogging := datakit.GetEnv("ENV_SINK_L")
	sinkTracing := datakit.GetEnv("ENV_SINK_T")
	sinkRUM := datakit.GetEnv("ENV_SINK_R")
	sinkSecurity := datakit.GetEnv("ENV_SINK_S")
	sinkProfiling := datakit.GetEnv("ENV_SINK_P")

	categoryShorts := []string{
		datakit.SinkCategoryMetric,
		datakit.SinkCategoryNetwork,
		datakit.SinkCategoryKeyEvent,
		datakit.SinkCategoryObject,
		datakit.SinkCategoryCustomObject,
		datakit.SinkCategoryLogging,
		datakit.SinkCategoryTracing,
		datakit.SinkCategoryRUM,
		datakit.SinkCategorySecurity,
		datakit.SinkCategoryProfiling,
	}

	args := []string{
		sinkMetric,
		sinkNetwork,
		sinkKeyEvent,
		sinkObject,
		sinkCustomObject,
		sinkLogging,
		sinkTracing,
		sinkRUM,
		sinkSecurity,
		sinkProfiling,
	}

	sinks, err := sinkfuncs.GetSinkFromEnvs(categoryShorts, args)
	if err != nil {
		return err
	}
	c.Sinks.Sink = sinks

	return nil
}

func (c *Config) loadElectionEnvs() {
	if v := datakit.GetEnv("ENV_ENABLE_ELECTION"); v == "" {
		return
	}

	c.Election.Enable = true

	// default election namespace is `default`
	if v := datakit.GetEnv("ENV_NAMESPACE"); v != "" {
		c.Election.Namespace = v
	}

	if v := datakit.GetEnv("ENV_ENABLE_ELECTION_NAMESPACE_TAG"); v != "" {
		// add to global-env-tags
		c.Election.EnableNamespaceTag = true
		c.Election.Tags["election_namespace"] = c.Election.Namespace
	}

	if v := datakit.GetEnv("ENV_CLUSTER_NAME_K8S"); v != "" {
		c.Election.Tags["cluster_name_k8s"] = v
	}

	for _, x := range []string{
		"ENV_GLOBAL_ELECTION_TAGS",
		"ENV_GLOBAL_ENV_TAGS", // Deprecated
	} {
		if v := datakit.GetEnv(x); v != "" {
			for k, v := range ParseGlobalTags(v) {
				c.Election.Tags[k] = v
			}
			break
		}
	}
}

func (c *Config) loadIOEnvs() {
	if v := datakit.GetEnv("ENV_IO_MAX_CACHE_COUNT"); v != "" {
		val, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			l.Warnf("invalid env key ENV_IO_MAX_CACHE_COUNT, value %s, ignored", v)
		} else {
			if val < 1000 {
				l.Warnf("reset cache count from %d to %d", val, 1000)
			} else {
				l.Infof("set cache count to %d", val)
				c.IOConf.MaxCacheCount = int(val)
			}
		}
	}

	if v := datakit.GetEnv("ENV_IO_ENABLE_CACHE"); v != "" {
		l.Info("ENV_IO_ENABLE_CACHE enabled")
		c.IOConf.EnableCache = true
	}

	if v := datakit.GetEnv("ENV_IO_CACHE_MAX_SIZE_GB"); v != "" {
		val, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			l.Warnf("invalid env key ENV_IO_CACHE_MAX_SIZE_GB, value %s, err: %s ignored", v, err)
		} else {
			l.Infof("set ENV_IO_CACHE_MAX_SIZE_GB to %d", val)
			c.IOConf.CacheSizeGB = int(val)
		}
	}

	if v := datakit.GetEnv("ENV_IO_FLUSH_INTERVAL"); v != "" {
		du, err := time.ParseDuration(v)
		if err != nil {
			l.Warnf("invalid env key ENV_IO_FLUSH_INTERVAL, value %s, err: %s ignored", v, err)
		} else {
			l.Infof("set ENV_IO_FLUSH_INTERVAL to %s", du)
			c.IOConf.FlushInterval = v
		}
	}

	if v := datakit.GetEnv("ENV_IO_CACHE_CLEAN_INTERVAL"); v != "" {
		du, err := time.ParseDuration(v)
		if err != nil {
			l.Warnf("invalid env key ENV_IO_CACHE_CLEAN_INTERVAL, value %s, err: %s ignored", v, err)
		} else {
			l.Infof("set ENV_IO_CACHE_CLEAN_INTERVAL to %s", du)
			c.IOConf.CacheCleanInterval = v
		}
	}
}

//nolint:funlen
func (c *Config) LoadEnvs() error {
	if c.IOConf == nil {
		c.IOConf = &io.IOConfig{}
	}

	c.loadIOEnvs()

	if v := datakit.GetEnv("ENV_IPDB"); v != "" {
		switch v {
		case "iploc":
			c.Pipeline.IPdbType = v
		default:
			l.Warnf("unknown IPDB type: %s, ignored", v)
		}
	}

	if v := datakit.GetEnv("ENV_REFER_TABLE_URL"); v != "" {
		c.Pipeline.ReferTableURL = v
	}

	if v := datakit.GetEnv("ENV_REFER_TABLE_PULL_INTERVAL"); v != "" {
		c.Pipeline.ReferTablePullInterval = v
	}

	if v := datakit.GetEnv("ENV_REFER_TABLE_USE_SQLITE"); v != "" {
		c.Pipeline.UseSQLite = true
	}

	if v := datakit.GetEnv("ENV_REFER_TABLE_SQLITE_MEM_MODE"); v != "" {
		c.Pipeline.SQLiteMemMode = true
	}

	if v := datakit.GetEnv("ENV_REQUEST_RATE_LIMIT"); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err != nil {
			l.Warnf("invalid ENV_REQUEST_RATE_LIMIT, expect int or float, got %s, ignored", v)
		} else {
			c.HTTPAPI.RequestRateLimit = x
		}
	}

	for _, x := range []string{
		"ENV_K8S_NODE_NAME",
		"NODE_NAME", // Deprecated
	} {
		if v := datakit.GetEnv(x); v != "" {
			c.Hostname = v
			datakit.DatakitHostName = c.Hostname
			break
		}
	}

	c.loadElectionEnvs()

	for _, x := range []string{
		"ENV_GLOBAL_HOST_TAGS",
		"ENV_GLOBAL_TAGS", // Deprecated
	} {
		if v := datakit.GetEnv(x); v != "" {
			for k, v := range ParseGlobalTags(v) {
				c.GlobalHostTags[k] = v
			}
			break
		}
	}

	// set logging
	if v := datakit.GetEnv("ENV_LOG_LEVEL"); v != "" {
		c.Logging.Level = v
	}

	if v := datakit.GetEnv("ENV_LOG"); v != "" {
		c.Logging.Log = v
	}

	if v := datakit.GetEnv("ENV_GIN_LOG"); v != "" {
		c.Logging.GinLog = v
	}

	if v := datakit.GetEnv("ENV_DISABLE_LOG_COLOR"); v != "" {
		c.Logging.DisableColor = true
	}

	// 多个 dataway 支持 ',' 分割
	if v := datakit.GetEnv("ENV_DATAWAY"); v != "" {
		if c.DataWayCfg == nil {
			c.DataWayCfg = &dataway.DataWayCfg{}
		}
		c.DataWayCfg.URLs = strings.Split(v, ",")
	}

	if v := datakit.GetEnv("ENV_DATAWAY_TIMEOUT"); v != "" {
		if c.DataWayCfg == nil {
			c.DataWayCfg = &dataway.DataWayCfg{}
		}
		_, err := time.ParseDuration(v)
		if err != nil {
			l.Warnf("invalid ENV_DATAWAY_TIMEOUT: %s", v)
			c.DataWayCfg.HTTPTimeout = "30s"
		} else {
			c.DataWayCfg.HTTPTimeout = v
		}
	}

	if v := datakit.GetEnv("ENV_DATAWAY_ENABLE_HTTPTRACE"); v != "" {
		c.DataWayCfg.EnableHTTPTrace = true
	}

	if v := datakit.GetEnv("ENV_DATAWAY_HTTP_PROXY"); v != "" {
		c.DataWayCfg.HTTPProxy = v
		c.DataWayCfg.Proxy = true
	}

	if v := datakit.GetEnv("ENV_DATAWAY_MAX_IDLE_CONNS_PER_HOST"); v != "" {
		if c.DataWayCfg == nil {
			c.DataWayCfg = &dataway.DataWayCfg{}
		}
		value, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			if value <= 0 {
				l.Warnf("invalid ENV_DATAWAY_MAX_IDLE_CONNS_PER_HOST: %s", v)
			} else {
				c.DataWayCfg.MaxIdleConnsPerHost = int(value)
			}
		}
	}

	if v := datakit.GetEnv("ENV_HOSTNAME"); v != "" {
		c.Hostname = v
	}

	if v := datakit.GetEnv("ENV_NAME"); v != "" {
		c.Name = v
	}

	// HTTP server setting
	if v := datakit.GetEnv("ENV_HTTP_LISTEN"); v != "" {
		c.HTTPAPI.Listen = v
	}

	if v := datakit.GetEnv("ENV_HTTP_TIMEOUT"); v != "" {
		c.HTTPAPI.Timeout = v
	}

	if v := datakit.GetEnv("ENV_HTTP_CLOSE_IDLE_CONNECTION"); v != "" {
		c.HTTPAPI.CloseIdleConnection = true
	}

	if v := datakit.GetEnv("ENV_HTTP_PUBLIC_APIS"); v != "" {
		c.HTTPAPI.PublicAPIs = strings.Split(v, ",")
	}

	// filters
	if v := datakit.GetEnv("ENV_IO_FILTERS"); v != "" {
		var x map[string][]string
		if err := json.Unmarshal([]byte(v), &x); err != nil {
			l.Warnf("json.Unmarshal: %s, ignored", err)
		} else {
			for k, arr := range x {
				for _, c := range arr {
					if parser.GetConds(c) == nil {
						l.Warnf("invalid filter condition on %s: %s, ignored", k, c)
					} else {
						l.Debugf("filter condition ok: %s", c)
					}
				}
			}

			c.IOConf.Filters = x
		}
	}

	// DCA settings
	if v := datakit.GetEnv("ENV_DCA_LISTEN"); v != "" {
		c.DCAConfig.Enable = true
		c.DCAConfig.Listen = v
	}

	if v := datakit.GetEnv("ENV_DCA_WHITE_LIST"); v != "" {
		c.DCAConfig.WhiteList = strings.Split(v, ",")
	}

	// RUM related
	if v := datakit.GetEnv("ENV_RUM_ORIGIN_IP_HEADER"); v != "" {
		c.HTTPAPI.RUMOriginIPHeader = v
	}

	if v := datakit.GetEnv("ENV_RUM_APP_ID_WHITE_LIST"); v != "" {
		c.HTTPAPI.RUMAppIDWhiteList = strings.Split(v, ",")
	}

	if v := datakit.GetEnv("ENV_DISABLE_404PAGE"); v != "" {
		c.HTTPAPI.Disable404Page = true
	}

	if v := datakit.GetEnv("ENV_ENABLE_PPROF"); v != "" {
		c.EnablePProf = true
	}

	if v := datakit.GetEnv("ENV_PPROF_LISTEN"); v != "" {
		c.PProfListen = v
	}

	if v := datakit.GetEnv("ENV_DISABLE_PROTECT_MODE"); v != "" {
		c.ProtectMode = false
	}

	for _, x := range []string{
		"ENV_DEFAULT_ENABLED_INPUTS",
		"ENV_ENABLE_INPUTS", // Deprecated
	} {
		if v := datakit.GetEnv(x); v != "" {
			if v == "-" {
				l.Warnf("no default inputs enabled!")
			} else {
				c.DefaultEnabledInputs = strings.Split(v, ",")
			}
			break
		}
	}

	// k8s ENV confd
	if backend := datakit.GetEnv("ENV_CONFD_BACKEND"); backend != "" {
		authToken := datakit.GetEnv("ENV_CONFD_AUTH_TOKEN")
		authType := datakit.GetEnv("ENV_CONFD_AUTH_TYPE")
		basicAuthBool := datakit.GetEnv("ENV_CONFD_BASIC_AUTH")
		clientCaKeys := datakit.GetEnv("ENV_CONFD_CLIENT_CA_KEYS")
		clientCert := datakit.GetEnv("ENV_CONFD_CLIENT_CERT")
		clientKey := datakit.GetEnv("ENV_CONFD_CLIENT_KEY")
		clientInsecureBool := datakit.GetEnv("ENV_CONFD_CLIENT_INSECURE")
		backendNodesArry := datakit.GetEnv("ENV_CONFD_BACKEND_NODES")
		password := datakit.GetEnv("ENV_CONFD_PASSWORD")
		scheme := datakit.GetEnv("ENV_CONFD_SCHEME")
		table := datakit.GetEnv("ENV_CONFD_TABLE")
		separator := datakit.GetEnv("ENV_CONFD_SEPARATOR")
		username := datakit.GetEnv("ENV_CONFD_USERNAME")
		appID := datakit.GetEnv("ENV_CONFD_APP_ID")
		userID := datakit.GetEnv("ENV_CONFD_USER_ID")
		roleID := datakit.GetEnv("ENV_CONFD_ROLE_ID")
		secretID := datakit.GetEnv("ENV_CONFD_SECRET_ID")
		filter := datakit.GetEnv("ENV_CONFD_FILTER")
		path := datakit.GetEnv("ENV_CONFD_PATH")
		role := datakit.GetEnv("ENV_CONFD_ROLE")
		accessKey := datakit.GetEnv("ENV_CONFD_ACCESS_KEY")
		secretKey := datakit.GetEnv("ENV_CONFD_SECRET_KEY")
		circleIntervalInt := datakit.GetEnv("ENV_CONFD_CIRCLE_INTERVAL")
		confdNamespace := datakit.GetEnv("ENV_CONFD_CONFD_NAMESPACE")
		pipelineNamespace := datakit.GetEnv("ENV_CONFD_PIPELINE_NAMESPACE")
		region := datakit.GetEnv("ENV_CONFD_REGION")

		// some data types need to be converted
		var backendNodes []string
		err := json.Unmarshal([]byte(backendNodesArry), &backendNodes)
		if err != nil {
			l.Warnf("parse ENV_CONFD_BACKEND_NODES: %s, ignore", err)
			backendNodes = make([]string, 0)
		}
		basicAuth := false
		if basicAuthBool == "true" {
			basicAuth = true
		}
		clientInsecure := false
		if clientInsecureBool == "true" {
			clientInsecure = true
		}
		circleInterval := 60
		if interval, err := strconv.Atoi(circleIntervalInt); err == nil {
			circleInterval = interval
		} else {
			l.Warnf("parse ENV_CONFD_CIRCLE_INTERVAL: %s, ignore", err)
		}

		c.Confds = append(c.Confds, &ConfdCfg{
			Enable:            true,
			Backend:           backend,
			AuthToken:         authToken,
			AuthType:          authType,
			BasicAuth:         basicAuth,
			ClientCaKeys:      clientCaKeys,
			ClientCert:        clientCert,
			ClientKey:         clientKey,
			ClientInsecure:    clientInsecure,
			BackendNodes:      append(backendNodes[0:0], backendNodes...),
			Password:          password,
			Scheme:            scheme,
			Table:             table,
			Separator:         separator,
			Username:          username,
			AppID:             appID,
			UserID:            userID,
			RoleID:            roleID,
			SecretID:          secretID,
			Filter:            filter,
			Path:              path,
			Role:              role,
			AccessKey:         accessKey,
			SecretKey:         secretKey,
			CircleInterval:    circleInterval,
			ConfdNamespace:    confdNamespace,
			PipelineNamespace: pipelineNamespace,
			Region:            region,
		})
	}

	if v := datakit.GetEnv("ENV_GIT_URL"); v != "" {
		interval := datakit.GetEnv("ENV_GIT_INTERVAL")
		keyPath := datakit.GetEnv("ENV_GIT_KEY_PATH")
		keyPasswd := datakit.GetEnv("ENV_GIT_KEY_PW")
		branch := datakit.GetEnv("ENV_GIT_BRANCH")

		c.GitRepos = &GitRepost{
			PullInterval: interval,
			Repos: []*GitRepository{
				{
					Enable:                true,
					URL:                   v,
					SSHPrivateKeyPath:     keyPath,
					SSHPrivateKeyPassword: keyPasswd,
					Branch:                branch,
				}, // GitRepository
			}, // Repos
		} // GitRepost
	}

	if err := c.loadSinkEnvs(); err != nil {
		l.Fatalf("loadSinkEnvs failed: %v", err)
		return err
	}
	if v := datakit.GetEnv("ENV_LOG_SINK_DETAIL"); v != "" {
		c.LogSinkDetail = true
	}

	if v := datakit.GetEnv("ENV_ULIMIT"); v != "" {
		u, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			l.Warnf("invalid ulimit input through ENV_ULIMIT: %v", err)
		} else {
			c.Ulimit = u
		}
	}

	return nil
}
