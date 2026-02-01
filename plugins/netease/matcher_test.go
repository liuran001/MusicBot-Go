package netease

import (
	"testing"
)

func TestURLMatcherMatchURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantID    string
		wantMatch bool
	}{
		// Standard song URLs with query parameter
		{
			name:      "standard song URL",
			url:       "https://music.163.com/song?id=12345",
			wantID:    "12345",
			wantMatch: true,
		},
		{
			name:      "song URL with larger ID",
			url:       "https://music.163.com/song?id=1234567890",
			wantID:    "1234567890",
			wantMatch: true,
		},
		{
			name:      "song URL with hash fragment",
			url:       "https://music.163.com/#/song?id=67890",
			wantID:    "67890",
			wantMatch: true,
		},
		// Mobile URLs
		{
			name:      "mobile URL",
			url:       "https://y.music.163.com/m/song?id=111111",
			wantID:    "111111",
			wantMatch: true,
		},
		{
			name:      "mobile URL with hash fragment",
			url:       "https://y.music.163.com/#/song?id=222222",
			wantID:    "222222",
			wantMatch: true,
		},
		// Album URLs
		{
			name:      "album URL",
			url:       "https://music.163.com/album?id=12345",
			wantID:    "12345",
			wantMatch: true,
		},
		{
			name:      "album URL with hash fragment",
			url:       "https://music.163.com/#/album?id=54321",
			wantID:    "54321",
			wantMatch: true,
		},
		// Playlist URLs
		{
			name:      "playlist URL",
			url:       "https://music.163.com/playlist?id=99999",
			wantID:    "99999",
			wantMatch: true,
		},
		{
			name:      "playlist URL with hash fragment",
			url:       "https://music.163.com/#/playlist?id=88888",
			wantID:    "88888",
			wantMatch: true,
		},
		// Artist URLs
		{
			name:      "artist URL",
			url:       "https://music.163.com/artist?id=33333",
			wantID:    "33333",
			wantMatch: true,
		},
		{
			name:      "artist URL with hash fragment",
			url:       "https://music.163.com/#/artist?id=44444",
			wantID:    "44444",
			wantMatch: true,
		},
		// DJ URLs
		{
			name:      "DJ URL",
			url:       "https://music.163.com/dj?id=55555",
			wantID:    "55555",
			wantMatch: true,
		},
		// Path-based URL (fallback)
		{
			name:      "path-based song URL",
			url:       "https://music.163.com/song/12345",
			wantID:    "12345",
			wantMatch: true,
		},
		// URLs with multiple query parameters
		{
			name:      "song URL with multiple query params",
			url:       "https://music.163.com/song?id=12345&foo=bar",
			wantID:    "12345",
			wantMatch: true,
		},
		{
			name:      "song URL with id not first param",
			url:       "https://music.163.com/song?foo=bar&id=67890",
			wantID:    "67890",
			wantMatch: true,
		},
		// Complex hash fragments with multiple segments
		{
			name:      "complex hash fragment",
			url:       "https://music.163.com/#/discover/toplist?id=19723756",
			wantID:    "19723756",
			wantMatch: true,
		},
		// Edge cases with different ID formats
		{
			name:      "very large ID",
			url:       "https://music.163.com/song?id=999999999999999",
			wantID:    "999999999999999",
			wantMatch: true,
		},
		{
			name:      "single digit ID",
			url:       "https://music.163.com/song?id=1",
			wantID:    "1",
			wantMatch: true,
		},
		// Invalid URLs - wrong domain
		{
			name:      "wrong domain - spotify",
			url:       "https://open.spotify.com/song?id=12345",
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "wrong domain - youtube",
			url:       "https://www.youtube.com/watch?v=12345",
			wantID:    "",
			wantMatch: false,
		},
		// Invalid URLs - no ID parameter
		{
			name:      "no ID parameter",
			url:       "https://music.163.com/song?foo=bar",
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "hash fragment with no ID",
			url:       "https://music.163.com/#/song",
			wantID:    "",
			wantMatch: false,
		},
		// Invalid URLs - malformed
		{
			name:      "invalid URL format",
			url:       "not a valid url",
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "empty string",
			url:       "",
			wantID:    "",
			wantMatch: false,
		},
		// URLs with http instead of https
		{
			name:      "http URL",
			url:       "http://music.163.com/song?id=12345",
			wantID:    "12345",
			wantMatch: true,
		},
		// Short link variants (163cn.tv and 163cn.link are handled by router, not this matcher)
		// Our matcher focuses only on music.163.com domains
		{
			name:      "non-music.163.com domain",
			url:       "https://163cn.tv/12345",
			wantID:    "",
			wantMatch: false,
		},
		// URL with port number
		{
			name:      "URL with explicit port",
			url:       "https://music.163.com:443/song?id=12345",
			wantID:    "12345",
			wantMatch: true,
		},
		// Subdomain variants
		{
			name:      "m.music.163.com subdomain",
			url:       "https://m.music.163.com/song?id=12345",
			wantID:    "12345",
			wantMatch: true,
		},
		// URL with trailing slash
		{
			name:      "URL with trailing slash",
			url:       "https://music.163.com/song/?id=12345",
			wantID:    "12345",
			wantMatch: true,
		},
		// Real-world example from handler
		{
			name:      "real-world song link",
			url:       "https://music.163.com/song/1234567890",
			wantID:    "1234567890",
			wantMatch: true,
		},
	}

	matcher := NewURLMatcher()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotMatch := matcher.MatchURL(tt.url)
			if gotMatch != tt.wantMatch {
				t.Errorf("MatchURL() gotMatch = %v, want %v", gotMatch, tt.wantMatch)
			}
			if got != tt.wantID {
				t.Errorf("MatchURL() gotID = %q, want %q", got, tt.wantID)
			}
		})
	}
}

func TestNewURLMatcher(t *testing.T) {
	matcher := NewURLMatcher()
	if matcher == nil {
		t.Fatal("NewURLMatcher() returned nil")
	}
}

func TestURLMatcherConsistency(t *testing.T) {
	// Test that the matcher is stateless and returns consistent results
	matcher := NewURLMatcher()
	url := "https://music.163.com/song?id=12345"

	id1, match1 := matcher.MatchURL(url)
	id2, match2 := matcher.MatchURL(url)

	if id1 != id2 || match1 != match2 {
		t.Errorf("MatchURL() returned inconsistent results")
	}
}

func TestURLMatcherDifferentInstances(t *testing.T) {
	// Test that different matcher instances produce the same results
	matcher1 := NewURLMatcher()
	matcher2 := NewURLMatcher()
	url := "https://music.163.com/song?id=12345"

	id1, match1 := matcher1.MatchURL(url)
	id2, match2 := matcher2.MatchURL(url)

	if id1 != id2 || match1 != match2 {
		t.Errorf("Different matcher instances returned different results")
	}
}
