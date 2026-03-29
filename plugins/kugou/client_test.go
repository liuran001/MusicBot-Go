package kugou

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guohuiyuan/music-lib/model"
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
		if req.URL.String() == kugouGatewaySongInfoURL {
			body := `{"status":1,"data":[[{"album_audio_id":"294998706","author_name":"花玲、喵酱油、宴宁、Kinsen","ori_audio_name":"让风告诉你","audio_info":{"audio_id":"95107805","hash":"559C36F5F6B29AD0207142B9AF2C89FE","hash_128":"559C36F5F6B29AD0207142B9AF2C89FE","hash_320":"12DD3A2E9BB73E141C55CEB0AD94F370","hash_flac":"45D94DD31FD2944C20AF222C9CC5631F","hash_high":"6C6406145993FFA5BC5C1FB1729BE3FF","filesize":"3631039","filesize_128":"3631039","filesize_320":"9077256","filesize_flac":"28651483","filesize_high":"50172685","timelength":"226899","bitrate":"128","extname":"mp3","privilege":"0"},"album_info":{"album_id":"41668184","album_name":"让风告诉你","sizable_cover":"http://imge.kugou.com/stdmusic/{size}/20210205/20210205170311505744.jpg"}}]]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}
		if strings.HasPrefix(req.URL.String(), "http://songsearch.kugou.com/song_search_v2?") {
			body := `{"data":{"lists":[{"SongName":"让风告诉你","SingerName":"花玲、喵酱油、宴宁、Kinsen","SingerId":[766730,6792161,1078494,2503850],"AlbumName":"让风告诉你","AlbumID":"41668184","Audioid":95107805,"MixSongID":294998706,"Duration":226,"FileHash":"559C36F5F6B29AD0207142B9AF2C89FE","SQFileHash":"45D94DD31FD2944C20AF222C9CC5631F","HQFileHash":"12DD3A2E9BB73E141C55CEB0AD94F370","ResFileHash":"6C6406145993FFA5BC5C1FB1729BE3FF","FileSize":"3631039","SQFileSize":28651483,"HQFileSize":9077256,"ResFileSize":50172685,"Image":"http://imge.kugou.com/stdmusic/{size}/20210205/20210205170311505744.jpg","Privilege":0,"trans_param":{"ogg_320_hash":"","ogg_128_hash":"","singerid":[766730,6792161,1078494,2503850]}}]}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		return nil, fmt.Errorf("unexpected url: %s", req.URL.String())
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
	if song.Extra["singer_ids"] != "766730,6792161,1078494,2503850" {
		t.Fatalf("song.Extra[singer_ids]=%q", song.Extra["singer_ids"])
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

func TestMobilePlayInfoRequiresAuth(t *testing.T) {
	tests := []struct {
		name string
		info *kugouMobilePlayInfoResponse
		want bool
	}{
		{
			name: "pay type requires auth",
			info: &kugouMobilePlayInfoResponse{Error: "需要付费", Privilege: "10", PayType: "3"},
			want: true,
		},
		{
			name: "cookie message requires auth",
			info: &kugouMobilePlayInfoResponse{Error: "cookie required"},
			want: true,
		},
		{
			name: "plain unavailable not auth",
			info: &kugouMobilePlayInfoResponse{Error: "unknown"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mobilePlayInfoRequiresAuth(tt.info); got != tt.want {
				t.Fatalf("mobilePlayInfoRequiresAuth()=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestApplyMobilePlayInfoMetadata(t *testing.T) {
	song := &model.Song{ID: "hash", Extra: map[string]string{}}
	applyMobilePlayInfoMetadata(song, &kugouMobilePlayInfoResponse{
		URL:          "https://cdn.test/song.mp3",
		Bitrate:      "320",
		Timelength:   "239000",
		ExtName:      "mp3",
		SongName:     "青花瓷",
		AuthorName:   "周杰伦",
		AlbumID:      "979856",
		AlbumAudioID: "32218352",
		Privilege:    "10",
		PayType:      "3",
	}, kugouDownloadPlan{Quality: platform.QualityHigh, Format: "mp3"})

	if song.URL != "https://cdn.test/song.mp3" {
		t.Fatalf("song.URL=%q", song.URL)
	}
	if song.Name != "青花瓷" || song.Artist != "周杰伦" {
		t.Fatalf("song meta=%+v", song)
	}
	if song.AlbumID != "979856" {
		t.Fatalf("song.AlbumID=%q", song.AlbumID)
	}
	if song.Duration != 239 || song.Bitrate != 320 {
		t.Fatalf("song duration/bitrate=%d/%d", song.Duration, song.Bitrate)
	}
	if song.Extra["album_audio_id"] != "32218352" || song.Extra["pay_type"] != "3" || song.Extra["privilege"] != "10" {
		t.Fatalf("song.Extra=%v", song.Extra)
	}
	if song.Extra["resolved_quality"] != platform.QualityHigh.String() {
		t.Fatalf("resolved quality=%q", song.Extra["resolved_quality"])
	}
}

func TestResolveShareChainURLExtractsSongMeta(t *testing.T) {
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "www.kugou.com/share/bJ2np35FZV2.html") {
			return nil, fmt.Errorf("unexpected url: %s", req.URL.String())
		}
		body := `<!DOCTYPE html><script>var data=[{"hash":"37A8F50A9EC3B267C3CC6BEC633D9C4A","album_id":"979856","mixsongid":"32218352","encode_album_audio_id":"j6ju8e3"}]</script>`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	defer func() { http.DefaultClient = oldClient }()

	client := NewClient("", nil)
	resolved, err := client.resolveShareChainURL(context.Background(), "bJ2np35FZV2")
	if err != nil {
		t.Fatalf("resolveShareChainURL() error = %v", err)
	}
	want := "https://h5.kugou.com/v2/v-5a15aeb1/index.html?album_audio_id=j6ju8e3&album_id=979856&hash=37a8f50a9ec3b267c3cc6bec633d9c4a"
	if resolved != want {
		t.Fatalf("resolveShareChainURL()=%q want=%q", resolved, want)
	}
}
