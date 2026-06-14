package lyric

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
)

// krcLineRe matches a KRC lyric line "[lineStartMs,lineDurMs]rest".
var krcLineRe = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)

// krcWordRe matches a KRC word tag "<relStartMs,durMs,flag>".
var krcWordRe = regexp.MustCompile(`<(\d+),(\d+),(\d+)>`)

// krcLangTagRe matches the "[language:BASE64]" header carrying translation/roma.
var krcLangTagRe = regexp.MustCompile(`^\[language:(.*)\]$`)

// KRCResult holds the tracks extracted from a decrypted KRC document.
type KRCResult struct {
	// RawQRC is the KRC body re-encoded as a QRC-style token track (absolute
	// word timings), suitable for the converter's word-by-word pipeline.
	RawQRC string
	// Lyric is the line-timed LRC derived from the KRC body.
	Lyric string
	// Translation is the LRC translation track, if the KRC embedded one.
	Translation string
	// Roma is the LRC romanization track, if the KRC embedded one.
	Roma string
}

// ParseKRC converts decrypted KRC text into canonical tracks. KRC word tags are
// RELATIVE to their line start; this resolves them to absolute milliseconds and
// re-emits QRC-style tokens. Embedded "[language:...]" translation (type 1) and
// romanization (type 0) tracks are decoded and aligned by line index.
func ParseKRC(krc string) KRCResult {
	rows := splitLines(krc)

	var transContent [][]string // type 1: one string per line
	var romaContent [][]string  // type 0: per-word strings per line
	for _, row := range rows {
		line := strings.TrimSpace(row)
		m := krcLangTagRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		t, r := decodeKRCLanguages(m[1])
		if t != nil {
			transContent = t
		}
		if r != nil {
			romaContent = r
		}
	}

	var qrcLines []string
	var lrcLines []string
	var transLines []string
	var romaLines []string
	lineIdx := 0

	for _, row := range rows {
		line := strings.TrimSpace(row)
		if line == "" {
			continue
		}
		m := krcLineRe.FindStringSubmatch(line)
		if m == nil {
			continue // header tag ([ti:]/[ar:]/[language:]/...)
		}
		lineStart := mustAtoi(m[1])
		lineDur := mustAtoi(m[2])
		content := m[3]

		locs := krcWordRe.FindAllStringSubmatchIndex(content, -1)
		var qrcBody strings.Builder
		var plain strings.Builder
		if len(locs) == 0 {
			text := strings.TrimSpace(content)
			qrcBody.WriteString(text)
			plain.WriteString(text)
		} else {
			prev := 0
			// Text for word i is between tag i's end and tag i+1's start.
			for i, loc := range locs {
				relStart := mustAtoi(content[loc[2]:loc[3]])
				dur := mustAtoi(content[loc[4]:loc[5]])
				textStart := loc[1]
				textEnd := len(content)
				if i+1 < len(locs) {
					textEnd = locs[i+1][0]
				}
				_ = prev
				text := content[textStart:textEnd]
				abs := lineStart + relStart
				if text != "" {
					qrcBody.WriteString(text + "(" + itoa(abs) + "," + itoa(dur) + ")")
					plain.WriteString(text)
				}
			}
		}

		qrcLine := "[" + itoa(lineStart) + "," + itoa(lineDur) + "]" + qrcBody.String()
		qrcLines = append(qrcLines, qrcLine)
		lrcLines = append(lrcLines, formatLRCTagFromMs(lineStart, 2)+strings.TrimSpace(plain.String()))

		if lineIdx < len(transContent) {
			if t := joinKRCWords(transContent[lineIdx]); t != "" {
				transLines = append(transLines, formatLRCTagFromMs(lineStart, 2)+t)
			}
		}
		if lineIdx < len(romaContent) {
			if r := joinKRCWords(romaContent[lineIdx]); r != "" {
				romaLines = append(romaLines, formatLRCTagFromMs(lineStart, 2)+r)
			}
		}
		lineIdx++
	}

	return KRCResult{
		RawQRC:      strings.Join(qrcLines, "\n"),
		Lyric:       strings.Join(lrcLines, "\n"),
		Translation: strings.Join(transLines, "\n"),
		Roma:        strings.Join(romaLines, "\n"),
	}
}

func joinKRCWords(words []string) string {
	return strings.TrimSpace(strings.Join(words, ""))
}

// decodeKRCLanguages decodes the base64 JSON of a "[language:...]" tag, returning
// the translation track (type 1, one line per entry) and roma track (type 0,
// per-word entries). Either may be nil if absent.
func decodeKRCLanguages(b64 string) (translation, roma [][]string) {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil
	}
	var payload struct {
		Content []struct {
			Type         int        `json:"type"`
			LyricContent [][]string `json:"lyricContent"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, nil
	}
	for _, c := range payload.Content {
		switch c.Type {
		case 1:
			translation = c.LyricContent
		case 0:
			roma = c.LyricContent
		}
	}
	return translation, roma
}
