package netease

import (
	"context"
	"fmt"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

func init() {
	if err := platformplugins.Register("netease", buildContribution); err != nil {
		panic(err)
	}
}

func buildContribution(cfg *config.Config, logger *logpkg.Logger) (*platformplugins.Contribution, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	musicU := cfg.GetPluginString("netease", "music_u")
	if musicU == "" {
		musicU = cfg.GetString("MUSIC_U")
	}
	spoofIP := true
	if pluginCfg, ok := cfg.GetPluginConfig("netease"); ok {
		if _, exists := pluginCfg["spoof_ip"]; exists {
			spoofIP = cfg.GetPluginBool("netease", "spoof_ip")
		}
	}
	autoRenewEnabled := cfg.GetPluginBool("netease", "auto_renew_enabled")
	intervalSec := cfg.GetPluginInt("netease", "auto_renew_interval_sec")
	var interval time.Duration
	if intervalSec > 0 {
		interval = time.Duration(intervalSec) * time.Second
	}
	persist := func(pairs map[string]string) error {
		return cfg.PersistPluginConfig("netease", pairs)
	}
	client := New(musicU, spoofIP, logger, persist)
	client.ConfigureAutoRenew(autoRenewEnabled, interval)
	client.StartAutoRenewDaemon(context.Background())
	if err := client.SetAPIProxy(cfg.ResolveAPIProxyConfig("netease")); err != nil {
		return nil, err
	}
	disableRadar := true
	if pluginCfg, ok := cfg.GetPluginConfig("netease"); ok {
		if _, exists := pluginCfg["disable_radar"]; exists {
			disableRadar = cfg.GetPluginBool("netease", "disable_radar")
		}
	}
	platform := NewPlatform(client, disableRadar)
	id3Provider := NewID3Provider(client)

	contrib := &platformplugins.Contribution{
		Platform: platform,
		ID3:      id3Provider,
	}

	if cfg.GetBool("EnableRecognize") {
		recognizeService := NewRecognizeService(cfg.GetInt("RecognizePort"))
		contrib.Recognizer = NewRecognizer(recognizeService)
	}

	return contrib, nil
}
