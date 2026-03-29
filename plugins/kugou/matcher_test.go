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
		{name: "hash query", url: "https://www.kugou.com/share/song?song=foo&hash=ABCDEF1234567890ABCDEF1234567890", wantID: "abcdef1234567890abcdef1234567890", wantMatch: true},
		{name: "m share chain", url: "https://m.kugou.com/share/song.html?chain=bJ2np35FZV2", wantID: "sharechain:bJ2np35FZV2", wantMatch: true},
		{name: "www share chain", url: "https://www.kugou.com/share/bJ2np35FZV2.html#j6ju8e3", wantID: "sharechain:bJ2np35FZV2", wantMatch: true},
		{name: "wc short path", url: "https://m.kugou.com/wc/s/bRMyd3fFZV2", wantID: "sharechain:bRMyd3fFZV2", wantMatch: true},
		{name: "fragment only hash", url: "https://www.kugou.com/song/#ABCDEF1234567890ABCDEF1234567890", wantID: "abcdef1234567890abcdef1234567890", wantMatch: true},
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
		{name: "playlist query id", url: "https://www.kugou.com/playlist/?specialid=546903", wantID: "546903", wantMatch: true},
		{name: "playlist path variant", url: "https://www.kugou.com/playlist/546903", wantID: "546903", wantMatch: true},
		{name: "songlist playlist", url: "https://www.kugou.com/songlist/gcid_abcd1234/", wantID: "gcid_abcd1234", wantMatch: true},
		{name: "songlist query", url: "https://www.kugou.com/songlist/?gcid=gcid_abcd1234", wantID: "gcid_abcd1234", wantMatch: true},
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
		{name: "prefixed share chain", text: "kugou:bJ2np35FZV2", wantID: "sharechain:bJ2np35FZV2", wantMatch: true},
		{name: "prefixed url", text: "kg:https://www.kugou.com/share/song?song=foo&hash=ABCDEF1234567890ABCDEF1234567890", wantID: "abcdef1234567890abcdef1234567890", wantMatch: true},
		{name: "text with url", text: "分享链接 https://www.kugou.com/share/song?song=foo&hash=ABCDEF1234567890ABCDEF1234567890 快来听", wantID: "abcdef1234567890abcdef1234567890", wantMatch: true},
		{name: "text with share link", text: "分享链接 https://m.kugou.com/share/song.html?chain=bJ2np35FZV2 快来听", wantID: "sharechain:bJ2np35FZV2", wantMatch: true},
		{name: "text with wc short link", text: "分享链接 https://m.kugou.com/wc/s/bRMyd3fFZV2 快来听", wantID: "sharechain:bRMyd3fFZV2", wantMatch: true},
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

func TestShareTrackIDEncoding(t *testing.T) {
	encoded := encodeShareTrackID("bJ2np35FZV2")
	if encoded != "sharechain:bJ2np35FZV2" {
		t.Fatalf("encodeShareTrackID()=%q", encoded)
	}
	chain, ok := decodeShareTrackID(encoded)
	if !ok || chain != "bJ2np35FZV2" {
		t.Fatalf("decodeShareTrackID()=(%q,%v)", chain, ok)
	}
}
