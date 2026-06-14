package lyric

import (
	"encoding/json"
	"regexp"
	"strings"
)

// xmlEscape escapes text for XML attribute/content (ENT_QUOTES | ENT_XML1).
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// lrcToTTML renders line-timed LRC to AMLL-flavored TTML. Mirrors lrcToTtml.
func lrcToTTML(lrc, tlyric string, p Payload, roma string, inlineTracks, romaFirst bool) string {
	entries := parseLRCEntries(lrc)
	if len(entries) == 0 {
		return buildTTMLDocument(nil, 0, 0, p, "")
	}
	translationMap := parseTranslationMap(tlyric)
	translationEntries := parseTranslationEntries(tlyric)
	romaMap := parseTranslationMap(roma)
	romaEntries := parseTranslationEntries(roma)
	translationsByKey := map[string]string{}
	romasByKey := map[string]string{}
	var orderedKeys []string

	var lines []string
	for i, e := range entries {
		start := e.Time
		var end float64
		if i+1 < len(entries) {
			end = entries[i+1].Time
		} else {
			end = start + 3.0
		}
		text := xmlEscape(e.Text)
		translation := strings.TrimSpace(translationMap[e.Tag])
		if (translation == "" || translation == "//") && !isCreditLikeLine(e.Text) {
			translation = findNearestTranslationText(translationEntries, start, 0.5)
		}
		lineKey := "L" + itoa(i+1)
		romaLine := strings.TrimSpace(romaMap[e.Tag])
		if (romaLine == "" || romaLine == "//") && !isCreditLikeLine(e.Text) {
			romaLine = findNearestTranslationText(romaEntries, start, 0.5)
		}

		line := "<p begin=\"" + secondsToTTMLTime(start) + "\" end=\"" + secondsToTTMLTime(end) + "\" itunes:key=\"" + lineKey + "\" ttm:agent=\"v1\">" + text
		if translation != "" && translation != "//" {
			if _, seen := translationsByKey[lineKey]; !seen {
				orderedKeys = appendKey(orderedKeys, lineKey, translationsByKey, romasByKey)
			}
			translationsByKey[lineKey] = strings.TrimSpace(translation)
		}
		if romaLine != "" && romaLine != "//" {
			if _, seen := romasByKey[lineKey]; !seen {
				orderedKeys = appendKey(orderedKeys, lineKey, translationsByKey, romasByKey)
			}
			romasByKey[lineKey] = strings.TrimSpace(romaLine)
		}

		if inlineTracks {
			translationSpan := ""
			if translation != "" && translation != "//" {
				translationSpan = "<span ttm:role=\"x-translation\" xml:lang=\"zh-Hans\">" + xmlEscape(translation) + "</span>"
			}
			romaSpan := ""
			if romaLine != "" && romaLine != "//" {
				romaSpan = "<span ttm:role=\"x-roman\" xml:lang=\"ja-Latn\">" + xmlEscape(romaLine) + "</span>"
			}
			for _, extra := range buildOrderedOutputLines(translationSpan, romaSpan, romaFirst) {
				line += extra
			}
		}
		line += "</p>"
		lines = append(lines, "      "+line)
	}
	firstBegin := entries[0].Time
	duration := entries[len(entries)-1].Time + 3.0
	itunesMetadata := ""
	if !inlineTracks {
		itunesMetadata = buildITunesMetadataLocalizations(orderedKeys, translationsByKey, romasByKey, romaFirst)
	}
	return buildTTMLDocument(lines, duration, firstBegin, p, itunesMetadata)
}

