package registry

import (
	"sync"
	"testing"
)

// mockPlatform is a mock implementation of Platform interface for testing.
type mockPlatform struct {
	name    string
	pattern string
}

func (m *mockPlatform) Name() string {
	return m.name
}

func (m *mockPlatform) MatchURL(url string) (string, bool) {
	if m.pattern == "" {
		return "", false
	}
	// Simple pattern matching for testing
	if len(url) >= len(m.pattern) && url[:len(m.pattern)] == m.pattern {
		return "matched", true
	}
	return "", false
}

func newMockPlatform(name, pattern string) Platform {
	return &mockPlatform{name: name, pattern: pattern}
}

func TestRegister_Success(t *testing.T) {
	r := New()
	p := newMockPlatform("test", "https://test.com")

	err := r.Register(p)
	if err != nil {
		t.Errorf("Register() error = %v, want nil", err)
	}

	// Verify platform was registered
	got, ok := r.Get("test")
	if !ok {
		t.Error("Get() returned false, want true")
	}
	if got.Name() != "test" {
		t.Errorf("Get() name = %v, want test", got.Name())
	}
}

func TestRegister_EmptyName(t *testing.T) {
	r := New()
	p := newMockPlatform("", "https://test.com")

	err := r.Register(p)
	if err == nil {
		t.Error("Register() error = nil, want error for empty name")
	}
}

func TestRegister_Duplicate(t *testing.T) {
	r := New()
	p1 := newMockPlatform("test", "https://test1.com")
	p2 := newMockPlatform("test", "https://test2.com")

	err := r.Register(p1)
	if err != nil {
		t.Errorf("First Register() error = %v, want nil", err)
	}

	err = r.Register(p2)
	if err == nil {
		t.Error("Duplicate Register() error = nil, want error")
	}

	// Verify original platform is still registered
	got, ok := r.Get("test")
	if !ok {
		t.Error("Get() returned false, want true")
	}
	if id, _ := got.MatchURL("https://test1.com/123"); id == "" {
		t.Error("Original platform was replaced, want it to remain")
	}
}

func TestGet_Success(t *testing.T) {
	r := New()
	p := newMockPlatform("spotify", "https://spotify.com")
	r.Register(p)

	tests := []struct {
		name     string
		key      string
		wantName string
		wantOk   bool
	}{
		{
			name:     "existing platform",
			key:      "spotify",
			wantName: "spotify",
			wantOk:   true,
		},
		{
			name:     "case sensitive",
			key:      "Spotify",
			wantName: "",
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := r.Get(tt.key)
			if ok != tt.wantOk {
				t.Errorf("Get() ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && got.Name() != tt.wantName {
				t.Errorf("Get() name = %v, want %v", got.Name(), tt.wantName)
			}
		})
	}
}

func TestGet_NotFound(t *testing.T) {
	r := New()

	got, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get() ok = true, want false")
	}
	if got != nil {
		t.Errorf("Get() = %v, want nil", got)
	}
}

func TestGetAll(t *testing.T) {
	r := New()

	// Test empty registry
	platforms := r.GetAll()
	if len(platforms) != 0 {
		t.Errorf("GetAll() len = %v, want 0 for empty registry", len(platforms))
	}

	// Register multiple platforms
	p1 := newMockPlatform("platform1", "https://p1.com")
	p2 := newMockPlatform("platform2", "https://p2.com")
	p3 := newMockPlatform("platform3", "https://p3.com")

	r.Register(p1)
	r.Register(p2)
	r.Register(p3)

	platforms = r.GetAll()
	if len(platforms) != 3 {
		t.Errorf("GetAll() len = %v, want 3", len(platforms))
	}

	// Verify all platforms are present
	names := make(map[string]bool)
	for _, p := range platforms {
		names[p.Name()] = true
	}

	expected := []string{"platform1", "platform2", "platform3"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("GetAll() missing platform %v", name)
		}
	}
}

func TestMatchURL_Success(t *testing.T) {
	r := New()
	p := newMockPlatform("youtube", "https://youtube.com")
	r.Register(p)

	id, platform, ok := r.MatchURL("https://youtube.com/watch?v=123")
	if !ok {
		t.Error("MatchURL() ok = false, want true")
	}
	if platform.Name() != "youtube" {
		t.Errorf("MatchURL() platform = %v, want youtube", platform.Name())
	}
	if id != "matched" {
		t.Errorf("MatchURL() id = %v, want matched", id)
	}
}

func TestMatchURL_NotFound(t *testing.T) {
	r := New()
	p := newMockPlatform("youtube", "https://youtube.com")
	r.Register(p)

	id, platform, ok := r.MatchURL("https://spotify.com/track/123")
	if ok {
		t.Error("MatchURL() ok = true, want false")
	}
	if platform != nil {
		t.Errorf("MatchURL() platform = %v, want nil", platform)
	}
	if id != "" {
		t.Errorf("MatchURL() id = %v, want empty", id)
	}
}

