package handler

import "testing"

func TestParseMusicCallbackDataV2_CompatibleCases(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want parsedMusicCallback
	}{
		{
			name: "old format music id only",
			args: []string{"music", "12345"},
			want: parsedMusicCallback{platformName: "netease", trackID: "12345", requesterID: 0, qualityOverride: "", ok: true},
		},
		{
			name: "old format music id requester",
			args: []string{"music", "12345", "6789"},
			want: parsedMusicCallback{platformName: "netease", trackID: "12345", requesterID: 6789, qualityOverride: "", ok: true},
		},
		{
			name: "new format platform track",
			args: []string{"music", "qqmusic", "abc123"},
			want: parsedMusicCallback{platformName: "qqmusic", trackID: "abc123", requesterID: 0, qualityOverride: "", ok: true},
		},
		{
			name: "new format platform track requester",
			args: []string{"music", "netease", "2750754678", "6030752690"},
			want: parsedMusicCallback{platformName: "netease", trackID: "2750754678", requesterID: 6030752690, qualityOverride: "", ok: true},
		},
		{
			name: "new format platform track quality requester",
			args: []string{"music", "netease", "2750754678", "hires", "6030752690"},
			want: parsedMusicCallback{platformName: "netease", trackID: "2750754678", requesterID: 6030752690, qualityOverride: "hires", ok: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := parseMusicCallbackDataV2(tt.args)
			if parsed != tt.want {
				t.Fatalf("v2 parse mismatch: got %+v want %+v", parsed, tt.want)
			}
		})
	}
}

func TestParseMusicCallbackData_InvalidArgs(t *testing.T) {
	v2 := parseMusicCallbackDataV2([]string{"music"})
	if v2.ok {
		t.Fatalf("expected parser to return not ok for invalid args, got v2=%+v", v2)
	}
}
