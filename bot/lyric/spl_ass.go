package lyric

import (
	"regexp"
	"strings"
)

// --- SPL (Salt Player Lyrics) ---

// lrcToSpl renders line-timed LRC to SPL with optional translation/roma.
// Mirrors lrcToSpl.
func lrcToSpl(lrc, tlyric, roma string, romaFirst bool) string {
	entries := parseLRCEntries(lrc)
	if len(entries) == 0 {
		return ""
	}
	translationMap := parseTranslationMap(tlyric)
	romaMap := parseTranslationMap(roma)
	adjacentMap := parseAdjacentTranslationMap(lrc)
	romaEntries := parseTranslationEntries(roma)

	var out []string
	for _, e := range entries {
		out = append(out, formatSplLineModeTag(e.Time)+e.Text)

		translation := strings.TrimSpace(translationMap[e.Tag])
		if (translation == "" || translation == "//") && !isCreditLikeLine(e.Text) {
			translation = strings.TrimSpace(adjacentMap[e.Tag])
		}
		romaLine := strings.TrimSpace(romaMap[e.Tag])
		if (romaLine == "" || romaLine == "//") && !isCreditLikeLine(e.Text) {
			romaLine = findNearestTranslationText(romaEntries, e.Time, 0.5)
		}

		translationLine := ""
		if translation != "" && translation != "//" {
			translationLine = formatSplLineModeTag(e.Time) + translation
		}
		romaOutputLine := ""
		if romaLine != "" && romaLine != "//" && romaLine != translation {
			romaOutputLine = formatSplLineModeTag(e.Time) + romaLine
		}
		out = append(out, buildOrderedOutputLines(translationLine, romaOutputLine, romaFirst)...)
	}
	return strings.Join(out, "\n")
}

// tokenToSpl renders token lines to SPL with per-word "<mm:ss.cc>" timing.
// Mirrors tokenToSpl.
func tokenToSpl(token, tlyric, roma string, romaFirst bool) string {
	lines := parseTokenLines(token)
	if len(lines) == 0 {
		return lrcToSpl(tokenToLRC(token), tlyric, roma, romaFirst)
	}
	translationMap := parseTranslationMap(tlyric)
	translationEntries := parseTranslationEntries(tlyric)
	romaMap := parseTranslationMap(roma)
	romaEntries := parseTranslationEntries(roma)

	var out []string
	for _, line := range lines {
		lineText := formatSplTimestamp(line.Start, false)
		lastEnd := -1
		for _, tk := range line.Tokens {
			if lastEnd < 0 || tk.Start != lastEnd {
				lineText += formatSplTimestamp(tk.Start, true)
			}
			lineText += tk.Text
			lineText += formatSplTimestamp(tk.End, true)
			lastEnd = tk.End
		}
		if line.End > 0 && (lastEnd < 0 || line.End > lastEnd) {
			lineText += formatSplTimestamp(line.End, false)
		}
		out = append(out, lineText)

		tag := formatLRCTagFromMs(line.Start, 2)
		translation := strings.TrimSpace(translationMap[tag])
		if (translation == "" || translation == "//") && !isCreditLikeLine(line.Text) {
			translation = findNearestTranslationText(translationEntries, float64(line.Start)/1000.0, 0.5)
		}
		romaLine := strings.TrimSpace(romaMap[tag])
		if (romaLine == "" || romaLine == "//") && !isCreditLikeLine(line.Text) {
			romaLine = findNearestTranslationText(romaEntries, float64(line.Start)/1000.0, 0.5)
		}

		translationLine := ""
		if translation != "" && translation != "//" {
			translationLine = formatSplTimestamp(line.Start, false) + translation
		}
		romaOutputLine := ""
		if romaLine != "" && romaLine != "//" && romaLine != translation {
			romaOutputLine = formatSplTimestamp(line.Start, false) + romaLine
		}
		out = append(out, buildOrderedOutputLines(translationLine, romaOutputLine, romaFirst)...)
	}
	return strings.Join(out, "\n")
}

var adjTagPrefixRe = regexp.MustCompile(`^\[([0-9]{1,2}):([0-9]{1,2})(?:[.:]([0-9]{1,3}))?\]`)

// parseAdjacentTranslationMap maps a line's tag to the following untimed line,
// used as a translation fallback. Mirrors parseAdjacentTranslationMapFromLyric.
func parseAdjacentTranslationMap(lrc string) map[string]string {
	m := map[string]string{}
	rows := splitLines(lrc)
	for i := 0; i < len(rows); i++ {
		current := strings.TrimSpace(rows[i])
		if current == "" {
			continue
		}
		hm := adjTagPrefixRe.FindStringSubmatch(current)
		if hm == nil {
			continue
		}
		min := mustAtoi(hm[1])
		sec := mustAtoi(hm[2])
		ms := parseLRCFractionToMs(hm[3])
		tag := formatLRCTagFromParts(min, sec, ms)

		j := i + 1
		for j < len(rows) && strings.TrimSpace(rows[j]) == "" {
			j++
		}
		if j >= len(rows) {
			continue
		}
		next := strings.TrimSpace(rows[j])
		if next == "" {
			continue
		}
		if adjTagPrefixRe.MatchString(next) {
			continue
		}
		m[tag] = next
	}
	return m
}

