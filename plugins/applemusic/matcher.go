package applemusic

import (
	"net/url"
	"regexp"
	"strings"
)

// URLMatcher matches Apple Music URLs and extracts track, playlist, album, and artist IDs.
type URLMatcher struct{}

var (
	// Song: music.apple.com/{country}/song/{slug}/{numericID}
	appleSongPathRe = regexp.MustCompile(`^/[a-z]{2}/song/[^/]+/(\d{6,})$`)
	// Album: music.apple.com/{country}/album/{slug}/{numericID}
	appleAlbumPathRe = regexp.MustCompile(`^/[a-z]{2}/album/[^/]+/(\d{6,})$`)
	// Artist: music.apple.com/{country}/artist/{slug}/{numericID}
	appleArtistPathRe = regexp.MustCompile(`^/[a-z]{2}/artist/[^/]+/(\d{6,})$`)
	// Playlist: music.apple.com/{country}/playlist/{slug}/{playlistID}
	// Playlist IDs have the format pl.xxxxxxxx
	applePlaylistPathRe = regexp.MustCompile(`^/[a-z]{2}/playlist/[^/]+/(pl\.[a-zA-Z0-9-]+)$`)
	// Numeric ID pattern for query parameter extraction.
	numericIDRe = regexp.MustCompile(`^\d{6,}$`)
)

func NewURLMatcher() *URLMatcher { return &URLMatcher{} }

func (m *URLMatcher) MatchURL(rawURL string) (trackID string, matched bool) {
	parsed, ok := parseAppleURL(rawURL)
	if !ok {
		return "", false
	}
	// Check query params first: ?i= or ?songId= may carry a song ID in an album URL.
	query := parsed.Query()
	if songID := strings.TrimSpace(query.Get("i")); numericIDRe.MatchString(songID) {
		return songID, true
	}
	if songID := strings.TrimSpace(query.Get("songId")); numericIDRe.MatchString(songID) {
		return songID, true
	}
	// Match song path.
	if match := appleSongPathRe.FindStringSubmatch(parsed.Path); len(match) == 2 {
		return match[1], true
	}
	return "", false
}

func (m *URLMatcher) MatchPlaylistURL(rawURL string) (playlistID string, matched bool) {
	parsed, ok := parseAppleURL(rawURL)
	if !ok {
		return "", false
	}
	// Match album path -> encode as collection.
	if match := appleAlbumPathRe.FindStringSubmatch(parsed.Path); len(match) == 2 {
		// Only treat as album collection when there is no song-level query param.
		query := parsed.Query()
		if strings.TrimSpace(query.Get("i")) == "" && strings.TrimSpace(query.Get("songId")) == "" {
			if encoded := buildAlbumCollectionID(match[1]); encoded != "" {
				return encoded, true
			}
		}
	}
	// Match playlist path.
	if match := applePlaylistPathRe.FindStringSubmatch(parsed.Path); len(match) == 2 {
		return match[1], true
	}
	return "", false
}

func (m *URLMatcher) MatchArtistURL(rawURL string) (artistID string, matched bool) {
	parsed, ok := parseAppleURL(rawURL)
	if !ok {
		return "", false
	}
	if match := appleArtistPathRe.FindStringSubmatch(parsed.Path); len(match) == 2 {
		return match[1], true
	}
	return "", false
}

func parseAppleURL(rawURL string) (*url.URL, bool) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, false
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return nil, false
	}
	if host == "music.apple.com" || strings.HasSuffix(host, ".music.apple.com") {
		return parsed, true
	}
	return nil, false
}
