package platform

import (
	"context"
	"fmt"
	"io"

	"github.com/liuran001/MusicBot-Go/bot/platform/registry"
)

// DefaultManager implements the Manager interface by wrapping the registry.
// It provides a high-level API for managing and interacting with multiple music platforms.
type DefaultManager struct {
	registry *registry.Registry
}

// NewManager creates a new manager instance with the default global registry.
func NewManager() *DefaultManager {
	return &DefaultManager{
		registry: registry.Default,
	}
}

// NewManagerWithRegistry creates a new manager with a custom registry.
// This is useful for testing or isolated instances.
func NewManagerWithRegistry(reg *registry.Registry) *DefaultManager {
	return &DefaultManager{
		registry: reg,
	}
}

// Register adds a platform implementation to the manager.
// If a platform with the same name already exists, it will be replaced.
// The platform must implement both the Platform interface and the URLMatcher interface
// (from registry package) to be properly registered.
func (m *DefaultManager) Register(platform Platform) {
	// Wrap the platform to implement registry.Platform interface
	wrapper := &platformWrapper{platform: platform}
	// Note: Registry.Register returns an error, but Manager.Register doesn't
	// We'll ignore the error here to match the Manager interface signature
	_ = m.registry.Register(wrapper)
}

// Get retrieves a platform by name.
// Returns nil if no platform with that name is registered.
func (m *DefaultManager) Get(name string) Platform {
	p, ok := m.registry.Get(name)
	if !ok {
		return nil
	}
	if wrapper, ok := p.(*platformWrapper); ok {
		return wrapper.platform
	}
	return nil
}

// List returns all registered platform names.
func (m *DefaultManager) List() []string {
	platforms := m.registry.GetAll()
	names := make([]string, 0, len(platforms))
	for _, p := range platforms {
		names = append(names, p.Name())
	}
	return names
}

// MatchURL attempts to match a URL against all registered platforms.
// Returns the platform name, track ID, and true if a match is found.
// Returns empty strings and false if no platform matches the URL.
func (m *DefaultManager) MatchURL(url string) (platformName, trackID string, matched bool) {
	id, p, ok := m.registry.MatchURL(url)
	if !ok {
		return "", "", false
	}
	return p.Name(), id, true
}

// GetPlatform retrieves a platform by name and returns an error if not found.
// This is a convenience method that provides better error handling than Get.
func (m *DefaultManager) GetPlatform(name string) (Platform, error) {
	p := m.Get(name)
	if p == nil {
		return nil, fmt.Errorf("platform not found: %s", name)
	}
	return p, nil
}

// MustGet retrieves a platform by name or panics if not found.
// This is useful during initialization where missing platforms should fail fast.
func (m *DefaultManager) MustGet(name string) Platform {
	p := m.Get(name)
	if p == nil {
		panic(fmt.Sprintf("platform not found: %s", name))
	}
	return p
}

// Download is a convenience method that retrieves a platform and downloads a track.
// It combines Get and Platform.Download into a single call.
func (m *DefaultManager) Download(ctx context.Context, platformName, trackID string, quality Quality) (io.ReadCloser, *TrackMetadata, error) {
	platform, err := m.GetPlatform(platformName)
	if err != nil {
		return nil, nil, err
	}
	return platform.Download(ctx, trackID, quality)
}

// Search is a convenience method that retrieves a platform and performs a search.
// It combines Get and Platform.Search into a single call.
func (m *DefaultManager) Search(ctx context.Context, platformName, query string, limit int) ([]Track, error) {
	platform, err := m.GetPlatform(platformName)
	if err != nil {
		return nil, err
	}
	return platform.Search(ctx, query, limit)
}

// GetLyrics is a convenience method that retrieves a platform and fetches lyrics.
// It combines Get and Platform.GetLyrics into a single call.
func (m *DefaultManager) GetLyrics(ctx context.Context, platformName, trackID string) (*Lyrics, error) {
	platform, err := m.GetPlatform(platformName)
	if err != nil {
		return nil, err
	}
	return platform.GetLyrics(ctx, trackID)
}

// GetTrack is a convenience method that retrieves a platform and fetches track details.
// It combines Get and Platform.GetTrack into a single call.
func (m *DefaultManager) GetTrack(ctx context.Context, platformName, trackID string) (*Track, error) {
	platform, err := m.GetPlatform(platformName)
	if err != nil {
		return nil, err
	}
	return platform.GetTrack(ctx, trackID)
}

// platformWrapper adapts a platform.Platform to implement registry.Platform.
// It delegates the MatchURL method to the platform if it implements URLMatcher.
type platformWrapper struct {
	platform Platform
}

// Name implements registry.Platform.
func (w *platformWrapper) Name() string {
	return w.platform.Name()
}

// MatchURL implements registry.Platform.
// If the underlying platform implements URLMatcher, it delegates to that.
// Otherwise, it returns false (no match).
func (w *platformWrapper) MatchURL(url string) (string, bool) {
	if matcher, ok := w.platform.(URLMatcher); ok {
		return matcher.MatchURL(url)
	}
	return "", false
}
