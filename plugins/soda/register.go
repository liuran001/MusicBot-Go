package soda

import (
	"fmt"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

func init() {
	if err := platformplugins.Register("soda", buildContribution); err != nil {
		panic(err)
	}
}

func buildContribution(cfg *config.Config, logger *logpkg.Logger) (*platformplugins.Contribution, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	cookie := cfg.GetPluginString("soda", "cookie")
	timeoutSec := cfg.GetPluginInt("soda", "timeout")
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	if cookie == "" {
		cookie = strings.Trim(cfg.GetPluginString("soda", "cookie"), "`\"'")
	}
	client := NewClient(cookie, time.Duration(timeoutSec)*time.Second, logger)
	client.persistFunc = func(pairs map[string]string) error {
		if logger != nil {
			logger.Debug("soda: persist plugin config", "pairs", pairs)
		}
		return cfg.PersistPluginConfig("soda", pairs)
	}
	if err := client.SetAPIProxy(cfg.ResolveAPIProxyConfig("soda")); err != nil {
		return nil, err
	}
	return &platformplugins.Contribution{Platform: NewPlatform(client)}, nil
}
