package applemusic

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"

	widevine "github.com/iyear/gowidevine"
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

	// Load Widevine L3 device for native DRM decryption.
	// Look for device files: client_id.bin + private_key.pem
	// in configured path or default locations.
	wvClientID := cfg.GetPluginString("applemusic", "wv_client_id")
	wvPrivateKey := cfg.GetPluginString("applemusic", "wv_private_key")
	if wvClientID == "" {
		wvClientID = "widevine/client_id.bin"
	}
	if wvPrivateKey == "" {
		wvPrivateKey = "widevine/private_key.pem"
	}
	if dev := loadWVDevice(wvClientID, wvPrivateKey, logger); dev != nil {
		client.wvDevice = dev
	}

	if err := client.SetAPIProxy(cfg.ResolveAPIProxyConfig("applemusic")); err != nil {
		return nil, err
	}

	return &platformplugins.Contribution{Platform: NewPlatform(client)}, nil
}

// loadWVDevice tries to load Widevine L3 device credentials from files.
func loadWVDevice(clientIDPath, privKeyPath string, logger *logpkg.Logger) *widevine.Device {
	clientID, err := os.ReadFile(clientIDPath)
	if err != nil {
		if logger != nil {
			logger.Debug("applemusic: widevine client_id not found, native decrypt disabled",
				"path", clientIDPath, "error", err)
		}
		return nil
	}

	privKey, err := os.ReadFile(privKeyPath)
	if err != nil {
		if logger != nil {
			logger.Debug("applemusic: widevine private_key not found, native decrypt disabled",
				"path", privKeyPath, "error", err)
		}
		return nil
	}

	dev, err := widevine.NewDevice(widevine.FromRaw(clientID, privKey))
	if err != nil {
		if logger != nil {
			logger.Warn("applemusic: widevine device init failed", "error", err)
		}
		return nil
	}

	if logger != nil {
		logger.Info("applemusic: widevine native decrypt enabled")
	}
	return dev
}
