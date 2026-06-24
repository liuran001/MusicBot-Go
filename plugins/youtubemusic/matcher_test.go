package youtubemusic

import "testing"

func TestMatchURL(t *testing.T) {
	m := NewURLMatcher()
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"music watch", "https://music.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"www watch", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"m watch", "https://m.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"youtu.be", "https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"watch with list", "https://music.youtube.com/watch?v=dQw4w9WgXcQ&list=RDABC", "dQw4w9WgXcQ", true},
		{"shorts", "https://www.youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"embed", "https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"no scheme", "music.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"bad host", "https://vimeo.com/watch?v=dQw4w9WgXcQ", "", false},
		{"playlist not track", "https://music.youtube.com/playlist?list=PLabcdefghijk", "", false},
		{"short id rejected", "https://youtu.be/short", "", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := m.MatchURL(c.in)
			if got != c.want || ok != c.ok {
				t.Fatalf("MatchURL(%q) = (%q,%v) want (%q,%v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestMatchPlaylistURL(t *testing.T) {
	m := NewURLMatcher()
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"music playlist", "https://music.youtube.com/playlist?list=PLabcdefghijk", "PLabcdefghijk", true},
		{"www playlist", "https://www.youtube.com/playlist?list=OLAK5uy_abcdefghij", "OLAK5uy_abcdefghij", true},
		{"watch with list is not playlist", "https://music.youtube.com/watch?v=dQw4w9WgXcQ&list=PLabcdefghijk", "", false},
		{"empty list", "https://music.youtube.com/playlist?list=", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := m.MatchPlaylistURL(c.in)
			if got != c.want || ok != c.ok {
				t.Fatalf("MatchPlaylistURL(%q) = (%q,%v) want (%q,%v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestMatchText(t *testing.T) {
	m := NewTextMatcher()
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"bare id", "dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"ytm prefix", "ytm:dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"youtube prefix", "youtube:dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"embedded url", "check this https://youtu.be/dQw4w9WgXcQ out", "dQw4w9WgXcQ", true},
		{"url trailing punct", "https://youtu.be/dQw4w9WgXcQ.", "dQw4w9WgXcQ", true},
		{"random text", "just some words", "", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := m.MatchText(c.in)
			if got != c.want || ok != c.ok {
				t.Fatalf("MatchText(%q) = (%q,%v) want (%q,%v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}