// tokenToTTML renders token lines to word-timed TTML. Mirrors tokenToTtml,
// including the background-line folding for short echo lines.
func tokenToTTML(token, tlyric string, p Payload, roma string, inlineTracks, romaFirst bool) string {
	lines := parseTokenLines(token)
	if len(lines) == 0 {
		return lrcToTTML(tokenToLRC(token), tlyric, p, roma, inlineTracks, romaFirst)
	}
	translationMap := parseTranslationMap(tlyric)
	translationEntries := parseTranslationEntries(tlyric)
	romaMap := parseTranslationMap(roma)
	romaEntries := parseTranslationEntries(roma)
	translationsByKey := map[string]string{}
	romasByKey := map[string]string{}
	var orderedKeys []string
	skip := map[int]bool{}

	var pLines []string
	for idx := 0; idx < len(lines); idx++ {
		if skip[idx] {
			continue
		}
		line := lines[idx]
		var spans []string
		var bgInner []string
		bgStartMs, bgEndMs := -1, -1

		flushBg := func() {
			if len(bgInner) == 0 {
				return
			}
			start := bgStartMs
			if start < 0 {
				start = line.Start
			}
			end := bgEndMs
			if end < 0 {
				end = line.End
			}
			if end < start {
				end = start
			}
			spans = append(spans, "<span ttm:role=\"x-bg\" begin=\""+secondsToTTMLTime(float64(start)/1000.0)+"\" end=\""+secondsToTTMLTime(float64(end)/1000.0)+"\">"+strings.Join(bgInner, "")+"</span>")
			bgInner = nil
			bgStartMs, bgEndMs = -1, -1
		}

		for _, tk := range line.Tokens {
			txt := xmlEscape(tk.Text)
			startMs := tk.Start
			endMs := tk.End
			isBg := isBackgroundTokenText(tk.Text)

			tokenSpan := ""
			if endMs <= startMs || startMs < 0 || endMs <= 0 {
				tokenSpan = txt
			} else {
				tokenSpan = "<span begin=\"" + secondsToTTMLTime(float64(startMs)/1000.0) + "\" end=\"" + secondsToTTMLTime(float64(endMs)/1000.0) + "\">" + txt + "</span>"
			}

			if isBg {
				bgInner = append(bgInner, tokenSpan)
				if startMs > 0 && (bgStartMs < 0 || startMs < bgStartMs) {
					bgStartMs = startMs
				}
				if endMs > 0 && (bgEndMs < 0 || endMs > bgEndMs) {
					bgEndMs = endMs
				}
				continue
			}
			flushBg()
			spans = append(spans, tokenSpan)
		}
		flushBg()

		lineStartMs := resolveLineStartFromTokens(line)
		lineEndMs := resolveLineEndFromNext(lines, idx)
		lineKey := "L" + itoa(idx+1)
		mainContent := strings.Join(spans, "")
		if mainContent == "" {
			mainContent = "<span>" + xmlEscape(line.Text) + "</span>"
		}
		base := "<p begin=\"" + secondsToTTMLTime(float64(lineStartMs)/1000.0) + "\" end=\"" + secondsToTTMLTime(float64(lineEndMs)/1000.0) + "\" itunes:key=\"" + lineKey + "\" ttm:agent=\"v1\">" + mainContent

		tag := formatLRCTagFromMs(lineStartMs, 2)
		translation := strings.TrimSpace(translationMap[tag])
		if (translation == "" || translation == "//") && !isCreditLikeLine(line.Text) {
			translation = findNearestTranslationText(translationEntries, float64(lineStartMs)/1000.0, 0.5)
		}
		if translation != "" && translation != "//" {
			if _, seen := translationsByKey[lineKey]; !seen {
				orderedKeys = appendKey(orderedKeys, lineKey, translationsByKey, romasByKey)
			}
			translationsByKey[lineKey] = strings.TrimSpace(translation)
		}
		romaLine := strings.TrimSpace(romaMap[tag])
		if (romaLine == "" || romaLine == "//") && !isCreditLikeLine(line.Text) {
			romaLine = findNearestTranslationText(romaEntries, float64(lineStartMs)/1000.0, 0.5)
		}
		if romaLine != "" && romaLine != "//" {
			if _, seen := romasByKey[lineKey]; !seen {
				orderedKeys = appendKey(orderedKeys, lineKey, translationsByKey, romasByKey)
			}
			romasByKey[lineKey] = strings.TrimSpace(romaLine)
		}

		if inlineTracks {
			translationSpan := ""
			if translation != "" && translation != "//" {
				translationSpan = "<span ttm:role=\"x-translation\" xml:lang=\"zh-Hans\">" + xmlEscape(strings.TrimSpace(translation)) + "</span>"
			}
			romaSpan := ""
			if romaLine != "" && romaLine != "//" {
				romaSpan = "<span ttm:role=\"x-roman\" xml:lang=\"ja-Latn\">" + xmlEscape(strings.TrimSpace(romaLine)) + "</span>"
			}
			for _, extra := range buildOrderedOutputLines(translationSpan, romaSpan, romaFirst) {
				base += extra
			}
		}

		// Fold a short overlapping echo line into x-bg.
		if idx+1 < len(lines) && !skip[idx+1] {
			nextLine := lines[idx+1]
			if shouldAttachAsBackgroundLine(line, nextLine) {
				var inner []string
				for _, ntk := range nextLine.Tokens {
					ntxt := xmlEscape(ntk.Text)
					ns := ntk.Start
					ne := ntk.End
					if ne <= ns || ns <= 0 || ne <= 0 {
						inner = append(inner, ntxt)
					} else {
						inner = append(inner, "<span begin=\""+secondsToTTMLTime(float64(ns)/1000.0)+"\" end=\""+secondsToTTMLTime(float64(ne)/1000.0)+"\">"+ntxt+"</span>")
					}
				}
				if len(inner) > 0 {
					bgStart := nextLine.Start
					bgEnd := nextLine.End
					if bgEnd < bgStart {
						bgEnd = bgStart
					}
					base += "<span ttm:role=\"x-bg\" begin=\"" + secondsToTTMLTime(float64(bgStart)/1000.0) + "\" end=\"" + secondsToTTMLTime(float64(bgEnd)/1000.0) + "\">" + strings.Join(inner, "") + "</span>"
					skip[idx+1] = true
				}
			}
		}

		base += "</p>"
		pLines = append(pLines, "      "+base)
	}

	firstBegin := float64(lines[0].Start) / 1000.0
	duration := float64(lines[len(lines)-1].End) / 1000.0
	itunesMetadata := ""
	if !inlineTracks {
		itunesMetadata = buildITunesMetadataLocalizations(orderedKeys, translationsByKey, romasByKey, romaFirst)
	}
	return buildTTMLDocument(pLines, duration, firstBegin, p, itunesMetadata)
}

