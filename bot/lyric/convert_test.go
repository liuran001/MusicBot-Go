package lyric

import (
	"strings"
	"testing"
)

// sampleYRC is a small netease-style yrc snippet (absolute word timing,
// "(start,dur,flag)text" shape).
const sampleYRC = "[1000,2000](1000,500,0)Hello (1500,500,0)world\n[3000,1500](3000,700,0)Test (3700,800,0)line"

// sampleQRC is a QQ-style qrc snippet ("text(start,dur)" shape).
const sampleQRC = "[1000,2000]Hello (1000,500)world (1500,500)\n[3000,1500]Test (3000,700)line (3700,800)"

func TestParseTokenLinesYRC(t *testing.T) {
	lines := parseTokenLines(sampleYRC)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].Text != "Hello world" {
		t.Errorf("line0 text = %q, want %q", lines[0].Text, "Hello world")
	}
	if len(lines[0].Tokens) != 2 {
		t.Fatalf("line0 tokens = %d, want 2", len(lines[0].Tokens))
	}
	if lines[0].Tokens[0].Start != 1000 || lines[0].Tokens[0].End != 1500 {
		t.Errorf("token0 = %+v, want start=1000 end=1500", lines[0].Tokens[0])
	}
	if lines[0].Tokens[1].Text != "world" || lines[0].Tokens[1].Start != 1500 {
		t.Errorf("token1 = %+v, want text=world start=1500", lines[0].Tokens[1])
	}
}

func TestParseTokenLinesQRC(t *testing.T) {
	lines := parseTokenLines(sampleQRC)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].Text != "Hello world " {
		t.Errorf("line0 text = %q, want %q", lines[0].Text, "Hello world ")
	}
	if lines[0].Tokens[0].Text != "Hello " || lines[0].Tokens[0].Start != 1000 {
		t.Errorf("token0 = %+v, want text='Hello ' start=1000", lines[0].Tokens[0])
	}
}

func TestConvertYRCToLRC(t *testing.T) {
	out := Convert(Payload{RawYRC: sampleYRC}, "lrc", Options{})
	want := "[00:01.00]Hello world\n[00:03.00]Test line"
	if out != want {
		t.Errorf("lrc =\n%q\nwant\n%q", out, want)
	}
}

func TestConvertYRCRoundTrip(t *testing.T) {
	out := Convert(Payload{RawYRC: sampleYRC}, "yrc", Options{})
	// Raw yrc is returned verbatim when present.
	if out != sampleYRC {
		t.Errorf("yrc passthrough =\n%q\nwant\n%q", out, sampleYRC)
	}
	// From a qrc source, yrc must be synthesized.
	out2 := Convert(Payload{RawQRC: sampleQRC}, "yrc", Options{})
	if !strings.Contains(out2, "(1000,500,0)Hello") {
		t.Errorf("synthesized yrc missing word tag: %q", out2)
	}
}

func TestConvertToSPL(t *testing.T) {
	out := Convert(Payload{RawYRC: sampleYRC}, "spl", Options{})
	if !strings.HasPrefix(out, "[00:01.00]") {
		t.Errorf("spl should start with line tag, got %q", out)
	}
	if !strings.Contains(out, "<00:01.00>") {
		t.Errorf("spl should contain word tag <00:01.00>, got %q", out)
	}
}

func TestConvertToTTML(t *testing.T) {
	out := Convert(Payload{RawYRC: sampleYRC, MusicName: "Song", Artist: "Artist"}, "ttml", Options{})
	for _, want := range []string{
		"<tt xmlns",
		"itunes:timing=\"Word\"",
		"<span begin=\"00:01.000\" end=\"00:01.500\">Hello ",
		"musicName\" value=\"Song\"",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ttml missing %q in:\n%s", want, out)
		}
	}
}

func TestConvertToAmjson(t *testing.T) {
	out := Convert(Payload{RawYRC: sampleYRC}, "amjson", Options{})
	if !strings.Contains(out, "syllable-lyrics") {
		t.Errorf("amjson missing syllable-lyrics: %q", out)
	}
	if !strings.Contains(out, "ttmlLocalizations") {
		t.Errorf("amjson missing ttmlLocalizations: %q", out)
	}
}

func TestConvertToASS(t *testing.T) {
	out := Convert(Payload{RawYRC: sampleYRC}, "ass", Options{})
	if !strings.Contains(out, "[Script Info]") {
		t.Errorf("ass missing header")
	}
	if !strings.Contains(out, "{\\k") {
		t.Errorf("ass missing karaoke timing")
	}
}

func TestConvertWithTranslation(t *testing.T) {
	tl := "[00:01.00]你好世界\n[00:03.00]测试行"
	yes := true
	out := Convert(Payload{RawYRC: sampleYRC, Translation: tl}, "spl", Options{IncludeTranslation: &yes})
	if !strings.Contains(out, "你好世界") {
		t.Errorf("spl with translation missing translated text: %q", out)
	}
}

func TestConvertTransOnly(t *testing.T) {
	tl := "[00:01.00]你好世界\n[00:03.00]测试行"
	out := Convert(Payload{RawYRC: sampleYRC, Translation: tl}, "trans", Options{})
	if out != tl {
		t.Errorf("trans = %q, want %q", out, tl)
	}
}

