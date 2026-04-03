package soda

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestURLMatcherMatchURL(t *testing.T) {
	matcher := NewURLMatcher()
	tests := []struct {
		url      string
		wantID   string
		wantBool bool
	}{
		{"https://www.qishui.com/track/123456789", "123456789", true},
		{"https://music.douyin.com/qishui/share/track?track_id=987654321", "987654321", true},
		{"https://qishui.douyin.com/abc123?track_id=123456780", "123456780", true},
		{"https://www.qishui.com/#/track?id=567890123", "567890123", true},
		{"https://bubble.qishui.com/song/2468101214", "2468101214", true},
		{"https://www.qishui.com/playlist/123456789", "", false},
		{"https://music.douyin.com/qishui/share/artist?artist_id=423456789", "", false},
		{"https://fake-qishui.com/track/123456789", "", false},
	}
	for _, tt := range tests {
		gotID, gotOK := matcher.MatchURL(tt.url)
		if gotID != tt.wantID || gotOK != tt.wantBool {
			t.Fatalf("MatchURL(%q) = (%q,%v), want (%q,%v)", tt.url, gotID, gotOK, tt.wantID, tt.wantBool)
		}
	}
}

func TestURLMatcherMatchPlaylistURL(t *testing.T) {
	matcher := NewURLMatcher()
	tests := []struct {
		url    string
		wantID string
		ok     bool
	}{
		{"https://www.qishui.com/playlist/123456789", "123456789", true},
		{"https://www.qishui.com/#/playlist?id=222333444", "222333444", true},
		{"https://music.douyin.com/qishui/share/playlist?playlist_id=666777888", "666777888", true},
		{"https://music.douyin.com/qishui/share/album?album_id=777888999", "album:777888999", true},
		{"https://qishui.douyin.com/share?album_id=777888111", "album:777888111", true},
		{"https://www.qishui.com/#/album?id=111222333", "album:111222333", true},
	}
	for _, tt := range tests {
		gotID, gotOK := matcher.MatchPlaylistURL(tt.url)
		if gotID != tt.wantID || gotOK != tt.ok {
			t.Fatalf("MatchPlaylistURL(%q) = (%q,%v), want (%q,%v)", tt.url, gotID, gotOK, tt.wantID, tt.ok)
		}
	}
}

func TestURLMatcherMatchArtistURL(t *testing.T) {
	matcher := NewURLMatcher()
	tests := []struct {
		url    string
		wantID string
		ok     bool
	}{
		{"https://music.douyin.com/qishui/share/artist?artist_id=423456789", "423456789", true},
		{"https://qishui.douyin.com/share?artist_id=523456789", "523456789", true},
		{"https://www.qishui.com/#/artist?id=623456789", "623456789", true},
		{"https://www.qishui.com/artist/723456789", "723456789", true},
		{"https://www.qishui.com/track/123456789", "", false},
	}
	for _, tt := range tests {
		gotID, gotOK := matcher.MatchArtistURL(tt.url)
		if gotID != tt.wantID || gotOK != tt.ok {
			t.Fatalf("MatchArtistURL(%q) = (%q,%v), want (%q,%v)", tt.url, gotID, gotOK, tt.wantID, tt.ok)
		}
	}
}

func TestCollectionIDRoundTrip(t *testing.T) {
	encoded := encodeAlbumCollectionID("123456789")
	if encoded != "album:123456789" {
		t.Fatalf("encodeAlbumCollectionID() = %q", encoded)
	}
	isAlbum, raw := parseCollectionID(encoded)
	if !isAlbum || raw != "123456789" {
		t.Fatalf("parseCollectionID() = (%v,%q)", isAlbum, raw)
	}
}

func TestTextMatcherMatchText(t *testing.T) {
	matcher := NewTextMatcher()
	tests := []struct {
		text   string
		wantID string
		ok     bool
	}{
		{"soda:123456789", "123456789", true},
		{"qs:123456780", "123456780", true},
		{"汽水:987654321", "987654321", true},
		{"分享 https://www.qishui.com/track/24681012 给你", "24681012", true},
		{"1234567", "", false},
	}
	for _, tt := range tests {
		gotID, gotOK := matcher.MatchText(tt.text)
		if gotID != tt.wantID || gotOK != tt.ok {
			t.Fatalf("MatchText(%q) = (%q,%v), want (%q,%v)", tt.text, gotID, gotOK, tt.wantID, tt.ok)
		}
	}
}

func TestSodaMetadataIncludesQSShortcutAlias(t *testing.T) {
	aliases := NewPlatform(nil).Metadata().Aliases
	joined := strings.Join(aliases, ",")
	if !strings.Contains(joined, "qs") {
		t.Fatalf("Metadata().Aliases missing qs: %v", aliases)
	}
}

func TestShortLinkHostsIncludeRealSodaShortDomain(t *testing.T) {
	plat := NewPlatform(nil)
	hosts := plat.ShortLinkHosts()
	joined := strings.Join(hosts, ",")
	if !strings.Contains(joined, "qishui.douyin.com") {
		t.Fatalf("ShortLinkHosts() missing qishui.douyin.com: %v", hosts)
	}
}

func TestParseSodaLyric(t *testing.T) {
	raw := "[1234,1000]<0,500,0>你<500,500,0>好"
	got := parseSodaLyric(raw)
	want := "[00:01.23]你好"
	if got != want {
		t.Fatalf("parseSodaLyric() = %q, want %q", got, want)
	}
}

