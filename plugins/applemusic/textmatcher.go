package applemusic

import (
	"regexp"
	"strings"
)

// TextMatcher matches Apple Music track IDs from text input.
type TextMatcher struct{}

var appleURLPattern = regexp.MustCompile(`https?://[^\s]+`)

// appleSongIDRe matches a purely numeric ID with at least 6 digits.
var appleSongIDRe = regexp.MustCompile(`^\d{6,}$`)

func NewTextMatcher() *TextMatcher { return &TextMatcher{} }

func (m *TextMatcher) MatchText(text string) (trackID string, matched bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	// Check for prefixed input: applemusic:ID, apple:ID, am:ID.
	if prefix, value := parseApplePrefix(text); prefix != "" && appleSongIDRe.MatchString(value) {
		return value, true
	}
	// Check for embedded Apple Music URL.
	if urlStr := extractAppleURL(text); urlStr != "" {
		if id, ok := NewURLMatcher().MatchURL(urlStr); ok {
			return id, true
		}
	}
	return "", false
}

func parseApplePrefix(text string) (string, string) {
	parts := strings.SplitN(text, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	prefix := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch prefix {
	case "applemusic", "apple", "am", "apple-music", "apple_music", "苹果音乐":
		return prefix, value
	default:
		return "", ""
	}
}

func extractAppleURL(text string) string {
	match := appleURLPattern.FindString(text)
	match = strings.TrimRight(match, ".,!?)]}>")
	return strings.TrimSpace(match)
}
