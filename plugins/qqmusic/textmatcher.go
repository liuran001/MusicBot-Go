package qqmusic

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type TextMatcher struct{}

func NewTextMatcher() *TextMatcher {
	return &TextMatcher{}
}

func (m *TextMatcher) MatchText(text string) (trackID string, matched bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	if prefix, value := parsePlatformPrefix(text); prefix != "" && value != "" {
		if isTencentSongMID(value) || isNumericID(value) {
			return value, true
		}
	}
	if urlStr := extractURL(text); urlStr != "" {
		if id, ok := NewURLMatcher().MatchURL(urlStr); ok {
			return id, true
		}
		if resolved := resolveTencentShortURL(urlStr); resolved != "" {
			if id, ok := NewURLMatcher().MatchURL(resolved); ok {
				return id, true
			}
		}
	}
	if isTencentSongMID(text) {
		return text, true
	}
	if isNumericID(text) && len(text) >= 5 {
		return text, true
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
	case "qqmusic", "qq", "tencent":
		return prefix, value
	default:
		return "", ""
	}
}

var urlPattern = regexp.MustCompile(`https?://[^\s]+`)

func extractURL(text string) string {
	match := urlPattern.FindString(text)
	match = strings.TrimRight(match, ".,!?)]}>")
	return strings.TrimSpace(match)
}

func resolveTencentShortURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	pathValue := strings.Trim(parsed.Path, "/")
	if !strings.Contains(host, "y.qq.com") {
		return ""
	}
	if pathValue != "base/fcgi-bin/u" {
		return ""
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(parsed.String())
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return ""
	}
	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location == "" {
		return ""
	}
	if resolved, err := parsed.Parse(location); err == nil {
		return resolved.String()
	}
	return location
}

func isTencentSongMID(text string) bool {
	if strings.ContainsAny(text, " /?&=") {
		return false
	}
	if len(text) < 8 || len(text) > 20 {
		return false
	}
	hasLetter := false
	hasDigit := false
	for _, ch := range text {
		switch {
		case ch >= '0' && ch <= '9':
			hasDigit = true
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z'):
			hasLetter = true
		default:
			return false
		}
	}
	return hasLetter && hasDigit
}
