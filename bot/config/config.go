package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
	"github.com/spf13/viper"
	"gopkg.in/ini.v1"
)

// PluginConfig stores plugin-specific configuration as key-value pairs.
type PluginConfig map[string]interface{}

// Config wraps viper and provides typed accessors.
type Config struct {
	v           *viper.Viper
	plugins     map[string]PluginConfig
	botProfiles map[string]map[string]string
	path        string
	mu          sync.Mutex
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
			v:           v,
			plugins:     make(map[string]PluginConfig),
			botProfiles: make(map[string]map[string]string),
			path:        path,
		}

		loadPlugins(cfg, c)
		loadBotProfiles(cfg, c)
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
		v:           v,
		plugins:     make(map[string]PluginConfig),
		botProfiles: make(map[string]map[string]string),
		path:        path,
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return c, nil
}

// PersistPluginConfig writes plugin key-values back to current config file.
// It always upserts [plugins.<name>] and missing keys/sections automatically.
func (c *Config) PersistPluginConfig(plugin string, pairs map[string]string) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	plugin = strings.TrimSpace(plugin)
	if plugin == "" {
		return fmt.Errorf("plugin name empty")
	}
	if len(pairs) == 0 {
		return nil
	}

	path := strings.TrimSpace(c.path)
	if path == "" {
		path = strings.TrimSpace(c.v.ConfigFileUsed())
	}
	if path == "" {
		path = "config.ini"
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := ensureParentDir(path); err != nil {
		return err
	}

	persistPairs := make(map[string]string, len(pairs))
	for key, value := range pairs {
		persistPairs[key] = formatINIPersistValue(value)
	}
	if err := upsertINIWithoutReformat(path, "plugins."+plugin, persistPairs); err != nil {
		return err
	}

	pluginCfg, ok := c.plugins[plugin]
	if !ok || pluginCfg == nil {
		pluginCfg = make(PluginConfig)
		c.plugins[plugin] = pluginCfg
	}
	for key, value := range pairs {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		pluginCfg[key] = value
	}

	return nil
}

func formatINIPersistValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return value
	}
	if (strings.HasPrefix(trimmed, "`") && strings.HasSuffix(trimmed, "`")) ||
		(strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`)) ||
		(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
		return value
	}
	if strings.ContainsAny(value, ";\n\r") {
		escaped := strings.ReplaceAll(value, "`", "'")
		return "`" + escaped + "`"
	}
	return value
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

// atomicWriteFile writes data to a file atomically via temp+rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func upsertINIWithoutReformat(path, sectionName string, pairs map[string]string) error {
	cleanPairs := make(map[string]string, len(pairs))
	for key, value := range pairs {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		cleanPairs[k] = value
	}
	if len(cleanPairs) == 0 {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return writeNewINI(path, sectionName, cleanPairs)
	}

	content := string(data)
	lineSep := "\n"
	if strings.Contains(content, "\r\n") {
		lineSep = "\r\n"
	}
	lines := strings.Split(content, lineSep)

	sectionStart, sectionEnd := findSectionRange(lines, sectionName)
	if sectionStart < 0 {
		return appendNewSection(path, content, lineSep, sectionName, cleanPairs)
	}

	pending := make(map[string]string, len(cleanPairs))
	for k, v := range cleanPairs {
		pending[k] = v
	}
	indent := ""
	for i := sectionStart + 1; i < sectionEnd; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		if indent == "" {
			indent = leadingWhitespace(line)
		}
		key := strings.TrimSpace(line[:eq])
		value, ok := pending[key]
		if !ok {
			continue
		}
		rest := line[eq+1:]
		j := 0
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		lines[i] = line[:eq+1] + rest[:j] + value
		delete(pending, key)
	}

	if len(pending) > 0 {
		keys := make([]string, 0, len(pending))
		for k := range pending {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		extra := make([]string, 0, len(keys))
		for _, k := range keys {
			extra = append(extra, fmt.Sprintf("%s%s = %s", indent, k, pending[k]))
		}

		newLines := make([]string, 0, len(lines)+len(extra))
		newLines = append(newLines, lines[:sectionEnd]...)
		newLines = append(newLines, extra...)
		newLines = append(newLines, lines[sectionEnd:]...)
		lines = newLines
	}

	out := strings.Join(lines, lineSep)
	return atomicWriteFile(path, []byte(out), 0o644)
}

func writeNewINI(path, sectionName string, pairs map[string]string) error {
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := []string{"[" + sectionName + "]"}
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s = %s", k, pairs[k]))
	}
	return atomicWriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func appendNewSection(path, content, lineSep, sectionName string, pairs map[string]string) error {
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(content)
	if content != "" {
		if !strings.HasSuffix(content, lineSep) {
			b.WriteString(lineSep)
		}
		if !strings.HasSuffix(content, lineSep+lineSep) {
			b.WriteString(lineSep)
		}
	}
	b.WriteString("[")
	b.WriteString(sectionName)
	b.WriteString("]")
	b.WriteString(lineSep)
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(" = ")
		b.WriteString(pairs[k])
		b.WriteString(lineSep)
	}

	return atomicWriteFile(path, []byte(b.String()), 0o644)
}

