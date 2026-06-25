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
)

func init() {
	if err := platformplugins.Register(platformName, buildContribution); err != nil {
		panic(err)
	}
}

// buildContribution constructs the Spotify platform. Metadata and search come
// from the Web API (Client Credentials flow). Audio is REAL Spotify audio:
// decrypted Ogg Vorbis fetched via the embedded librespot path, gated behind a
// one-time OAuth login (run `-spotify-login`). There is no cross-platform
// fallback — a track that can't be served natively fails with a clear error.
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

	plat := NewPlatform(client)

	// Build the native (real Spotify audio) source. It is proxy-aware and
	// persists its reusable login credentials to a state file so the one-time
	// OAuth login survives restarts. Until the operator logs in the source
	// reports unavailable and downloads fail with a clear "not authenticated"
	// error (no silent substitution of another platform's audio).
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
