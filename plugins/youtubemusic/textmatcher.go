package youtubemusic

import (
	"regexp"
	"strings"
)

// TextMatcher parses a track ID from arbitrary user text: an explicit
// "ytm:<id>" / "youtube:<id>" prefix, a bare 11-char video ID, or any embedded
// YouTube URL.
type TextMatcher struct{}

// NewTextMatcher returns a TextMatcher.
func NewTextMatcher() *TextMatcher { return &TextMatcher{} }

var ytURLInText = regexp.MustCompile(`https?://[^\s]+`)

// MatchText extracts a video ID from text. It recognizes, in order: an explicit
// platform prefix ("ytm:dQw4w9WgXcQ"), an embedded URL, then a bare video ID.
func (m *TextMatcher) MatchText(text string) (trackID string, matched bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}

	if prefix, value := parseYTMPrefix(text); prefix != "" {
		if videoIDPattern.MatchString(value) {
			return value, true
		}
		// A prefixed URL ("ytm: https://...") is still worth resolving.
		if urlStr := ytURLInText.FindString(value); urlStr != "" {
			if id, ok := NewURLMatcher().MatchURL(trimURLPunct(urlStr)); ok {
				return id, true
			}
		}
		return "", false
	}

	if urlStr := ytURLInText.FindString(text); urlStr != "" {
		if id, ok := NewURLMatcher().MatchURL(trimURLPunct(urlStr)); ok {
			return id, true
		}
	}

	if videoIDPattern.MatchString(text) {
		return text, true
	}
	return "", false
}

// parseYTMPrefix splits a "prefix:value" string and returns the prefix only if
// it is a recognized YouTube Music alias.
func parseYTMPrefix(text string) (string, string) {
	parts := strings.SplitN(text, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	prefix := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch prefix {
	case "ytm", "ytmusic", "youtubemusic", "youtube", "yt":
		return prefix, value
	default:
		return "", ""
	}
}

// trimURLPunct strips trailing sentence punctuation that commonly clings to a
// URL pasted mid-message.
func trimURLPunct(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), ".,!?)]}>")
}
