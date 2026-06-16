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

// TestBitrateToQuality tests NetEase bitrate to quality enum conversion. This
// is the last-resort fallback (no level field, non-lossless format), so a
// lossless-grade bitrate such as 999kbps must resolve to Lossless, not High.
func TestBitrateToQuality(t *testing.T) {
	p := &NeteasePlatform{}

	tests := []struct {
		bitrate  int
		expected platform.Quality
	}{
		{128000, platform.QualityStandard},
		{192000, platform.QualityStandard},
		{320000, platform.QualityHigh},
		{999000, platform.QualityLossless},
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

// TestResolveQuality verifies the layered quality resolution: the authoritative
// level field wins, a lossless container is at least Lossless even at the
// br≈999kbps that NetEase reports for FLAC, and pure bitrate is the last resort.
func TestResolveQuality(t *testing.T) {
	p := &NeteasePlatform{}

	tests := []struct {
		name     string
		level    string
		format   string
		bitrate  int
		expected platform.Quality
	}{
		// Authoritative level field wins regardless of bitrate/format.
		{"level standard", "standard", "mp3", 128000, platform.QualityStandard},
		{"level higher", "higher", "mp3", 320000, platform.QualityHigh},
		{"level exhigh", "exhigh", "mp3", 320000, platform.QualityHigh},
		{"level lossless", "lossless", "flac", 999000, platform.QualityLossless},
		{"level hires", "hires", "flac", 1900000, platform.QualityHiRes},
		{"level jymaster", "jymaster", "flac", 2800000, platform.QualityHiRes},
		// level wins even when it disagrees with the (misleading) bitrate.
		{"level lossless low br", "lossless", "flac", 999000, platform.QualityLossless},
		// No level: FLAC container is at least Lossless (the core bug).
		{"flac no level 999k", "", "flac", 999000, platform.QualityLossless},
		{"flac no level hires br", "", "flac", 1900000, platform.QualityHiRes},
		{"ape no level", "", "ape", 900000, platform.QualityLossless},
		// No level, lossy format: fall back to pure bitrate.
		{"mp3 no level 320k", "", "mp3", 320000, platform.QualityHigh},
		{"mp3 no level 128k", "", "mp3", 128000, platform.QualityStandard},
		// Unknown level string falls through to format/bitrate.
		{"unknown level flac", "mystery", "flac", 999000, platform.QualityLossless},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.resolveQuality(tt.level, tt.format, tt.bitrate); got != tt.expected {
				t.Errorf("resolveQuality(%q, %q, %d) = %v, want %v",
					tt.level, tt.format, tt.bitrate, got, tt.expected)
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

// TestConvertLyricsSurfacesRawTracks verifies that convertLyrics surfaces the
// word-by-word yrc and roma side-tracks for the lyric format converter.
func TestConvertLyricsSurfacesRawTracks(t *testing.T) {
	p := &NeteasePlatform{}
	data := &SongLyricData{}
	data.Lrc.Lyric = "[00:01.00]hello"
	data.Tlyric.Lyric = "[00:01.00]你好"
	data.Romalrc.Lyric = "[00:01.00]haro"
	data.Yrc.Lyric = "[1000,500](1000,500,0)hello"

	got := p.convertLyrics(data)
	if got.RawYRC != data.Yrc.Lyric {
		t.Errorf("RawYRC = %q, want %q", got.RawYRC, data.Yrc.Lyric)
	}
	if got.Roma != data.Romalrc.Lyric {
		t.Errorf("Roma = %q, want %q", got.Roma, data.Romalrc.Lyric)
	}
	if got.Translation != data.Tlyric.Lyric {
		t.Errorf("Translation = %q, want %q", got.Translation, data.Tlyric.Lyric)
	}
	if got.Plain != data.Lrc.Lyric {
		t.Errorf("Plain = %q, want %q", got.Plain, data.Lrc.Lyric)
	}
}