// --- ASS (Advanced SubStation Alpha) ---

func lrcToAss(lrc, tlyric, roma string, romaFirst bool) string {
	entries := parseLRCEntries(lrc)
	if len(entries) == 0 {
		return ""
	}
	translationMap := parseTranslationMap(tlyric)
	romaMap := parseTranslationMap(roma)
	romaEntries := parseTranslationEntries(roma)

	var dialogues []string
	for i, e := range entries {
		start := e.Time
		var end float64
		if i+1 < len(entries) {
			end = maxFloat(start+0.3, entries[i+1].Time-0.01)
		} else {
			end = start + 3.0
		}
		dialogues = append(dialogues, "Dialogue: 0,"+secondsToASSTime(start)+","+secondsToASSTime(end)+",Default,v1,0,0,0,,"+escapeAssText(e.Text))

		translation := strings.TrimSpace(translationMap[e.Tag])
		romaji := strings.TrimSpace(romaMap[e.Tag])
		if romaji == "" || romaji == "//" {
			romaji = findNearestTranslationText(romaEntries, start, 0.5)
		}

		translationDialogue := ""
		if translation != "" && translation != "//" {
			translationDialogue = "Dialogue: 0," + secondsToASSTime(start) + "," + secondsToASSTime(end) + ",ts,x-lang:zh-Hans,0,0,0,," + escapeAssText(translation)
		}
		romaDialogue := ""
		if romaji != "" && romaji != "//" {
			romaDialogue = "Dialogue: 0," + secondsToASSTime(start) + "," + secondsToASSTime(end) + ",roma,x-lang:ja-Latn,0,0,0,," + escapeAssText(romaji)
		}
		dialogues = append(dialogues, buildOrderedOutputLines(translationDialogue, romaDialogue, romaFirst)...)
	}
	return buildAssDocument(dialogues)
}

func tokenToAss(token, tlyric, roma string, romaFirst bool) string {
	lines := parseTokenLines(token)
	if len(lines) == 0 {
		return lrcToAss(tokenToLRC(token), tlyric, roma, romaFirst)
	}
	translationMap := parseTranslationMap(tlyric)
	translationEntries := parseTranslationEntries(tlyric)
	romaMap := parseTranslationMap(roma)
	romaEntries := parseTranslationEntries(roma)

	var dialogues []string
	for i, line := range lines {
		startMs := resolveLineStartFromTokens(line)
		endMs := resolveLineEndFromNext(lines, i)
		if endMs <= startMs {
			endMs = startMs + 3000
		}
		karaoke := buildAssKaraokeFromTokens(line.Tokens, line.Text)
		dialogues = append(dialogues, "Dialogue: 0,"+secondsToASSTime(float64(startMs)/1000.0)+","+secondsToASSTime(float64(endMs)/1000.0)+",Default,v1,0,0,0,,"+karaoke)

		tag := formatLRCTagFromMs(startMs, 2)
		translation := strings.TrimSpace(translationMap[tag])
		if translation == "" || translation == "//" {
			translation = findNearestTranslationText(translationEntries, float64(startMs)/1000.0, 0.5)
		}
		romaji := strings.TrimSpace(romaMap[tag])
		if romaji == "" || romaji == "//" {
			romaji = findNearestTranslationText(romaEntries, float64(startMs)/1000.0, 0.5)
		}

		translationDialogue := ""
		if translation != "" && translation != "//" {
			translationDialogue = "Dialogue: 0," + secondsToASSTime(float64(startMs)/1000.0) + "," + secondsToASSTime(float64(endMs)/1000.0) + ",ts,x-lang:zh-Hans,0,0,0,," + escapeAssText(translation)
		}
		romaDialogue := ""
		if romaji != "" && romaji != "//" {
			romaKaraoke := buildAssKaraokeFromRomaLine(romaji, startMs, endMs)
			romaDialogue = "Dialogue: 0," + secondsToASSTime(float64(startMs)/1000.0) + "," + secondsToASSTime(float64(endMs)/1000.0) + ",roma,x-lang:ja-Latn,0,0,0,," + romaKaraoke
		}
		dialogues = append(dialogues, buildOrderedOutputLines(translationDialogue, romaDialogue, romaFirst)...)
	}
	return buildAssDocument(dialogues)
}

func buildAssKaraokeFromTokens(tokens []tokenWord, fallbackText string) string {
	if len(tokens) == 0 {
		return escapeAssText(fallbackText)
	}
	var sb strings.Builder
	for _, tk := range tokens {
		if tk.Text == "" {
			continue
		}
		durCs := roundDiv(max0(tk.End-tk.Start), 10)
		if durCs <= 0 {
			durCs = 1
		}
		sb.WriteString("{\\k" + itoa(durCs) + "}" + escapeAssText(tk.Text))
	}
	if sb.Len() == 0 {
		return escapeAssText(fallbackText)
	}
	return sb.String()
}

