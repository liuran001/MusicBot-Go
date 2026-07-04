package native

import (
	"encoding/json"
	"testing"

	widevine "github.com/iyear/gowidevine"
)

func TestSelectMP4(t *testing.T) {
	// Files are passed already sorted high→low (resolveMP4Files guarantees this).
	high := []wvFile{
		{FileID: "a", Format: "11", Bitrate: 256000},
		{FileID: "b", Format: "10", Bitrate: 128000},
	}

	cases := []struct {
		name      string
		files     []wvFile
		preferKbs int
		wantID    string
	}{
		{"highest when no preference", high, 0, "a"},
		{"exact 256k", high, 256, "a"},
		{"exact 128k", high, 128, "b"},
		{"prefer 192 picks at-or-below (128k)", high, 192, "b"},
		{"prefer above ceiling picks highest", high, 320, "a"},
		{"prefer below floor picks lowest", high, 96, "b"},
		{"single file", []wvFile{{FileID: "x", Format: "11", Bitrate: 256000}}, 128, "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectMP4(tc.files, tc.preferKbs)
			if got.FileID != tc.wantID {
				t.Fatalf("selectMP4(%d) = %q, want %q", tc.preferKbs, got.FileID, tc.wantID)
			}
		})
	}
}

func TestSelectMP4Candidates(t *testing.T) {
	files := []wvFile{
		{FileID: "high", Format: "11", Bitrate: 256000},
		{FileID: "standard", Format: "10", Bitrate: 128000},
	}
	cases := []struct {
		name      string
		preferKbs int
		want      []string
	}{
		{"highest falls back to standard", 0, []string{"high", "standard"}},
		{"high falls back to standard", 256, []string{"high", "standard"}},
		{"above ceiling falls back to standard", 320, []string{"high", "standard"}},
		{"standard does not upgrade", 128, []string{"standard"}},
		{"middle tier selects standard", 192, []string{"standard"}},
		{"below floor selects standard", 96, []string{"standard"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectMP4Candidates(files, tc.preferKbs)
			if len(got) != len(tc.want) {
				t.Fatalf("candidate count = %d, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, want := range tc.want {
				if got[i].FileID != want {
					t.Fatalf("candidate %d = %q, want %q", i, got[i].FileID, want)
				}
			}
		})
	}
}

func TestMP4FormatBitrate(t *testing.T) {
	cases := map[string]int{"11": 256000, "10": 128000, "0": 0, "": 0}
	for format, want := range cases {
		if got := mp4FormatBitrate(format); got != want {
			t.Fatalf("mp4FormatBitrate(%q) = %d, want %d", format, got, want)
		}
	}
}

func TestTrackPlaybackParse(t *testing.T) {
	// Shape per the live track-playback response: `media` is an OBJECT keyed by
	// an opaque id (not an array); each entry carries the storage-resolve format
	// id ("10"/"11") in `format`.
	raw := `{
		"media": {
			"abc123": {"item": {"manifest": {"file_ids_mp4": [
				{"file_id": "deadbeef01", "format": "10"},
				{"file_id": "deadbeef02", "format": "11"}
			]}}}
		}
	}`
	var tp trackPlaybackResp
	if err := json.Unmarshal([]byte(raw), &tp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tp.Media) != 1 {
		t.Fatalf("media len = %d, want 1", len(tp.Media))
	}
	files := tp.Media["abc123"].Item.Manifest.FileIDsMP4
	if len(files) != 2 {
		t.Fatalf("file_ids_mp4 len = %d, want 2", len(files))
	}
	if files[1].FileID != "deadbeef02" || files[1].Format != "11" {
		t.Fatalf("second file = %+v, want {deadbeef02 11}", files[1])
	}
}

func TestBuildSpotifyPSSH(t *testing.T) {
	// A valid 40-hex (20-byte) Spotify file id.
	fileID := "0123456789abcdef0123456789abcdef01234567"
	box, err := buildSpotifyPSSH(fileID)
	if err != nil {
		t.Fatalf("buildSpotifyPSSH: %v", err)
	}
	// The box must carry the 'pssh' fourcc at offset 4.
	if len(box) < 32 || string(box[4:8]) != "pssh" {
		t.Fatalf("not a pssh box: len=%d", len(box))
	}
	// gowidevine must be able to parse the box we built (proves it's well-formed).
	parsed, perr := widevine.NewPSSH(box)
	if perr != nil {
		t.Fatalf("widevine.NewPSSH rejected our box: %v", perr)
	}
	// Verify the inner data has all 5 fields set (matching votify).
	inner := parsed.Data()
	if inner.GetProvider() != "spotify" {
		t.Errorf("provider = %q, want %q", inner.GetProvider(), "spotify")
	}
	if inner.GetProtectionScheme() != 1667591779 {
		t.Errorf("protection_scheme = %d, want 1667591779", inner.GetProtectionScheme())
	}
	if len(inner.GetKeyIds()) != 1 || len(inner.GetKeyIds()[0]) != 16 {
		t.Errorf("key_ids = %v, want 1 key of 16 bytes", inner.GetKeyIds())
	}
}

func TestBuildSpotifyPSSHRejectsBad(t *testing.T) {
	if _, err := buildSpotifyPSSH("00"); err == nil {
		t.Fatal("expected error for too-short file id, got nil")
	}
	if _, err := buildSpotifyPSSH("not-hex-at-all"); err == nil {
		t.Fatal("expected error for non-hex file id, got nil")
	}
}
