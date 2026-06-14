package lyric

import "strings"

// lrcToLqe renders a Lyricify Quick Export document with separate lyric,
// translation, and pronunciation blocks. Mirrors lrcToLqe.
func lrcToLqe(lrc, tlyric, roma string, p Payload, tokenLyric string, romaFirst bool) string {
	meta := extractLRCMetadata(lrc, tlyric, roma)
	musicName := firstNonEmpty(p.MusicName, meta["ti"])
	artists := firstNonEmpty(p.Artist, meta["ar"])
	by := meta["by"]

	lyricsBody := sanitizeLRCTrackForLqe(lrc, false, nil)
	if strings.TrimSpace(tokenLyric) != "" {
		lyricsBody = tokenToLys(tokenLyric, "[4]", true)
	}

	translationBody := sanitizeLRCTrackForLqe(tlyric, true, nil)
	translationTagSet := extractLRCTagSet(translationBody)
	romaBody := sanitizeLRCTrackForLqe(roma, true, translationTagSet)

	var out []string
	out = append(out, "[Lyricify Quick Export]", "[version:1.0]")
	if musicName != "" {
		out = append(out, "[ti:"+musicName+"]")
	}
	if artists != "" {
		out = append(out, "[ar:"+artists+"]")
	}
	out = append(out, "[by:"+by+"]", "")

	out = append(out, "[lyrics: format@lys, language@und]")
	if musicName != "" {
		out = append(out, "[ti:"+musicName+"]")
	}
	if artists != "" {
		out = append(out, "[ar:"+artists+"]")
	}
	out = append(out, "[by:"+by+"]", "", lyricsBody)

	var translationBlock []string
	if translationBody != "" {
		translationBlock = append(translationBlock, "", "[translation: format@lrc, language@zh-Hans]")
		if musicName != "" {
			translationBlock = append(translationBlock, "[ti:"+musicName+"]")
		}
		if artists != "" {
			translationBlock = append(translationBlock, "[ar:"+artists+"]")
		}
		translationBlock = append(translationBlock, "[by:"+by+"]", translationBody)
	}

	var romaBlock []string
	if romaBody != "" {
		romaBlock = append(romaBlock, "", "[pronunciation: format@lrc, language@ja-Latn]")
		if musicName != "" {
			romaBlock = append(romaBlock, "[ti:"+musicName+"]")
		}
		if artists != "" {
			romaBlock = append(romaBlock, "[ar:"+artists+"]")
		}
		romaBlock = append(romaBlock, "[by:"+by+"]", romaBody)
	}

	translationStr := ""
	if len(translationBlock) > 0 {
		translationStr = strings.Join(translationBlock, "\n")
	}
	romaStr := ""
	if len(romaBlock) > 0 {
		romaStr = strings.Join(romaBlock, "\n")
	}
	for _, extra := range buildOrderedOutputLines(translationStr, romaStr, romaFirst) {
		if extra != "" {
			out = append(out, extra)
		}
	}

	return strings.Join(out, "\n")
}

// sanitizeLRCTrackForLqe re-tags an LRC track at millisecond precision,
// dropping empty/credit lines and (optionally) lines whose tags are not in
// allowTagSet. Mirrors sanitizeLrcTrackForLqe.
func sanitizeLRCTrackForLqe(track string, stripCreditLikeLines bool, allowTagSet map[string]bool) string {
	if strings.TrimSpace(track) == "" {
		return ""
	}
	entries := parseLRCEntries(track)
	if len(entries) == 0 {
		return ""
	}
	var out []string
	for _, e := range entries {
		text := strings.TrimSpace(e.Text)
		if text == "" || text == "//" {
			continue
		}
		if stripCreditLikeLines && isCreditLikeLine(text) {
			continue
		}
		tag := formatLRCTagFromMs(int(e.Time*1000+0.5), 3)
		if len(allowTagSet) > 0 && !allowTagSet[tag] {
			continue
		}
		out = append(out, tag+text)
	}
	return strings.Join(out, "\n")
}

func extractLRCTagSet(track string) map[string]bool {
	set := map[string]bool{}
	for _, e := range parseLRCEntries(track) {
		set[formatLRCTagFromMs(int(e.Time*1000+0.5), 3)] = true
	}
	return set
}
