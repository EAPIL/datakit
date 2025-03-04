// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package installer

import (
	"os"

	"github.com/GuanceCloud/cliutils/logger"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/config"
	cp "gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/colorprint"
)

var l = logger.DefaultSLogger("upgrade")

func SetLog() {
	l = logger.SLogger("upgrade")
}

func Upgrade() error {
	mc := config.Cfg

	// load exists datakit.conf
	if err := mc.LoadMainTOML(datakit.MainConfPath); err == nil {
		mc = upgradeMainConfig(mc)

		if OTA {
			l.Debugf("set auto update(OTA enabled)")
			mc.AutoUpdate = OTA
		}

		writeDefInputToMainCfg(mc)
	} else {
		l.Warnf("load main config: %s, ignored", err.Error())
		return err
	}

	// build datakit main config
	if err := mc.InitCfg(datakit.MainConfPath); err != nil {
		l.Fatalf("failed to init datakit main config: %s", err.Error())
	}

	for _, dir := range []string{datakit.DataDir, datakit.ConfdDir} {
		if err := os.MkdirAll(dir, datakit.ConfPerm); err != nil {
			return err
		}
	}

	return nil
}

func upgradeMainConfig(c *config.Config) *config.Config {
	// setup dataway
	if c.DataWayCfg != nil {
		c.DataWayCfg.DeprecatedURL = ""
		c.DataWayCfg.HTTPProxy = Proxy
	}

	cp.Infof("Set log to %s\n", c.Logging.Log)
	cp.Infof("Set gin log to %s\n", c.Logging.GinLog)

	// upgrade logging settings
	if c.LogDeprecated != "" {
		c.Logging.Log = c.LogDeprecated
		c.LogDeprecated = ""
	}

	if c.LogLevelDeprecated != "" {
		c.Logging.Level = c.LogLevelDeprecated
		c.LogLevelDeprecated = ""
	}

	if c.LogRotateDeprecated != 0 {
		c.Logging.Rotate = c.LogRotateDeprecated
		c.LogRotateDeprecated = 0
	}

	if c.GinLogDeprecated != "" {
		c.Logging.GinLog = c.GinLogDeprecated
		c.GinLogDeprecated = ""
	}

	// upgrade HTTP settings
	if c.HTTPListenDeprecated != "" {
		c.HTTPAPI.Listen = c.HTTPListenDeprecated
		c.HTTPListenDeprecated = ""
	}

	if c.Disable404PageDeprecated {
		c.HTTPAPI.Disable404Page = true
		c.Disable404PageDeprecated = false
	}

	// upgrade IO settings
	if c.IOCacheCountDeprecated != 0 {
		c.IOConf.MaxCacheCount = c.IOCacheCountDeprecated
		c.IOCacheCountDeprecated = 0
	}

	if c.IOConf.MaxCacheCount < 1000 {
		c.IOConf.MaxCacheCount = 1000
	}

	if c.OutputFileDeprecated != "" {
		c.IOConf.OutputFile = c.OutputFileDeprecated
		c.OutputFileDeprecated = ""
	}

	if c.IntervalDeprecated != "" {
		c.IOConf.FlushInterval = c.IntervalDeprecated
		c.IntervalDeprecated = ""
	}

	if c.IOConf.FeedChanSize > 1 {
		c.IOConf.FeedChanSize = 1 // reset to 1
	}

	if c.IOConf.MaxDynamicCacheCountDeprecated > 0 {
		c.IOConf.MaxDynamicCacheCountDeprecated = 0 // clear the config
	}

	// upgrade election settings
	if c.ElectionNamespaceDeprecated != "" {
		c.Election.Namespace = c.ElectionNamespaceDeprecated
		c.ElectionNamespaceDeprecated = ""
	}

	if c.NamespaceDeprecated != "" {
		c.Election.Namespace = c.NamespaceDeprecated
		c.NamespaceDeprecated = ""
	}

	if c.GlobalEnvTagsDeprecated != nil {
		c.Election.Tags = c.GlobalEnvTagsDeprecated
		c.GlobalEnvTagsDeprecated = nil
	}

	if c.EnableElectionDeprecated {
		c.Election.Enable = true
		c.EnableElectionDeprecated = false
	}

	if c.EnableElectionTagDeprecated {
		c.Election.EnableNamespaceTag = true
		c.EnableElectionTagDeprecated = false
	}

	c.InstallVer = DataKitVersion

	return c
}
