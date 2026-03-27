package kugou

import (
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

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

func TestImplementsInterface(t *testing.T) {
	var _ platform.Platform = (*KugouPlatform)(nil)
}
