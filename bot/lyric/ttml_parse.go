package lyric

import (
	"encoding/xml"
	"strings"
)

// ttmlInDoc is a minimal TTML reader for Apple Music word-timed lyrics.
type ttmlInDoc struct {
	Body struct {
		Divs []struct {
			Ps []ttmlInP `xml:"p"`
		} `xml:"div"`
	} `xml:"body"`
}

type ttmlInP struct {
	Begin string       `xml:"begin,attr"`
	End   string       `xml:"end,attr"`
	Text  string       `xml:",chardata"`
	Spans []ttmlInSpan `xml:"span"`
}

type ttmlInSpan struct {
	Begin string       `xml:"begin,attr"`
	End   string       `xml:"end,attr"`
	Role  string       `xml:"role,attr"`
	Text  string       `xml:",chardata"`
	Spans []ttmlInSpan `xml:"span"`
}

// ttmlToTokenTrack parses an Apple Music TTML document into a QRC-style token
// track (absolute millisecond word timing) so the converter can re-emit any
// word-by-word format. Background/translation/roman spans are skipped — only
// the primary word spans are emitted. Returns "" when no word timing is found.
func ttmlToTokenTrack(ttmlXML string) string {
	var doc ttmlInDoc
	if err := xml.Unmarshal([]byte(ttmlXML), &doc); err != nil {
		return ""
	}
	var lines []string
	for _, div := range doc.Body.Divs {
		for _, p := range div.Ps {
			line := ttmlLineToToken(p)
			if line != "" {
				lines = append(lines, line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// ttmlHasWordSpans reports whether a TTML document contains primary word spans
// with timing — i.e. genuine word-by-word lyrics. Apple commonly serves
// line-level TTML (itunes:timing="Line") with no per-word spans; that returns
// false. Translation/romanization/background spans are ignored.
func ttmlHasWordSpans(ttmlXML string) bool {
	var doc ttmlInDoc
	if err := xml.Unmarshal([]byte(ttmlXML), &doc); err != nil {
		return false
	}
	for _, div := range doc.Body.Divs {
		for _, p := range div.Ps {
			for _, sp := range p.Spans {
				if ttmlSpanHasPrimaryWord(sp) {
					return true
				}
			}
		}
	}
	return false
}

// ttmlSpanHasPrimaryWord reports whether a span (or one of its nested spans) is
// a primary word span carrying timing and text. Non-primary roles
// (translation/romanization/background) are skipped.
func ttmlSpanHasPrimaryWord(sp ttmlInSpan) bool {
	role := strings.TrimSpace(strings.ToLower(sp.Role))
	if strings.Contains(role, "translation") || strings.Contains(role, "roman") || strings.Contains(role, "bg") {
		return false
	}
	if strings.TrimSpace(sp.Text) != "" && strings.TrimSpace(sp.Begin) != "" && strings.TrimSpace(sp.End) != "" {
		return true
	}
	for _, child := range sp.Spans {
		if ttmlSpanHasPrimaryWord(child) {
			return true
		}
	}
	return false
}

func ttmlLineToToken(p ttmlInP) string {
	lineStart := ttmlTimeToMs(p.Begin)
	lineEnd := ttmlTimeToMs(p.End)

	type word struct {
		start, end int
		text       string
	}
	var words []word
	for _, sp := range p.Spans {
		role := strings.TrimSpace(strings.ToLower(sp.Role))
		// Skip non-primary roles (translation, romanization, background).
		if strings.Contains(role, "translation") || strings.Contains(role, "roman") || strings.Contains(role, "bg") {
			continue
		}
		s := ttmlTimeToMs(sp.Begin)
		e := ttmlTimeToMs(sp.End)
		text := sp.Text
		if text == "" {
			continue
		}
		words = append(words, word{start: s, end: e, text: text})
	}

	if len(words) == 0 {
		// Line-level only: emit the whole text as a single token.
		text := strings.TrimSpace(p.Text)
		if text == "" {
			return ""
		}
		if lineEnd <= lineStart {
			lineEnd = lineStart
		}
		return "[" + itoa(lineStart) + "," + itoa(max0(lineEnd-lineStart)) + "]" + text + "(" + itoa(lineStart) + "," + itoa(max0(lineEnd-lineStart)) + ")"
	}

	if lineStart == 0 && len(words) > 0 {
		lineStart = words[0].start
	}
	if lineEnd <= lineStart {
		lineEnd = words[len(words)-1].end
	}

	var sb strings.Builder
	sb.WriteString("[" + itoa(lineStart) + "," + itoa(max0(lineEnd-lineStart)) + "]")
	for _, w := range words {
		dur := max0(w.end - w.start)
		sb.WriteString(w.text + "(" + itoa(w.start) + "," + itoa(dur) + ")")
	}
	return sb.String()
}

// ttmlTimeToMs parses a TTML clock value ("HH:MM:SS.mmm", "MM:SS.mmm",
// "SS.mmm", or "SSs") to milliseconds.
func ttmlTimeToMs(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if strings.HasSuffix(v, "s") {
		// Offset form like "12.5s".
		sec := strings.TrimSuffix(v, "s")
		return parseSecondsToMs(sec)
	}
	parts := strings.Split(v, ":")
	var h, m int
	var sPart string
	switch len(parts) {
	case 3:
		h = mustAtoi(parts[0])
		m = mustAtoi(parts[1])
		sPart = parts[2]
	case 2:
		m = mustAtoi(parts[0])
		sPart = parts[1]
	case 1:
		sPart = parts[0]
	default:
		return 0
	}
	secMs := parseSecondsToMs(sPart)
	return h*3600000 + m*60000 + secMs
}

// parseSecondsToMs parses "SS.fff" to milliseconds.
func parseSecondsToMs(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	whole := s
	frac := ""
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		whole = s[:dot]
		frac = s[dot+1:]
	}
	ms := mustAtoi(whole) * 1000
	if frac != "" {
		ms += parseLRCFractionToMs(frac)
	}
	return ms
}