func TestConvertLRCToOtherFallback(t *testing.T) {
	// A pure LRC source with no token data: yrc should fall back to the LRC.
	lrc := "[00:01.00]Hello\n[00:03.00]World"
	out := Convert(Payload{Lyric: lrc}, "yrc", Options{})
	if out != lrc {
		t.Errorf("yrc fallback = %q, want %q", out, lrc)
	}
}

func TestNormalizeFormat(t *testing.T) {
	cases := map[string]string{
		"":               "lrc",
		"auto":           "lrc",
		"TTML":           "ttml",
		"lrcx":           "elrc",
		"applemusicjson": "amjson",
		"origin":         "raw",
	}
	for in, want := range cases {
		if got := NormalizeFormat(in); got != want {
			t.Errorf("NormalizeFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConvertTTMLPassthrough(t *testing.T) {
	raw := "<tt>apple ttml here</tt>"
	out := Convert(Payload{RawTTML: raw, Lyric: "[00:01.00]x"}, "ttml", Options{})
	if out != raw {
		t.Errorf("ttml passthrough = %q, want %q", out, raw)
	}
}

func TestConvertLRCBilingualMerge(t *testing.T) {
	lrc := "[00:01.00]Hello world\n[00:03.00]Test line"
	tl := "[00:01.00]你好世界\n[00:03.00]测试行"
	roma := "[00:01.00]haro\n[00:03.00]tesuto"
	yes := true

	// Default: single-track, no merge.
	if got := Convert(Payload{Lyric: lrc, Translation: tl}, "lrc", Options{}); got != lrc {
		t.Errorf("lrc default should not merge, got %q", got)
	}

	// Translation merged as interleaved lines under the same tag.
	wantTL := "[00:01.00]Hello world\n[00:01.00]你好世界\n[00:03.00]Test line\n[00:03.00]测试行"
	if got := Convert(Payload{Lyric: lrc, Translation: tl}, "lrc", Options{IncludeTranslation: &yes}); got != wantTL {
		t.Errorf("lrc+trans =\n%q\nwant\n%q", got, wantTL)
	}

	// Roma merged after translation.
	out := Convert(Payload{Lyric: lrc, Translation: tl, Roma: roma}, "lrc", Options{IncludeTranslation: &yes, IncludeRoma: true})
	if !strings.Contains(out, "[00:01.00]你好世界\n[00:01.00]haro") {
		t.Errorf("lrc+trans+roma order wrong:\n%s", out)
	}

	// romaFirst flips the order.
	outRF := Convert(Payload{Lyric: lrc, Translation: tl, Roma: roma}, "lrc", Options{IncludeTranslation: &yes, IncludeRoma: true, RomaFirst: true})
	if !strings.Contains(outRF, "[00:01.00]haro\n[00:01.00]你好世界") {
		t.Errorf("lrc romaFirst order wrong:\n%s", outRF)
	}

	// "raw" must never merge.
	if got := Convert(Payload{Lyric: lrc, Translation: tl}, "raw", Options{IncludeTranslation: &yes}); got != lrc {
		t.Errorf("raw should stay verbatim, got %q", got)
	}
}

func TestIsCreditLikeLineBareWords(t *testing.T) {
	// QQ Music writes credits as bare "词："/"曲：" (not "作词："); these must be
	// recognized so the translation/roma fuzzy-match guard skips them.
	credits := []string{"词：米津玄師", "曲：米津玄師", "作曲：周杰伦", "作词 : 赵雷", "编曲：钟兴民", "制作人 : 赵雷", "和声：someone"}
	for _, c := range credits {
		if !isCreditLikeLine(c) {
			t.Errorf("isCreditLikeLine(%q) = false, want true", c)
		}
	}
	// Real lyric lines must NOT be flagged as credits.
	nonCredits := []string{"夢ならば", "Hello world", "窗外的麻雀在电线杆上多嘴", "曲终人散"}
	for _, c := range nonCredits {
		if isCreditLikeLine(c) {
			t.Errorf("isCreditLikeLine(%q) = true, want false", c)
		}
	}
}

func TestTTMLCreditGuardSkipsTranslation(t *testing.T) {
	// A QQ-style payload: credit lines have "//" translation, real lines have text.
	// The credit line must NOT borrow the next line's translation via fuzzy match.
	qrc := "[1060,490]曲：artist(1060,490)\n[1550,1330]夢ならば(1550,1330)"
	trans := "[00:01.06]//\n[00:01.55]如果只是一场梦"
	yes := true
	out := Convert(Payload{RawQRC: qrc, Translation: trans}, "ttml", Options{IncludeTranslation: &yes})
	// The credit <p> (曲：artist) must not contain a translation span.
	creditP := out[strings.Index(out, "曲：artist"):]
	creditP = creditP[:strings.Index(creditP, "</p>")]
	if strings.Contains(creditP, "x-translation") {
		t.Errorf("credit line wrongly got a translation span:\n%s", creditP)
	}
}
