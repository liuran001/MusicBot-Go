package youtubemusic

import (
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// The InnerTube JSON is deep and unstable, so these helpers walk decoded
// map[string]any trees defensively: they look for the small set of keys we need
// and tolerate everything else changing.

// asMap / asSlice / asString are nil-safe type assertions.
func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// runsText concatenates a {"runs":[{"text":...}]} structure into a plain string.
func runsText(v any) string {
	m := asMap(v)
	if m == nil {
		return asString(v)
	}
	if s, ok := m["simpleText"].(string); ok {
		return s
	}
	runs := asSlice(m["runs"])
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(asString(asMap(r)["text"]))
	}
	return b.String()
}

// collectMusicListItems walks a search response and returns every
// musicResponsiveListItemRenderer object found, in document order.
func collectMusicListItems(root map[string]any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if item, ok := n["musicResponsiveListItemRenderer"].(map[string]any); ok {
				out = append(out, item)
			}
			for _, v := range n {
				walk(v)
			}
		case []any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(root)
	return out
}

// trackFromListItem extracts a Track from a musicResponsiveListItemRenderer.
// It pulls the videoId from any nested watchEndpoint, the title from the first
// flex column, and artist/album from the remaining run text.
func trackFromListItem(item map[string]any) (platform.Track, bool) {
	videoID := findVideoID(item)
	if videoID == "" {
		return platform.Track{}, false
	}
	cols := asSlice(item["flexColumns"])
	title := ""
	var artists []platform.Artist
	for idx, col := range cols {
		renderer := asMap(asMap(col)["musicResponsiveListItemFlexColumnRenderer"])
		text := strings.TrimSpace(runsText(renderer["text"]))
		if text == "" {
			continue
		}
		if idx == 0 || title == "" {
			title = text
			continue
		}
		// Subsequent columns: "Artist • Album • Duration" joined by " • ".
		for _, part := range strings.Split(text, "•") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			// Skip pure-duration tokens (e.g. "3:45").
			if looksLikeDuration(part) {
				continue
			}
			artists = append(artists, platform.Artist{Name: part, Platform: platformName})
			break // first non-duration token is the primary artist
		}
	}
	if title == "" {
		return platform.Track{}, false
	}
	return platform.Track{
		ID:       videoID,
		Platform: platformName,
		Title:    title,
		Artists:  artists,
		URL:      "https://music.youtube.com/watch?v=" + videoID,
	}, true
}

// findVideoID recursively searches a node for the first "videoId" string.
func findVideoID(node any) string {
	switch n := node.(type) {
	case map[string]any:
		if id, ok := n["videoId"].(string); ok && id != "" {
			return id
		}
		for _, v := range n {
			if id := findVideoID(v); id != "" {
				return id
			}
		}
	case []any:
		for _, v := range n {
			if id := findVideoID(v); id != "" {
				return id
			}
		}
	}
	return ""
}

// looksLikeDuration reports whether s is a mm:ss / h:mm:ss timestamp.
func looksLikeDuration(s string) bool {
	if !strings.Contains(s, ":") {
		return false
	}
	for _, r := range s {
		if r != ':' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// findLyricsBrowseID walks a /next response for the lyrics tab's browseId.
// The lyrics tab endpoint browseId starts with "MPLYt".
func findLyricsBrowseID(node any) string {
	switch n := node.(type) {
	case map[string]any:
		if bid, ok := n["browseId"].(string); ok && strings.HasPrefix(bid, "MPLYt") {
			return bid
		}
		for _, v := range n {
			if id := findLyricsBrowseID(v); id != "" {
				return id
			}
		}
	case []any:
		for _, v := range n {
			if id := findLyricsBrowseID(v); id != "" {
				return id
			}
		}
	}
	return ""
}

// findLyricsText walks a lyrics /browse response for the description text inside
// a musicDescriptionShelfRenderer.
func findLyricsText(node any) string {
	switch n := node.(type) {
	case map[string]any:
		if shelf, ok := n["musicDescriptionShelfRenderer"].(map[string]any); ok {
			if text := strings.TrimSpace(runsText(shelf["description"])); text != "" {
				return text
			}
		}
		for _, v := range n {
			if text := findLyricsText(v); text != "" {
				return text
			}
		}
	case []any:
		for _, v := range n {
			if text := findLyricsText(v); text != "" {
				return text
			}
		}
	}
	return ""
}
