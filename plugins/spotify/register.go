package spotify

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
	"github.com/liuran001/MusicBot-Go/plugins/spotify/native"
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

	// Build a YouTube Music delegate for audio. It reads the youtubemusic
	// plugin section's own cookie / API-proxy config (independent of Spotify's),
	// so the delegate behaves like the standalone youtubemusic plugin.
	ytCookie := cfg.GetPluginString("youtubemusic", "cookie")
	ytClient := youtubemusic.NewClient(ytCookie, timeout, logger)
	if err := ytClient.SetAPIProxy(cfg.ResolveAPIProxyConfig("youtubemusic")); err != nil {
		return nil, err
	}
	resolver := youtubemusic.NewPlatform(ytClient)

	plat := NewPlatform(client, resolver)

	// Build the native (real Spotify audio) source. It is proxy-aware and
	// persists its reusable login credentials to a state file so the one-time
	// OAuth login survives restarts. When the operator has not logged in yet the
	// source reports unavailable and downloads transparently use the YTM
	// delegate, so the plugin still works out of the box.
	nativeHTTP, err := httpproxy.NewHTTPClient(cfg.ResolveAPIProxyConfig(platformName), timeout)
	if err != nil {
		return nil, err
	}
	statePath := strings.TrimSpace(cfg.GetPluginString(platformName, "credentials_path"))
	if statePath == "" {
		cacheDir := strings.TrimSpace(cfg.GetString("CacheDir"))
		if cacheDir == "" {
			cacheDir = "./cache"
		}
		statePath = filepath.Join(cacheDir, "spotify-credentials.json")
	}
	nativeClient := native.NewClient(native.ClientOptions{
		StatePath:  statePath,
		Logger:     logger,
		HTTPClient: nativeHTTP,
	})
	plat.WithNativeSource(newNativeSource(nativeClient))

	return &platformplugins.Contribution{Platform: plat}, nil
}
