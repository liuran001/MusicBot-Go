package bilibili

import (
	"regexp"
)

// URLMatcher extracts track IDs from Bilibili URLs.
type URLMatcher struct {
	audioPattern *regexp.Regexp
	videoPattern *regexp.Regexp
	b23Pattern   *regexp.Regexp
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
	}
}

// MatchURL implements platform.URLMatcher.
func (m *URLMatcher) MatchURL(url string) (string, bool) {
	// First check explicit video formats as they might be within short domains
	if matches := m.videoPattern.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1], true
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
