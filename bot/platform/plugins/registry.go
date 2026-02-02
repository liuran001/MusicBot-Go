package plugins

import (
	"fmt"
	"sort"
	"sync"

	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/recognize"
)

// Contribution describes the components a plugin can provide.
type Contribution struct {
	Platform   platform.Platform
	Platforms  []platform.Platform
	ID3        id3.ID3TagProvider
	Recognizer recognize.Service
}

// Factory creates a plugin contribution based on config and logger.
type Factory func(cfg *config.Config, logger *logpkg.Logger) (*Contribution, error)

var (
	mu        sync.RWMutex
	factories = make(map[string]Factory)
)

// Register registers a plugin factory by name.
func Register(name string, factory Factory) error {
	if name == "" {
		return fmt.Errorf("plugin name required")
	}
	if factory == nil {
		return fmt.Errorf("plugin factory required")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := factories[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}
	factories[name] = factory
	return nil
}

// Get returns a registered factory by name.
func Get(name string) (Factory, bool) {
	mu.RLock()
	defer mu.RUnlock()
	factory, ok := factories[name]
	return factory, ok
}

// Names returns all registered plugin names.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	nameList := make([]string, 0, len(factories))
	for name := range factories {
		nameList = append(nameList, name)
	}
	sort.Strings(nameList)
	return nameList
}
