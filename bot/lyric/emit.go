package lyric

import (
	"regexp"
	"strconv"
	"strings"
)

// tokenToLRC flattens token lines to line-timed LRC. Mirrors tokenToLrc.
func tokenToLRC(token string) string {
	lines := parseTokenLines(token)
	if len(lines) == 0 {
		return ""
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, formatLRCTagFromMs(line.Start, 2)+line.Text)
	}
	return strings.Join(out, "\n")
}

// tokenToYRC re-emits token lines as netease yrc. Mirrors tokenToYrc.
func tokenToYRC(token string) string {
	lines := parseTokenLines(token)
	if len(lines) == 0 {
		return ""
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		prefix := "[" + itoa(line.Start) + "," + itoa(max0(line.End-line.Start)) + "]"
		var sb strings.Builder
		for _, tk := range line.Tokens {
			dur := max0(tk.End - tk.Start)
			sb.WriteString("(" + itoa(tk.Start) + "," + itoa(dur) + ",0)" + tk.Text)
		}
		content := sb.String()
		if content == "" {
			content = line.Text
		}
		out = append(out, prefix+content)
	}
	return strings.Join(out, "\n")
}

// tokenToQRC re-emits token lines as QQ qrc. Mirrors tokenToQrc.
func tokenToQRC(token string) string {
	lines := parseTokenLines(token)
	if len(lines) == 0 {
		return ""
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		prefix := "[" + itoa(line.Start) + "," + itoa(max0(line.End-line.Start)) + "]"
		var sb strings.Builder
		for _, tk := range line.Tokens {
			dur := max0(tk.End - tk.Start)
			sb.WriteString(tk.Text + "(" + itoa(tk.Start) + "," + itoa(dur) + ")")
		}
		content := sb.String()
		if content == "" {
			content = line.Text
		}
		out = append(out, prefix+content)
	}
	return strings.Join(out, "\n")
}

// tokenToLys re-emits token lines as Lyricify Syllable, with a line-role prefix.
// Mirrors tokenToLys (text(start,dur) segments).
func tokenToLys(token, linePrefix string, stripLeadingPreface bool) string {
	lines := parseTokenLines(token)
	if len(lines) == 0 {
		return ""
	}
	out := make([]string, 0, len(lines))
	started := false
	for _, line := range lines {
		var sb strings.Builder
		for _, tk := range line.Tokens {
			dur := max0(tk.End - tk.Start)
			sb.WriteString(tk.Text + "(" + itoa(tk.Start) + "," + itoa(dur) + ")")
		}
		content := sb.String()
		if content == "" {
			content = line.Text
		}
		if stripLeadingPreface && !started {
			if isLqePrefaceLikeLine(strings.TrimSpace(line.Text)) {
				continue
			}
			started = true
		}
		out = append(out, linePrefix+content)
	}
	return strings.Join(out, "\n")
}

// tokenToLysDocument wraps tokenToLys with [ti]/[ar]/[by] headers. Mirrors
// tokenToLysDocument (also used for the krc export path).
func tokenToLysDocument(token string, p Payload, lyric, tlyric, roma string) string {
	body := tokenToLys(token, "[4]", true)
	if strings.TrimSpace(body) == "" {
		return ""
	}
	meta := extractLRCMetadata(lyric, tlyric, roma)
	musicName := firstNonEmpty(p.MusicName, meta["ti"])
	artists := firstNonEmpty(p.Artist, meta["ar"])
	by := meta["by"]

	var out []string
	if musicName != "" {
		out = append(out, "[ti:"+musicName+"]")
	}
	if artists != "" {
		out = append(out, "[ar:"+artists+"]")
	}
	out = append(out, "[by:"+by+"]", "", body)
	return strings.Join(out, "\n")
}

