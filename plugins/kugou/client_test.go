package kugou

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestFormatKugouIDList(t *testing.T) {
	got := formatKugouIDList("[766730, 6792161,1078494,2503850]")
	want := "766730,6792161,1078494,2503850"
	if got != want {
		t.Fatalf("formatKugouIDList()=%q want=%q", got, want)
	}
}

func TestHasVIPDownloadCookie(t *testing.T) {
	tests := []struct {
		name   string
		cookie string
		want   bool
	}{
		{name: "missing all", cookie: "", want: false},
		{name: "missing kugoo id", cookie: "t=token-only", want: false},
		{name: "missing token", cookie: "KugooID=12345", want: false},
		{name: "has required keys", cookie: "foo=bar; t=token123; KugooID=12345; mid=abc", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{cookie: tt.cookie}
			if got := client.HasVIPDownloadCookie(); got != tt.want {
				t.Fatalf("HasVIPDownloadCookie()=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestWrapErrorMappings(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "rate limited", err: errors.New("kugou song info unavailable, errcode=1002"), want: platform.ErrRateLimited},
		{name: "auth required by cookie fields", err: errors.New("kugou songinfo v2 requires cookie t and KugooID"), want: platform.ErrAuthRequired},
		{name: "auth required by cookie required", err: errors.New("cookie required for kugou vip download"), want: platform.ErrAuthRequired},
		{name: "not found", err: errors.New("invalid hash"), want: platform.ErrNotFound},
		{name: "unavailable", err: errors.New("download url not found"), want: platform.ErrUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapError("kugou", "track", "abc", tt.err)
			if !errors.Is(got, tt.want) {
				t.Fatalf("wrapError()=%v want errors.Is(..., %v)", got, tt.want)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchGatewayTrackInfoPreservesAlbumAndLink(t *testing.T) {
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != kugouGatewaySongInfoURL {
			t.Fatalf("unexpected url: %s", req.URL.String())
		}
		body := `{"status":1,"data":[[{"album_audio_id":"294998706","author_name":"花玲、喵酱油、宴宁、Kinsen","ori_audio_name":"让风告诉你","audio_info":{"audio_id":"95107805","hash":"559C36F5F6B29AD0207142B9AF2C89FE","hash_128":"559C36F5F6B29AD0207142B9AF2C89FE","hash_320":"12DD3A2E9BB73E141C55CEB0AD94F370","hash_flac":"45D94DD31FD2944C20AF222C9CC5631F","hash_high":"6C6406145993FFA5BC5C1FB1729BE3FF","filesize":"3631039","filesize_128":"3631039","filesize_320":"9077256","filesize_flac":"28651483","filesize_high":"50172685","timelength":"226899","bitrate":"128","extname":"mp3","privilege":"0"},"album_info":{"album_id":"41668184","album_name":"让风告诉你","sizable_cover":"http://imge.kugou.com/stdmusic/{size}/20210205/20210205170311505744.jpg"}}]]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	defer func() { http.DefaultClient = oldClient }()

	client := NewClient("", nil)
	song, err := client.fetchGatewayTrackInfo(context.Background(), "559c36f5f6b29ad0207142b9af2c89fe")
	if err != nil {
		t.Fatalf("fetchGatewayTrackInfo() error = %v", err)
	}
	if song == nil {
		t.Fatal("fetchGatewayTrackInfo() returned nil song")
	}
	if song.Album != "让风告诉你" {
		t.Fatalf("song.Album=%q want %q", song.Album, "让风告诉你")
	}
	if song.AlbumID != "41668184" {
		t.Fatalf("song.AlbumID=%q want %q", song.AlbumID, "41668184")
	}
	wantLink := "https://www.kugou.com/song/#hash=559c36f5f6b29ad0207142b9af2c89fe&album_id=41668184"
	if song.Link != wantLink {
		t.Fatalf("song.Link=%q want %q", song.Link, wantLink)
	}
	if song.Size != 3631039 {
		t.Fatalf("song.Size=%d want 3631039", song.Size)
	}
	if song.Duration != 226 {
		t.Fatalf("song.Duration=%d want 226", song.Duration)
	}
	if song.Bitrate != 128 {
		t.Fatalf("song.Bitrate=%d want 128", song.Bitrate)
	}
	if song.Extra["album_audio_id"] != "294998706" {
		t.Fatalf("song.Extra[album_audio_id]=%q", song.Extra["album_audio_id"])
	}
	if song.Extra["album_id"] != "41668184" {
		t.Fatalf("song.Extra[album_id]=%q", song.Extra["album_id"])
	}
	if song.Extra["res_hash"] != "6c6406145993ffa5bc5c1fb1729be3ff" {
		t.Fatalf("song.Extra[res_hash]=%q", song.Extra["res_hash"])
	}
	if song.Cover == "" || !strings.Contains(song.Cover, "/480/") {
		t.Fatalf("song.Cover=%q want size-normalized cover", song.Cover)
	}
}