// appendKey records first-seen key order across both side-track maps.
func appendKey(keys []string, key string, m1, m2 map[string]string) []string {
	if _, ok := m1[key]; ok {
		return keys
	}
	if _, ok := m2[key]; ok {
		return keys
	}
	return append(keys, key)
}

var bgOpenRe = regexp.MustCompile(`^[（(]`)
var bgCloseRe = regexp.MustCompile(`[)）]$`)

func isBackgroundTokenText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return bgOpenRe.MatchString(text) || bgCloseRe.MatchString(text)
}

var comparablePunctRe = regexp.MustCompile(`[\s\p{P}\p{S}]+`)

func normalizeComparableText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	return comparablePunctRe.ReplaceAllString(text, "")
}

func shouldAttachAsBackgroundLine(mainLine, nextLine tokenLine) bool {
	mainStart, mainEnd := mainLine.Start, mainLine.End
	nextStart, nextEnd := nextLine.Start, nextLine.End
	if nextStart <= 0 || nextEnd <= nextStart || mainEnd <= mainStart {
		return false
	}
	if nextStart >= mainEnd {
		return false
	}
	if nextEnd-nextStart > 1400 {
		return false
	}
	if len(nextLine.Tokens) > 6 {
		return false
	}
	mainText := normalizeComparableText(mainLine.Text)
	nextText := normalizeComparableText(nextLine.Text)
	if mainText == "" || nextText == "" {
		return false
	}
	if strings.Contains(mainText, nextText) {
		return true
	}
	return strings.HasSuffix(mainText, nextText)
}

