package spotify

import (
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestSpotifyQualityForBitrate(t *testing.T) {
	cases := []struct {
		bitrate int
		want    platform.Quality
	}{
		{0, platform.QualityStandard},
		{128, platform.QualityStandard},
		{256, platform.QualityHigh},
	}
	for _, tc := range cases {
		if got := spotifyQualityForBitrate(tc.bitrate); got != tc.want {
			t.Fatalf("spotifyQualityForBitrate(%d) = %s, want %s", tc.bitrate, got, tc.want)
		}
	}
}
