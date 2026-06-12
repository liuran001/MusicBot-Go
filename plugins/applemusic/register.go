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
	// Uses built-in default L3 credentials; can be overridden with files.
	wvClientID := cfg.GetPluginString("applemusic", "wv_client_id")
	wvPrivateKey := cfg.GetPluginString("applemusic", "wv_private_key")
	if dev := loadWVDevice(wvClientID, wvPrivateKey, logger); dev != nil {
		client.wvDevice = dev
	}

	if err := client.SetAPIProxy(cfg.ResolveAPIProxyConfig("applemusic")); err != nil {
		return nil, err
	}

	return &platformplugins.Contribution{Platform: NewPlatform(client)}, nil
}

// loadWVDevice loads Widevine L3 device credentials.
// If file paths are provided, loads from files (override).
// Otherwise uses built-in default L3 credentials.
func loadWVDevice(clientIDPath, privKeyPath string, logger *logpkg.Logger) *widevine.Device {
	// Try loading from files first (user override).
	if clientIDPath != "" && privKeyPath != "" {
		clientID, err1 := os.ReadFile(clientIDPath)
		privKey, err2 := os.ReadFile(privKeyPath)
		if err1 == nil && err2 == nil {
			dev, err := widevine.NewDevice(widevine.FromRaw(clientID, privKey))
			if err == nil {
				if logger != nil {
					logger.Info("applemusic: widevine device loaded from files")
				}
				return dev
			}
			if logger != nil {
				logger.Warn("applemusic: widevine device file init failed, using built-in", "error", err)
			}
		}
	}

	// Use built-in default L3 credentials.
	clientID, privKey := defaultWVCredentials()
	dev, err := widevine.NewDevice(widevine.FromRaw(clientID, privKey))
	if err != nil {
		if logger != nil {
			logger.Warn("applemusic: widevine built-in device init failed", "error", err)
		}
		return nil
	}

	if logger != nil {
		logger.Info("applemusic: widevine native decrypt enabled (built-in L3)")
	}
	return dev
}