var romaInlineTagRe = regexp.MustCompile(`\[([0-9]{1,2}):([0-9]{2})(?:[.:]([0-9]{1,3}))?\]`)

func buildAssKaraokeFromRomaLine(romaLine string, lineStartMs, lineEndMs int) string {
	if romaLine == "" {
		return ""
	}
	locs := romaInlineTagRe.FindAllStringSubmatchIndex(romaLine, -1)
	if len(locs) == 0 {
		return escapeAssText(strings.TrimSpace(romaLine))
	}
	type seg struct {
		start, end int
		text       string
	}
	var segments []seg
	currentStart := lineStartMs
	prev := 0
	for _, loc := range locs {
		segText := romaLine[prev:loc[0]]
		min := romaLine[loc[2]:loc[3]]
		sec := romaLine[loc[4]:loc[5]]
		frac := "0"
		if loc[6] >= 0 {
			frac = romaLine[loc[6]:loc[7]]
		}
		tsMs := parseInlineTimeTagToMs(min, sec, frac)
		prev = loc[1]
		if tsMs <= currentStart {
			continue
		}
		if segText != "" {
			segments = append(segments, seg{start: currentStart, end: tsMs, text: segText})
		}
		currentStart = tsMs
	}
	tail := romaLine[prev:]
	if tail != "" && lineEndMs > currentStart {
		segments = append(segments, seg{start: currentStart, end: lineEndMs, text: tail})
	}
	if len(segments) == 0 {
		return escapeAssText(strings.TrimSpace(romaInlineTagRe.ReplaceAllString(romaLine, " ")))
	}
	var sb strings.Builder
	for _, s := range segments {
		if s.text == "" {
			continue
		}
		durCs := roundDiv(max0(s.end-s.start), 10)
		if durCs <= 0 {
			durCs = 1
		}
		sb.WriteString("{\\k" + itoa(durCs) + "}" + escapeAssText(s.text))
	}
	if sb.Len() == 0 {
		return escapeAssText(strings.TrimSpace(romaLine))
	}
	return sb.String()
}

func parseInlineTimeTagToMs(min, sec, frac string) int {
	msRaw := frac
	if msRaw == "" {
		msRaw = "0"
	}
	switch len(msRaw) {
	case 1:
		msRaw += "00"
	case 2:
		msRaw += "0"
	}
	ms := 0
	if len(msRaw) >= 3 {
		ms = mustAtoi(msRaw[:3])
	}
	return mustAtoi(min)*60000 + mustAtoi(sec)*1000 + ms
}

func buildAssDocument(dialogues []string) string {
	return "[Script Info]\n" +
		"ScriptType: v4.00+\n" +
		"PlayResX: 1920\n" +
		"PlayResY: 1080\n" +
		"\n[V4+ Styles]\n" +
		"Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
		"Style: Default,Arial,100,&H00FFFFFF,&H003F3F3F,&H00000000,&H00000000,-1,0,0,0,100,100,0,0,1,2,1,2,10,10,10,1\n" +
		"Style: Orig,Arial,100,&H00FFFFFF,&H003F3F3F,&H00000000,&H00000000,-1,0,0,0,100,100,0,0,1,2,1,2,10,10,10,1\n" +
		"Style: ts,Arial,55,&H00D3D3D3,&H000000FF,&H00000000,&H99000000,0,0,0,0,100,100,0,0,1,2,1,2,10,10,50,1\n" +
		"Style: roma,Arial,55,&H00D3D3D3,&H000000FF,&H00000000,&H99000000,0,0,0,0,100,100,0,0,1,2,1,2,10,10,50,1\n" +
		"Style: bg-ts,Arial,45,&H00A0A0A0,&H000000FF,&H00000000,&H99000000,0,0,0,0,100,100,0,0,1,1.5,1,8,10,10,55,1\n" +
		"Style: bg-roma,Arial,45,&H00A0A0A0,&H000000FF,&H00000000,&H99000000,0,0,0,0,100,100,0,0,1,1.5,1,8,10,10,55,1\n" +
		"Style: meta,Arial,40,&H00C0C0C0,&H000000FF,&H00000000,&H99000000,0,0,0,0,100,100,0,0,1,1,0,5,10,10,10,1\n" +
		"\n[Events]\n" +
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		strings.Join(dialogues, "\n") +
		"\n"
}

func escapeAssText(text string) string {
	text = strings.ReplaceAll(text, "\r", "")
	text = strings.ReplaceAll(text, "\n", "\\N")
	return strings.ReplaceAll(text, ",", "，")
}

func roundDiv(value, divisor int) int {
	if divisor == 0 {
		return 0
	}
	return (value + divisor/2) / divisor
}
