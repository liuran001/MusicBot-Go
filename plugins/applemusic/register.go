package applemusic

import (
	"fmt"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

func init() {
	if err := platformplugins.Register("applemusic", buildContribution); err != nil {
		panic(err)
	}
}

func buildContribution(cfg *config.Config, logger *logpkg.Logger) (*platformplugins.Contribution, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}

	// Read config values
	mediaUserToken := cfg.GetPluginString("applemusic", "media_user_token")
	if mediaUserToken == "" {
		mediaUserToken = strings.Trim(cfg.GetPluginString("applemusic", "media_user_token"), "`\"'")
	}

	storefront := cfg.GetPluginString("applemusic", "storefront")
	if storefront == "" {
		storefront = "us"
	}

	language := cfg.GetPluginString("applemusic", "language")
	if language == "" {
		language = "en-US"
	}

	timeoutSec := cfg.GetPluginInt("applemusic", "timeout")
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	client := NewClient(mediaUserToken, storefront, language, time.Duration(timeoutSec)*time.Second, logger)
	client.wrapperHost = strings.TrimSpace(cfg.GetPluginString("applemusic", "wrapper_host"))
	client.persistFunc = func(pairs map[string]string) error {
		if logger != nil {
			logger.Debug("applemusic: persist plugin config", "pairs", pairs)
		}
		return cfg.PersistPluginConfig("applemusic", pairs)
	}

	if err := client.SetAPIProxy(cfg.ResolveAPIProxyConfig("applemusic")); err != nil {
		return nil, err
	}

	return &platformplugins.Contribution{Platform: NewPlatform(client)}, nil
}
