package spotify

import "testing"

func TestMatchURL(t *testing.T) {
	m := NewURLMatcher()
	cases := []struct {
		url     string
		want    string
		matched bool
	}{
		{"https://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6", "6rqhFgbbKwnb9MLmUQDhG6", true},
		{"https://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6?si=abc123", "6rqhFgbbKwnb9MLmUQDhG6", true},
		{"http://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6", "6rqhFgbbKwnb9MLmUQDhG6", true},
		{"open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6", "6rqhFgbbKwnb9MLmUQDhG6", true},
		{"https://open.spotify.com/intl-de/track/6rqhFgbbKwnb9MLmUQDhG6", "6rqhFgbbKwnb9MLmUQDhG6", true},
		// non-track URLs are not track matches
		{"https://open.spotify.com/album/1DFixLWuPkv3KT3TnV35m3", "", false},
		{"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M", "", false},
		{"https://example.com/track/6rqhFgbbKwnb9MLmUQDhG6", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := m.MatchURL(c.url)
		if ok != c.matched || got != c.want {
			t.Errorf("MatchURL(%q) = (%q,%v), want (%q,%v)", c.url, got, ok, c.want, c.matched)
		}
	}
}

func TestMatchPlaylistURL(t *testing.T) {
	m := NewURLMatcher()
	cases := []struct {
		url     string
		want    string
		matched bool
	}{
		{"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M", "playlist:37i9dQZF1DXcBWIGoYBM5M", true},
		{"https://open.spotify.com/album/1DFixLWuPkv3KT3TnV35m3", "album:1DFixLWuPkv3KT3TnV35m3", true},
		{"https://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6", "", false},
	}
	for _, c := range cases {
		got, ok := m.MatchPlaylistURL(c.url)
		if ok != c.matched || got != c.want {
			t.Errorf("MatchPlaylistURL(%q) = (%q,%v), want (%q,%v)", c.url, got, ok, c.want, c.matched)
		}
	}
}

func TestMatchText(t *testing.T) {
	m := NewTextMatcher()
	cases := []struct {
		text    string
		want    string
		matched bool
	}{
		{"spotify:track:6rqhFgbbKwnb9MLmUQDhG6", "6rqhFgbbKwnb9MLmUQDhG6", true},
		{"sp:6rqhFgbbKwnb9MLmUQDhG6", "6rqhFgbbKwnb9MLmUQDhG6", true},
		{"check this https://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6 out", "6rqhFgbbKwnb9MLmUQDhG6", true},
		{"6rqhFgbbKwnb9MLmUQDhG6", "6rqhFgbbKwnb9MLmUQDhG6", true},
		{"just some text", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := m.MatchText(c.text)
		if ok != c.matched || got != c.want {
			t.Errorf("MatchText(%q) = (%q,%v), want (%q,%v)", c.text, got, ok, c.want, c.matched)
		}
	}
}
