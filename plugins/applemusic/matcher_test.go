package applemusic

import "testing"

func TestMatchURL(t *testing.T) {
	m := NewURLMatcher()

	tests := []struct {
		name    string
		url     string
		wantID  string
		wantOK  bool
	}{
		{"song URL", "https://music.apple.com/us/song/idol/1480785411", "1480785411", true},
		{"song URL with query", "https://music.apple.com/jp/album/the-book/1559070892?i=1559070896", "1559070896", true},
		{"song URL with songId query", "https://music.apple.com/us/album/test/1234567890?songId=9876543210", "9876543210", true},
		{"album URL (not a song)", "https://music.apple.com/us/album/1989/1440935467", "", false},
		{"artist URL (not a song)", "https://music.apple.com/us/artist/yoasobi/1467327778", "", false},
		{"playlist URL (not a song)", "https://music.apple.com/us/playlist/hits/pl.f4d106fed2bd41149aaacabb233eb5eb", "", false},
		{"non-apple URL", "https://open.spotify.com/track/123", "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := m.MatchURL(tt.url)
			if ok != tt.wantOK {
				t.Errorf("MatchURL(%q) ok = %v, want %v", tt.url, ok, tt.wantOK)
			}
			if id != tt.wantID {
				t.Errorf("MatchURL(%q) id = %q, want %q", tt.url, id, tt.wantID)
			}
		})
	}
}

func TestMatchPlaylistURL(t *testing.T) {
	m := NewURLMatcher()

	tests := []struct {
		name    string
		url     string
		wantID  string
		wantOK  bool
	}{
		{"playlist URL", "https://music.apple.com/us/playlist/hits/pl.f4d106fed2bd41149aaacabb233eb5eb", "pl.f4d106fed2bd41149aaacabb233eb5eb", true},
		{"album URL as collection", "https://music.apple.com/us/album/1989/1440935467", "album:1440935467", true},
		{"album URL with song query (not playlist)", "https://music.apple.com/us/album/1989/1440935467?i=1440935397", "", false},
		{"song URL (not playlist)", "https://music.apple.com/us/song/idol/1480785411", "", false},
		{"non-apple URL", "https://example.com/playlist/123", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := m.MatchPlaylistURL(tt.url)
			if ok != tt.wantOK {
				t.Errorf("MatchPlaylistURL(%q) ok = %v, want %v", tt.url, ok, tt.wantOK)
			}
			if id != tt.wantID {
				t.Errorf("MatchPlaylistURL(%q) id = %q, want %q", tt.url, id, tt.wantID)
			}
		})
	}
}

func TestMatchArtistURL(t *testing.T) {
	m := NewURLMatcher()

	tests := []struct {
		name    string
		url     string
		wantID  string
		wantOK  bool
	}{
		{"artist URL", "https://music.apple.com/us/artist/yoasobi/1467327778", "1467327778", true},
		{"song URL (not artist)", "https://music.apple.com/us/song/idol/1480785411", "", false},
		{"non-apple URL", "https://example.com/artist/123", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := m.MatchArtistURL(tt.url)
			if ok != tt.wantOK {
				t.Errorf("MatchArtistURL(%q) ok = %v, want %v", tt.url, ok, tt.wantOK)
			}
			if id != tt.wantID {
				t.Errorf("MatchArtistURL(%q) id = %q, want %q", tt.url, id, tt.wantID)
			}
		})
	}
}

func TestTextMatcher(t *testing.T) {
	m := NewTextMatcher()

	tests := []struct {
		name    string
		text    string
		wantID  string
		wantOK  bool
	}{
		{"am prefix", "am:1480785411", "1480785411", true},
		{"apple prefix", "apple:1480785411", "1480785411", true},
		{"applemusic prefix", "applemusic:1480785411", "1480785411", true},
		{"AM prefix uppercase", "AM:1480785411", "1480785411", true},
		{"embedded URL", "check https://music.apple.com/us/song/idol/1480785411 out", "1480785411", true},
		{"short ID (too few digits)", "am:12345", "", false},
		{"random text", "hello world", "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := m.MatchText(tt.text)
			if ok != tt.wantOK {
				t.Errorf("MatchText(%q) ok = %v, want %v", tt.text, ok, tt.wantOK)
			}
			if id != tt.wantID {
				t.Errorf("MatchText(%q) id = %q, want %q", tt.text, id, tt.wantID)
			}
		})
	}
}

func TestCollectionID(t *testing.T) {
	// Build
	got := buildAlbumCollectionID("1440935467")
	if got != "album:1440935467" {
		t.Errorf("buildAlbumCollectionID = %q, want %q", got, "album:1440935467")
	}

	// Parse album
	isAlbum, id := parseCollectionID("album:1440935467")
	if !isAlbum || id != "1440935467" {
		t.Errorf("parseCollectionID(album:...) = (%v, %q), want (true, 1440935467)", isAlbum, id)
	}

	// Parse non-album
	isAlbum, id = parseCollectionID("pl.abc123")
	if isAlbum || id != "pl.abc123" {
		t.Errorf("parseCollectionID(pl...) = (%v, %q), want (false, pl.abc123)", isAlbum, id)
	}
}

func TestFormatArtworkURL(t *testing.T) {
	artwork := &appleMusicArtwork{URL: "https://example.com/{w}x{h}bb.jpg", Width: 3000, Height: 3000}
	got := formatArtworkURL(artwork, 600)
	want := "https://example.com/600x600bb.jpg"
	if got != want {
		t.Errorf("formatArtworkURL = %q, want %q", got, want)
	}

	// Nil artwork
	got = formatArtworkURL(nil, 600)
	if got != "" {
		t.Errorf("formatArtworkURL(nil) = %q, want empty", got)
	}
}

func TestParseTTMLToLRC(t *testing.T) {
	ttml := `<?xml version="1.0" encoding="UTF-8"?>
<tt xmlns="http://www.w3.org/ns/ttml">
  <body>
    <div>
      <p begin="00:00:15.000" end="00:00:20.000">First line</p>
      <p begin="00:00:25.500" end="00:00:30.000">Second line</p>
      <p begin="01:02:03.456" end="01:02:10.000">Third line</p>
    </div>
  </body>
</tt>`

	result := parseTTMLToLRC(ttml)
	if result == "" {
		t.Fatal("parseTTMLToLRC returned empty")
	}
	// Check first line timestamp
	if !contains(result, "[00:15.00]First line") {
		t.Errorf("expected '[00:15.00]First line' in result:\n%s", result)
	}
	// Check second line
	if !contains(result, "[00:25.50]Second line") {
		t.Errorf("expected '[00:25.50]Second line' in result:\n%s", result)
	}
	// Check hour handling
	if !contains(result, "[62:03.45]Third line") {
		t.Errorf("expected '[62:03.45]Third line' in result:\n%s", result)
	}
}

func TestParseTimeToMillis(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"00:15.000", 15000},
		{"01:30.500", 90500},
		{"1:02:03.456", 3723456},
		{"00:00.000", 0},
	}
	for _, tt := range tests {
		got := parseTimeToMillis(tt.input)
		if got != tt.want {
			t.Errorf("parseTimeToMillis(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeMediaUserToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  token123  ", "token123"},
		{"`token123`", "token123"},
		{`"token123"`, "token123"},
		{"'token123'", "token123"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeMediaUserToken(tt.input)
		if got != tt.want {
			t.Errorf("normalizeMediaUserToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMaskTokenValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"1234", "****"},
		{"123456789012", "1234****9012"},
	}
	for _, tt := range tests {
		got := maskTokenValue(tt.input)
		if got != tt.want {
			t.Errorf("maskTokenValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
