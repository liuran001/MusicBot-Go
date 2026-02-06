package qqmusic

import "testing"

func TestBuildTrackCoverURLPrefersAlbumCover(t *testing.T) {
	got := buildTrackCoverURL("001CMlm52RlccK")
	want := "https://y.gtimg.cn/music/photo_new/T002M000001CMlm52RlccK.jpg"
	if got != want {
		t.Fatalf("buildTrackCoverURL() = %q, want %q", got, want)
	}
}

func TestBuildTrackCoverURLEmptyWhenAlbumMissing(t *testing.T) {
	got := buildTrackCoverURL("")
	want := ""
	if got != want {
		t.Fatalf("buildTrackCoverURL() = %q, want %q", got, want)
	}
}

func TestBuildSongCoverURL(t *testing.T) {
	got := buildSongCoverURL("0037PjBY3tjPVk")
	want := "https://y.qq.com/music/photo_new/T062M0000037PjBY3tjPVk.jpg"
	if got != want {
		t.Fatalf("buildSongCoverURL() = %q, want %q", got, want)
	}
}

func TestConvertSongDetailLeavesCoverEmptyWhenAlbumMissing(t *testing.T) {
	track := convertSongDetail(&qqSongDetail{
		ID:   107196623,
		Mid:  "001DRnxC0twkty",
		Name: "Lifeline",
		Singer: []qqSinger{
			{Mid: "001qeO0Y2fEZN7", Name: "Zeraphym"},
		},
		Album: qqAlbum{},
	})

	wantCover := ""
	if track.CoverURL != wantCover {
		t.Fatalf("track.CoverURL = %q, want %q", track.CoverURL, wantCover)
	}
	if track.Album != nil {
		t.Fatalf("track.Album should be nil when album info missing")
	}
}
