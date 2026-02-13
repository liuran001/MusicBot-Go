package netease

import (
	"net/url"
	"strings"
)

// URLMatcher implements platform.URLMatcher for NetEase song URLs.
// It extracts track IDs from NetEase song URLs in various formats.

var radarPlaylistIDs = map[string]struct{}{
	"3136952023":  {},
	"8402996200":  {},
	"2829896389":  {},
	"2829883282":  {},
	"5327906368":  {},
	"5341776086":  {},
	"2829816518":  {},
	"8819359201":  {},
	"2829920189":  {},
	"5300458264":  {},
	"10106461201": {},
	"5362359247":  {},
	"5320167908":  {},
}

type URLMatcher struct {
	disableRadar bool
}

// NewURLMatcher creates a new NetEase URL matcher.
func NewURLMatcher() *URLMatcher {
	return &URLMatcher{disableRadar: false}
}

// NewURLMatcherWithRadarDisabled creates a new NetEase URL matcher with radar playlist filter option.
func NewURLMatcherWithRadarDisabled(disableRadar bool) *URLMatcher {
	return &URLMatcher{disableRadar: disableRadar}
}

// MatchURL implements platform.URLMatcher.
// It attempts to extract a track ID from a NetEase music URL.
// Supports the following URL patterns:
//   - https://music.163.com/song?id=1234567
//   - https://music.163.com/#/song?id=1234567
//   - https://y.music.163.com/m/song?id=1234567 (mobile)
//
// Returns the extracted ID and true if the URL is a valid NetEase song URL,
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

	if !hasSongMarker(parsed.Path, parsed.Fragment) {
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
			if len(id) < 5 {
				return "", false
			}
			return id, true
		}
	}

	// As a fallback, try to extract ID from the path
	// Handle URLs like https://music.163.com/song/1234567 (without query parameter)
	if pathID := extractPathID(parsed.Path, "song"); pathID != "" {
		return pathID, true
	}

	return "", false
}

// MatchPlaylistURL implements platform.PlaylistURLMatcher.
// It attempts to extract a playlist ID from a NetEase music URL.
// Supports the following URL patterns:
//   - https://music.163.com/playlist?id=1234567
//   - https://music.163.com/#/playlist?id=1234567
//   - https://y.music.163.com/m/playlist?id=1234567 (mobile)
func (m *URLMatcher) MatchPlaylistURL(rawURL string) (playlistID string, matched bool) {
	if rawURL == "" {
		return "", false
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return "", false
	}
	if !strings.Contains(hostname, "music.163.com") {
		return "", false
	}

	kind := ""
	if hasPlaylistMarker(parsed.Path, parsed.Fragment) {
		kind = "playlist"
	} else if hasAlbumMarker(parsed.Path, parsed.Fragment) {
		kind = "album"
	}
	if kind == "" {
		return "", false
	}

	queryString := parsed.RawQuery
	if queryString == "" && parsed.Fragment != "" {
		parts := strings.Split(parsed.Fragment, "?")
		if len(parts) > 1 {
			queryString = parts[len(parts)-1]
		}
	}

	if queryString != "" {
		params, err := url.ParseQuery(queryString)
		if err != nil {
			return "", false
		}
		id := params.Get("id")
		if id != "" {
			if len(id) < 5 {
				return "", false
			}
			if kind == "playlist" && m.disableRadar && isRadarPlaylistID(id) {
				return "", false
			}
			if kind == "album" {
				if encoded := encodeAlbumCollectionID(id); encoded != "" {
					return encoded, true
				}
				return "", false
			}
			return id, true
		}
	}

	pathID := extractPathID(parsed.Path, kind)
	if pathID != "" {
		if kind == "playlist" && m.disableRadar && isRadarPlaylistID(pathID) {
			return "", false
		}
		if kind == "album" {
			if encoded := encodeAlbumCollectionID(pathID); encoded != "" {
				return encoded, true
			}
			return "", false
		}
		return pathID, true
	}

	return "", false
}

func isRadarPlaylistID(id string) bool {
	_, ok := radarPlaylistIDs[strings.TrimSpace(id)]
	return ok
}

func hasSongMarker(path, fragment string) bool {
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg == "song" {
			return true
		}
	}
	if fragment == "" {
		return false
	}
	frag := strings.TrimPrefix(fragment, "/")
	frag = strings.SplitN(frag, "?", 2)[0]
	parts := strings.Split(strings.Trim(frag, "/"), "/")
	if len(parts) > 0 && parts[0] == "song" {
		return true
	}
	return false
}

func hasPlaylistMarker(path, fragment string) bool {
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg == "playlist" {
			return true
		}
	}
	if fragment == "" {
		return false
	}
	frag := strings.TrimPrefix(fragment, "/")
	frag = strings.SplitN(frag, "?", 2)[0]
	parts := strings.Split(strings.Trim(frag, "/"), "/")
	if len(parts) > 0 && parts[0] == "playlist" {
		return true
	}
	return false
}

func hasAlbumMarker(path, fragment string) bool {
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg == "album" {
			return true
		}
	}
	if fragment == "" {
		return false
	}
	frag := strings.TrimPrefix(fragment, "/")
	frag = strings.SplitN(frag, "?", 2)[0]
	parts := strings.Split(strings.Trim(frag, "/"), "/")
	if len(parts) > 0 && parts[0] == "album" {
		return true
	}
	return false
}

func extractPathID(path, marker string) string {
	if marker == "" {
		return ""
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 {
		return ""
	}
	for i := 0; i < len(segments)-1; i++ {
		if segments[i] != marker {
			continue
		}
		id := strings.TrimSpace(segments[i+1])
		if allDigits(id) && len(id) >= 5 {
			return id
		}
	}
	for i := len(segments) - 1; i >= 0; i-- {
		seg := strings.TrimSpace(segments[i])
		if allDigits(seg) && len(seg) >= 5 {
			return seg
		}
	}
	return ""
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