// tokenToElrc re-emits token lines as enhanced LRC (per-word "<mm:ss.fff>" tags).
// Mirrors tokenToElrc.
func tokenToElrc(token string, p Payload, lyric, tlyric, roma string) string {
	lines := parseTokenLines(token)
	if len(lines) == 0 {
		return ""
	}
	meta := extractLRCMetadata(lyric, tlyric, roma)
	musicName := firstNonEmpty(p.MusicName, meta["ti"])
	artists := firstNonEmpty(p.Artist, meta["ar"])
	by := meta["by"]

	var out []string
	if musicName != "" {
		out = append(out, "[ti:"+musicName+"]")
	}
	if artists != "" {
		out = append(out, "[ar:"+artists+"]")
	}
	out = append(out, "[by:"+by+"]")

	started := false
	for _, line := range lines {
		plain := strings.TrimSpace(line.Text)
		if !started && isLqePrefaceLikeLine(plain) {
			continue
		}
		started = true
		lineText := formatLRCTagFromMs(line.Start, 3)
		if len(line.Tokens) == 0 {
			lineText += formatElrcWordTag(line.Start) + plain
		} else {
			for _, tk := range line.Tokens {
				lineText += formatElrcWordTag(tk.Start) + tk.Text
			}
		}
		out = append(out, lineText)
	}
	return strings.Join(out, "\n")
}

func formatElrcWordTag(ms int) string {
	tag := formatLRCTagFromMs(ms, 3)
	return "<" + tag[1:len(tag)-1] + ">"
}

// --- simple text formats ---

func lrcToTxt(lrc string) string {
	var out []string
	for _, line := range splitLines(lrc) {
		clean := strings.TrimSpace(lrcStripAllTags(line))
		if clean != "" {
			out = append(out, clean)
		}
	}
	return strings.Join(out, "\n")
}

var lrcBareTagRe = regexp.MustCompile(`\[[0-9:.]+\]`)

func lrcStripAllTags(line string) string {
	return lrcBareTagRe.ReplaceAllString(line, "")
}

