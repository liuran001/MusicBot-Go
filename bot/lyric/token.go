// Package lyric ports meting-api-new's LyricConverterService to Go. It parses
// the various platform word-by-word lyric formats (netease yrc, QQ qrc, kugou
// krc, Apple TTML) into a common token model and re-emits them to any of the
// supported target formats (lrc/yrc/qrc/lys/krc/elrc/spl/ass/lqe/ttml/amjson/
// srt/txt).
package lyric

import (
	"regexp"
	"strconv"
	"strings"
)

// tokenWord is a single timed word/syllable. Start/End are absolute
// milliseconds from the start of the track.
type tokenWord struct {
	Start int
	End   int
	Text  string
}

// tokenLine is a single lyric line with its word-level timing. Start/End are
// absolute milliseconds. Text is the concatenation of all word texts.
type tokenLine struct {
	Start  int
	End    int
	Text   string
	Tokens []tokenWord
}

var (
	// lineHeadRe matches the leading "[lineStart,lineDur]" tag shared by
	// yrc/qrc/lys/krc line formats.
	lineHeadRe = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)
	// wordTagRe matches a "(start,dur)" or "(start,dur,flag)" word tag.
	wordTagRe = regexp.MustCompile(`\((\d+),(\d+)(?:,(\d+))?\)`)
)

// parseTokenLines parses yrc/qrc/lys token text into canonical token lines.
//
// It mirrors LyricConverterService::parseTokenLines. Two on-wire shapes are
// supported, distinguished by whether the line content starts with "(":
//   - yrc:      [lineStart,lineDur](wStart,wDur,flag)text(wStart,wDur,flag)text
//   - qrc/lys:  [lineStart,lineDur]text(wStart,wDur)text(wStart,wDur)
//
// Word timestamps are absolute milliseconds in both shapes.
func parseTokenLines(token string) []tokenLine {
	rows := splitLines(token)
	out := make([]tokenLine, 0, len(rows))
	for _, row := range rows {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		m := lineHeadRe.FindStringSubmatch(row)
		if m == nil {
			continue
		}
		lineStart := mustAtoi(m[1])
		lineDur := mustAtoi(m[2])
		lineEnd := lineStart + lineDur
		content := m[3]

		var tokens []tokenWord
		if strings.HasPrefix(content, "(") {
			tokens = parseYRCWords(content)
		} else {
			tokens = parseQRCWords(content)
		}

		if len(tokens) == 0 {
			text := strings.TrimSpace(wordTagRe.ReplaceAllString(content, ""))
			if text != "" {
				tokens = []tokenWord{{Start: lineStart, End: lineEnd, Text: text}}
			}
		}

		var sb strings.Builder
		for _, tk := range tokens {
			sb.WriteString(tk.Text)
		}
		lineText := sb.String()
		if lineText == "" {
			continue
		}
		out = append(out, tokenLine{Start: lineStart, End: lineEnd, Text: lineText, Tokens: tokens})
	}
	return out
}

// parseYRCWords parses the yrc shape "(start,dur,flag)text...". The text for a
// tag is everything between the end of that tag and the start of the next tag
// (or end of string). This replaces the PHP lookahead regex, which RE2 lacks.
func parseYRCWords(content string) []tokenWord {
	locs := wordTagRe.FindAllStringSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return nil
	}
	tokens := make([]tokenWord, 0, len(locs))
	for i, loc := range locs {
		start := mustAtoi(content[loc[2]:loc[3]])
		dur := mustAtoi(content[loc[4]:loc[5]])
		textStart := loc[1]
		textEnd := len(content)
		if i+1 < len(locs) {
			textEnd = locs[i+1][0]
		}
		text := content[textStart:textEnd]
		if text == "" {
			continue
		}
		tokens = append(tokens, tokenWord{Start: start, End: start + dur, Text: text})
	}
	return tokens
}

// parseQRCWords parses the qrc/lys shape "text(start,dur)...". The text for a
// tag is everything between the previous tag (or start of string) and the tag.
func parseQRCWords(content string) []tokenWord {
	locs := wordTagRe.FindAllStringSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return nil
	}
	tokens := make([]tokenWord, 0, len(locs))
	prev := 0
	for _, loc := range locs {
		text := content[prev:loc[0]]
		start := mustAtoi(content[loc[2]:loc[3]])
		dur := mustAtoi(content[loc[4]:loc[5]])
		prev = loc[1]
		if text == "" {
			continue
		}
		tokens = append(tokens, tokenWord{Start: start, End: start + dur, Text: text})
	}
	return tokens
}

// resolveLineStartFromTokens returns the first valid token start, falling back
// to the line start. Mirrors LyricConverterService::resolveLineStartFromTokens.
func resolveLineStartFromTokens(line tokenLine) int {
	if len(line.Tokens) == 0 {
		return line.Start
	}
	for _, tk := range line.Tokens {
		if tk.Start >= 0 && tk.End >= tk.Start {
			return tk.Start
		}
	}
	return line.Start
}

// resolveLineEndFromNext returns the next line's resolved start when positive,
// else the current line end. Mirrors resolveLineEndFromNext.
func resolveLineEndFromNext(lines []tokenLine, idx int) int {
	cur := lines[idx].End
	if idx+1 >= len(lines) {
		return cur
	}
	nextStart := resolveLineStartFromTokens(lines[idx+1])
	if nextStart > 0 {
		return nextStart
	}
	return cur
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

func mustAtoi(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}
