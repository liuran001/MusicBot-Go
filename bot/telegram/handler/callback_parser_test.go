package handler

import (
	"strings"
	"testing"
)

func TestIsInlineMusicCallbackArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "inline direct", args: []string{"music", "i", "netease", "123"}, want: true},
		{name: "inline token", args: []string{"music", "it", "token"}, want: true},
		{name: "inline episode direct", args: []string{"music", "iep", "s", "netease", "123", "hires", "1", "1"}, want: true},
		{name: "inline episode token", args: []string{"music", "iet", "token"}, want: true},
		{name: "non inline episode", args: []string{"music", "ep", "s", "netease"}, want: false},
		{name: "too short", args: []string{"music", "i"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInlineMusicCallbackArgs(tt.args); got != tt.want {
				t.Fatalf("isInlineMusicCallbackArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

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

// 长 trackID 经 token store 往返时必须完整保留 quality override，
// 不能像旧的精简 fallback 那样丢字段导致音质回落。
func TestBuildEpisodeCallbackData_PreservesQualityViaToken(t *testing.T) {
	trackID := strings.Repeat("a", 60)
	data := buildEpisodeSelectCallbackData("bilibili", trackID, "lossless", 12345, 3)
	if data == "" {
		t.Fatal("callback data should not be empty")
	}
	if len(data) > 64 {
		t.Fatalf("callback data %q exceeds Telegram 64-byte limit (%d)", data, len(data))
	}
	if !strings.HasPrefix(data, "music ept ") {
		t.Fatalf("long trackID should fall back to token form, got %q", data)
	}
	action, platformName, parsedTrackID, qualityValue, requesterID, page, ok := parseEpisodeCallbackArgs(strings.Fields(data))
	if !ok {
		t.Fatalf("parseEpisodeCallbackArgs failed for %q", data)
	}
	if action != "p" || platformName != "bilibili" || parsedTrackID != trackID {
		t.Fatalf("parsed = (%q,%q,%q), want (p,bilibili,%q)", action, platformName, parsedTrackID, trackID)
	}
	if qualityValue != "lossless" {
		t.Fatalf("qualityValue = %q, want lossless", qualityValue)
	}
	if requesterID != 12345 || page != 3 {
		t.Fatalf("requesterID/page = (%d,%d), want (12345,3)", requesterID, page)
	}
}

// 短 trackID 走普通拼接格式，仍能完整解析。
func TestBuildEpisodeCallbackData_ShortRoundTrip(t *testing.T) {
	data := buildEpisodeSelectCallbackData("netease", "12345", "hires", 6789, 2)
	if !strings.HasPrefix(data, "music ep ") || strings.HasPrefix(data, "music ept ") {
		t.Fatalf("short trackID should use plain format, got %q", data)
	}
	action, platformName, trackID, qualityValue, requesterID, page, ok := parseEpisodeCallbackArgs(strings.Fields(data))
	if !ok {
		t.Fatalf("parseEpisodeCallbackArgs failed for %q", data)
	}
	if action != "p" || platformName != "netease" || trackID != "12345" || qualityValue != "hires" || requesterID != 6789 || page != 2 {
		t.Fatalf("parsed = (%q,%q,%q,%q,%d,%d), want (p,netease,12345,hires,6789,2)", action, platformName, trackID, qualityValue, requesterID, page)
	}
}

// 旧的 7 段精简 fallback（无 quality 段）仍可兼容解析，且补默认 hires，
// 与 inline 路径 parseInlineEpisodeCallbackArgs 行为一致。
func TestParseEpisodeCallbackArgs_LegacyFallbackDefaultsQuality(t *testing.T) {
	args := strings.Fields("music ep p bilibili BV1xx 6789 2")
	action, platformName, trackID, qualityValue, requesterID, page, ok := parseEpisodeCallbackArgs(args)
	if !ok {
		t.Fatalf("parseEpisodeCallbackArgs failed for legacy fallback")
	}
	if action != "p" || platformName != "bilibili" || trackID != "BV1xx" || requesterID != 6789 || page != 2 {
		t.Fatalf("parsed = (%q,%q,%q,%d,%d), want (p,bilibili,BV1xx,6789,2)", action, platformName, trackID, requesterID, page)
	}
	if qualityValue != "hires" {
		t.Fatalf("qualityValue = %q, want hires (default)", qualityValue)
	}
}

// 短且安全的 trackID 走普通 "music ..." 拼接，解析回原值。
func TestBuildMusicSendCallbackData_ShortPlainRoundTrip(t *testing.T) {
	data := buildMusicSendCallbackData("netease", "12345", "hires", 6789)
	if !strings.HasPrefix(data, "music ") || strings.HasPrefix(data, "music mt ") {
		t.Fatalf("safe short trackID should use plain format, got %q", data)
	}
	parsed := parseMusicCallbackDataV2(strings.Fields(data))
	if !parsed.ok || parsed.tokenExpired {
		t.Fatalf("parse failed: %+v", parsed)
	}
	if parsed.platformName != "netease" || parsed.trackID != "12345" || parsed.qualityOverride != "hires" || parsed.requesterID != 6789 {
		t.Fatalf("parsed = %+v, want netease/12345/hires/6789", parsed)
	}
}

// 含空格的 trackID 不能直接拼接(会被 strings.Split 错位),必须走 token store
// 完整保留所有字段。这是 #24 的核心:原裸 fmt.Sprintf 会让 requesterID 错位、
// 鉴权失效并丢 quality。
func TestBuildMusicSendCallbackData_SpaceInTrackIDUsesToken(t *testing.T) {
	trackID := "abc 123 def"
	data := buildMusicSendCallbackData("qqmusic", trackID, "lossless", 4242)
	if !strings.HasPrefix(data, "music mt ") {
		t.Fatalf("trackID with spaces should fall back to token form, got %q", data)
	}
	parsed := parseMusicCallbackDataV2(strings.Fields(data))
	if !parsed.ok || parsed.tokenExpired {
		t.Fatalf("parse failed: %+v", parsed)
	}
	if parsed.platformName != "qqmusic" || parsed.trackID != trackID || parsed.qualityOverride != "lossless" || parsed.requesterID != 4242 {
		t.Fatalf("parsed = %+v, want qqmusic/%q/lossless/4242", parsed, trackID)
	}
}

// 超长 trackID 直接拼接会超过 Telegram 64 字节上限,必须走 token store 且不超限。
func TestBuildMusicSendCallbackData_LongTrackIDUsesToken(t *testing.T) {
	trackID := strings.Repeat("a", 80)
	data := buildMusicSendCallbackData("bilibili", trackID, "hires", 7777)
	if len(data) > 64 {
		t.Fatalf("callback data %q exceeds 64-byte limit (%d)", data, len(data))
	}
	if !strings.HasPrefix(data, "music mt ") {
		t.Fatalf("long trackID should fall back to token form, got %q", data)
	}
	parsed := parseMusicCallbackDataV2(strings.Fields(data))
	if !parsed.ok || parsed.trackID != trackID || parsed.requesterID != 7777 {
		t.Fatalf("parsed = %+v, want trackID len 80 / requester 7777", parsed)
	}
}

// 过期/未知 token 应被标记 tokenExpired,而不是当成普通参数误解析。
func TestParseMusicCallbackDataV2_ExpiredToken(t *testing.T) {
	parsed := parseMusicCallbackDataV2([]string{"music", "mt", "nonexistent-token"})
	if !parsed.ok || !parsed.tokenExpired {
		t.Fatalf("expected tokenExpired=true, got %+v", parsed)
	}
}

// 缺字段时返回空串,调用方据此跳过该按钮。
func TestBuildMusicSendCallbackData_MissingFieldsEmpty(t *testing.T) {
	if data := buildMusicSendCallbackData("", "123", "hires", 1); data != "" {
		t.Fatalf("empty platform should yield empty data, got %q", data)
	}
	if data := buildMusicSendCallbackData("netease", "", "hires", 1); data != "" {
		t.Fatalf("empty trackID should yield empty data, got %q", data)
	}
}
