package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadINI(t *testing.T) {
	path := filepath.Join("..", "..", "config_example.ini")
	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if conf.GetString("BOT_TOKEN") == "" {
		t.Fatalf("expected BOT_TOKEN to be present")
	}

	admins := conf.GetIntSlice("BotAdmin")
	if len(admins) == 0 {
		t.Fatalf("expected BotAdmin to be parsed")
	}
}

func TestPluginSections(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_config_*.ini")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	configContent := `BOT_TOKEN = test_token
MUSIC_U = test_music_u
BotAdmin = 123,456

[plugins.netease]
api_url = https://netease.api
timeout = 30
enabled = true

[plugins.spotify]
client_id = spotify_client
client_secret = spotify_secret
enabled = false
`

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tmpFile.Close()

	conf, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if conf.GetString("BOT_TOKEN") != "test_token" {
		t.Errorf("expected BOT_TOKEN=test_token, got %s", conf.GetString("BOT_TOKEN"))
	}

	neteaseCfg, ok := conf.GetPluginConfig("netease")
	if !ok {
		t.Fatal("expected netease plugin config to exist")
	}

	if neteaseCfg["api_url"] != "https://netease.api" {
		t.Errorf("expected api_url=https://netease.api, got %v", neteaseCfg["api_url"])
	}

	if conf.GetPluginString("netease", "api_url") != "https://netease.api" {
		t.Errorf("GetPluginString failed")
	}

	if conf.GetPluginInt("netease", "timeout") != 30 {
		t.Errorf("GetPluginInt failed, got %d", conf.GetPluginInt("netease", "timeout"))
	}

	if !conf.GetPluginBool("netease", "enabled") {
		t.Errorf("GetPluginBool failed for netease.enabled")
	}

	if conf.GetPluginBool("spotify", "enabled") {
		t.Errorf("GetPluginBool should return false for spotify.enabled")
	}

	if conf.GetPluginString("spotify", "client_id") != "spotify_client" {
		t.Errorf("GetPluginString failed for spotify.client_id")
	}
}

func TestPluginConfigNotFound(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_config_*.ini")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	configContent := `BOT_TOKEN = test_token`

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tmpFile.Close()

	conf, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	_, ok := conf.GetPluginConfig("nonexistent")
	if ok {
		t.Error("expected nonexistent plugin to not be found")
	}

	if conf.GetPluginString("nonexistent", "key") != "" {
		t.Error("expected empty string for nonexistent plugin")
	}

	if conf.GetPluginInt("nonexistent", "key") != 0 {
		t.Error("expected 0 for nonexistent plugin")
	}

	if conf.GetPluginBool("nonexistent", "key") {
		t.Error("expected false for nonexistent plugin")
	}
}

func TestBackwardCompatibility(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_config_*.ini")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	configContent := `BOT_TOKEN = legacy_token
MUSIC_U = legacy_music_u
BotAdmin = 111,222,333
BotAPI = https://api.telegram.org
BotDebug = true
Database = test.db
LogLevel = debug
DownloadTimeout = 120
ReverseProxy = proxy.example.com
CheckMD5 = false
`

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tmpFile.Close()

	conf, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if conf.GetString("BOT_TOKEN") != "legacy_token" {
		t.Errorf("backward compatibility broken for BOT_TOKEN")
	}

	if conf.GetString("MUSIC_U") != "legacy_music_u" {
		t.Errorf("backward compatibility broken for MUSIC_U")
	}

	admins := conf.GetIntSlice("BotAdmin")
	if len(admins) != 3 || admins[0] != 111 || admins[1] != 222 || admins[2] != 333 {
		t.Errorf("backward compatibility broken for BotAdmin: got %v", admins)
	}

	if conf.GetBool("BotDebug") != true {
		t.Errorf("backward compatibility broken for BotDebug")
	}

	if conf.GetInt("DownloadTimeout") != 120 {
		t.Errorf("backward compatibility broken for DownloadTimeout")
	}

	if conf.GetBool("CheckMD5") {
		t.Errorf("backward compatibility broken for CheckMD5")
	}
}

func TestMixedFormat(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_config_*.ini")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	configContent := `BOT_TOKEN = mixed_token
MUSIC_U = mixed_music_u
BotAdmin = 999

[plugins.custom]
feature_x = enabled
priority = 10
`

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tmpFile.Close()

	conf, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if conf.GetString("BOT_TOKEN") != "mixed_token" {
		t.Errorf("flat key access failed in mixed format")
	}

	if conf.GetPluginString("custom", "feature_x") != "enabled" {
		t.Errorf("plugin section access failed in mixed format")
	}

	if conf.GetPluginInt("custom", "priority") != 10 {
		t.Errorf("plugin int access failed in mixed format")
	}
}
