// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

// Package io implements datakits data transfer among inputs.
package io

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	lp "github.com/GuanceCloud/cliutils/lineproto"
	"github.com/GuanceCloud/cliutils/logger"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/dataway"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/filter"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/sender"
	pb "google.golang.org/protobuf/proto"
)

var (
	log = logger.DefaultSLogger("io")

	g = datakit.G("io")
)

type IOConfig struct {
	FeedChanSize int `toml:"feed_chan_size,omitzero"`

	MaxCacheCount                  int `toml:"max_cache_count"`
	MaxDynamicCacheCountDeprecated int `toml:"max_dynamic_cache_count,omitzero"`

	FlushInterval string `toml:"flush_interval"`

	OutputFile       string   `toml:"output_file"`
	OutputFileInputs []string `toml:"output_file_inputs"`

	EnableCache        bool   `toml:"enable_cache"`
	CacheSizeGB        int    `toml:"cache_max_size_gb"`
	CacheCleanInterval string `toml:"cache_clean_interval"`

	Filters map[string][]string `toml:"filters"`
}

type IO struct {
	conf *IOConfig

	dw dataway.DataWay

	chans map[string]chan *iodata
	fcs   map[string]*failCache

	inLastErr chan *lastError

	inputstats map[string]*InputsStat

	lock sync.RWMutex

	fd *os.File

	flushInterval time.Duration

	droppedTotal int64

	outputFileSize int64
	sender         *sender.Sender
}

func (x *IO) matchOutputFileInput(feedName string) bool {
	// NOTE: if no inputs configure, all inputs matched
	if len(x.conf.OutputFileInputs) == 0 {
		return true
	}

	for _, v := range x.conf.OutputFileInputs {
		if v == feedName {
			return true
		}
	}
	return false
}

func (x *IO) cacheData(c *consumer, d *iodata, tryClean bool) {
	if d == nil {
		log.Warn("get empty data, ignored")
		return
	}

	log.Debugf("get iodata(%d points) from %s|%s", len(d.pts), d.category, d.from)

	x.updateStats(d)

	if x.conf.OutputFile != "" && x.matchOutputFileInput(d.from) {
		log.Debugf("write %d(%s) points to %s", len(d.pts), d.from, x.conf.OutputFile)

		if err := x.fileOutput(d); err != nil {
			log.Errorf("fileOutput: %s", err)
		}

		// do not send data to remote.
		return
	}

	if d.opt != nil && d.opt.HTTPHost != "" {
		c.dynamicDatawayPts[d.opt.HTTPHost] = append(c.dynamicDatawayPts[d.opt.HTTPHost], d.pts...)
	} else {
		c.pts = append(c.pts, d.pts...)
	}

	if (tryClean &&
		x.conf.MaxCacheCount > 0 &&
		len(c.pts) > x.conf.MaxCacheCount) ||
		len(c.dynamicDatawayPts) > 0 {
		log.Debugf("on cache full(%d pts, %d dynamicDatawayPts) to flush %s...",
			len(c.pts), len(c.dynamicDatawayPts), d.category)

		x.flush(c)

		// reset consumer flush ticker to prevent send small packages
		c.flushTiker.Reset(x.flushInterval)
	}
}

func (x *IO) StartIO(recoverable bool) {
	g.Go(func(_ context.Context) error {
		filter.StartFilter(defaultIO.conf.Filters, defaultIO.dw)
		return nil
	})

	fn := func(category string, n int) {
		for i := 0; i < n; i++ {
			g.Go(func(_ context.Context) error {
				x.runConsumer(category)
				return nil
			})
		}
	}

	for _, c := range []string{
		datakit.Metric,
		datakit.Network,
		datakit.KeyEvent,
		datakit.Object,
		datakit.CustomObject,
		datakit.Logging,
		datakit.Tracing,
		datakit.RUM,
		datakit.Security,
		datakit.Profiling,
		datakit.DynamicDatawayCategory,
	} {
		log.Infof("starting consumer on %s...", c)
		switch c {
		case datakit.Metric, datakit.Network, datakit.Logging, datakit.Tracing, datakit.RUM:
			fn(c, runtime.NumCPU()*2+1)
		default:
			fn(c, 1)
		}
	}
}