func lrcToSrt(lrc string) string {
	entries := parseLRCEntries(lrc)
	if len(entries) == 0 {
		return ""
	}
	var out []string
	for i, e := range entries {
		start := e.Time
		var end float64
		if i+1 < len(entries) {
			end = maxFloat(start+0.3, entries[i+1].Time-0.01)
		} else {
			end = start + 3.0
		}
		out = append(out, itoa(i+1))
		out = append(out, secondsToSRTTime(start)+" --> "+secondsToSRTTime(end))
		out = append(out, e.Text)
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

func translationOnly(tlyric string) string {
	if strings.TrimSpace(tlyric) == "" {
		return ""
	}
	tagOnly := regexp.MustCompile(`^\[[0-9]{1,2}:[0-9]{1,2}(?:[.:][0-9]{1,3})?\].*$`)
	emptyTag := regexp.MustCompile(`^\[[0-9]{1,2}:[0-9]{1,2}(?:[.:][0-9]{1,3})?\]\s*//$`)
	var out []string
	for _, row := range splitLines(tlyric) {
		line := strings.TrimSpace(row)
		if line == "" || line == "//" {
			continue
		}
		if !tagOnly.MatchString(line) {
			continue
		}
		if emptyTag.MatchString(line) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// mergeLrcTracks interleaves translation and/or roma side-tracks under each
// original LRC line, matching by the "[mm:ss.xx]" tag prefix. Mirrors
// LyricConverterService::mergeLrcTracks. The original line text is preserved
// verbatim; extra lines reuse the original line's tag so players keep them in
// sync. Order honors romaFirst.
func mergeLrcTracks(lyric, tlyric, roma string, romaFirst bool) string {
	if strings.TrimSpace(lyric) == "" {
		return lyric
	}
	if strings.TrimSpace(tlyric) == "" && strings.TrimSpace(roma) == "" {
		return lyric
	}

	transMap := lrcTagPrefixMap(tlyric)
	romaMap := lrcTagPrefixMap(roma)

	var out []string
	for _, raw := range strings.Split(lyric, "\n") {
		line := raw
		if line == "" {
			continue
		}
		out = append(out, line)

		// Key is the bracketed tag prefix, e.g. "[00:12.34]".
		key, ok := lrcLeadingTagKey(line)
		if !ok {
			continue
		}
		trans := strings.TrimSpace(transMap[key])
		romaji := strings.TrimSpace(romaMap[key])
		if romaji == "//" {
			romaji = ""
		}

		transLine := ""
		if trans != "" && trans != "//" {
			transLine = key + trans
		}
		romaLine := ""
		if romaji != "" && romaji != trans {
			romaLine = key + romaji
		}
		out = append(out, buildOrderedOutputLines(transLine, romaLine, romaFirst)...)
	}
	return strings.Join(out, "\n")
}

// lrcTagPrefixMap maps each line's bracketed tag prefix (e.g. "[00:12.34]") to
// its text, collapsing runs of whitespace. Lines without a leading tag are
// skipped. Mirrors the explode(']') maps in PHP mergeLrcTracks.
func lrcTagPrefixMap(track string) map[string]string {
	m := map[string]string{}
	if strings.TrimSpace(track) == "" {
		return m
	}
	spaceRe := regexp.MustCompile(`\s\s+`)
	for _, raw := range strings.Split(track, "\n") {
		if raw == "" {
			continue
		}
		key, ok := lrcLeadingTagKey(raw)
		if !ok {
			continue
		}
		text := strings.TrimSpace(spaceRe.ReplaceAllString(raw[len(key):], " "))
		m[key] = text
	}
	return m
}

// lrcLeadingTagKey returns the leading "[...]" tag (including brackets) of a
// line, or false when the line does not start with one.
func lrcLeadingTagKey(line string) (string, bool) {
	if !strings.HasPrefix(line, "[") {
		return "", false
	}
	end := strings.IndexByte(line, ']')
	if end < 0 {
		return "", false
	}
	return line[:end+1], true
}
func buildOrderedOutputLines(translationLine, romaLine string, romaFirst bool) []string {
	var ordered []string
	if romaFirst {
		ordered = []string{romaLine, translationLine}
	} else {
		ordered = []string{translationLine, romaLine}
	}
	out := make([]string, 0, 2)
	for _, l := range ordered {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// creditLineRe matches leading credit/metadata lines (作词/作曲/编曲/制作…). Bare
// 词/曲 are included because QQ Music writes credits as "词：…"/"曲：…" rather
// than "作词：…"; anchored at line start + immediate colon, a real lyric almost
// never collides. Mirrors isLqePrefaceLikeLine's bare 词|曲 precedent.
var creditLineRe = regexp.MustCompile(`(?i)^(演唱|歌手|调教|作词|作曲|编曲|制作人|制作|监制|录音|混音|母带|和声|合声|弦乐团|出品人|出品|艺术总监|歌词翻译|词曲|词|曲|Lyricist|Composer|Producer|Arranger|Vocal|Mix|Master|OP|SP)([^:：]{0,20})?[:：]`)

func isCreditLikeLine(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return creditLineRe.MatchString(text)
}

var (
	lqePrefaceDashRe = regexp.MustCompile(`\s-\s`)
	lqePrefaceWordRe = regexp.MustCompile(`(?i)^(词|曲|作词|作曲|Lyricist|Composer)\s*[:：]`)
)

func isLqePrefaceLikeLine(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return lqePrefaceDashRe.MatchString(text) || lqePrefaceWordRe.MatchString(text)
}

// normalizeRomaLyric strips secondary inline timestamps from roma content.
// Mirrors normalizeRomaLyric.
func normalizeRomaLyric(roma string) string {
	if strings.TrimSpace(roma) == "" {
		return ""
	}
	prefixRe := regexp.MustCompile(`^(\[[0-9]{1,2}:[0-9]{2}(?:[.:][0-9]{1,3})?\])(.*)$`)
	multiSpace := regexp.MustCompile(`\s{2,}`)
	var out []string
	for _, line := range splitLines(roma) {
		if line == "" {
			out = append(out, line)
			continue
		}
		prefix := ""
		content := line
		if m := prefixRe.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			content = m[2]
		}
		content = lrcAnyTagRe.ReplaceAllString(content, " ")
		content = strings.TrimSpace(multiSpace.ReplaceAllString(content, " "))
		out = append(out, prefix+content)
	}
	return strings.Join(out, "\n")
}

// extractLRCMetadata pulls [ti]/[ar]/[by] from the given tracks (first wins).
func extractLRCMetadata(tracks ...string) map[string]string {
	meta := map[string]string{}
	for _, track := range tracks {
		if track == "" {
			continue
		}
		for _, row := range splitLines(track) {
			line := strings.TrimSpace(row)
			if line == "" {
				continue
			}
			m := lrcMetaRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			key := strings.ToLower(m[1])
			val := strings.TrimSpace(m[2])
			if val != "" {
				if _, ok := meta[key]; !ok {
					meta[key] = val
				}
			}
		}
	}
	return meta
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