func findSectionRange(lines []string, sectionName string) (start, end int) {
	start, end = -1, len(lines)
	target := strings.ToLower(strings.TrimSpace(sectionName))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			if start >= 0 {
				end = i
				break
			}
			if name == target {
				start = i
			}
		}
	}
	return start, end
}

// deleteINIKey removes a single key from a section, preserving all other
// formatting. Missing file/section/key is a no-op.
func deleteINIKey(path, sectionName, key string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content := string(data)
	lineSep := "\n"
	if strings.Contains(content, "\r\n") {
		lineSep = "\r\n"
	}
	lines := strings.Split(content, lineSep)
	start, end := findSectionRange(lines, sectionName)
	if start < 0 {
		return nil
	}
	target := strings.ToLower(strings.TrimSpace(key))
	out := make([]string, 0, len(lines))
	out = append(out, lines[:start+1]...)
	for i := start + 1; i < end; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, ";") {
			if eq := strings.Index(line, "="); eq >= 0 {
				if strings.ToLower(strings.TrimSpace(line[:eq])) == target {
					continue
				}
			}
		}
		out = append(out, line)
	}
	out = append(out, lines[end:]...)
	return atomicWriteFile(path, []byte(strings.Join(out, lineSep)), 0o644)
}

// deleteINISection removes an entire [section] and its body. Missing
// file/section is a no-op.
func deleteINISection(path, sectionName string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content := string(data)
	lineSep := "\n"
	if strings.Contains(content, "\r\n") {
		lineSep = "\r\n"
	}
	lines := strings.Split(content, lineSep)
	start, end := findSectionRange(lines, sectionName)
	if start < 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	out = append(out, lines[:start]...)
	out = append(out, lines[end:]...)
	return atomicWriteFile(path, []byte(strings.Join(out, lineSep)), 0o644)
}

func leadingWhitespace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
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
	v.SetDefault("DefaultLyricFormat", "lrc")
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
	v.SetDefault("EnableAprilFools", false)
	v.SetDefault("AprilFoolsTextPrankProbability", 0.01)
	v.SetDefault("AprilFoolsTrackHijackProbability", 0.15)
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

// ResolveAPIProxyConfig returns effective API proxy config for a plugin.
// Plugin-level api_proxy_* overrides global ApiProxy* only when explicitly set.
func (c *Config) ResolveAPIProxyConfig(plugin string) httpproxy.Config {
	resolved := httpproxy.Config{
		Enabled: c.GetBool("ApiProxyEnabled"),
		Type:    c.GetString("ApiProxyType"),
		Host:    c.GetString("ApiProxyHost"),
		Port:    c.GetInt("ApiProxyPort"),
		Auth:    c.GetString("ApiProxyAuth"),
		Headers: httpproxy.ParseHeaders(c.GetString("ApiProxyHeaders")),
	}
	pluginCfg, ok := c.GetPluginConfig(plugin)
	if !ok {
		return resolved.Normalized()
	}
	if _, exists := pluginCfg["api_proxy_enabled"]; exists {
		resolved.Enabled = c.GetPluginBool(plugin, "api_proxy_enabled")
	}
	if _, exists := pluginCfg["api_proxy_type"]; exists {
		resolved.Type = c.GetPluginString(plugin, "api_proxy_type")
	}
	if _, exists := pluginCfg["api_proxy_host"]; exists {
		resolved.Host = c.GetPluginString(plugin, "api_proxy_host")
	}
	if _, exists := pluginCfg["api_proxy_port"]; exists {
		resolved.Port = c.GetPluginInt(plugin, "api_proxy_port")
	}
	if _, exists := pluginCfg["api_proxy_auth"]; exists {
		resolved.Auth = c.GetPluginString(plugin, "api_proxy_auth")
	}
	if _, exists := pluginCfg["api_proxy_headers"]; exists {
		resolved.Headers = httpproxy.ParseHeaders(c.GetPluginString(plugin, "api_proxy_headers"))
	}
	return resolved.Normalized()
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

// botProfilePrefix is the INI section prefix for per-language bot profile
// overrides, e.g. [bot_profile.zh]. Keys inside are name / description /
// short_description; any present key overrides the embedded i18n default.
const botProfilePrefix = "bot_profile."

// BotProfileKeys are the recognized fields inside a [bot_profile.<lang>]
// section, in display order.
var BotProfileKeys = []string{"name", "description", "short_description"}

// BotProfileLimits is the maximum length (in characters) Telegram accepts for
// each profile field. Validating here gives a friendly error before the API
// call instead of a raw Bad Request from Telegram.
var BotProfileLimits = map[string]int{
	"name":              64,
	"description":       512,
	"short_description": 120,
}

// IsBotProfileKey reports whether key is a recognized bot profile field.
func IsBotProfileKey(key string) bool {
	for _, k := range BotProfileKeys {
		if k == key {
			return true
		}
	}
	return false
}

// BotProfileFieldLimit returns Telegram's max character length for a profile
// field, or 0 if the key is not a recognized field.
func BotProfileFieldLimit(key string) int {
	return BotProfileLimits[strings.ToLower(strings.TrimSpace(key))]
}

// loadBotProfiles reads every [bot_profile.<lang>] section into c.botProfiles.
func loadBotProfiles(cfg *ini.File, c *Config) {
	for _, section := range cfg.Sections() {
		sectionName := section.Name()
		if !strings.HasPrefix(sectionName, botProfilePrefix) {
			continue
		}
		lang := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(sectionName, botProfilePrefix)))
		if lang == "" {
			continue
		}
		fields := make(map[string]string)
		for _, key := range section.Keys() {
			name := strings.ToLower(strings.TrimSpace(key.Name()))
			if !IsBotProfileKey(name) {
				continue
			}
			fields[name] = key.Value()
		}
		if len(fields) > 0 {
			c.botProfiles[lang] = fields
		}
	}
}