type consumer struct {
	ch chan *iodata
	fc *failCache

	sendPts *uint64
	failPts *uint64

	category string

	flushTiker *time.Ticker

	pts               []*point.Point
	lastFlush         time.Time
	dynamicDatawayPts map[string][]*point.Point // 拨测数据
}

func (x *IO) runConsumer(category string) {
	ch, ok := x.chans[category]
	if !ok {
		log.Panicf("invalid category %s, should not been here", category)
	}

	fc, ok := x.fcs[category]
	if !ok {
		if x.conf.EnableCache && category != datakit.DynamicDatawayCategory {
			log.Panicf("invalid category %s, should not been here", category)
		}
	}

	c := &consumer{
		ch:                ch,
		flushTiker:        time.NewTicker(x.flushInterval),
		fc:                fc,
		category:          category,
		dynamicDatawayPts: map[string][]*point.Point{},
	}

	defer c.flushTiker.Stop()

	switch category {
	case datakit.Metric:
		c.sendPts = &MSendPts
		c.failPts = &MFailPts
	case datakit.Network:
		c.sendPts = &NSendPts
		c.failPts = &NFailPts
	case datakit.KeyEvent:
		c.sendPts = &ESendPts
		c.failPts = &EFailPts
	case datakit.Object:
		c.sendPts = &OSendPts
		c.failPts = &OFailPts
	case datakit.CustomObject:
		c.sendPts = &COSendPts
		c.failPts = &COFailPts
	case datakit.Logging:
		c.sendPts = &LSendPts
		c.failPts = &LFailPts
	case datakit.Tracing:
		c.sendPts = &TSendPts
		c.failPts = &TFailPts
	case datakit.RUM:
		c.sendPts = &RSendPts
		c.failPts = &RFailPts
	case datakit.Security:
		c.sendPts = &SSendPts
		c.failPts = &SFailPts
	case datakit.Profiling:
		c.sendPts = &PSendPts
		c.failPts = &PFailPts
	case datakit.DynamicDatawayCategory:
		c.sendPts = &LSendPts
		c.failPts = &LFailPts

		// NOTE: 目前只有拨测的数据会将数据打到 dynamicDatawayPts 中，而拨测数据
		// 是写日志，故将 category 设置为 logging
		c.category = datakit.Logging
	}

	du, err := time.ParseDuration(x.conf.CacheCleanInterval)
	if err != nil {
		log.Warnf("parse CacheCleanInterval failed: %s, use default 5s", err)
		du = time.Second * 5
	}

	walTick := time.NewTicker(du)
	defer walTick.Stop()

	log.Infof("run consumer on %s", category)
	for {
		select {
		case d := <-ch:
			x.cacheData(c, d, true)

		case <-c.flushTiker.C:
			log.Debugf("on tick(%s) to flush %s(%d pts), last flush %s ago...",
				x.flushInterval, category, len(c.pts), time.Since(c.lastFlush))
			x.flush(c)

		case <-walTick.C:
			log.Debugf("wal try flush failed data on %s", category)
			x.flushWal(c)

		case e := <-x.inLastErr:
			x.updateLastErr(e)

		case <-datakit.Exit.Wait():
			log.Infof("io consumer on %s exit on exit", c.category)
			return
		}
	}
}

func (x *IO) updateLastErr(e *lastError) {
	x.lock.Lock()
	defer x.lock.Unlock()

	stat, ok := x.inputstats[e.from]
	if !ok {
		stat = &InputsStat{
			First: time.Now(),
			Last:  time.Now(),
		}
		x.inputstats[e.from] = stat
	}

	stat.LastErr = e.err
	stat.LastErrTS = e.ts
}