func buildTTMLDocument(pLines []string, durationSeconds, divBeginSeconds float64, p Payload, itunesMetadata string) string {
	ncmMusicID := p.NcmMusicID
	qqMusicID := p.QqMusicID
	if p.Source == "netease" && ncmMusicID == "" {
		ncmMusicID = ""
	}

	meta := []string{"      <ttm:agent type=\"person\" xml:id=\"v1\"/>"}
	if itunesMetadata != "" {
		meta = append(meta, itunesMetadata)
	}
	meta = append(meta,
		"      <amll:meta key=\"musicName\" value=\""+xmlEscape(p.MusicName)+"\"/>",
		"      <amll:meta key=\"artists\" value=\""+xmlEscape(p.Artist)+"\"/>",
		"      <amll:meta key=\"album\" value=\""+xmlEscape(p.Album)+"\"/>",
	)
	if ncmMusicID != "" {
		meta = append(meta, "      <amll:meta key=\"ncmMusicId\" value=\""+xmlEscape(ncmMusicID)+"\"/>")
	}
	if qqMusicID != "" {
		meta = append(meta, "      <amll:meta key=\"qqMusicId\" value=\""+xmlEscape(qqMusicID)+"\"/>")
	}

	dur := secondsToTTMLTime(maxFloat(0, durationSeconds))
	divBegin := secondsToTTMLTime(maxFloat(0, divBeginSeconds))
	return "<tt xmlns=\"http://www.w3.org/ns/ttml\" xmlns:amll=\"http://www.example.com/ns/amll\" xmlns:itunes=\"http://music.apple.com/lyric-ttml-internal\" xmlns:ttm=\"http://www.w3.org/ns/ttml#metadata\" itunes:timing=\"Word\">\n" +
		"  <head>\n" +
		"    <metadata>\n" +
		strings.Join(meta, "\n") +
		"\n    </metadata>\n" +
		"  </head>\n" +
		"  <body dur=\"" + dur + "\">\n" +
		"    <div begin=\"" + divBegin + "\" end=\"" + dur + "\">\n" +
		strings.Join(pLines, "\n") +
		"\n    </div>\n" +
		"  </body>\n" +
		"</tt>"
}

func buildITunesMetadataLocalizations(orderedKeys []string, translationsByKey, romasByKey map[string]string, romaFirst bool) string {
	hasTranslation := len(translationsByKey) > 0
	hasRoma := len(romasByKey) > 0
	if !hasTranslation && !hasRoma {
		return ""
	}

	out := []string{"      <iTunesMetadata xmlns=\"http://music.apple.com/lyric-ttml-internal\">"}

	translationBlock := ""
	if hasTranslation {
		lines := []string{"        <translations>", "          <translation type=\"subtitle\" xml:lang=\"zh-Hans\">"}
		for _, key := range orderedKeys {
			if text, ok := translationsByKey[key]; ok {
				lines = append(lines, "            <text for=\""+xmlEscape(key)+"\">"+xmlEscape(text)+"</text>")
			}
		}
		lines = append(lines, "          </translation>", "        </translations>")
		translationBlock = strings.Join(lines, "\n")
	}

	romaBlock := ""
	if hasRoma {
		lines := []string{"        <transliterations>", "          <transliteration xml:lang=\"ja-Latn\">"}
		for _, key := range orderedKeys {
			if text, ok := romasByKey[key]; ok {
				lines = append(lines, "            <text for=\""+xmlEscape(key)+"\">"+xmlEscape(text)+"</text>")
			}
		}
		lines = append(lines, "          </transliteration>", "        </transliterations>")
		romaBlock = strings.Join(lines, "\n")
	}

	for _, block := range buildOrderedOutputLines(translationBlock, romaBlock, romaFirst) {
		if block != "" {
			out = append(out, block)
		}
	}
	out = append(out, "      </iTunesMetadata>")
	return strings.Join(out, "\n")
}

var xmlDeclRe = regexp.MustCompile(`(?s)^\s*<\?xml[^>]*\?>\s*`)
var xmlGapRe = regexp.MustCompile(`(?s)>\s+<`)

func compactTTMLForAmjson(ttml string) string {
	ttml = xmlDeclRe.ReplaceAllString(ttml, "")
	ttml = xmlGapRe.ReplaceAllString(ttml, "><")
	return strings.TrimSpace(ttml)
}

func ttmlToAppleMusicJSON(ttml string) string {
	compact := compactTTMLForAmjson(ttml)
	doc := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{
				"id":   "",
				"type": "syllable-lyrics",
				"attributes": map[string]interface{}{
					"playParams": map[string]interface{}{
						"catalogId":   "",
						"displayType": 3,
						"id":          "AP_",
						"kind":        "lyric",
					},
					"ttmlLocalizations": compact,
				},
			},
		},
	}
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return ""
	}
	return strings.TrimRight(sb.String(), "\n")
}
