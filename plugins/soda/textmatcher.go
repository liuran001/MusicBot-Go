package soda

import (
	"regexp"
	"strings"
)

type TextMatcher struct{}

var sodaURLPattern = regexp.MustCompile(`https?://[^\s]+`)

func NewTextMatcher() *TextMatcher { return &TextMatcher{} }

func (m *TextMatcher) MatchText(text string) (trackID string, matched bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	if prefix, value := parseSodaPrefix(text); prefix != "" && isNumericSodaID(value) {
		return value, true
	}
	if urlStr := extractSodaURL(text); urlStr != "" {
		if id, ok := NewURLMatcher().MatchURL(urlStr); ok {
			return id, true
		}
	}
	if isNumericSodaID(text) {
		return text, true
	}
	return "", false
}

func parseSodaPrefix(text string) (string, string) {
	parts := strings.SplitN(text, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	prefix := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch prefix {
	case "soda", "qs", "qishui", "汽水":
		return prefix, value
	default:
		return "", ""
	}
}

func extractSodaURL(text string) string {
	match := sodaURLPattern.FindString(text)
	match = strings.TrimRight(match, ".,!?)]}>")
	return strings.TrimSpace(match)
}
