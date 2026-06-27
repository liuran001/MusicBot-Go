package spotify

import (
	"fmt"
	"os"
	"strings"
	"time"

	widevine "github.com/iyear/gowidevine"

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

	// Build the native (real Spotify audio) source: decrypted AAC/MP4 via the
	// web-player + Widevine path. It needs an sp_dc cookie (a logged-in
	// open.spotify.com web session) to mint web-player tokens. Without the
	// cookie the source reports unavailable and downloads fail with a clear
	// "not authenticated" error (no silent substitution of another platform's
	// audio). The HTTP client is proxy-aware.
	nativeHTTP, err := httpproxy.NewHTTPClient(cfg.ResolveAPIProxyConfig(platformName), timeout)
	if err != nil {
		return nil, err
	}
	spDC := strings.TrimSpace(cfg.GetPluginString(platformName, "sp_dc"))
	// Optional operator-supplied Widevine L3 device (.wvd). Empty = built-in.
	wvDevicePath := strings.TrimSpace(cfg.GetPluginString(platformName, "wvd_path"))
	var wvDevice *widevine.Device
	if wvDevicePath != "" {
		if f, oerr := os.Open(wvDevicePath); oerr == nil {
			if dev, derr := widevine.NewDevice(widevine.FromWVD(f)); derr == nil {
				wvDevice = dev
			} else if logger != nil {
				logger.Warn("spotify: failed loading wvd, using built-in", "error", derr)
			}
			_ = f.Close()
		}
	}
	nativeClient := native.NewWidevineClient(native.WidevineOptions{
		Cookie:     spDC,
		HTTPClient: nativeHTTP,
		Device:     wvDevice,
	})
	plat.WithNativeSource(newNativeSource(nativeClient))

	return &platformplugins.Contribution{Platform: plat}, nil
}
