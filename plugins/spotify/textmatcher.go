package spotify

import (
	"regexp"
	"strings"
)

// TextMatcher parses a track ID from arbitrary user text: an explicit
// "spotify:<id>" / "sp:<id>" alias prefix, a bare 22-char base-62 ID, an
// embedded open.spotify.com URL, or a spotify: URI.
type TextMatcher struct{}

// NewTextMatcher returns a TextMatcher.
func NewTextMatcher() *TextMatcher { return &TextMatcher{} }

var spotifyURLInText = regexp.MustCompile(`(?:https?://[^\s]+|spotify:[A-Za-z0-9:]+)`)

// MatchText extracts a track ID from text.
func (m *TextMatcher) MatchText(text string) (trackID string, matched bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}

	if prefix, value := parseSpotifyAlias(text); prefix != "" {
		if spotifyIDPattern.MatchString(value) {
			return value, true
		}
		if urlStr := spotifyURLInText.FindString(value); urlStr != "" {
			if id, ok := NewURLMatcher().MatchURL(trimURLPunct(urlStr)); ok {
				return id, true
			}
		}
		return "", false
	}

	if urlStr := spotifyURLInText.FindString(text); urlStr != "" {
		if id, ok := NewURLMatcher().MatchURL(trimURLPunct(urlStr)); ok {
			return id, true
		}
	}

	if spotifyIDPattern.MatchString(text) {
		return text, true
	}
	return "", false
}

// parseSpotifyAlias splits a "prefix:value" string and returns the prefix only
// if it is a recognized Spotify alias. The "spotify:track:<id>" URI form is NOT
// treated as an alias here (it has no bare value); URL matching handles it.
func parseSpotifyAlias(text string) (string, string) {
	parts := strings.SplitN(text, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	prefix := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch prefix {
	case "sp", "spot", "spotify":
		// Guard against the "spotify:track:..." URI being mis-split: if the
		// value still starts with a known resource kind, let URL matching own it.
		if prefix == "spotify" {
			low := strings.ToLower(value)
			if strings.HasPrefix(low, "track:") || strings.HasPrefix(low, "album:") ||
				strings.HasPrefix(low, "artist:") || strings.HasPrefix(low, "playlist:") ||
				strings.HasPrefix(low, "user:") {
				return "", ""
			}
		}
		return prefix, value
	default:
		return "", ""
	}
}

// trimURLPunct strips trailing sentence punctuation clinging to a pasted URL.
func trimURLPunct(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), ".,!?)]}>")
}
