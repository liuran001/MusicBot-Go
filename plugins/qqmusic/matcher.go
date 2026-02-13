package qqmusic

import (
	"net/url"
	"regexp"
	"strings"
)

type URLMatcher struct{}

var (
	qqSongPathPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^n/ryqq/songDetail/([^/?#]+)$`),
		regexp.MustCompile(`^n/ryqq_v2/songDetail/([^/?#]+)$`),
		regexp.MustCompile(`^n/ryqq/song/([^/?#]+)$`),
		regexp.MustCompile(`^song/([^/?#]+)$`),
	}
	qqAlbumPathPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^n/ryqq/albumDetail/([^/?#]+)$`),
		regexp.MustCompile(`^n/ryqq_v2/albumDetail/([^/?#]+)$`),
		regexp.MustCompile(`^n/yqq/album/([^/?#]+)\.html$`),
		regexp.MustCompile(`^n2/m/share/details/album\.html$`),
		regexp.MustCompile(`^n3/other/pages/details/album\.html$`),
		regexp.MustCompile(`^albumDetail/([^/?#]+)$`),
		regexp.MustCompile(`^album/([^/?#]+)$`),
	}
	qqPlaylistPathPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^n/ryqq/playlist/([^/?#]+)$`),
		regexp.MustCompile(`^n/ryqq_v2/playlist/([^/?#]+)$`),
		regexp.MustCompile(`^playlist/([^/?#]+)$`),
		regexp.MustCompile(`^n2/m/share/details/taoge\.html$`),
		regexp.MustCompile(`^n3/other/pages/details/playlist\.html$`),
	}
)

func NewURLMatcher() *URLMatcher {
	return &URLMatcher{}
}

func (m *URLMatcher) MatchURL(rawURL string) (trackID string, matched bool) {
	if strings.TrimSpace(rawURL) == "" {
		return "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", false
	}
	return matchQQMusicURL(parsed)
}

// MatchPlaylistURL implements platform.PlaylistURLMatcher.
// It attempts to extract a playlist ID from QQ Music playlist URLs.
func (m *URLMatcher) MatchPlaylistURL(rawURL string) (playlistID string, matched bool) {
	if strings.TrimSpace(rawURL) == "" {
		return "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", false
	}
	return matchQQMusicPlaylistURL(parsed)
}

func matchQQMusicURL(parsed *url.URL) (string, bool) {
	if parsed == nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || !strings.Contains(host, "qq.com") {
		return "", false
	}
	pathValue := strings.Trim(parsed.Path, "/")
	for _, re := range qqSongPathPatterns {
		if match := re.FindStringSubmatch(pathValue); len(match) == 2 {
			return match[1], true
		}
	}
	query := parsed.Query()
	if songMid := strings.TrimSpace(query.Get("songmid")); songMid != "" {
		return songMid, true
	}
	if songID := strings.TrimSpace(query.Get("songid")); songID != "" {
		return songID, true
	}
	if songID := strings.TrimSpace(query.Get("id")); songID != "" {
		return songID, true
	}
	return "", false
}

func matchQQMusicPlaylistURL(parsed *url.URL) (string, bool) {
	if parsed == nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || !strings.Contains(host, "qq.com") {
		return "", false
	}
	pathValue := strings.Trim(parsed.Path, "/")
	for _, re := range qqAlbumPathPatterns {
		if match := re.FindStringSubmatch(pathValue); len(match) == 2 {
			if albumID := encodeAlbumCollectionID(match[1]); albumID != "" {
				return albumID, true
			}
		}
	}
	for _, re := range qqPlaylistPathPatterns {
		if match := re.FindStringSubmatch(pathValue); len(match) == 2 {
			return match[1], true
		}
	}
	query := parsed.Query()
	if albumMid := strings.TrimSpace(query.Get("albummid")); albumMid != "" {
		if albumID := encodeAlbumCollectionID(albumMid); albumID != "" {
			return albumID, true
		}
	}
	if albumMid := strings.TrimSpace(query.Get("albumMid")); albumMid != "" {
		if albumID := encodeAlbumCollectionID(albumMid); albumID != "" {
			return albumID, true
		}
	}
	if albumID := strings.TrimSpace(query.Get("albumid")); albumID != "" {
		if encoded := encodeAlbumCollectionID(albumID); encoded != "" {
			return encoded, true
		}
	}
	if albumID := strings.TrimSpace(query.Get("albumId")); albumID != "" {
		if encoded := encodeAlbumCollectionID(albumID); encoded != "" {
			return encoded, true
		}
	}
	if hasQQAlbumMarker(pathValue) {
		if id := strings.TrimSpace(query.Get("id")); id != "" {
			if encoded := encodeAlbumCollectionID(id); encoded != "" {
				return encoded, true
			}
		}
	}
	if disstid := strings.TrimSpace(query.Get("disstid")); disstid != "" {
		return disstid, true
	}
	if id := strings.TrimSpace(query.Get("id")); id != "" {
		return id, true
	}
	if listID := strings.TrimSpace(query.Get("listid")); listID != "" {
		return listID, true
	}
	if tid := strings.TrimSpace(query.Get("tid")); tid != "" {
		return tid, true
	}
	return "", false
}

func hasQQAlbumMarker(pathValue string) bool {
	pathValue = strings.ToLower(strings.TrimSpace(pathValue))
	if pathValue == "" {
		return false
	}
	return strings.Contains(pathValue, "album")
}