func TestMatchURL_EmptyRegistry(t *testing.T) {
	r := New()

	id, platform, ok := r.MatchURL("https://youtube.com/watch?v=123")
	if ok {
		t.Error("MatchURL() ok = true, want false for empty registry")
	}
	if platform != nil {
		t.Errorf("MatchURL() platform = %v, want nil", platform)
	}
	if id != "" {
		t.Errorf("MatchURL() id = %v, want empty", id)
	}
}

func TestMatchURL_MultiplePlatforms(t *testing.T) {
	r := New()

	// Register platforms in specific order
	p1 := newMockPlatform("youtube", "https://youtube.com")
	p2 := newMockPlatform("youtu.be", "https://youtu.be")
	p3 := newMockPlatform("spotify", "https://spotify.com")

	r.Register(p1)
	r.Register(p2)
	r.Register(p3)

	tests := []struct {
		name         string
		url          string
		wantPlatform string
		wantOk       bool
	}{
		{
			name:         "first match",
			url:          "https://youtube.com/watch?v=123",
			wantPlatform: "youtube",
			wantOk:       true,
		},
		{
			name:         "second platform",
			url:          "https://youtu.be/123",
			wantPlatform: "youtu.be",
			wantOk:       true,
		},
		{
			name:         "third platform",
			url:          "https://spotify.com/track/123",
			wantPlatform: "spotify",
			wantOk:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, platform, ok := r.MatchURL(tt.url)
			if ok != tt.wantOk {
				t.Errorf("MatchURL() ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && platform.Name() != tt.wantPlatform {
				t.Errorf("MatchURL() platform = %v, want %v", platform.Name(), tt.wantPlatform)
			}
		})
	}
}

func TestConcurrency(t *testing.T) {
	r := New()
	const numGoroutines = 100
	const numOps = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent registration
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			// Try to register (some will fail due to duplicates, that's expected)
			platformName := "platform"
			if id%10 == 0 {
				// Create unique platform names for some goroutines
				platformName = string(rune('a' + id/10))
			}
			p := newMockPlatform(platformName, "https://test.com")
			r.Register(p)

			// Perform multiple reads
			for j := 0; j < numOps; j++ {
				r.Get(platformName)
				r.GetAll()
				r.MatchURL("https://test.com/123")
			}
		}(i)
	}

	wg.Wait()

	// Verify registry is still functional
	platforms := r.GetAll()
	if len(platforms) == 0 {
		t.Error("Registry is empty after concurrent operations")
	}
}

func TestConcurrency_RegisterAndGet(t *testing.T) {
	r := New()
	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent registrations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			name := string(rune('A' + id))
			p := newMockPlatform(name, "https://"+name+".com")
			r.Register(p)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			name := string(rune('A' + id))
			r.Get(name)
		}(i)
	}

	wg.Wait()

	// Verify all platforms were registered
	platforms := r.GetAll()
	if len(platforms) != numGoroutines {
		t.Errorf("GetAll() len = %v, want %v", len(platforms), numGoroutines)
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Test that Default is properly initialized
	if Default == nil {
		t.Error("Default registry is nil")
	}

	// Test basic operations on default registry
	p := newMockPlatform("test_default", "https://test.com")
	err := Default.Register(p)
	if err != nil {
		t.Errorf("Register() on Default error = %v", err)
	}

	got, ok := Default.Get("test_default")
	if !ok {
		t.Error("Get() on Default returned false")
	}
	if got.Name() != "test_default" {
		t.Errorf("Get() on Default name = %v, want test_default", got.Name())
	}

	// Clean up
	// Note: In real implementation, we might want a Reset() method for testing
}

func TestRegister_NilPlatform(t *testing.T) {
	r := New()

	err := r.Register(nil)
	if err == nil {
		t.Error("Register() with nil platform should return error")
	}
}

func TestMatchURL_OrderPreservation(t *testing.T) {
	r := New()

	// Register platforms that could both match similar URLs
	// The first one registered should win
	p1 := newMockPlatform("generic", "https://")
	p2 := newMockPlatform("specific", "https://specific.com")

	r.Register(p1)
	r.Register(p2)

	// Should match the first registered platform (generic)
	_, platform, ok := r.MatchURL("https://specific.com/path")
	if !ok {
		t.Error("MatchURL() ok = false, want true")
	}
	if platform.Name() != "generic" {
		t.Errorf("MatchURL() platform = %v, want generic (first registered)", platform.Name())
	}
}
