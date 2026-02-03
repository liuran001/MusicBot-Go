package dynplugin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

type Manager struct {
	mu        sync.RWMutex
	platforms map[string]*scriptPlatform
	plugins   map[string]PluginInfo
	logger    *logpkg.Logger
}

// PluginInfo describes metadata for a loaded script plugin.
type PluginInfo struct {
	Name    string
	Version string
	URL     string
}

func NewManager(logger *logpkg.Logger) *Manager {
	return &Manager{
		platforms: make(map[string]*scriptPlatform),
		plugins:   make(map[string]PluginInfo),
		logger:    logger,
	}
}

func (m *Manager) Load(ctx context.Context, cfg *config.Config, platformManager platform.Manager) error {
	return m.reload(ctx, cfg, platformManager)
}

func (m *Manager) Reload(ctx context.Context, cfg *config.Config, platformManager platform.Manager) error {
	return m.reload(ctx, cfg, platformManager)
}

func (m *Manager) reload(ctx context.Context, cfg *config.Config, platformManager platform.Manager) error {
	if cfg == nil {
		return fmt.Errorf("config required")
	}
	pluginNames := cfg.PluginNames()
	if len(pluginNames) == 0 {
		return nil
	}
	loaded := make(map[string]struct{})
	pluginInfos := make(map[string]PluginInfo)

	for _, name := range pluginNames {
		if name == "" {
			continue
		}
		if !pluginEnabled(cfg, name) {
			continue
		}
		if _, ok := platformplugins.Get(name); ok {
			continue
		}
		plug, meta, err := loadScriptPlugin(ctx, name, cfg, m.logger)
		if err != nil {
			if m.logger != nil {
				m.logger.Warn("script plugin load failed", "plugin", name, "error", err)
			}
			continue
		}
		if meta == nil || len(meta.Platforms) == 0 {
			if m.logger != nil {
				m.logger.Warn("script plugin returned no platforms", "plugin", name)
			}
			continue
		}
		pluginInfo := PluginInfo{
			Name:    strings.TrimSpace(meta.Name),
			Version: strings.TrimSpace(meta.Version),
			URL:     strings.TrimSpace(meta.URL),
		}
		if pluginInfo.Name == "" {
			pluginInfo.Name = name
		}
		pluginInfos[name] = pluginInfo
		for _, info := range meta.Platforms {
			if info.Name == "" {
				continue
			}
			loaded[info.Name] = struct{}{}
			m.mu.Lock()
			if existing, ok := m.platforms[info.Name]; ok {
				existing.update(plug, info)
				m.mu.Unlock()
				continue
			}
			plat := newScriptPlatform(plug, info)
			m.platforms[info.Name] = plat
			m.mu.Unlock()
			if platformManager != nil {
				platformManager.Register(plat)
			}
			if m.logger != nil {
				m.logger.Info("script platform registered", "plugin", name, "platform", info.Name)
			}
		}
	}

	m.mu.RLock()
	for name, plat := range m.platforms {
		if _, ok := loaded[name]; !ok {
			plat.disable()
			if m.logger != nil {
				m.logger.Info("script platform disabled", "platform", name)
			}
		}
	}
	m.mu.RUnlock()
	m.mu.Lock()
	m.plugins = pluginInfos
	m.mu.Unlock()

	return nil
}

// PluginInfos returns metadata for loaded script plugins.
func (m *Manager) PluginInfos() []PluginInfo {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]PluginInfo, 0, len(m.plugins))
	for _, info := range m.plugins {
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func pluginEnabled(cfg *config.Config, name string) bool {
	pluginCfg, ok := cfg.GetPluginConfig(name)
	if !ok {
		return true
	}
	if _, hasKey := pluginCfg["enabled"]; hasKey {
		return cfg.GetPluginBool(name, "enabled")
	}
	return true
}
