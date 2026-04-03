package soda

import (
	"net/url"
	"regexp"
	"strings"
)

type URLMatcher struct{}

var (
	sodaTrackPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^track/(\d+)$`),
		regexp.MustCompile(`^song/(\d+)$`),
		regexp.MustCompile(`^qishui/share/track$`),
	}
	sodaPlaylistPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^playlist/(\d+)$`),
		regexp.MustCompile(`^sheet/(\d+)$`),
		regexp.MustCompile(`^qishui/share/playlist$`),
		regexp.MustCompile(`^qishui/share/album$`),
		regexp.MustCompile(`^album/(\d+)$`),
	}
	sodaArtistPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^artist/(\d+)$`),
		regexp.MustCompile(`^qishui/share/artist$`),
	}
)

func NewURLMatcher() *URLMatcher { return &URLMatcher{} }

func (m *URLMatcher) MatchURL(rawURL string) (trackID string, matched bool) {
	parsed, ok := parseSodaURL(rawURL)
	if !ok {
		return "", false
	}
	pathValue := strings.Trim(parsed.Path, "/")
	for _, re := range sodaTrackPatterns {
		if match := re.FindStringSubmatch(pathValue); len(match) == 2 {
			return strings.TrimSpace(match[1]), true
		}
	}
	query := parsed.Query()
	if trackID := strings.TrimSpace(firstNonEmptyString(query.Get("track_id"), query.Get("trackId"), query.Get("id"))); isNumericSodaID(trackID) {
		return trackID, true
	}
	if trackID, ok := matchSodaFragment(parsed.Fragment, true); ok {
		return trackID, true
	}
	return "", false
}

func (m *URLMatcher) MatchPlaylistURL(rawURL string) (playlistID string, matched bool) {
	parsed, ok := parseSodaURL(rawURL)
	if !ok {
		return "", false
	}
	pathValue := strings.Trim(parsed.Path, "/")
	if match := regexp.MustCompile(`^album/(\d+)$`).FindStringSubmatch(pathValue); len(match) == 2 {
		if encoded := encodeAlbumCollectionID(match[1]); encoded != "" {
			return encoded, true
		}
	}
	for _, re := range sodaPlaylistPatterns {
		if match := re.FindStringSubmatch(pathValue); len(match) == 2 {
			return strings.TrimSpace(match[1]), true
		}
	}
	query := parsed.Query()
	if albumID := strings.TrimSpace(firstNonEmptyString(query.Get("album_id"), query.Get("albumId"))); isNumericSodaID(albumID) {
		if encoded := encodeAlbumCollectionID(albumID); encoded != "" {
			return encoded, true
		}
	}
	if playlistID := strings.TrimSpace(firstNonEmptyString(query.Get("playlist_id"), query.Get("playlistId"), query.Get("id"))); isNumericSodaID(playlistID) {
		return playlistID, true
	}
	if playlistID, ok := matchSodaFragment(parsed.Fragment, false); ok {
		return playlistID, true
	}
	return "", false
}

func (m *URLMatcher) MatchArtistURL(rawURL string) (artistID string, matched bool) {
	parsed, ok := parseSodaURL(rawURL)
	if !ok {
		return "", false
	}
	pathValue := strings.Trim(parsed.Path, "/")
	for _, re := range sodaArtistPatterns {
		if match := re.FindStringSubmatch(pathValue); len(match) == 2 {
			return strings.TrimSpace(match[1]), true
		}
	}
	query := parsed.Query()
	if artistID := strings.TrimSpace(firstNonEmptyString(query.Get("artist_id"), query.Get("artistId"), query.Get("id"))); isNumericSodaID(artistID) {
		return artistID, true
	}
	if artistID, ok := matchSodaArtistFragment(parsed.Fragment); ok {
		return artistID, true
	}
	return "", false
}

func parseSodaURL(rawURL string) (*url.URL, bool) {
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
	for _, suffix := range []string{"qishui.com", "qishui.douyin.com", "music.douyin.com", "bubble.qishui.com", "douyin.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return parsed, true
		}
	}
	return nil, false
}

func matchSodaFragment(fragment string, track bool) (string, bool) {
	fragment = strings.TrimSpace(strings.TrimPrefix(fragment, "#"))
	if fragment == "" {
		return "", false
	}
	if !strings.Contains(fragment, "?") {
		return "", false
	}
	parts := strings.SplitN(fragment, "?", 2)
	pathPart := strings.Trim(parts[0], "/")
	query, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", false
	}
	if track {
		if strings.Contains(pathPart, "track") || strings.Contains(pathPart, "song") {
			id := strings.TrimSpace(firstNonEmptyString(query.Get("track_id"), query.Get("trackId"), query.Get("id")))
			return id, isNumericSodaID(id)
		}
		return "", false
	}
	if strings.Contains(pathPart, "playlist") || strings.Contains(pathPart, "sheet") {
		id := strings.TrimSpace(firstNonEmptyString(query.Get("playlist_id"), query.Get("playlistId"), query.Get("id")))
		return id, isNumericSodaID(id)
	}
	if strings.Contains(pathPart, "album") {
		id := strings.TrimSpace(firstNonEmptyString(query.Get("album_id"), query.Get("albumId"), query.Get("id")))
		if isNumericSodaID(id) {
			return encodeAlbumCollectionID(id), true
		}
	}
	return "", false
}

func matchSodaArtistFragment(fragment string) (string, bool) {
	fragment = strings.TrimSpace(strings.TrimPrefix(fragment, "#"))
	if fragment == "" || !strings.Contains(fragment, "?") {
		return "", false
	}
	parts := strings.SplitN(fragment, "?", 2)
	pathPart := strings.Trim(parts[0], "/")
	query, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", false
	}
	if strings.Contains(pathPart, "artist") || strings.Contains(pathPart, "singer") {
		id := strings.TrimSpace(firstNonEmptyString(query.Get("artist_id"), query.Get("artistId"), query.Get("id")))
		return id, isNumericSodaID(id)
	}
	return "", false
}

func isNumericSodaID(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return len(value) >= 8
}
