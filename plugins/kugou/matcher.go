package kugou

import (
	"net/url"
	"regexp"
	"strings"
)

type URLMatcher struct{}

var (
	kugouHashPattern         = regexp.MustCompile(`(?i)hash=([a-f0-9]{32})`)
	kugouSongPathHashPattern = regexp.MustCompile(`(?i)/song/[^?#]*#hash=([a-f0-9]{32})`)
	kugouPathHashPattern     = regexp.MustCompile(`(?i)/(?:song|share)/(?:[^/?#]+/)?([a-f0-9]{32})(?:[/?#]|$)`)
	kugouPlaylistPattern     = regexp.MustCompile(`(?i)special/single/(\d+)\.html`)
	kugouPlaylistPathPattern = regexp.MustCompile(`(?i)/(?:special|playlist)/(?:single/)?(\d+)(?:\.html)?(?:[/?#]|$)`)
	kugouSonglistPattern     = regexp.MustCompile(`(?i)songlist/(gcid_[a-z0-9]+)/?`)
	kugouHashOnlyPattern     = regexp.MustCompile(`(?i)^[a-f0-9]{32}$`)
)

func NewURLMatcher() *URLMatcher {
	return &URLMatcher{}
}

func (m *URLMatcher) MatchURL(rawURL string) (trackID string, matched bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || !strings.Contains(host, "kugou.com") {
		return "", false
	}
	if matches := kugouHashPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		return strings.ToLower(matches[1]), true
	}
	if matches := kugouSongPathHashPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		return strings.ToLower(matches[1]), true
	}
	if hash := normalizeHash(parsed.Fragment); hash != "" {
		return hash, true
	}
	query := parsed.Query()
	for _, key := range []string{"hash", "fileHash", "filehash", "encode_album_audio_id"} {
		if hash := normalizeHash(query.Get(key)); hash != "" {
			return hash, true
		}
	}
	if matches := kugouPathHashPattern.FindStringSubmatch(strings.ToLower(parsed.Path)); len(matches) == 2 {
		return strings.ToLower(matches[1]), true
	}
	return "", false
}

func (m *URLMatcher) MatchPlaylistURL(rawURL string) (playlistID string, matched bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || !strings.Contains(host, "kugou.com") {
		return "", false
	}
	if matches := kugouPlaylistPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		return matches[1], true
	}
	if matches := kugouPlaylistPathPattern.FindStringSubmatch(strings.ToLower(parsed.Path)); len(matches) == 2 {
		return matches[1], true
	}
	if matches := kugouSonglistPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		return strings.ToLower(matches[1]), true
	}
	query := parsed.Query()
	for _, key := range []string{"specialid", "specialId", "plistid", "listid", "id"} {
		if value := strings.TrimSpace(query.Get(key)); value != "" && isNumericText(value) {
			return value, true
		}
	}
	for _, key := range []string{"gcid", "songlistid"} {
		if value := strings.TrimSpace(query.Get(key)); strings.HasPrefix(strings.ToLower(value), "gcid_") {
			return strings.ToLower(value), true
		}
	}
	return "", false
}

type TextMatcher struct{}

func NewTextMatcher() *TextMatcher {
	return &TextMatcher{}
}

func (m *TextMatcher) MatchText(text string) (trackID string, matched bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	if prefix, value := parsePlatformPrefix(text); prefix != "" && kugouHashOnlyPattern.MatchString(value) {
		return strings.ToLower(value), true
	}
	if prefix, value := parsePlatformPrefix(text); prefix != "" {
		if urlStr := extractURL(value); urlStr != "" {
			if id, ok := NewURLMatcher().MatchURL(urlStr); ok {
				return id, true
			}
		}
		if hash := normalizeHash(value); hash != "" {
			return hash, true
		}
	}
	if urlStr := extractURL(text); urlStr != "" {
		if id, ok := NewURLMatcher().MatchURL(urlStr); ok {
			return id, true
		}
	}
	if kugouHashOnlyPattern.MatchString(text) {
		return strings.ToLower(text), true
	}
	return "", false
}

func parsePlatformPrefix(text string) (string, string) {
	parts := strings.SplitN(text, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	prefix := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch prefix {
	case "kugou", "kg", "酷狗", "酷狗音乐":
		return prefix, value
	default:
		return "", ""
	}
}

var textURLPattern = regexp.MustCompile(`https?://[^\s]+`)

func extractURL(text string) string {
	match := textURLPattern.FindString(text)
	match = strings.TrimRight(match, ".,!?)]}>")
	return strings.TrimSpace(match)
}

func isNumericText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
