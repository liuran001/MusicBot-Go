package spotify

import (
	"net/url"
	"regexp"
	"strings"
)

// spotifyIDPattern matches a Spotify base-62 ID: 22 characters from
// [A-Za-z0-9]. Track, album, artist, and playlist IDs all share this shape.
var spotifyIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{22}$`)

// URLMatcher extracts Spotify track IDs from open.spotify.com URLs and
// spotify: URIs.
type URLMatcher struct{}

// NewURLMatcher returns a URLMatcher.
func NewURLMatcher() *URLMatcher { return &URLMatcher{} }

// MatchURL extracts a track ID from any recognized Spotify track reference:
//
//	https://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6
//	https://open.spotify.com/intl-ja/track/6rqhFgbbKwnb9MLmUQDhG6
//	spotify:track:6rqhFgbbKwnb9MLmUQDhG6
//
// A locale path segment ("intl-ja", "intl-de", …) that Spotify inserts before
// the resource type is tolerated.
func (m *URLMatcher) MatchURL(rawURL string) (trackID string, matched bool) {
	if id, kind, ok := parseSpotifyURI(rawURL); ok && kind == "track" {
		return id, true
	}
	id, kind, ok := parseSpotifyWebURL(rawURL)
	if !ok || kind != "track" {
		return "", false
	}
	return id, true
}

// MatchPlaylistURL extracts a playlist or album ID (both render as a track
// list) from a Spotify URL or URI.
func (m *URLMatcher) MatchPlaylistURL(rawURL string) (playlistID string, matched bool) {
	if id, kind, ok := parseSpotifyURI(rawURL); ok && (kind == "playlist" || kind == "album") {
		return encodeCollectionID(kind, id), true
	}
	id, kind, ok := parseSpotifyWebURL(rawURL)
	if !ok || (kind != "playlist" && kind != "album") {
		return "", false
	}
	return encodeCollectionID(kind, id), true
}

// MatchArtistURL extracts an artist ID from a Spotify URL or URI.
func (m *URLMatcher) MatchArtistURL(rawURL string) (artistID string, matched bool) {
	if id, kind, ok := parseSpotifyURI(rawURL); ok && kind == "artist" {
		return id, true
	}
	id, kind, ok := parseSpotifyWebURL(rawURL)
	if !ok || kind != "artist" {
		return "", false
	}
	return id, true
}

// encodeCollectionID tags a collection ID with its kind so the platform layer
// can fetch albums and playlists through the right endpoint. Format: "<kind>:<id>".
func encodeCollectionID(kind, id string) string {
	return kind + ":" + id
}

// decodeCollectionID reverses encodeCollectionID. A bare ID (no prefix) is
// treated as a playlist for backward compatibility.
func decodeCollectionID(value string) (kind, id string) {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	if len(parts) == 2 && (parts[0] == "playlist" || parts[0] == "album") {
		return parts[0], parts[1]
	}
	return "playlist", strings.TrimSpace(value)
}

// parseSpotifyURI parses a "spotify:<kind>:<id>" URI. Returns the id, the
// resource kind ("track"/"album"/"artist"/"playlist"), and whether it parsed.
func parseSpotifyURI(raw string) (id, kind string, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "spotify:") {
		return "", "", false
	}
	parts := strings.Split(raw, ":")
	// spotify:track:<id>  → 3 parts. spotify:user:<u>:playlist:<id> → 5 parts.
	if len(parts) >= 3 {
		kind = strings.ToLower(parts[len(parts)-2])
		candidate := parts[len(parts)-1]
		if spotifyIDPattern.MatchString(candidate) {
			return candidate, kind, true
		}
	}
	return "", "", false
}

// parseSpotifyWebURL parses an open.spotify.com web URL, tolerating an optional
// "intl-xx" locale segment before the resource type.
func parseSpotifyWebURL(raw string) (id, kind string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", "", false
	}
	host := strings.ToLower(parsed.Host)
	host = strings.TrimPrefix(host, "www.")
	if host != "open.spotify.com" && host != "play.spotify.com" {
		return "", "", false
	}
	segments := strings.FieldsFunc(parsed.Path, func(r rune) bool { return r == '/' })
	// Drop a leading locale segment like "intl-ja".
	if len(segments) > 0 && strings.HasPrefix(strings.ToLower(segments[0]), "intl-") {
		segments = segments[1:]
	}
	// Expect "<kind>/<id>" (also handles "user/<u>/playlist/<id>").
	for i := 0; i+1 < len(segments); i++ {
		switch strings.ToLower(segments[i]) {
		case "track", "album", "artist", "playlist":
			candidate := segments[i+1]
			if spotifyIDPattern.MatchString(candidate) {
				return candidate, strings.ToLower(segments[i]), true
			}
		}
	}
	return "", "", false
}
