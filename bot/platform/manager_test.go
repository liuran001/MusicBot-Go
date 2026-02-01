package platform

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform/registry"
)

// mockPlatform is a mock implementation of Platform for testing.
type mockPlatform struct {
	name                string
	supportsDownload    bool
	supportsSearch      bool
	supportsLyrics      bool
	supportsRecognition bool
	downloadFunc        func(ctx context.Context, trackID string, quality Quality) (io.ReadCloser, *TrackMetadata, error)
	searchFunc          func(ctx context.Context, query string, limit int) ([]Track, error)
	getLyricsFunc       func(ctx context.Context, trackID string) (*Lyrics, error)
	recognizeAudioFunc  func(ctx context.Context, audioData io.Reader) (*Track, error)
	getTrackFunc        func(ctx context.Context, trackID string) (*Track, error)
	getArtistFunc       func(ctx context.Context, artistID string) (*Artist, error)
	getAlbumFunc        func(ctx context.Context, albumID string) (*Album, error)
	getPlaylistFunc     func(ctx context.Context, playlistID string) (*Playlist, error)
	matchURLFunc        func(url string) (trackID string, matched bool)
}

func (m *mockPlatform) Name() string {
	return m.name
}

func (m *mockPlatform) SupportsDownload() bool {
	return m.supportsDownload
}

func (m *mockPlatform) SupportsSearch() bool {
	return m.supportsSearch
}

func (m *mockPlatform) SupportsLyrics() bool {
	return m.supportsLyrics
}

func (m *mockPlatform) SupportsRecognition() bool {
	return m.supportsRecognition
}

func (m *mockPlatform) Download(ctx context.Context, trackID string, quality Quality) (io.ReadCloser, *TrackMetadata, error) {
	if m.downloadFunc != nil {
		return m.downloadFunc(ctx, trackID, quality)
	}
	return nil, nil, ErrUnsupported
}

func (m *mockPlatform) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, query, limit)
	}
	return nil, ErrUnsupported
}

func (m *mockPlatform) GetLyrics(ctx context.Context, trackID string) (*Lyrics, error) {
	if m.getLyricsFunc != nil {
		return m.getLyricsFunc(ctx, trackID)
	}
	return nil, ErrUnsupported
}

func (m *mockPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*Track, error) {
	if m.recognizeAudioFunc != nil {
		return m.recognizeAudioFunc(ctx, audioData)
	}
	return nil, ErrUnsupported
}

func (m *mockPlatform) GetTrack(ctx context.Context, trackID string) (*Track, error) {
	if m.getTrackFunc != nil {
		return m.getTrackFunc(ctx, trackID)
	}
	return nil, ErrUnsupported
}

func (m *mockPlatform) GetArtist(ctx context.Context, artistID string) (*Artist, error) {
	if m.getArtistFunc != nil {
		return m.getArtistFunc(ctx, artistID)
	}
	return nil, ErrUnsupported
}

func (m *mockPlatform) GetAlbum(ctx context.Context, albumID string) (*Album, error) {
	if m.getAlbumFunc != nil {
		return m.getAlbumFunc(ctx, albumID)
	}
	return nil, ErrUnsupported
}

func (m *mockPlatform) GetPlaylist(ctx context.Context, playlistID string) (*Playlist, error) {
	if m.getPlaylistFunc != nil {
		return m.getPlaylistFunc(ctx, playlistID)
	}
	return nil, ErrUnsupported
}

func (m *mockPlatform) MatchURL(url string) (trackID string, matched bool) {
	if m.matchURLFunc != nil {
		return m.matchURLFunc(url)
	}
	return "", false
}

func TestNewManager(t *testing.T) {
	manager := NewManager()
	if manager == nil {
		t.Fatal("NewManager returned nil")
	}
	if manager.registry == nil {
		t.Fatal("Manager registry is nil")
	}
}

func TestNewManagerWithRegistry(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)
	if manager == nil {
		t.Fatal("NewManagerWithRegistry returned nil")
	}
	if manager.registry != reg {
		t.Fatal("Manager registry is not the provided registry")
	}
}

func TestManager_Register(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	mock := &mockPlatform{name: "test-platform"}
	manager.Register(mock)

	// Verify platform was registered
	platform := manager.Get("test-platform")
	if platform == nil {
		t.Fatal("Platform not found after registration")
	}
	if platform.Name() != "test-platform" {
		t.Errorf("Expected platform name 'test-platform', got '%s'", platform.Name())
	}
}

