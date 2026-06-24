package youtubemusic

import (
	"net/url"
	"regexp"
	"strings"
)

// videoIDPattern matches a canonical YouTube video ID: exactly 11 characters
// from the URL-safe base64 alphabet. Used to validate IDs extracted from URLs
// or bare text so we don't treat arbitrary tokens as track IDs.
var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// playlistIDPattern matches a YouTube playlist ID. These start with a known
// prefix (PL, OLAK5uy_, RDCLAK5uy_, VL, etc.) and contain URL-safe characters.
// We keep this permissive: anything 13+ chars from the playlist alphabet.
var playlistIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{13,}$`)

// URLMatcher extracts YouTube / YouTube Music track (video) IDs from URLs.
type URLMatcher struct{}

// NewURLMatcher returns a URLMatcher.
func NewURLMatcher() *URLMatcher { return &URLMatcher{} }

// ytHosts are the hostnames (after stripping a leading "www."/"m.") that this
// platform recognizes for track and playlist links.
var ytHosts = map[string]struct{}{
	"music.youtube.com": {},
	"youtube.com":       {},
	"youtu.be":          {},
}

// normalizeHost lowercases the host and strips common "www."/"m." subdomain
// prefixes so "www.youtube.com" and "m.youtube.com" match "youtube.com".
func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i] // strip port
	}
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	return host
}

// isYouTubeHost reports whether host (already normalized) is a recognized
// YouTube host.
func isYouTubeHost(host string) bool {
	_, ok := ytHosts[host]
	return ok
}

// MatchURL extracts an 11-char video ID from any recognized YouTube URL form:
//
//	https://music.youtube.com/watch?v=VIDEOID
//	https://www.youtube.com/watch?v=VIDEOID
//	https://youtu.be/VIDEOID
//	https://music.youtube.com/watch?v=VIDEOID&list=...
//	https://www.youtube.com/shorts/VIDEOID
//	https://www.youtube.com/embed/VIDEOID
func (m *URLMatcher) MatchURL(rawURL string) (trackID string, matched bool) {
	parsed, ok := parseYouTubeURL(rawURL)
	if !ok {
		return "", false
	}
	host := normalizeHost(parsed.Host)
	if !isYouTubeHost(host) {
		return "", false
	}

	// youtu.be/<id> — the id is the first path segment.
	if host == "youtu.be" {
		seg := firstPathSegment(parsed.Path)
		if videoIDPattern.MatchString(seg) {
			return seg, true
		}
		return "", false
	}

	// watch?v=<id>
	if v := strings.TrimSpace(parsed.Query().Get("v")); videoIDPattern.MatchString(v) {
		return v, true
	}

	// /shorts/<id>, /embed/<id>, /v/<id>
	pathValue := strings.Trim(parsed.Path, "/")
	for _, prefix := range []string{"shorts/", "embed/", "v/"} {
		if strings.HasPrefix(pathValue, prefix) {
			candidate := firstPathSegment(strings.TrimPrefix(pathValue, prefix))
			if videoIDPattern.MatchString(candidate) {
				return candidate, true
			}
		}
	}
	return "", false
}

// MatchPlaylistURL extracts a playlist ID from a YouTube playlist URL:
//
//	https://music.youtube.com/playlist?list=PLAYLISTID
//	https://www.youtube.com/playlist?list=PLAYLISTID
//
// A watch URL that merely carries a &list= is treated as a track (handled by
// MatchURL), not a playlist, so single-track links keep working in groups.
func (m *URLMatcher) MatchPlaylistURL(rawURL string) (playlistID string, matched bool) {
	parsed, ok := parseYouTubeURL(rawURL)
	if !ok {
		return "", false
	}
	if !isYouTubeHost(normalizeHost(parsed.Host)) {
		return "", false
	}
	pathValue := strings.Trim(parsed.Path, "/")
	if pathValue != "playlist" && pathValue != "browse" {
		return "", false
	}
	list := strings.TrimSpace(parsed.Query().Get("list"))
	if list == "" {
		// /browse/<id> form
		list = strings.TrimSpace(parsed.Query().Get("browse_id"))
	}
	if playlistIDPattern.MatchString(list) {
		return list, true
	}
	return "", false
}

// parseYouTubeURL is a tolerant URL parser: it accepts input with or without a
// scheme (prepending https:// when absent) and returns the parsed URL only when
// a host is present.
func parseYouTubeURL(rawURL string) (*url.URL, bool) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, false
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return nil, false
	}
	return parsed, true
}

// firstPathSegment returns the first non-empty segment of a URL path.
func firstPathSegment(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}
