package kugou

import "testing"

func TestURLMatcherMatchURL(t *testing.T) {
	matcher := NewURLMatcher()
	tests := []struct {
		name      string
		url       string
		wantID    string
		wantMatch bool
	}{
		{name: "song link", url: "https://www.kugou.com/song/#hash=ABCDEF1234567890ABCDEF1234567890", wantID: "abcdef1234567890abcdef1234567890", wantMatch: true},
		{name: "playlist url not song", url: "https://www.kugou.com/yy/special/single/546903.html", wantID: "", wantMatch: false},
		{name: "non kugou", url: "https://music.163.com/song?id=12345", wantID: "", wantMatch: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotMatch := matcher.MatchURL(tt.url)
			if gotMatch != tt.wantMatch {
				t.Fatalf("MatchURL() matched=%v, want=%v", gotMatch, tt.wantMatch)
			}
			if gotID != tt.wantID {
				t.Fatalf("MatchURL() id=%q, want=%q", gotID, tt.wantID)
			}
		})
	}
}

func TestURLMatcherMatchPlaylistURL(t *testing.T) {
	matcher := NewURLMatcher()
	tests := []struct {
		name      string
		url       string
		wantID    string
		wantMatch bool
	}{
		{name: "special playlist", url: "https://www.kugou.com/yy/special/single/546903.html", wantID: "546903", wantMatch: true},
		{name: "songlist playlist", url: "https://www.kugou.com/songlist/gcid_abcd1234/", wantID: "gcid_abcd1234", wantMatch: true},
		{name: "song url", url: "https://www.kugou.com/song/#hash=abcdef1234567890abcdef1234567890", wantID: "", wantMatch: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotMatch := matcher.MatchPlaylistURL(tt.url)
			if gotMatch != tt.wantMatch {
				t.Fatalf("MatchPlaylistURL() matched=%v, want=%v", gotMatch, tt.wantMatch)
			}
			if gotID != tt.wantID {
				t.Fatalf("MatchPlaylistURL() id=%q, want=%q", gotID, tt.wantID)
			}
		})
	}
}

func TestTextMatcherMatchText(t *testing.T) {
	matcher := NewTextMatcher()
	tests := []struct {
		name      string
		text      string
		wantID    string
		wantMatch bool
	}{
		{name: "raw hash", text: "ABCDEF1234567890ABCDEF1234567890", wantID: "abcdef1234567890abcdef1234567890", wantMatch: true},
		{name: "prefixed hash", text: "kugou:abcdef1234567890abcdef1234567890", wantID: "abcdef1234567890abcdef1234567890", wantMatch: true},
		{name: "non hash", text: "jay chou", wantID: "", wantMatch: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotMatch := matcher.MatchText(tt.text)
			if gotMatch != tt.wantMatch {
				t.Fatalf("MatchText() matched=%v, want=%v", gotMatch, tt.wantMatch)
			}
			if gotID != tt.wantID {
				t.Fatalf("MatchText() id=%q, want=%q", gotID, tt.wantID)
			}
		})
	}
}