// GetBotProfile returns the override fields configured for a language, if any.
// The returned map is a copy and safe to mutate.
func (c *Config) GetBotProfile(lang string) (map[string]string, bool) {
	if c == nil {
		return nil, false
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	c.mu.Lock()
	defer c.mu.Unlock()
	fields, ok := c.botProfiles[lang]
	if !ok || len(fields) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out, true
}

// GetBotProfileField returns a single override field for a language, or empty.
func (c *Config) GetBotProfileField(lang, key string) string {
	if c == nil {
		return ""
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	key = strings.ToLower(strings.TrimSpace(key))
	c.mu.Lock()
	defer c.mu.Unlock()
	if fields, ok := c.botProfiles[lang]; ok {
		return fields[key]
	}
	return ""
}

// SetBotProfileField persists a single override field for a language to the
// config file and updates the in-memory copy. An empty value removes the key
// (falling back to the embedded i18n default). Only recognized keys are
// accepted.
func (c *Config) SetBotProfileField(lang, key, value string) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	key = strings.ToLower(strings.TrimSpace(key))
	if lang == "" {
		return fmt.Errorf("language empty")
	}
	if !IsBotProfileKey(key) {
		return fmt.Errorf("unknown profile field %q (allowed: %s)", key, strings.Join(BotProfileKeys, ", "))
	}
	// Telegram enforces per-field character limits (counted in code points, not
	// bytes). Reject early so the caller gets a friendly message instead of an
	// opaque API error at push time. An empty value means "remove override".
	if value != "" {
		if limit, ok := BotProfileLimits[key]; ok {
			if n := utf8.RuneCountInString(value); n > limit {
				return fmt.Errorf("%s 超出长度限制: %d/%d 字符", key, n, limit)
			}
		}
	}

	path := strings.TrimSpace(c.path)
	if path == "" {
		path = strings.TrimSpace(c.v.ConfigFileUsed())
	}
	if path == "" {
		path = "config.ini"
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := ensureParentDir(path); err != nil {
		return err
	}

	section := botProfilePrefix + lang
	if value == "" {
		if err := deleteINIKey(path, section, key); err != nil {
			return err
		}
		if fields, ok := c.botProfiles[lang]; ok {
			delete(fields, key)
			if len(fields) == 0 {
				delete(c.botProfiles, lang)
			}
		}
		return nil
	}

	if err := upsertINIWithoutReformat(path, section, map[string]string{key: formatINIPersistValue(value)}); err != nil {
		return err
	}
	fields, ok := c.botProfiles[lang]
	if !ok || fields == nil {
		fields = make(map[string]string)
		c.botProfiles[lang] = fields
	}
	fields[key] = value
	return nil
}

// ResetBotProfile removes all override fields for a language, restoring the
// embedded i18n defaults. It is a no-op if no overrides exist.
func (c *Config) ResetBotProfile(lang string) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return fmt.Errorf("language empty")
	}

	path := strings.TrimSpace(c.path)
	if path == "" {
		path = strings.TrimSpace(c.v.ConfigFileUsed())
	}
	if path == "" {
		path = "config.ini"
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.botProfiles[lang]; !ok {
		return nil
	}
	if err := deleteINISection(path, botProfilePrefix+lang); err != nil {
		return err
	}
	delete(c.botProfiles, lang)
	return nil
}
