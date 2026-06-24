package youtubemusic

import (
	"fmt"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

func init() {
	if err := platformplugins.Register(platformName, buildContribution); err != nil {
		panic(err)
	}
}

// buildContribution constructs the YouTube Music platform from config. A cookie
// is optional: anonymous InnerTube works for search/metadata/most downloads; a
// cookie (from a logged-in music.youtube.com session) can unlock 256k streams
// and reduce rate limiting.
func buildContribution(cfg *config.Config, logger *logpkg.Logger) (*platformplugins.Contribution, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	cookie := cfg.GetPluginString(platformName, "cookie")
	timeoutSec := cfg.GetPluginInt(platformName, "timeout")
	if timeoutSec <= 0 {
		timeoutSec = 20
	}
	client := NewClient(cookie, time.Duration(timeoutSec)*time.Second, logger)
	if err := client.SetAPIProxy(cfg.ResolveAPIProxyConfig(platformName)); err != nil {
		return nil, err
	}
	return &platformplugins.Contribution{Platform: NewPlatform(client)}, nil
}
