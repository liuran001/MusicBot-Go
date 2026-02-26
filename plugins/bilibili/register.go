package bilibili

import (
	"context"
	"fmt"
	"strings"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

func init() {
	if err := platformplugins.Register("bilibili", buildContribution); err != nil {
		panic(err)
	}
}

func buildContribution(cfg *config.Config, logger *logpkg.Logger) (*platformplugins.Contribution, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}

	cookie := strings.Trim(cfg.GetPluginString("bilibili", "cookie"), "`\"'")
	refreshToken := strings.Trim(cfg.GetPluginString("bilibili", "refresh_token"), "`\"'")
	autoRenewEnabled := cfg.GetPluginBool("bilibili", "auto_renew_enabled")
	intervalSec := cfg.GetPluginInt("bilibili", "auto_renew_interval_sec")
	var interval time.Duration
	if intervalSec > 0 {
		interval = time.Duration(intervalSec) * time.Second
	}
	streamURLPriority := cfg.GetPluginString("bilibili", "stream_url_priority")
	searchMaxPages := cfg.GetPluginInt("bilibili", "search_max_pages")
	if searchMaxPages <= 0 {
		searchMaxPages = 5
	}
	persist := func(pairs map[string]string) error {
		return cfg.PersistPluginConfig("bilibili", pairs)
	}

	client := New(logger, cookie, refreshToken, autoRenewEnabled, interval, persist)
	client.StartAutoRefreshDaemon(context.Background())
	platform := NewPlatform(client, searchMaxPages)
	platform.ConfigureStreamURLPriority(streamURLPriority)

	contrib := &platformplugins.Contribution{
		Platform: platform,
		SettingDefinitions: []botpkg.PluginSettingDefinition{
			ParseModeDefinition(),
			SearchFilterDefinition(),
		},
		// ID3 is skipped since Bilibili audio does not usually serve ID3 tags directly in the same way,
		// or if we needed to, we'd add an id3provider.go later.
	}

	return contrib, nil
}
