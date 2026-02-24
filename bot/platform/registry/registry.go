package registry

import (
	"errors"
	"sync"
)

// Platform represents a music streaming platform that can handle URLs.
type Platform interface {
	// Name returns the platform's unique identifier.
	Name() string

	// MatchURL checks if the platform can handle the given URL.
	// Returns the extracted ID and true if matched, or empty string and false if not.
	MatchURL(url string) (string, bool)
}

// Registry manages registered Platform implementations in a thread-safe manner.
type Registry struct {
	mu        sync.RWMutex
	platforms map[string]Platform
	// Order preserving list for MatchURL to maintain registration order
	ordered []Platform
}

// New creates a new Registry instance.
func New() *Registry {
	return &Registry{
		platforms: make(map[string]Platform),
		ordered:   make([]Platform, 0),
	}
}

// Register adds a platform to the registry.
// Returns an error if the platform is nil, has an empty name, or is already registered.
func (r *Registry) Register(p Platform) error {
	if p == nil {
		return errors.New("platform cannot be nil")
	}

	name := p.Name()
	if name == "" {
		return errors.New("platform name cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.platforms[name]; exists {
		return errors.New("platform already registered: " + name)
	}

	r.platforms[name] = p
	r.ordered = append(r.ordered, p)

	return nil
}

// Get retrieves a platform by name.
// Returns the platform and true if found, or nil and false if not found.
func (r *Registry) Get(name string) (Platform, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.platforms[name]
	return p, ok
}

// GetAll returns all registered platforms.
// The returned slice is a copy and safe for concurrent use.
func (r *Registry) GetAll() []Platform {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Platform, 0, len(r.ordered))
	result = append(result, r.ordered...)

	return result
}

// MatchURL finds the first platform that can handle the given URL.
// Returns the extracted ID, the platform, and true if a match is found.
// Returns empty string, nil, and false if no platform matches.
// Platforms are checked in registration order.
func (r *Registry) MatchURL(url string) (string, Platform, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.ordered {
		if id, ok := p.MatchURL(url); ok {
			return id, p, true
		}
	}

	return "", nil, false
}

// Reset clears all registered platforms.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.platforms = make(map[string]Platform)
	r.ordered = r.ordered[:0]
}

// Default is the global default registry instance.
// Platforms can register themselves by calling Default.Register() in their init() functions.
var Default = New()
