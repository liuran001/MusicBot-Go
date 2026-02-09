package netease

import (
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// TestName verifies the platform name is "netease".
func TestName(t *testing.T) {
	p := &NeteasePlatform{}
	if name := p.Name(); name != "netease" {
		t.Errorf("expected name 'netease', got '%s'", name)
	}
}

// TestCapabilities verifies all capability methods return correct values.
func TestCapabilities(t *testing.T) {
	p := &NeteasePlatform{}

	tests := []struct {
		name     string
		check    func() bool
		expected bool
	}{
		{"SupportsDownload", p.SupportsDownload, true},
		{"SupportsSearch", p.SupportsSearch, true},
		{"SupportsLyrics", p.SupportsLyrics, true},
		{"SupportsRecognition", p.SupportsRecognition, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.check(); got != tt.expected {
				t.Errorf("%s() = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}

// TestQualityToBitrateLevel tests quality enum to NetEase bitrate level conversion.
func TestQualityToBitrateLevel(t *testing.T) {
	p := &NeteasePlatform{}

	tests := []struct {
		quality  platform.Quality
		expected string
	}{
		{platform.QualityStandard, "standard"},
		{platform.QualityHigh, "higher"},
		{platform.QualityLossless, "lossless"},
		{platform.QualityHiRes, "hires"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := p.qualityToBitrateLevel(tt.quality); got != tt.expected {
				t.Errorf("qualityToBitrateLevel(%v) = %s, want %s", tt.quality, got, tt.expected)
			}
		})
	}
}

// TestBitrateToQuality tests NetEase bitrate to quality enum conversion.
func TestBitrateToQuality(t *testing.T) {
	p := &NeteasePlatform{}

	tests := []struct {
		bitrate  int
		expected platform.Quality
	}{
		{128000, platform.QualityStandard},
		{192000, platform.QualityStandard},
		{320000, platform.QualityHigh},
		{999000, platform.QualityHigh},
		{1411000, platform.QualityLossless},
		{2000000, platform.QualityHiRes},
	}

	for _, tt := range tests {
		t.Run(tt.expected.String(), func(t *testing.T) {
			if got := p.bitrateToQuality(tt.bitrate); got != tt.expected {
				t.Errorf("bitrateToQuality(%d) = %v, want %v", tt.bitrate, got, tt.expected)
			}
		})
	}
}

// TestParseLyricLines tests LRC format lyric parsing.
func TestParseLyricLines(t *testing.T) {
	p := &NeteasePlatform{}

	lrc := `[00:00.00]Line 1
[00:05.50]Line 2
[01:23.99]Line 3
[00:01:10]Line 4
[invalid]Should be skipped
[00:10.00]`

	lines := p.parseLyricLines(lrc)

	// Should parse valid lines only (6 lines total, but 1 is empty, 1 is invalid)
	if len(lines) != 4 {
		t.Errorf("expected 4 parsed lines, got %d", len(lines))
	}

	// Verify first line
	if lines[0].Text != "Line 1" {
		t.Errorf("expected first line text 'Line 1', got '%s'", lines[0].Text)
	}

	// Verify timing of second line (5.5 seconds)
	expectedDuration := int64(5500) // milliseconds
	if lines[1].Time.Milliseconds() != expectedDuration {
		t.Errorf("expected second line time %dms, got %dms",
			expectedDuration, lines[1].Time.Milliseconds())
	}

	// Verify malformed [mm:ss:xx] timestamp is auto-normalized as centiseconds.
	if lines[3].Text != "Line 4" {
		t.Errorf("expected fourth line text 'Line 4', got '%s'", lines[3].Text)
	}
	if lines[3].Time.Milliseconds() != 1100 {
		t.Errorf("expected fourth line time 1100ms, got %dms", lines[3].Time.Milliseconds())
	}
}

// TestImplementsInterface ensures NeteasePlatform implements platform.Platform.
func TestImplementsInterface(t *testing.T) {
	var _ platform.Platform = (*NeteasePlatform)(nil)
}
