package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/liuran001/MusicBot-Go/bot/platform/registry"
)

// DefaultManager implements the Manager interface by wrapping the registry.
// It provides a high-level API for managing and interacting with multiple music platforms.
// It now supports multiple providers per platform name with automatic fallback.
type DefaultManager struct {
	registry *registry.Registry
	mu       sync.RWMutex
	// providers maps platform name to a list of providers in registration order
	providers map[string][]Platform
	meta      map[string]Meta
	aliases   map[string]string
}

// NewManager creates a new manager instance with the default global registry.
func NewManager() *DefaultManager {
	return &DefaultManager{
		registry:  registry.Default,
		providers: make(map[string][]Platform),
		meta:      make(map[string]Meta),
		aliases:   make(map[string]string),
	}
}

// NewManagerWithRegistry creates a new manager with a custom registry.
// This is useful for testing or isolated instances.
func NewManagerWithRegistry(reg *registry.Registry) *DefaultManager {
	return &DefaultManager{
		registry:  reg,
		providers: make(map[string][]Platform),
		meta:      make(map[string]Meta),
		aliases:   make(map[string]string),
	}
}

// Register adds a platform implementation to the manager.
// Multiple providers can be registered for the same platform name.
// Providers are tried in registration order when Get returns a composite platform.
func (m *DefaultManager) Register(platform Platform) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := platform.Name()
	m.providers[name] = append(m.providers[name], platform)
	meta := buildMeta(platform, name)
	if existing, ok := m.meta[name]; ok {
		meta = mergeMeta(existing, meta)
	}
	m.meta[name] = meta
	m.indexAliases(meta)

	// For URL matching, register only the first provider for each platform name
	// to preserve URL matching behavior (first registered provider handles MatchURL)
	if len(m.providers[name]) == 1 {
		wrapper := &platformWrapper{platform: platform}
		_ = m.registry.Register(wrapper)
	}
}

// Get retrieves a platform by name.
// If multiple providers are registered for the same name, returns a composite
// platform that tries providers in registration order with automatic fallback.
// Returns nil if no platform with that name is registered.
func (m *DefaultManager) Get(name string) Platform {
	m.mu.RLock()
	defer m.mu.RUnlock()

	providers, ok := m.providers[name]
	if !ok || len(providers) == 0 {
		return nil
	}

	// Single provider: return directly
	if len(providers) == 1 {
		return providers[0]
	}

	// Multiple providers: return composite with fallback
	return &compositePlatform{
		name:      name,
		providers: providers,
	}
}

// compositePlatform implements Platform by trying multiple providers in order with fallback.
type compositePlatform struct {
	name      string
	providers []Platform
}

func (c *compositePlatform) Name() string {
	return c.name
}

func (c *compositePlatform) SupportsDownload() bool {
	for _, p := range c.providers {
		if p.SupportsDownload() {
			return true
		}
	}
	return false
}

func (c *compositePlatform) SupportsSearch() bool {
	for _, p := range c.providers {
		if p.SupportsSearch() {
			return true
		}
	}
	return false
}

func (c *compositePlatform) SupportsLyrics() bool {
	for _, p := range c.providers {
		if p.SupportsLyrics() {
			return true
		}
	}
	return false
}

func (c *compositePlatform) SupportsRecognition() bool {
	for _, p := range c.providers {
		if p.SupportsRecognition() {
			return true
		}
	}
	return false
}

func (c *compositePlatform) Capabilities() Capabilities {
	var combined Capabilities
	for _, p := range c.providers {
		caps := p.Capabilities()
		combined.Download = combined.Download || caps.Download
		combined.Search = combined.Search || caps.Search
		combined.Lyrics = combined.Lyrics || caps.Lyrics
		combined.Recognition = combined.Recognition || caps.Recognition
		combined.HiRes = combined.HiRes || caps.HiRes
	}
	return combined
}