func TestBuildSodaShareURLs(t *testing.T) {
	if got := buildSodaTrackURL("123456789"); got != "https://music.douyin.com/qishui/share/track?track_id=123456789" {
		t.Fatalf("buildSodaTrackURL() = %q", got)
	}
	if got := buildSodaPlaylistURL("223456789"); got != "https://music.douyin.com/qishui/share/playlist?playlist_id=223456789" {
		t.Fatalf("buildSodaPlaylistURL() = %q", got)
	}
	if got := buildSodaAlbumURL("323456789"); got != "https://music.douyin.com/qishui/share/album?album_id=323456789" {
		t.Fatalf("buildSodaAlbumURL() = %q", got)
	}
	if got := buildSodaArtistURL("423456789"); got != "https://music.douyin.com/qishui/share/artist?artist_id=423456789" {
		t.Fatalf("buildSodaArtistURL() = %q", got)
	}
}

func TestConvertSodaTrackIncludesArtistURL(t *testing.T) {
	track := convertSodaTrack(sodaTrack{
		ID:       "123456789",
		Name:     "Track",
		Duration: 180000,
		Artists: []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		}{
			{Name: "Artist A", ID: "423456789"},
		},
		Album: struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			URLCover struct {
				URLs []string `json:"urls"`
				URI  string   `json:"uri"`
			} `json:"url_cover"`
		}{
			ID:   "323456789",
			Name: "Album",
		},
	})
	if len(track.Artists) != 1 || track.Artists[0].URL == "" {
		t.Fatalf("convertSodaTrack() artist url missing: %+v", track.Artists)
	}
	if track.Artists[0].URL != "https://music.douyin.com/qishui/share/artist?artist_id=423456789" {
		t.Fatalf("convertSodaTrack() artist url = %q", track.Artists[0].URL)
	}
}

func TestConvertSodaAlbumIncludesArtistURL(t *testing.T) {
	album := convertSodaAlbum(sodaAlbumMeta{
		ID:   "323456789",
		Name: "Album",
		Artists: []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		}{
			{Name: "Artist A", ID: "423456789"},
		},
	})
	if album == nil || len(album.Artists) != 1 || album.Artists[0].URL == "" {
		t.Fatalf("convertSodaAlbum() artist url missing: %+v", album)
	}
	if album.Artists[0].URL != "https://music.douyin.com/qishui/share/artist?artist_id=423456789" {
		t.Fatalf("convertSodaAlbum() artist url = %q", album.Artists[0].URL)
	}
}

func TestConvertSodaPlaylistTrackCountFallback(t *testing.T) {
	pl := convertSodaPlaylist(sodaPlaylistMeta{
		ID:         "123",
		TrackCount: 18,
	})
	if pl == nil {
		t.Fatal("convertSodaPlaylist() returned nil")
	}
	if pl.TrackCount != 18 {
		t.Fatalf("convertSodaPlaylist() track count = %d", pl.TrackCount)
	}
	if pl.Title != "123" {
		t.Fatalf("convertSodaPlaylist() title = %q", pl.Title)
	}
	if pl.Creator != "汽水音乐" {
		t.Fatalf("convertSodaPlaylist() creator = %q", pl.Creator)
	}
}

func TestAlbumCollectionPlaylistUsesAlbumDescription(t *testing.T) {
	page := `<html><script>window._ROUTER_DATA = {"loaderData":{"album_page":{"albumInfo":{"id":"123","name":"Album","intro":"真实专辑简介","track_count":8,"artists":[{"id":"a1","name":"Artist A"}]},"trackList":[{"id":"t1","name":"Track 1"}]}}}</script></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/qishui/share/album" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("album_id"); got != "123" {
			t.Fatalf("album_id = %q", got)
		}
		_, _ = fmt.Fprint(w, page)
	}))
	defer server.Close()

	s := &SodaPlatform{client: newSodaTestClient(server.URL)}
	ctx := platform.WithPlaylistLimit(context.Background(), 1)
	playlist, err := s.GetPlaylist(ctx, encodeAlbumCollectionID("123"))
	if err != nil {
		t.Fatalf("GetPlaylist() error = %v", err)
	}
	if playlist == nil {
		t.Fatal("GetPlaylist() returned nil")
	}
	if playlist.Description != "真实专辑简介" {
		t.Fatalf("GetPlaylist() description = %q", playlist.Description)
	}
	if playlist.TrackCount != 8 {
		t.Fatalf("GetPlaylist() track count = %d", playlist.TrackCount)
	}
	if playlist.Creator != "Artist A" {
		t.Fatalf("GetPlaylist() creator = %q", playlist.Creator)
	}
}

func TestConvertSodaArtist(t *testing.T) {
	artist, trackCount := convertSodaArtist(sodaArtistMeta{
		ID:         "423456789",
		Name:       "Artist A",
		TrackCount: 12,
		Avatar: struct {
			URLs []string `json:"urls"`
			URI  string   `json:"uri"`
		}{
			URLs: []string{"https://p3.qishui.com/img/"},
			URI:  "avatar123",
		},
	})
	if artist == nil {
		t.Fatalf("convertSodaArtist() returned nil")
	}
	if artist.URL != "https://music.douyin.com/qishui/share/artist?artist_id=423456789" {
		t.Fatalf("convertSodaArtist() url = %q", artist.URL)
	}
	if artist.AvatarURL == "" {
		t.Fatalf("convertSodaArtist() avatar missing")
	}
	if trackCount != 12 {
		t.Fatalf("convertSodaArtist() trackCount = %d", trackCount)
	}
}
