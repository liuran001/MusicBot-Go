package spotify

import (
	"fmt"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
	"github.com/liuran001/MusicBot-Go/plugins/youtubemusic"
)

func init() {
	if err := platformplugins.Register(platformName, buildContribution); err != nil {
		panic(err)
	}
}

// buildContribution constructs the Spotify platform. Spotify supplies metadata
// and search via the Web API (Client Credentials flow); audio is delegated to a
// YouTube Music client built here, so a Spotify track download resolves to the
// matching YouTube Music stream. If credentials are absent the platform still
// registers but its API calls return an auth error.
func buildContribution(cfg *config.Config, logger *logpkg.Logger) (*platformplugins.Contribution, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	clientID := cfg.GetPluginString(platformName, "client_id")
	clientSecret := cfg.GetPluginString(platformName, "client_secret")
	market := cfg.GetPluginString(platformName, "market")
	timeoutSec := cfg.GetPluginInt(platformName, "timeout")
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	timeout := time.Duration(timeoutSec) * time.Second

	client := NewClient(clientID, clientSecret, market, timeout, logger)
	if err := client.SetAPIProxy(cfg.ResolveAPIProxyConfig(platformName)); err != nil {
		return nil, err
	}

	// Build a YouTube Music delegate for audio. It shares Spotify's plugin
	// section's proxy override is independent; the delegate reads its own
	// ytmusic cookie/timeout if present, else sensible defaults.
	ytCookie := cfg.GetPluginString("youtubemusic", "cookie")
	ytClient := youtubemusic.NewClient(ytCookie, timeout, logger)
	if err := ytClient.SetAPIProxy(cfg.ResolveAPIProxyConfig("youtubemusic")); err != nil {
		return nil, err
	}
	resolver := youtubemusic.NewPlatform(ytClient)

	return &platformplugins.Contribution{Platform: NewPlatform(client, resolver)}, nil
}