func (x *IO) flush(c *consumer) {
	c.lastFlush = time.Now()
	failed := 0

	if n, err := x.doFlush(c.pts, c.category, c.fc); err != nil {
		log.Errorf("post %d to %s failed: %s", len(c.pts), c.category, err)
		failed += n
	} else {
		failed += n
	}

	for k, pts := range c.dynamicDatawayPts {
		log.Debugf("try flush dynamic dataway %d pts on %s", len(pts), k)
		if n, err := x.doFlush(pts, k, c.fc); err != nil {
			log.Errorf("post %d to %s failed", len(pts), k)
			failed += n
		} else {
			failed += n
		}
	}

	atomic.AddUint64(c.sendPts, uint64(len(c.pts)+len(c.dynamicDatawayPts)))
	atomic.AddUint64(c.failPts, uint64(failed))

	// clear
	c.pts = nil
	c.dynamicDatawayPts = map[string][]*point.Point{}
}

func (x *IO) flushWal(c *consumer) {
	if c.fc != nil {
		if err := c.fc.get(getWrite, x.sender.Write); err != nil {
			log.Warnf("flushWal send failed: %v", err)
		} else {
			log.Debug("flushWal send ok")
		}
	}
}

func (x *IO) doFlush(pts []*point.Point, category string, fc *failCache) (int, error) {
	if x.sender == nil {
		return 0, fmt.Errorf("io sender is not initialized")
	}

	if len(pts) == 0 {
		return 0, nil
	}

	failed, err := x.sender.Write(category, pts)
	if err != nil {
		return 0, err
	}

	if x.conf.EnableCache && len(failed) > 0 {
		switch category {
		case datakit.Metric, datakit.MetricDeprecated, datakit.DynamicDatawayCategory:
			// Metric and DynamicDatawayCategory data doesn't need cache.
			log.Warnf("drop %d pts on %s, not cached", len(failed), category)

		default:
			log.Infof("caching %s(%d pts)...", category, len(failed))
			if err := x.cache(category, failed, fc); err != nil {
				log.Errorf("caching %s(%d pts) failed", category, len(pts))
			}
		} // switch category
	} // if

	return len(failed), nil
}

func (x *IO) cache(category string, pts []*point.Point, fc *failCache) error {
	if len(pts) == 0 || fc == nil {
		return nil
	}

	for _, pt := range pts {
		buf, err := pb.Marshal(&PBData{
			Category: category,
			Lines:    []byte(pt.String()),
		})
		if err != nil {
			log.Warnf("dump %s cache(%d) failed: %v", category, len(pts), err)
			return err
		}

		if err := fc.put(buf); err != nil {
			log.Warnf("dump %s cache(%d) failed: %v", category, len(pts), err)
			return err
		}
	}

	log.Debugf("put %s cache ok, %d pts", category, len(pts))
	return nil
}

func getWrite(data []byte, fn funcSend) error {
	pd := &PBData{}
	if err := pb.Unmarshal(data, pd); err != nil {
		return err
	}
	pts, err := lp.ParsePoints(pd.Lines, nil)
	if err != nil {
		return err
	}

	if len(pd.Category) == 0 || len(pts) == 0 {
		return nil
	}

	failed, err := fn(pd.Category, point.WrapPoint(pts))
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("send failed")
	}
	return nil
}

func (x *IO) fileOutput(d *iodata) error {
	// concurrent write
	x.lock.Lock()
	defer x.lock.Unlock()

	if n, err := x.fd.WriteString("# " + d.from + " > " + d.category + "\n"); err != nil {
		return err
	} else {
		x.outputFileSize += int64(n)
	}

	for _, pt := range d.pts {
		if n, err := x.fd.WriteString(pt.String() + "\n"); err != nil {
			return err
		} else {
			x.outputFileSize += int64(n)
			if x.outputFileSize > 32*1024*1024 { // truncate file on 32MB
				if err := x.fd.Truncate(0); err != nil {
					return fmt.Errorf("truncate error: %w", err)
				}

				if _, err := x.fd.Seek(0, io.SeekStart); err != nil {
					return fmt.Errorf("seek error: %w", err)
				}

				x.outputFileSize = 0
			}
		}
	}

	return nil
}

func (x *IO) ChanUsage() map[string][2]int {
	usage := map[string][2]int{}
	for k, v := range x.chans {
		usage[k] = [2]int{len(v), cap(v)}
	}
	return usage
}

func (x *IO) DroppedTotal() int64 {
	// NOTE: not thread-safe
	return x.droppedTotal
}
