package qqmusic

import (
	"net/url"
	"regexp"
	"strings"
)

type URLMatcher struct{}

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
	patterns := []string{
		`^n/ryqq/songDetail/([^/?#]+)$`,
		`^n/ryqq_v2/songDetail/([^/?#]+)$`,
		`^n/ryqq/song/([^/?#]+)$`,
		`^song/([^/?#]+)$`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
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
	patterns := []string{
		`^n/ryqq/playlist/([^/?#]+)$`,
		`^n/ryqq_v2/playlist/([^/?#]+)$`,
		`^playlist/([^/?#]+)$`,
		`^n2/m/share/details/taoge\.html$`,
		`^n3/other/pages/details/playlist\.html$`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if match := re.FindStringSubmatch(pathValue); len(match) == 2 {
			return match[1], true
		}
	}
	query := parsed.Query()
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