func TestManager_Get(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	mock := &mockPlatform{name: "test-platform"}
	manager.Register(mock)

	tests := []struct {
		name         string
		platformName string
		expectNil    bool
	}{
		{
			name:         "existing platform",
			platformName: "test-platform",
			expectNil:    false,
		},
		{
			name:         "non-existing platform",
			platformName: "non-existing",
			expectNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform := manager.Get(tt.platformName)
			if tt.expectNil {
				if platform != nil {
					t.Errorf("Expected nil platform, got %v", platform)
				}
			} else {
				if platform == nil {
					t.Error("Expected non-nil platform, got nil")
				}
			}
		})
	}
}

func TestManager_List(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	// Test with no platforms
	names := manager.List()
	if len(names) != 0 {
		t.Errorf("Expected empty list, got %v", names)
	}

	// Register multiple platforms
	mock1 := &mockPlatform{name: "platform1"}
	mock2 := &mockPlatform{name: "platform2"}
	mock3 := &mockPlatform{name: "platform3"}

	manager.Register(mock1)
	manager.Register(mock2)
	manager.Register(mock3)

	names = manager.List()
	if len(names) != 3 {
		t.Errorf("Expected 3 platforms, got %d", len(names))
	}

	expectedNames := map[string]bool{
		"platform1": false,
		"platform2": false,
		"platform3": false,
	}

	for _, name := range names {
		if _, ok := expectedNames[name]; ok {
			expectedNames[name] = true
		} else {
			t.Errorf("Unexpected platform name: %s", name)
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("Platform %s not found in list", name)
		}
	}
}

func TestManager_MatchURL(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	// Platform that matches specific URLs
	mockNetease := &mockPlatform{
		name: "netease",
		matchURLFunc: func(url string) (string, bool) {
			if url == "https://music.163.com/song?id=123456" {
				return "123456", true
			}
			return "", false
		},
	}

	mockSpotify := &mockPlatform{
		name: "spotify",
		matchURLFunc: func(url string) (string, bool) {
			if url == "https://open.spotify.com/track/abcdef" {
				return "abcdef", true
			}
			return "", false
		},
	}

	manager.Register(mockNetease)
	manager.Register(mockSpotify)

	tests := []struct {
		name             string
		url              string
		expectedPlatform string
		expectedTrackID  string
		expectedMatched  bool
	}{
		{
			name:             "netease url",
			url:              "https://music.163.com/song?id=123456",
			expectedPlatform: "netease",
			expectedTrackID:  "123456",
			expectedMatched:  true,
		},
		{
			name:             "spotify url",
			url:              "https://open.spotify.com/track/abcdef",
			expectedPlatform: "spotify",
			expectedTrackID:  "abcdef",
			expectedMatched:  true,
		},
		{
			name:             "no match",
			url:              "https://example.com/unknown",
			expectedPlatform: "",
			expectedTrackID:  "",
			expectedMatched:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platformName, trackID, matched := manager.MatchURL(tt.url)
			if platformName != tt.expectedPlatform {
				t.Errorf("Expected platform '%s', got '%s'", tt.expectedPlatform, platformName)
			}
			if trackID != tt.expectedTrackID {
				t.Errorf("Expected track ID '%s', got '%s'", tt.expectedTrackID, trackID)
			}
			if matched != tt.expectedMatched {
				t.Errorf("Expected matched %v, got %v", tt.expectedMatched, matched)
			}
		})
	}
}

func TestManager_GetPlatform(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	mock := &mockPlatform{name: "test-platform"}
	manager.Register(mock)

	tests := []struct {
		name         string
		platformName string
		expectError  bool
	}{
		{
			name:         "existing platform",
			platformName: "test-platform",
			expectError:  false,
		},
		{
			name:         "non-existing platform",
			platformName: "non-existing",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform, err := manager.GetPlatform(tt.platformName)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				if platform != nil {
					t.Error("Expected nil platform on error")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if platform == nil {
					t.Error("Expected non-nil platform")
				}
			}
		})
	}
}

func TestManager_MustGet(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	mock := &mockPlatform{name: "test-platform"}
	manager.Register(mock)

	// Test successful get
	platform := manager.MustGet("test-platform")
	if platform == nil {
		t.Error("Expected non-nil platform")
	}

	// Test panic on missing platform
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic on missing platform")
		}
	}()
	manager.MustGet("non-existing")
}

