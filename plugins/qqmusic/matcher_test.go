package qqmusic

import "testing"

func TestURLMatcherMatchPlaylistURL(t *testing.T) {
	matcher := NewURLMatcher()
	tests := []struct {
		name      string
		url       string
		wantID    string
		wantMatch bool
	}{
		{
			name:      "qq playlist url",
			url:       "https://y.qq.com/n/ryqq_v2/playlist/114514",
			wantID:    "114514",
			wantMatch: true,
		},
		{
			name:      "qq album url",
			url:       "https://y.qq.com/n/ryqq_v2/albumDetail/11078709?ADTAG=h5_share_album&redirecttag=mn.redirect.custom&mnst=1.18",
			wantID:    "album:11078709",
			wantMatch: true,
		},
		{
			name:      "qq album legacy url",
			url:       "https://y.qq.com/n/yqq/album/001CMlm52RlccK.html",
			wantID:    "album:001CMlm52RlccK",
			wantMatch: true,
		},
		{
			name:      "qq short link should be resolved upstream",
			url:       "https://c6.y.qq.com/base/fcgi-bin/u?__=4jkpgtWx6rlG",
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "qq short link resolved album url",
			url:       "https://i.y.qq.com/n2/m/share/details/album.html?ADTAG=pc_v17&albumId=11078709&channelId=10036163",
			wantID:    "album:11078709",
			wantMatch: true,
		},
		{
			name:      "non qq domain",
			url:       "https://music.163.com/playlist?id=12345",
			wantID:    "",
			wantMatch: false,
		},
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

func TestURLMatcherMatchURLDoesNotMatchAlbum(t *testing.T) {
	matcher := NewURLMatcher()
	if _, matched := matcher.MatchURL("https://y.qq.com/n/ryqq_v2/albumDetail/11078709"); matched {
		t.Fatalf("MatchURL() should not match album URLs")
	}
}
