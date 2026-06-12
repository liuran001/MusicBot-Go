package applemusic

import (
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// Real enhancedHls master playlist excerpt captured from the live API
// (track 1450695739, "bad guy"). Trimmed to the STREAM-INF section.
const testMasterPlaylist = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-alac-stereo-44100-24",AUTOSELECT=YES,CHANNELS="2",NAME="songEnhanced",SAMPLE-RATE=44100,BIT-DEPTH=24
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=260342,_AVG-BANDWIDTH=260342,BANDWIDTH=274168,CODECS="mp4a.40.2",STABLE-VARIANT-ID="a",AUDIO="audio-stereo-256"
P1249856578_A1450695739_audio_en_gr256_mp4a-40-2.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=770225,_AVG-BANDWIDTH=770225,BANDWIDTH=771866,CODECS="ec-3",STABLE-VARIANT-ID="b",AUDIO="audio-atmos-2768"
P1249856578_A1450695739_audio_en_gr2768_mp4a-A6.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=1448364,_AVG-BANDWIDTH=1448364,BANDWIDTH=1554084,CODECS="alac",STABLE-VARIANT-ID="c",AUDIO="audio-alac-stereo-44100-24"
P1249856578_A1450695739_audio_en_gr2116_alac.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=132924,_AVG-BANDWIDTH=132924,BANDWIDTH=137748,CODECS="mp4a.40.2",STABLE-VARIANT-ID="d",AUDIO="audio-stereo-128"
P1249856578_A1450695739_audio_en_gr128_mp4a-40-2.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=74092,_AVG-BANDWIDTH=74092,BANDWIDTH=78264,CODECS="mp4a.40.5",STABLE-VARIANT-ID="e",AUDIO="audio-HE-stereo-64"
P1249856578_A1450695739_audio_en_gr64_mp4a-40-2.m3u8
`

func TestParseEnhancedHLSMaster(t *testing.T) {
	variants, err := parseEnhancedHLSMaster(testMasterPlaylist)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(variants) != 5 {
		t.Fatalf("expected 5 variants, got %d", len(variants))
	}
	// Sorted by AvgBW desc -> first must be the ALAC (1448364).
	if !variants[0].isALAC() {
		t.Errorf("expected ALAC first (highest bw), got codecs=%q", variants[0].Codecs)
	}
	if variants[0].SampleRate != 44100 || variants[0].BitDepth != 24 {
		t.Errorf("ALAC details wrong: rate=%d depth=%d", variants[0].SampleRate, variants[0].BitDepth)
	}
	if variants[0].URI != "P1249856578_A1450695739_audio_en_gr2116_alac.m3u8" {
		t.Errorf("ALAC URI wrong: %q", variants[0].URI)
	}
	// Atmos second (770225).
	if !variants[1].isAtmos() {
		t.Errorf("expected Atmos second, got %q", variants[1].Codecs)
	}
}

func TestSelectVariantForQuality(t *testing.T) {
	variants, err := parseEnhancedHLSMaster(testMasterPlaylist)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tests := []struct {
		name       string
		quality    platform.Quality
		wantAtmos  bool
		wantCodec  string
		wantURIsub string
	}{
		{"hires->alac", platform.QualityHiRes, false, "alac", "alac"},
		{"lossless->alac", platform.QualityLossless, false, "alac", "alac"},
		{"high->aac256", platform.QualityHigh, false, "mp4a.40.2", "gr256"},
		{"standard->aac128", platform.QualityStandard, false, "mp4a.40.2", "gr128"},
		{"atmos-explicit", platform.QualityHiRes, true, "ec-3", "gr2768"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := selectVariantForQuality(variants, tc.quality, tc.wantAtmos)
			if !ok {
				t.Fatalf("no variant selected")
			}
			if v.Codecs != tc.wantCodec {
				t.Errorf("codec: got %q want %q", v.Codecs, tc.wantCodec)
			}
			if tc.wantURIsub != "" && !contains(v.URI, tc.wantURIsub) {
				t.Errorf("URI %q does not contain %q", v.URI, tc.wantURIsub)
			}
		})
	}
}

// When only AAC variants exist (no lossless), hires must fall back to AAC.
func TestSelectVariantFallbackToAAC(t *testing.T) {
	aacOnly := `#EXTM3U
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=260342,BANDWIDTH=274168,CODECS="mp4a.40.2",AUDIO="audio-stereo-256"
a.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=132924,BANDWIDTH=137748,CODECS="mp4a.40.2",AUDIO="audio-stereo-128"
b.m3u8
`
	variants, err := parseEnhancedHLSMaster(aacOnly)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v, ok := selectVariantForQuality(variants, platform.QualityHiRes, false)
	if !ok {
		t.Fatal("expected AAC fallback, got nothing")
	}
	if !v.isAAC() {
		t.Errorf("expected AAC fallback, got %q", v.Codecs)
	}
	// Should pick the ~256k one (closest to target).
	if v.AvgBW != 260342 {
		t.Errorf("expected 256k AAC, got avgbw=%d", v.AvgBW)
	}
}
