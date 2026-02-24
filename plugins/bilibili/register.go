package bilibili

import (
	"context"
	"fmt"
	"strings"

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

	client := New(logger, cookie, refreshToken)
	client.StartAutoRefreshDaemon(context.Background())
	platform := NewPlatform(client)

	contrib := &platformplugins.Contribution{
		Platform: platform,
		SettingDefinitions: []botpkg.PluginSettingDefinition{
			ParseModeDefinition(),
		},
		// ID3 is skipped since Bilibili audio does not usually serve ID3 tags directly in the same way,
		// or if we needed to, we'd add an id3provider.go later.
	}

	return contrib, nil
}