func (c *compositePlatform) GetDownloadInfo(ctx context.Context, trackID string, quality Quality) (*DownloadInfo, error) {
	var lastErr error
	for _, p := range c.providers {
		if !p.SupportsDownload() {
			continue
		}
		info, err := p.GetDownloadInfo(ctx, trackID, quality)
		if err == nil {
			return info, nil
		}
		lastErr = err
		if !shouldRetry(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnsupported
}

func (c *compositePlatform) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	var lastErr error
	anyProviderAttempted := false
	for _, p := range c.providers {
		if !p.SupportsSearch() {
			continue
		}
		anyProviderAttempted = true
		tracks, err := p.Search(ctx, query, limit)
		if err == nil && len(tracks) > 0 {
			return tracks, nil
		}
		// Continue to next provider if error occurs or empty results
		lastErr = err
		if err != nil && !shouldRetry(err) {
			return nil, err
		}
	}
	// Only return ErrUnsupported if no provider supports search
	if !anyProviderAttempted {
		return nil, ErrUnsupported
	}
	// If at least one provider attempted but all returned empty or errors, return last error or empty
	if lastErr != nil {
		return nil, lastErr
	}
	return []Track{}, nil
}

func (c *compositePlatform) GetLyrics(ctx context.Context, trackID string) (*Lyrics, error) {
	var lastErr error
	for _, p := range c.providers {
		if !p.SupportsLyrics() {
			continue
		}
		lyrics, err := p.GetLyrics(ctx, trackID)
		if err == nil {
			return lyrics, nil
		}
		lastErr = err
		if !shouldRetry(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnsupported
}

func (c *compositePlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*Track, error) {
	var lastErr error
	for _, p := range c.providers {
		if !p.SupportsRecognition() {
			continue
		}
		track, err := p.RecognizeAudio(ctx, audioData)
		if err == nil {
			return track, nil
		}
		lastErr = err
		if !shouldRetry(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnsupported
}

func (c *compositePlatform) GetTrack(ctx context.Context, trackID string) (*Track, error) {
	var lastErr error
	for _, p := range c.providers {
		track, err := p.GetTrack(ctx, trackID)
		if err == nil {
			return track, nil
		}
		lastErr = err
		if !shouldRetry(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnsupported
}

func (c *compositePlatform) GetArtist(ctx context.Context, artistID string) (*Artist, error) {
	var lastErr error
	for _, p := range c.providers {
		artist, err := p.GetArtist(ctx, artistID)
		if err == nil {
			return artist, nil
		}
		lastErr = err
		if !shouldRetry(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnsupported
}

func (c *compositePlatform) GetAlbum(ctx context.Context, albumID string) (*Album, error) {
	var lastErr error
	for _, p := range c.providers {
		album, err := p.GetAlbum(ctx, albumID)
		if err == nil {
			return album, nil
		}
		lastErr = err
		if !shouldRetry(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnsupported
}

func (c *compositePlatform) GetPlaylist(ctx context.Context, playlistID string) (*Playlist, error) {
	var lastErr error
	for _, p := range c.providers {
		playlist, err := p.GetPlaylist(ctx, playlistID)
		if err == nil {
			return playlist, nil
		}
		lastErr = err
		if !shouldRetry(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnsupported
}

func shouldRetry(err error) bool {
	return errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrUnavailable) ||
		errors.Is(err, ErrUnsupported) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrAuthRequired)
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

// MatchText attempts to match text against all registered platforms that support text matching.
// Returns the platform name, track ID, and true if a match is found.
// Returns empty strings and false if no platform matches the text.
func (m *DefaultManager) MatchText(text string) (platformName, trackID string, matched bool) {
	if m == nil {
		return "", "", false
	}
	for _, name := range m.List() {
		platform := m.Get(name)
		if platform == nil {
			continue
		}
		if matcher, ok := platform.(TextMatcher); ok {
			if id, ok := matcher.MatchText(text); ok {
				return name, id, true
			}
		}
	}
	return "", "", false
}

// ResolveAlias resolves a platform alias to its canonical platform name.
func (m *DefaultManager) ResolveAlias(alias string) (string, bool) {
	if m == nil {
		return "", false
	}
	key := normalizeAlias(alias)
	if key == "" {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.providers[key]; ok {
		return key, true
	}
	if name, ok := m.aliases[key]; ok {
		return name, true
	}
	return "", false
}

// Meta returns metadata for a platform name.
func (m *DefaultManager) Meta(name string) (Meta, bool) {
	if m == nil {
		return Meta{}, false
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Meta{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta, ok := m.meta[trimmed]
	if !ok {
		return Meta{Name: trimmed, DisplayName: trimmed, Emoji: "🎵"}, false
	}
	return meta, true
}

// ListMeta returns metadata for all registered platforms.
func (m *DefaultManager) ListMeta() []Meta {
	if m == nil {
		return nil
	}
	names := m.List()
	metas := make([]Meta, 0, len(names))
	for _, name := range names {
		meta, _ := m.Meta(name)
		metas = append(metas, meta)
	}
	return metas
}

func (m *DefaultManager) indexAliases(meta Meta) {
	for _, alias := range meta.Aliases {
		key := normalizeAlias(alias)
		if key == "" {
			continue
		}
		if existing, ok := m.aliases[key]; ok {
			if existing == meta.Name {
				continue
			}
			continue
		}
		m.aliases[key] = meta.Name
	}
}

func buildMeta(platform Platform, name string) Meta {
	meta := Meta{}
	if provider, ok := platform.(MetadataProvider); ok {
		meta = provider.Metadata()
	}
	if meta.Name == "" {
		meta.Name = name
	}
	if meta.DisplayName == "" {
		meta.DisplayName = meta.Name
	}
	if meta.Emoji == "" {
		meta.Emoji = "🎵"
	}
	return meta
}

func mergeMeta(oldMeta, newMeta Meta) Meta {
	if newMeta.Name == "" {
		newMeta.Name = oldMeta.Name
	}
	if newMeta.DisplayName == "" {
		newMeta.DisplayName = oldMeta.DisplayName
	}
	if newMeta.Emoji == "" {
		newMeta.Emoji = oldMeta.Emoji
	}
	newMeta.AllowGroupURL = newMeta.AllowGroupURL || oldMeta.AllowGroupURL
	newMeta.Aliases = mergeAliases(oldMeta.Aliases, newMeta.Aliases)
	return newMeta
}

func mergeAliases(existing, incoming []string) []string {
	if len(existing) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]struct{})
	merged := make([]string, 0, len(existing)+len(incoming))
	for _, alias := range existing {
		key := normalizeAlias(alias)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, alias)
	}
	for _, alias := range incoming {
		key := normalizeAlias(alias)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, alias)
	}
	return merged
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

// GetDownloadInfo is a convenience method that retrieves a platform and resolves download info.
// It combines Get and Platform.GetDownloadInfo into a single call.
func (m *DefaultManager) GetDownloadInfo(ctx context.Context, platformName, trackID string, quality Quality) (*DownloadInfo, error) {
	platform, err := m.GetPlatform(platformName)
	if err != nil {
		return nil, err
	}
	return platform.GetDownloadInfo(ctx, trackID, quality)
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
