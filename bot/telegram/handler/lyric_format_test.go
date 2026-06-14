package handler

import (
	"strings"
	"testing"
	"time"

	lyricpkg "github.com/liuran001/MusicBot-Go/bot/lyric"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestParseTrailingLyricFormat(t *testing.T) {
	cases := []struct {
		in         string
		wantRest   string
		wantFormat string
	}{
		{"https://music.163.com/song?id=123 ttml", "https://music.163.com/song?id=123", "ttml"},
		{"123456 yrc", "123456", "yrc"},
		{"123456 qrc", "123456", "qrc"},
		{"123456 applemusicjson", "123456", "amjson"},
		{"123456 lrcx", "123456", "elrc"},
		{"123456", "123456", "lrc"},
		{"some song name", "some song name", "lrc"}, // "name" is not a format
		{"", "", "lrc"},
		{"周杰伦 七里香 lys", "周杰伦 七里香", "lys"},
	}
	for _, c := range cases {
		rest, format := parseTrailingLyricFormat(c.in)
		if rest != c.wantRest || format != c.wantFormat {
			t.Errorf("parseTrailingLyricFormat(%q) = (%q, %q), want (%q, %q)", c.in, rest, format, c.wantRest, c.wantFormat)
		}
	}
}

func TestLyricPayloadFrom(t *testing.T) {
	lyrics := &platform.Lyrics{
		Plain:       "[00:01.00]hello",
		Translation: "[00:01.00]你好",
		Roma:        "[00:01.00]haro",
		RawYRC:      "[1000,500](1000,500,0)hello",
		RawQRC:      "[1000,500]hello(1000,500)",
	}
	p := lyricPayloadFrom(lyrics, "qqmusic")
	if p.Source != "tencent" {
		t.Errorf("qqmusic source should map to tencent, got %q", p.Source)
	}
	if p.RawYRC == "" || p.RawQRC == "" || p.Translation == "" || p.Roma == "" {
		t.Errorf("payload lost a track: %+v", p)
	}
}

func TestLyricPayloadFromTimestampedOnly(t *testing.T) {
	lyrics := &platform.Lyrics{
		Timestamped: []platform.LyricLine{
			{Time: time.Second, Text: "hello"},
			{Time: 3 * time.Second, Text: "world"},
		},
	}
	p := lyricPayloadFrom(lyrics, "applemusic")
	if !strings.Contains(p.Lyric, "[00:01.00]hello") {
		t.Errorf("derived LRC wrong: %q", p.Lyric)
	}
	// And it should convert to text fine.
	out := lyricpkg.Convert(p, "txt", lyricpkg.Options{})
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Errorf("txt convert wrong: %q", out)
	}
}

func TestBuildLyricFileNameForFormat(t *testing.T) {
	cases := map[string]string{
		"ttml":   "周杰伦 - 七里香.ttml",
		"amjson": "周杰伦 - 七里香.json",
		"yrc":    "周杰伦 - 七里香.yrc",
		"lrc":    "周杰伦 - 七里香.lrc",
		"txt":    "周杰伦 - 七里香.txt",
	}
	for format, want := range cases {
		got := buildLyricFileNameForFormat("周杰伦 - 七里香", format)
		if got != want {
			t.Errorf("buildLyricFileNameForFormat(_, %q) = %q, want %q", format, got, want)
		}
	}
}

func TestBuildLyricFormatKeyboard(t *testing.T) {
	state := lyricRenderState{format: "ttml", includeTranslation: true, includeRoma: false}
	kb := buildLyricFormatKeyboard("netease", "123456", state, 42)
	if kb == nil || len(kb.InlineKeyboard) == 0 {
		t.Fatal("expected a non-empty keyboard")
	}
	foundCurrent := false
	foundToggle := false
	var sampleData string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if strings.HasPrefix(btn.Text, "• ") {
				foundCurrent = true
			}
			if strings.HasPrefix(btn.Text, "翻译:") || strings.HasPrefix(btn.Text, "罗马音:") {
				foundToggle = true
			}
			if len(btn.CallbackData) > 64 {
				t.Errorf("callback data exceeds 64 bytes: %q", btn.CallbackData)
			}
			if sampleData == "" {
				sampleData = btn.CallbackData
			}
		}
	}
	if !foundCurrent {
		t.Error("current format (ttml) should be marked with •")
	}
	if !foundToggle {
		t.Error("ttml supports side tracks, expected translation/roma toggle buttons")
	}
	if !strings.HasPrefix(sampleData, "lyric f ") {
		t.Errorf("callback data should start with 'lyric f ', got %q", sampleData)
	}
}

func TestLyricFormatKeyboardNoTogglesForYRC(t *testing.T) {
	// Pure word-by-word formats like yrc don't carry side tracks → no toggles.
	state := lyricRenderState{format: "yrc"}
	kb := buildLyricFormatKeyboard("netease", "123456", state, 42)
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if strings.HasPrefix(btn.Text, "翻译:") || strings.HasPrefix(btn.Text, "罗马音:") {
				t.Errorf("yrc should not show side-track toggles, got %q", btn.Text)
			}
		}
	}
}

func TestLyricFlagsRoundTrip(t *testing.T) {
	cases := []struct{ tr, rm bool }{{false, false}, {true, false}, {false, true}, {true, true}}
	for _, c := range cases {
		enc := encodeLyricFlags(c.tr, c.rm)
		tr, rm, ok := decodeLyricFlags(enc)
		if !ok || tr != c.tr || rm != c.rm {
			t.Errorf("flags round-trip failed for (%v,%v): enc=%q decoded=(%v,%v,%v)", c.tr, c.rm, enc, tr, rm, ok)
		}
	}
	if _, _, ok := decodeLyricFlags("xy"); ok {
		t.Error("invalid flags should return ok=false")
	}
}

func TestLyricFormatDisplayName(t *testing.T) {
	if lyricFormatDisplayName("yrc") != "YRC 逐词" {
		t.Errorf("unexpected display name for yrc: %q", lyricFormatDisplayName("yrc"))
	}
}
