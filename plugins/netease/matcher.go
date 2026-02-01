package netease

import (
	"net/url"
	"strings"
)

// URLMatcher implements platform.URLMatcher for NetEase music URLs.
// It extracts track IDs from NetEase URLs in various formats.
type URLMatcher struct{}

// NewURLMatcher creates a new NetEase URL matcher.
func NewURLMatcher() *URLMatcher {
	return &URLMatcher{}
}

// MatchURL implements platform.URLMatcher.
// It attempts to extract a track ID from a NetEase music URL.
// Supports the following URL patterns:
//   - https://music.163.com/song?id=1234567
//   - https://music.163.com/#/song?id=1234567
//   - https://y.music.163.com/m/song?id=1234567 (mobile)
//   - https://music.163.com/album?id=67890
//   - https://music.163.com/playlist?id=11111
//   - https://music.163.com/artist?id=22222
//   - https://music.163.com/dj?id=33333
//
// Returns the extracted ID and true if the URL is a valid NetEase URL,
// or an empty string and false if the URL is not recognized.
func (m *URLMatcher) MatchURL(rawURL string) (trackID string, matched bool) {
	if rawURL == "" {
		return "", false
	}

	// Parse the URL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}

	// Check if the hostname contains music.163.com
	// Valid hostnames: music.163.com, y.music.163.com, etc.
	hostname := parsed.Hostname()
	if hostname == "" {
		return "", false
	}

	if !strings.Contains(hostname, "music.163.com") {
		return "", false
	}

	// Extract the ID from query parameters
	// Handle both direct query parameters (?id=xxx) and hash fragments (#/...?id=xxx)
	queryString := parsed.RawQuery

	// If there's no query string but there is a fragment, try to extract ID from the fragment
	if queryString == "" && parsed.Fragment != "" {
		// Fragment might be like: song?id=1234567 or /song?id=1234567
		// Extract the part after the last '?'
		parts := strings.Split(parsed.Fragment, "?")
		if len(parts) > 1 {
			queryString = parts[len(parts)-1]
		}
	}

	// Parse the query string to get the id parameter
	if queryString != "" {
		params, err := url.ParseQuery(queryString)
		if err != nil {
			return "", false
		}

		id := params.Get("id")
		if id != "" {
			return id, true
		}
	}

	// As a fallback, try to extract ID from the path
	// Handle URLs like https://music.163.com/song/1234567 (without query parameter)
	pathSegments := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(pathSegments) >= 2 {
		// Last segment might be the ID
		lastSegment := pathSegments[len(pathSegments)-1]
		if lastSegment != "" && allDigits(lastSegment) {
			return lastSegment, true
		}
	}

	return "", false
}

// allDigits checks if a string contains only digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
