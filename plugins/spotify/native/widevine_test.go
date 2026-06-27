package native

import (
	"encoding/json"
	"testing"
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

func TestMP4FormatBitrate(t *testing.T) {
	cases := map[string]int{"11": 256000, "10": 128000, "0": 0, "": 0}
	for format, want := range cases {
		if got := mp4FormatBitrate(format); got != want {
			t.Fatalf("mp4FormatBitrate(%q) = %d, want %d", format, got, want)
		}
	}
}

func TestTrackPlaybackParse(t *testing.T) {
	// Shape per the track-playback media manifest: media[].item.manifest.file_ids_mp4[].
	// Each entry carries the storage-resolve format id ("10"/"11") in `format`.
	raw := `{
		"media": [
			{"item": {"manifest": {"file_ids_mp4": [
				{"file_id": "deadbeef01", "format": "10"},
				{"file_id": "deadbeef02", "format": "11"}
			]}}}
		]
	}`
	var tp trackPlaybackResp
	if err := json.Unmarshal([]byte(raw), &tp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tp.Media) != 1 {
		t.Fatalf("media len = %d, want 1", len(tp.Media))
	}
	files := tp.Media[0].Item.Manifest.FileIDsMP4
	if len(files) != 2 {
		t.Fatalf("file_ids_mp4 len = %d, want 2", len(files))
	}
	if files[1].FileID != "deadbeef02" || files[1].Format != "11" {
		t.Fatalf("second file = %+v, want {deadbeef02 11}", files[1])
	}
}
