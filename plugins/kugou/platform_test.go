package kugou

import (
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestCollectionIDHelpers(t *testing.T) {
	if got := encodeAlbumCollectionID("979856"); got != "album:979856" {
		t.Fatalf("encodeAlbumCollectionID()=%q", got)
	}
	if got := encodePlaylistURLCollectionID("https://www.kugou.com/share/zlist.html?id=1"); got != "playlisturl:https://www.kugou.com/share/zlist.html?id=1" {
		t.Fatalf("encodePlaylistURLCollectionID()=%q", got)
	}
	kind, id := parseCollectionID("album:979856")
	if kind != "album" || id != "979856" {
		t.Fatalf("parseCollectionID(album)=(%q,%q)", kind, id)
	}
	kind, id = parseCollectionID("playlisturl:https://www.kugou.com/share/zlist.html?id=1")
	if kind != "playlist_url" || id != "https://www.kugou.com/share/zlist.html?id=1" {
		t.Fatalf("parseCollectionID(playlist_url)=(%q,%q)", kind, id)
	}
	if !isGlobalCollectionID("collection_3_1_2_3") {
		t.Fatal("expected global collection id to be recognized")
	}
	if isGlobalCollectionID("546903") {
		t.Fatal("numeric special id should not be treated as global collection id")
	}
	if got := buildPlaylistLink("collection_3_1_2_3"); got != "https://www.kugou.com/share/zlist.html?global_collection_id=collection_3_1_2_3" {
		t.Fatalf("buildPlaylistLink(global collection)=%q", got)
	}
}

func TestName(t *testing.T) {
	p := &KugouPlatform{}
	if name := p.Name(); name != "kugou" {
		t.Fatalf("expected name kugou, got %s", name)
	}
}

func TestCapabilities(t *testing.T) {
	p := &KugouPlatform{}
	if !p.SupportsDownload() || !p.SupportsSearch() || !p.SupportsLyrics() {
		t.Fatal("expected kugou platform capabilities enabled")
	}
	if p.SupportsRecognition() {
		t.Fatal("expected kugou recognition disabled")
	}
}

func TestQualityFromSong(t *testing.T) {
	tests := []struct {
		name    string
		bitrate int
		ext     string
		want    platform.Quality
	}{
		{name: "standard", bitrate: 128, ext: "mp3", want: platform.QualityStandard},
		{name: "high", bitrate: 320, ext: "mp3", want: platform.QualityHigh},
		{name: "lossless by ext", bitrate: 999, ext: "flac", want: platform.QualityLossless},
		{name: "hires by bitrate", bitrate: 2400, ext: "flac", want: platform.QualityHiRes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qualityFromSong(tt.bitrate, tt.ext); got != tt.want {
				t.Fatalf("qualityFromSong()=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestInferTrackID(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		link  string
		extra map[string]string
		want  string
	}{
		{
			name:  "prefer extra hash",
			id:    "raw-id",
			link:  "https://www.kugou.com/song/#hash=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			extra: map[string]string{"sq_hash": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "hash": "cccccccccccccccccccccccccccccccc"},
			want:  "cccccccccccccccccccccccccccccccc",
		},
		{
			name:  "fallback to link hash",
			id:    "raw-id",
			link:  "https://www.kugou.com/song/#hash=dddddddddddddddddddddddddddddddd",
			extra: nil,
			want:  "dddddddddddddddddddddddddddddddd",
		},
		{
			name:  "fallback to normalized id",
			id:    "EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE",
			link:  "",
			extra: nil,
			want:  "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		},
		{
			name:  "fallback to raw id",
			id:    "not-a-hash",
			link:  "",
			extra: nil,
			want:  "not-a-hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inferTrackID(tt.id, tt.link, tt.extra); got != tt.want {
				t.Fatalf("inferTrackID()=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestCollectCandidateURLs(t *testing.T) {
	urls := collectCandidateURLs("https://a.example/test.flac", map[string]string{
		"play_backup_url": "https://b.example/test.flac",
		"play_url":        "https://a.example/test.flac",
	})
	if len(urls) != 2 {
		t.Fatalf("collectCandidateURLs len=%d want=2", len(urls))
	}
	if urls[0] != "https://a.example/test.flac" || urls[1] != "https://b.example/test.flac" {
		t.Fatalf("collectCandidateURLs=%v", urls)
	}
}

func TestNormalizeRequestedQuality(t *testing.T) {
	if got := normalizeRequestedQuality(platform.QualityHiRes); got != platform.QualityHiRes {
		t.Fatalf("normalizeRequestedQuality(hires)=%v", got)
	}
	if got := normalizeRequestedQuality(platform.Quality(-100)); got != platform.QualityHigh {
		t.Fatalf("normalizeRequestedQuality(invalid)=%v want=%v", got, platform.QualityHigh)
	}
}

func TestBuildTrackURLPrefersHashAlbumPage(t *testing.T) {
	got := buildTrackURL(
		"abcdef1234567890abcdef1234567890",
		"41668184",
		"",
		map[string]string{"mix_song_id": "294998706"},
	)
	want := "https://www.kugou.com/song/#hash=abcdef1234567890abcdef1234567890&album_id=41668184"
	if got != want {
		t.Fatalf("buildTrackURL()=%q want=%q", got, want)
	}
}

func TestBuildTrackURLPrefersShareChain(t *testing.T) {
	got := buildTrackURL(
		"abcdef1234567890abcdef1234567890",
		"41668184",
		"",
		map[string]string{"share_chain": "bJ2np35FZV2"},
	)
	want := "https://www.kugou.com/share/bJ2np35FZV2.html"
	if got != want {
		t.Fatalf("buildTrackURL()=%q want=%q", got, want)
	}
}

func TestBuildTrackURLFallsBackToH5Link(t *testing.T) {
	got := buildTrackURL(
		"abcdef1234567890abcdef1234567890",
		"41668184",
		"",
		map[string]string{"album_audio_id": "32218352"},
	)
	want := "https://www.kugou.com/song/#hash=abcdef1234567890abcdef1234567890&album_id=41668184"
	if got != want {
		t.Fatalf("buildTrackURL()=%q want=%q", got, want)
	}
}

func TestSplitArtistsBuildsArtistURLs(t *testing.T) {
	artists := splitArtists("花玲、喵酱油、宴宁、Kinsen", map[string]string{"singer_ids": "766730,6792161,1078494,2503850"})
	if len(artists) != 4 {
		t.Fatalf("splitArtists len=%d want=4", len(artists))
	}
	if artists[0].URL != "https://www.kugou.com/singer/766730.html" {
		t.Fatalf("artists[0].URL=%q", artists[0].URL)
	}
	if artists[3].URL != "https://www.kugou.com/singer/2503850.html" {
		t.Fatalf("artists[3].URL=%q", artists[3].URL)
	}
}

func TestImplementsInterface(t *testing.T) {
	var _ platform.Platform = (*KugouPlatform)(nil)
}
