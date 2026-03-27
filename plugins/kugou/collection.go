package kugou

import "strings"

func buildPlaylistLink(playlistID string) string {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(playlistID), "gcid_") {
		return "https://www.kugou.com/songlist/" + playlistID + "/"
	}
	return "https://www.kugou.com/yy/special/single/" + playlistID + ".html"
}
