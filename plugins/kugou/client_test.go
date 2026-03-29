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

func TestBuildDownloadPlansPrefersHigherQualities(t *testing.T) {
	song := &model.Song{ID: "11111111111111111111111111111111", Ext: "mp3", Size: 1234, Extra: map[string]string{
		"res_hash":  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sq_hash":   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"hq_hash":   "cccccccccccccccccccccccccccccccc",
		"file_hash": "dddddddddddddddddddddddddddddddd",
	}}
	plans := buildDownloadPlans(song, platform.QualityHiRes)
	if len(plans) < 4 {
		t.Fatalf("plans len=%d", len(plans))
	}
	if plans[0].Quality != platform.QualityHiRes || plans[0].Hash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("first plan = %+v", plans[0])
	}
	if plans[1].Quality != platform.QualityLossless || plans[1].Hash != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("second plan = %+v", plans[1])
	}
}

func TestBuildDownloadPlansUsesConfiguredCheckTrackHash(t *testing.T) {
	if got := normalizeHash(kugouCookieCheckTrackID); got != kugouCookieCheckTrackID {
		t.Fatalf("kugouCookieCheckTrackID=%q not normalized", kugouCookieCheckTrackID)
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

func TestExtractPlaylistIDsFromHTML(t *testing.T) {
	html := `<script>var data={"encode_gic":"gcid_3zsk597iz1tz001","specialid":"546903","global_collection_id":"collection_3_1_2_3"}</script>`
	if got := extractPlaylistGCID(html); got != "gcid_3zsk597iz1tz001" {
		t.Fatalf("extractPlaylistGCID()=%q", got)
	}
	if got := extractPlaylistSpecialID(html); got != "546903" {
		t.Fatalf("extractPlaylistSpecialID()=%q", got)
	}
	if got := extractPlaylistGlobalCollectionID(html); got != "collection_3_1_2_3" {
		t.Fatalf("extractPlaylistGlobalCollectionID()=%q", got)
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

func TestConvertPlaylistSongEntryKeepsExtraFields(t *testing.T) {
	entry := map[string]any{
		"hash":       "11111111111111111111111111111111",
		"name":       "周杰伦 - 青花瓷",
		"timelen":    239000,
		"privilege":  10,
		"mixsongid":  "32218352",
		"audio_id":   339796,
		"albuminfo":  map[string]any{"id": "979856", "name": "我很忙"},
		"singerinfo": []any{map[string]any{"id": "3520", "name": "周杰伦"}},
		"relate_goods": []any{
			map[string]any{"level": "320", "hash": "22222222222222222222222222222222", "size": 9876543},
			map[string]any{"level": "flac", "hash": "33333333333333333333333333333333", "size": 12345678},
		},
	}
	song := convertPlaylistSongEntry(entry)
	if song.Name != "青花瓷" || song.Artist != "周杰伦" {
		t.Fatalf("song meta = %+v", song)
	}
	if song.Extra["mix_song_id"] != "32218352" {
		t.Fatalf("mix_song_id=%q", song.Extra["mix_song_id"])
	}
	if song.Extra["privilege"] != "10" {
		t.Fatalf("privilege=%q", song.Extra["privilege"])
	}
	if song.Extra["hq_hash"] != "22222222222222222222222222222222" {
		t.Fatalf("hq_hash=%q", song.Extra["hq_hash"])
	}
	if song.Extra["sq_hash"] != "33333333333333333333333333333333" {
		t.Fatalf("sq_hash=%q", song.Extra["sq_hash"])
	}
	if song.Extra["320_filesize"] != "9876543" || song.Extra["flac_filesize"] != "12345678" {
		t.Fatalf("filesizes=%v", song.Extra)
	}
	if song.Extra["singer_ids"] != "3520" {
		t.Fatalf("singer_ids=%q", song.Extra["singer_ids"])
	}
}

func TestApplyPlaylistInfoMetadataAndFallback(t *testing.T) {
	playlist := &model.Playlist{Extra: map[string]string{}}
	info := &kugouPlaylistInfoV2Response{}
	info.Data.SpecialName = "周杰伦精选"
	info.Data.ImgURL = "https://img.test/cover.jpg"
	info.Data.Intro = "简介"
	info.Data.Nickname = "酷狗用户"
	info.Data.PlayCount = 99
	info.Data.SongCount = 15
	info.Data.GlobalSpecialID = "collection_3_1_2_3"
	applyPlaylistInfoMetadata(playlist, info)
	if playlist.Name != "周杰伦精选" || playlist.Cover != "https://img.test/cover.jpg" || playlist.Creator != "酷狗用户" {
		t.Fatalf("playlist metadata=%+v", playlist)
	}
	if playlist.Extra["global_specialid"] != "collection_3_1_2_3" {
		t.Fatalf("global_specialid=%q", playlist.Extra["global_specialid"])
	}

	songs := []model.Song{{Name: "青花瓷", Album: "我很忙", Cover: "https://img.test/song.jpg"}}
	empty := &model.Playlist{}
	applyPlaylistSongMetadataFallback(empty, songs)
	if empty.Name != "我很忙" || empty.Cover != "https://img.test/song.jpg" || empty.TrackCount != 1 {
		t.Fatalf("fallback playlist=%+v", empty)
	}
}

func TestApplyPlaylistSongContext(t *testing.T) {
	playlist := &model.Playlist{ID: "collection_3_1_2_3", Name: "周杰伦精选", Link: "https://www.kugou.com/share/zlist.html?global_collection_id=collection_3_1_2_3"}
	songs := []model.Song{{ID: "hash1", Extra: map[string]string{}}, {ID: "hash2"}}
	applyPlaylistSongContext(playlist, songs)
	for _, song := range songs {
		if song.Extra["playlist_id"] != playlist.ID || song.Extra["playlist_url"] != playlist.Link || song.Extra["playlist_name"] != playlist.Name {
			t.Fatalf("song context not applied: %+v", song.Extra)
		}
	}
}

func TestConvertPlaylistSongEntrySplitsArtistAndTitle(t *testing.T) {
	entry := map[string]any{
		"name":      "周杰伦 - 龙战骑士",
		"hash":      "105a2705beee36a8bed029a8e1a6311d",
		"albuminfo": map[string]any{"id": "960399", "name": "魔杰座"},
	}
	song := convertPlaylistSongEntry(entry)
	if song.Name != "龙战骑士" {
		t.Fatalf("song.Name=%q want=%q", song.Name, "龙战骑士")
	}
	if song.Artist != "周杰伦" {
		t.Fatalf("song.Artist=%q want=%q", song.Artist, "周杰伦")
	}
}

func TestFetchAlbumSongsSplitsFilenameIntoTitleAndArtist(t *testing.T) {
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == kugouGatewaySongInfoURL {
			body := `{"status":1,"data":[[{"album_audio_id":"32042818","author_name":"周杰伦","ori_audio_name":"龙战骑士","audio_info":{"audio_id":"154262","hash":"105A2705BEEE36A8BED029A8E1A6311D","hash_128":"105A2705BEEE36A8BED029A8E1A6311D","hash_320":"","hash_flac":"","hash_high":"","filesize":"4397009","filesize_128":"4397009","timelength":"274000","bitrate":"128","extname":"mp3","privilege":"0"},"album_info":{"album_id":"960399","album_name":"魔杰座","sizable_cover":""}}]]}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		if !strings.HasPrefix(req.URL.String(), "http://mobilecdnbj.kugou.com/api/v3/album/song?") {
			if strings.HasPrefix(req.URL.String(), "http://songsearch.kugou.com/song_search_v2?") {
				body := `{"data":{"lists":[{"SongName":"龙战骑士","SingerName":"周杰伦","SingerId":[3520],"AlbumName":"魔杰座","AlbumID":"960399","Audioid":154262,"MixSongID":32042818,"Duration":274,"FileHash":"105A2705BEEE36A8BED029A8E1A6311D","FileSize":"4397009","Privilege":0,"trans_param":{"ogg_320_hash":"","ogg_128_hash":"","singerid":[3520]}}]}}`
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			}
			return nil, fmt.Errorf("unexpected url: %s", req.URL.String())
		}
		body := `{"data":{"total":2,"info":[{"filename":"周杰伦 - 龙战骑士","hash":"105A2705BEEE36A8BED029A8E1A6311D","album_id":"960399","album_audio_id":32042818,"audio_id":154262,"duration":274,"bitrate":128,"extname":"mp3","filesize":4397009,"privilege":0,"img":"","trans_param":{"ogg_320_hash":"","ogg_128_hash":"","singerid":[3520],"union_cover":"","hash_offset":{"clip_hash":""}}},{"filename":"杨瑞代、周杰伦 - 流浪诗人","hash":"FE529C3F9B74DD61EA5BFDEF077AB7CD","album_id":"960399","album_audio_id":32042825,"audio_id":123456,"duration":149,"bitrate":128,"extname":"mp3","filesize":2383879,"privilege":0,"img":"","trans_param":{"ogg_320_hash":"","ogg_128_hash":"","singerid":[8031,3520],"union_cover":"","hash_offset":{"clip_hash":""}}}]}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	defer func() { http.DefaultClient = oldClient }()

	client := NewClient("", nil)
	songs, total, err := client.fetchAlbumSongs(context.Background(), "960399")
	if err != nil {
		t.Fatalf("fetchAlbumSongs() error = %v", err)
	}
	if total != 2 || len(songs) != 2 {
		t.Fatalf("fetchAlbumSongs() total=%d len=%d", total, len(songs))
	}
	if songs[0].Name != "龙战骑士" || songs[0].Artist != "周杰伦" {
		t.Fatalf("song[0]=%+v", songs[0])
	}
	if songs[0].Extra["singer_ids"] != "3520" {
		t.Fatalf("song[0].Extra[singer_ids]=%q", songs[0].Extra["singer_ids"])
	}
	if songs[1].Name != "流浪诗人" || songs[1].Artist != "杨瑞代、周杰伦" {
		t.Fatalf("song[1]=%+v", songs[1])
	}
	if songs[1].Extra["singer_ids"] != "8031,3520" {
		t.Fatalf("song[1].Extra[singer_ids]=%q", songs[1].Extra["singer_ids"])
	}
}

func TestDecodePlaylistGCIDReturnsCollectionAndSpecialIDs(t *testing.T) {
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "/v1/songlist/batch_decode") {
			return nil, fmt.Errorf("unexpected url: %s", req.URL.String())
		}
		body := `{"status":1,"data":{"list":[{"global_collection_id":"collection_3_1667248245_79_0","info":{"specialid":5937849}}]}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	defer func() { http.DefaultClient = oldClient }()

	client := NewClient("", nil)
	identity, err := client.decodePlaylistGCID(context.Background(), "gcid_3zvq18y5z29z08e")
	if err != nil {
		t.Fatalf("decodePlaylistGCID() error = %v", err)
	}
	if identity.GlobalCollectionID != "collection_3_1667248245_79_0" {
		t.Fatalf("GlobalCollectionID=%q", identity.GlobalCollectionID)
	}
	if identity.SpecialID != "5937849" {
		t.Fatalf("SpecialID=%q", identity.SpecialID)
	}
	if identity.GlobalSpecialID != "collection_3_1667248245_79_0" {
		t.Fatalf("GlobalSpecialID=%q", identity.GlobalSpecialID)
	}
}

func TestApplyPlaylistSongContextSetsPlaylistMetadataOnTracks(t *testing.T) {
	playlist := &model.Playlist{ID: "collection_3_1667248245_79_0", Name: "小众高质量歌曲", Link: "https://www.kugou.com/share/zlist.html?global_collection_id=collection_3_1667248245_79_0"}
	songs := []model.Song{{ID: "hash1", Extra: map[string]string{}}, {ID: "hash2", Extra: map[string]string{}}}
	applyPlaylistSongContext(playlist, songs)
	for _, song := range songs {
		if song.Extra["playlist_id"] != playlist.ID || song.Extra["playlist_url"] != playlist.Link || song.Extra["playlist_name"] != playlist.Name {
			t.Fatalf("song context not applied: %+v", song.Extra)
		}
	}
}

func TestMergePlaylistSongsPrefersSupplementalMetadata(t *testing.T) {
	primary := []model.Song{{
		ID:    "23b49d4b5638a0f5bbf530a40f2b225c",
		Name:  "艳",
		Extra: map[string]string{"hash": "23b49d4b5638a0f5bbf530a40f2b225c"},
	}}
	supplemental := []model.Song{{
		ID:      "23b49d4b5638a0f5bbf530a40f2b225c",
		Name:    "艳",
		Artist:  "ONER",
		Album:   "镜象马戏团",
		AlbumID: "58038087",
		Cover:   "https://img.test/cover.jpg",
		Link:    "https://www.kugou.com/song/#hash=23b49d4b5638a0f5bbf530a40f2b225c&album_id=58038087",
		Extra: map[string]string{
			"hash":       "23b49d4b5638a0f5bbf530a40f2b225c",
			"singer_ids": "806266",
		},
	}}

	merged := mergePlaylistSongs(primary, supplemental)
	if len(merged) != 1 {
		t.Fatalf("merged len=%d", len(merged))
	}
	if merged[0].Artist != "ONER" || merged[0].Album != "镜象马戏团" || merged[0].AlbumID != "58038087" {
		t.Fatalf("merged song=%+v", merged[0])
	}
	if merged[0].Extra["singer_ids"] != "806266" {
		t.Fatalf("merged singer_ids=%q", merged[0].Extra["singer_ids"])
	}
}
