package qqmusic

import (
	"fmt"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

func init() {
	if err := platformplugins.Register("qqmusic", buildContribution); err != nil {
		panic(err)
	}
}

func buildContribution(cfg *config.Config, logger *logpkg.Logger) (*platformplugins.Contribution, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	cookie := cfg.GetPluginString("qqmusic", "cookie")
	timeoutSec := cfg.GetPluginInt("qqmusic", "timeout")
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	autoRenewEnabled := cfg.GetPluginBool("qqmusic", "auto_renew_enabled")
	intervalSec := cfg.GetPluginInt("qqmusic", "auto_renew_interval_sec")
	var interval time.Duration
	if intervalSec > 0 {
		interval = time.Duration(intervalSec) * time.Second
	}
	autoRenewPersist := true
	if raw := cfg.GetPluginString("qqmusic", "auto_renew_persist_enabled"); raw != "" {
		autoRenewPersist = cfg.GetPluginBool("qqmusic", "auto_renew_persist_enabled")
	}
	autoRenewPersistPath := cfg.GetPluginString("qqmusic", "auto_renew_persist_path")
	client := NewClient(cookie, time.Duration(timeoutSec)*time.Second, logger, autoRenewEnabled, interval, autoRenewPersist, autoRenewPersistPath)
	platform := NewPlatform(client)
	return &platformplugins.Contribution{Platform: platform}, nil
}