func TestManager_Download(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	expectedMetadata := &TrackMetadata{
		Format:  "mp3",
		Bitrate: 320,
		Quality: QualityHigh,
	}

	mock := &mockPlatform{
		name: "test-platform",
		downloadFunc: func(ctx context.Context, trackID string, quality Quality) (io.ReadCloser, *TrackMetadata, error) {
			if trackID == "123" {
				return nil, expectedMetadata, nil
			}
			return nil, nil, ErrNotFound
		},
	}

	manager.Register(mock)

	tests := []struct {
		name        string
		platform    string
		trackID     string
		quality     Quality
		expectError bool
	}{
		{
			name:        "successful download",
			platform:    "test-platform",
			trackID:     "123",
			quality:     QualityHigh,
			expectError: false,
		},
		{
			name:        "track not found",
			platform:    "test-platform",
			trackID:     "999",
			quality:     QualityHigh,
			expectError: true,
		},
		{
			name:        "platform not found",
			platform:    "non-existing",
			trackID:     "123",
			quality:     QualityHigh,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reader, metadata, err := manager.Download(ctx, tt.platform, tt.trackID, tt.quality)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if metadata == nil {
					t.Error("Expected metadata, got nil")
				}
				if metadata != nil && metadata.Bitrate != expectedMetadata.Bitrate {
					t.Errorf("Expected bitrate %d, got %d", expectedMetadata.Bitrate, metadata.Bitrate)
				}
			}

			// Clean up reader if present
			if reader != nil {
				reader.Close()
			}
		})
	}
}

func TestManager_Search(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	expectedTracks := []Track{
		{ID: "1", Platform: "test-platform", Title: "Track 1"},
		{ID: "2", Platform: "test-platform", Title: "Track 2"},
	}

	mock := &mockPlatform{
		name: "test-platform",
		searchFunc: func(ctx context.Context, query string, limit int) ([]Track, error) {
			if query == "test" {
				return expectedTracks[:limit], nil
			}
			return nil, ErrNotFound
		},
	}

	manager.Register(mock)

	tests := []struct {
		name        string
		platform    string
		query       string
		limit       int
		expectError bool
		expectCount int
	}{
		{
			name:        "successful search",
			platform:    "test-platform",
			query:       "test",
			limit:       2,
			expectError: false,
			expectCount: 2,
		},
		{
			name:        "no results",
			platform:    "test-platform",
			query:       "unknown",
			limit:       10,
			expectError: true,
			expectCount: 0,
		},
		{
			name:        "platform not found",
			platform:    "non-existing",
			query:       "test",
			limit:       10,
			expectError: true,
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tracks, err := manager.Search(ctx, tt.platform, tt.query, tt.limit)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if len(tracks) != tt.expectCount {
					t.Errorf("Expected %d tracks, got %d", tt.expectCount, len(tracks))
				}
			}
		})
	}
}

func TestManager_GetLyrics(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	expectedLyrics := &Lyrics{
		Plain: "Test lyrics",
		Timestamped: []LyricLine{
			{Time: 0, Text: "Test lyrics"},
		},
	}

	mock := &mockPlatform{
		name: "test-platform",
		getLyricsFunc: func(ctx context.Context, trackID string) (*Lyrics, error) {
			if trackID == "123" {
				return expectedLyrics, nil
			}
			return nil, ErrNotFound
		},
	}

	manager.Register(mock)

	tests := []struct {
		name        string
		platform    string
		trackID     string
		expectError bool
	}{
		{
			name:        "successful lyrics fetch",
			platform:    "test-platform",
			trackID:     "123",
			expectError: false,
		},
		{
			name:        "lyrics not found",
			platform:    "test-platform",
			trackID:     "999",
			expectError: true,
		},
		{
			name:        "platform not found",
			platform:    "non-existing",
			trackID:     "123",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			lyrics, err := manager.GetLyrics(ctx, tt.platform, tt.trackID)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if lyrics == nil {
					t.Error("Expected lyrics, got nil")
				}
				if lyrics != nil && lyrics.Plain != expectedLyrics.Plain {
					t.Errorf("Expected lyrics '%s', got '%s'", expectedLyrics.Plain, lyrics.Plain)
				}
			}
		})
	}
}

func TestManager_GetTrack(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	expectedTrack := &Track{
		ID:       "123",
		Platform: "test-platform",
		Title:    "Test Track",
		Duration: 3 * time.Minute,
	}

	mock := &mockPlatform{
		name: "test-platform",
		getTrackFunc: func(ctx context.Context, trackID string) (*Track, error) {
			if trackID == "123" {
				return expectedTrack, nil
			}
			return nil, ErrNotFound
		},
	}

	manager.Register(mock)

	tests := []struct {
		name        string
		platform    string
		trackID     string
		expectError bool
	}{
		{
			name:        "successful track fetch",
			platform:    "test-platform",
			trackID:     "123",
			expectError: false,
		},
		{
			name:        "track not found",
			platform:    "test-platform",
			trackID:     "999",
			expectError: true,
		},
		{
			name:        "platform not found",
			platform:    "non-existing",
			trackID:     "123",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			track, err := manager.GetTrack(ctx, tt.platform, tt.trackID)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if track == nil {
					t.Error("Expected track, got nil")
				}
				if track != nil && track.Title != expectedTrack.Title {
					t.Errorf("Expected track title '%s', got '%s'", expectedTrack.Title, track.Title)
				}
			}
		})
	}
}

