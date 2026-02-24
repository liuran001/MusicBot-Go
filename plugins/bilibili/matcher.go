package bilibili

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// URLMatcher extracts track IDs from Bilibili URLs.
type URLMatcher struct {
	audioPattern *regexp.Regexp
	videoPattern *regexp.Regexp
	b23Pattern   *regexp.Regexp
	pPattern     *regexp.Regexp
}

// NewURLMatcher creates a new Bilibili URLMatcher.
func NewURLMatcher() *URLMatcher {
	return &URLMatcher{
		// Match strings like bilibili.com/audio/au123456
		audioPattern: regexp.MustCompile(`bilibili\.com/audio/au(\d+)`),

		// Match BV video ids like BV1GJ411x7h7 or av numbers like av170001
		videoPattern: regexp.MustCompile(`(?i)(BV[a-zA-Z0-9]{10}|av\d+)`),

		// Match pure b23.tv short links like b23.tv/F78kbY
		b23Pattern: regexp.MustCompile(`(?i)b23\.tv/([a-zA-Z0-9]+)`),
		pPattern:   regexp.MustCompile(`(?i)[?&]p=(\d+)`),
	}
}

// MatchURL implements platform.URLMatcher.
func (m *URLMatcher) MatchURL(url string) (string, bool) {
	// First check explicit video formats as they might be within short domains
	if matches := m.videoPattern.FindStringSubmatch(url); len(matches) > 1 {
		videoID := strings.TrimSpace(matches[1])
		if page := m.extractPage(url); page > 1 {
			return videoID + "_p" + strconv.Itoa(page), true
		}
		return videoID, true
	}

	if matches := m.audioPattern.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1], true
	}

	// Finally, if it's just a raw b23.tv link without known formats inside
	if matches := m.b23Pattern.FindStringSubmatch(url); len(matches) > 1 {
		// prefix with b23: internal indicator
		return "b23:" + matches[1], true
	}

	return "", false
}

func (m *URLMatcher) extractPage(raw string) int {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && parsed != nil {
		if value := strings.TrimSpace(parsed.Query().Get("p")); value != "" {
			if parsedPage, parseErr := strconv.Atoi(value); parseErr == nil && parsedPage > 0 {
				return parsedPage
			}
		}
	}
	if m != nil && m.pPattern != nil {
		if matches := m.pPattern.FindStringSubmatch(raw); len(matches) > 1 {
			if parsedPage, parseErr := strconv.Atoi(strings.TrimSpace(matches[1])); parseErr == nil && parsedPage > 0 {
				return parsedPage
			}
		}
	}
	return 0
}

// Support Text Matching (e.g. "au123456" or "BV1GJ411x7h7" or "b23.tv/ysjTEMn")
func (m *URLMatcher) MatchText(text string) (string, bool) {
	audioPattern := regexp.MustCompile(`(?i)^au(\d+)$`)
	if matches := audioPattern.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1], true
	}

	videoPattern := regexp.MustCompile(`^(?i)(BV[a-zA-Z0-9]{10}|av\d+)$`)
	if matches := videoPattern.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1], true
	}

	b23Pattern := regexp.MustCompile(`^(?i)(?:https?://)?b23\.tv/([a-zA-Z0-9]+)$`)
	if matches := b23Pattern.FindStringSubmatch(text); len(matches) > 1 {
		return "b23:" + matches[1], true
	}

	return "", false
}
