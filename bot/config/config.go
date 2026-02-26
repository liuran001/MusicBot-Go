package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/ini.v1"
)

// PluginConfig stores plugin-specific configuration as key-value pairs.
type PluginConfig map[string]interface{}

// Config wraps viper and provides typed accessors.
type Config struct {
	v       *viper.Viper
	plugins map[string]PluginConfig
}

// Load reads an INI config file and prepares defaults.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("MUSIC163BOT")
	v.AutomaticEnv()

	setDefaults(v)

	if strings.EqualFold(filepath.Ext(path), ".ini") {
		cfg, err := loadINI(v, path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}

		c := &Config{
			v:       v,
			plugins: make(map[string]PluginConfig),
		}

		loadPlugins(cfg, c)
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("validate config: %w", err)
		}
		return c, nil
	} else {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	c := &Config{
		v:       v,
		plugins: make(map[string]PluginConfig),
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return c, nil
}

// Validate checks critical configuration values for sane ranges.
func (c *Config) Validate() error {
	if c == nil || c.v == nil {
		return fmt.Errorf("config is nil")
	}

	if strings.TrimSpace(c.GetString("BOT_TOKEN")) == "" {
		return fmt.Errorf("bot token is required")
	}
	if strings.TrimSpace(c.GetString("DefaultPlatform")) == "" {
		return fmt.Errorf("default platform cannot be empty")
	}
	if strings.TrimSpace(c.GetString("SearchFallbackPlatform")) == "" {
		return fmt.Errorf("search fallback platform cannot be empty")
	}
	if strings.TrimSpace(c.GetString("DefaultQuality")) == "" {
		return fmt.Errorf("default quality cannot be empty")
	}

	mustPositive := map[string]int{
		"DownloadTimeout":    c.GetInt("DownloadTimeout"),
		"ListPageSize":       c.GetInt("ListPageSize"),
		"InlineListPageSize": c.GetInt("InlineListPageSize"),
		"WorkerPoolSize":     c.GetInt("WorkerPoolSize"),
		"RateLimitBurst":     c.GetInt("RateLimitBurst"),
		"UploadWorkerCount":  c.GetInt("UploadWorkerCount"),
		"UploadQueueSize":    c.GetInt("UploadQueueSize"),
	}
	for k, v := range mustPositive {
		if v <= 0 {
			return fmt.Errorf("%s must be greater than 0", strings.ToLower(k))
		}
	}

	mustNonNegative := map[string]int{
		"DBMaxOpenConns":         c.GetInt("DBMaxOpenConns"),
		"DBMaxIdleConns":         c.GetInt("DBMaxIdleConns"),
		"DBConnMaxLifetimeSec":   c.GetInt("DBConnMaxLifetimeSec"),
		"MultipartMinSizeMB":     c.GetInt("MultipartMinSizeMB"),
		"GlobalRateLimitBurst":   c.GetInt("GlobalRateLimitBurst"),
		"DownloadConcurrency":    c.GetInt("DownloadConcurrency"),
		"DownloadMaxRetries":     c.GetInt("DownloadMaxRetries"),
		"DownloadQueueWaitLimit": c.GetInt("DownloadQueueWaitLimit"),
		"UploadConcurrency":      c.GetInt("UploadConcurrency"),
	}
	for k, v := range mustNonNegative {
		if v < 0 {
			return fmt.Errorf("%s must be non-negative", strings.ToLower(k))
		}
	}

	if c.GetBool("EnableMultipartDownload") && c.GetInt("MultipartConcurrency") <= 0 {
		return fmt.Errorf("multipart concurrency must be greater than 0 when multipart download is enabled")
	}

	ratePerSecond := c.GetFloat64("RateLimitPerSecond")
	if ratePerSecond <= 0 {
		return fmt.Errorf("rate limit per second must be greater than 0")
	}
	globalRatePerSecond := c.GetFloat64("GlobalRateLimitPerSecond")
	if globalRatePerSecond < 0 {
		return fmt.Errorf("global rate limit per second must be non-negative")
	}

	port := c.GetInt("RecognizePort")
	if port < 0 || port > 65535 {
		return fmt.Errorf("recognize port must be between 1 and 65535")
	}
	if c.GetBool("EnableRecognize") && port == 0 {
		return fmt.Errorf("recognize port must be between 1 and 65535")
	}

	return nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("BotAPI", "https://api.telegram.org")
	v.SetDefault("BotDebug", false)
	v.SetDefault("CacheDir", "./cache")
	v.SetDefault("DownloadTimeout", 60)
	v.SetDefault("CheckMD5", true)
	v.SetDefault("Database", "cache.db")
	v.SetDefault("DataDatabase", "data.db")
	v.SetDefault("DBMaxOpenConns", 1)
	v.SetDefault("DBMaxIdleConns", 1)
	v.SetDefault("DBConnMaxLifetimeSec", 3600)
	v.SetDefault("LogLevel", "info")
	v.SetDefault("LogFormat", "text")
	v.SetDefault("LogSource", false)
	v.SetDefault("GormLogLevel", "warn")
	v.SetDefault("DefaultPlatform", "netease")
	v.SetDefault("SearchFallbackPlatform", "netease")
	v.SetDefault("DefaultQuality", "hires")
	v.SetDefault("EnableMultipartDownload", true)
	v.SetDefault("MultipartConcurrency", 4)
	v.SetDefault("MultipartMinSizeMB", 5)
	v.SetDefault("ListPageSize", 8)
	v.SetDefault("InlineListPageSize", 30)
	v.SetDefault("WorkerPoolSize", 4)
	v.SetDefault("EnableRecognize", true)
	v.SetDefault("EnableWhitelist", false)
	v.SetDefault("WhitelistChatIDs", "")
	v.SetDefault("RecognizePort", 3737)
	v.SetDefault("RateLimitPerSecond", 1.0)
	v.SetDefault("RateLimitBurst", 3)
	v.SetDefault("GlobalRateLimitPerSecond", 0.0)
	v.SetDefault("GlobalRateLimitBurst", 0)
	v.SetDefault("DownloadConcurrency", 4)
	v.SetDefault("DownloadMaxRetries", 3)
	v.SetDefault("DownloadQueueWaitLimit", 0)
	v.SetDefault("UploadConcurrency", 1)
	v.SetDefault("UploadWorkerCount", 1)
	v.SetDefault("UploadQueueSize", 20)
	v.SetDefault("InlineUploadChatID", 0)
	v.SetDefault("PluginScriptDir", "./plugins/scripts")
}

// GetString returns a string value.
func (c *Config) GetString(key string) string {
	return c.v.GetString(key)
}

// GetInt returns an int value.
func (c *Config) GetInt(key string) int {
	return c.v.GetInt(key)
}

// GetFloat64 returns a float64 value.
func (c *Config) GetFloat64(key string) float64 {
	return c.v.GetFloat64(key)
}

// GetBool returns a bool value.
func (c *Config) GetBool(key string) bool {
	return c.v.GetBool(key)
}

// GetIntSlice returns a slice of ints.
func (c *Config) GetIntSlice(key string) []int {
	return c.v.GetIntSlice(key)
}

// GetPluginConfig retrieves plugin-specific configuration by plugin name.
// Returns the configuration map and true if found, or nil and false if not found.
func (c *Config) GetPluginConfig(name string) (PluginConfig, bool) {
	cfg, ok := c.plugins[name]
	return cfg, ok
}

// PluginNames returns the configured plugin names.
func (c *Config) PluginNames() []string {
	if len(c.plugins) == 0 {
		return nil
	}
	nameList := make([]string, 0, len(c.plugins))
	for name := range c.plugins {
		nameList = append(nameList, name)
	}
	sort.Strings(nameList)
	return nameList
}

// GetPluginString returns a string value from plugin configuration.
// Returns empty string if plugin or key not found.
func (c *Config) GetPluginString(plugin, key string) string {
	cfg, ok := c.plugins[plugin]
	if !ok {
		return ""
	}
	val, ok := cfg[key]
	if !ok {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}

// GetPluginInt returns an int value from plugin configuration.
// Returns 0 if plugin or key not found, or value cannot be converted to int.
func (c *Config) GetPluginInt(plugin, key string) int {
	cfg, ok := c.plugins[plugin]
	if !ok {
		return 0
	}
	val, ok := cfg[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case string:
		num, _ := strconv.Atoi(v)
		return num
	default:
		return 0
	}
}

// GetPluginBool returns a bool value from plugin configuration.
// Returns false if plugin or key not found, or value cannot be converted to bool.
func (c *Config) GetPluginBool(plugin, key string) bool {
	cfg, ok := c.plugins[plugin]
	if !ok {
		return false
	}
	val, ok := cfg[key]
	if !ok {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	case int, int64:
		return v != 0
	default:
		return false
	}
}

func loadINI(v *viper.Viper, path string) (*ini.File, error) {
	cfg, err := ini.Load(path)
	if err != nil {
		return nil, err
	}

	for _, key := range cfg.Section("").Keys() {
		v.Set(key.Name(), key.Value())
	}

	return cfg, nil
}

func loadPlugins(cfg *ini.File, c *Config) {
	const pluginPrefix = "plugins."

	for _, section := range cfg.Sections() {
		sectionName := section.Name()
		if sectionName == "" || sectionName == "DEFAULT" {
			continue
		}

		if strings.HasPrefix(sectionName, pluginPrefix) {
			pluginName := strings.TrimPrefix(sectionName, pluginPrefix)
			pluginCfg := make(PluginConfig)

			for _, key := range section.Keys() {
				pluginCfg[key.Name()] = key.Value()
			}

			c.plugins[pluginName] = pluginCfg
		}
	}
}