func TestPlatformWrapper(t *testing.T) {
	mock := &mockPlatform{
		name: "test-platform",
		matchURLFunc: func(url string) (string, bool) {
			if url == "test-url" {
				return "test-id", true
			}
			return "", false
		},
	}

	wrapper := &platformWrapper{platform: mock}

	// Test Name
	if wrapper.Name() != "test-platform" {
		t.Errorf("Expected name 'test-platform', got '%s'", wrapper.Name())
	}

	// Test MatchURL with URLMatcher implementation
	id, ok := wrapper.MatchURL("test-url")
	if !ok {
		t.Error("Expected match, got false")
	}
	if id != "test-id" {
		t.Errorf("Expected ID 'test-id', got '%s'", id)
	}

	// Test MatchURL without match
	id, ok = wrapper.MatchURL("unknown-url")
	if ok {
		t.Error("Expected no match, got true")
	}
	if id != "" {
		t.Errorf("Expected empty ID, got '%s'", id)
	}

	// Test with platform that doesn't implement URLMatcher
	mockNoMatcher := &mockPlatform{
		name: "no-matcher",
		// No matchURLFunc
	}
	mockNoMatcher.matchURLFunc = nil

	wrapperNoMatcher := &platformWrapper{platform: mockNoMatcher}
	id, ok = wrapperNoMatcher.MatchURL("any-url")
	if ok {
		t.Error("Expected no match for platform without URLMatcher")
	}
	if id != "" {
		t.Errorf("Expected empty ID for platform without URLMatcher, got '%s'", id)
	}
}

func TestManager_ConvenienceMethods_Errors(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	ctx := context.Background()

	// Test all convenience methods with non-existing platform
	t.Run("Download with non-existing platform", func(t *testing.T) {
		_, _, err := manager.Download(ctx, "non-existing", "123", QualityHigh)
		if err == nil {
			t.Error("Expected error for non-existing platform")
		}
	})

	t.Run("Search with non-existing platform", func(t *testing.T) {
		_, err := manager.Search(ctx, "non-existing", "query", 10)
		if err == nil {
			t.Error("Expected error for non-existing platform")
		}
	})

	t.Run("GetLyrics with non-existing platform", func(t *testing.T) {
		_, err := manager.GetLyrics(ctx, "non-existing", "123")
		if err == nil {
			t.Error("Expected error for non-existing platform")
		}
	})

	t.Run("GetTrack with non-existing platform", func(t *testing.T) {
		_, err := manager.GetTrack(ctx, "non-existing", "123")
		if err == nil {
			t.Error("Expected error for non-existing platform")
		}
	})
}

func TestManager_PlatformErrors(t *testing.T) {
	reg := registry.New()
	manager := NewManagerWithRegistry(reg)

	testErr := errors.New("test error")

	mock := &mockPlatform{
		name: "error-platform",
		downloadFunc: func(ctx context.Context, trackID string, quality Quality) (io.ReadCloser, *TrackMetadata, error) {
			return nil, nil, testErr
		},
		searchFunc: func(ctx context.Context, query string, limit int) ([]Track, error) {
			return nil, testErr
		},
		getLyricsFunc: func(ctx context.Context, trackID string) (*Lyrics, error) {
			return nil, testErr
		},
		getTrackFunc: func(ctx context.Context, trackID string) (*Track, error) {
			return nil, testErr
		},
	}

	manager.Register(mock)

	ctx := context.Background()

	t.Run("Download error propagation", func(t *testing.T) {
		_, _, err := manager.Download(ctx, "error-platform", "123", QualityHigh)
		if !errors.Is(err, testErr) {
			t.Errorf("Expected error to be %v, got %v", testErr, err)
		}
	})

	t.Run("Search error propagation", func(t *testing.T) {
		_, err := manager.Search(ctx, "error-platform", "query", 10)
		if !errors.Is(err, testErr) {
			t.Errorf("Expected error to be %v, got %v", testErr, err)
		}
	})

	t.Run("GetLyrics error propagation", func(t *testing.T) {
		_, err := manager.GetLyrics(ctx, "error-platform", "123")
		if !errors.Is(err, testErr) {
			t.Errorf("Expected error to be %v, got %v", testErr, err)
		}
	})

	t.Run("GetTrack error propagation", func(t *testing.T) {
		_, err := manager.GetTrack(ctx, "error-platform", "123")
		if !errors.Is(err, testErr) {
			t.Errorf("Expected error to be %v, got %v", testErr, err)
		}
	})
}
