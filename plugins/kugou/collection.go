package kugou

import "strings"

const (
	albumCollectionPrefix       = "album:"
	playlistURLCollectionPrefix = "playlisturl:"
)

func encodeAlbumCollectionID(albumID string) string {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return ""
	}
	return albumCollectionPrefix + albumID
}

func encodePlaylistURLCollectionID(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	return playlistURLCollectionPrefix + rawURL
}

func parseCollectionID(rawID string) (kind, id string) {
	rawID = strings.TrimSpace(rawID)
	if rawID == "" {
		return "", ""
	}
	if strings.HasPrefix(rawID, albumCollectionPrefix) {
		return "album", strings.TrimSpace(strings.TrimPrefix(rawID, albumCollectionPrefix))
	}
	if strings.HasPrefix(rawID, playlistURLCollectionPrefix) {
		return "playlist_url", strings.TrimSpace(strings.TrimPrefix(rawID, playlistURLCollectionPrefix))
	}
	return "playlist", rawID
}

func buildPlaylistLink(playlistID string) string {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return ""
	}
	if isGlobalCollectionID(playlistID) {
		return "https://www.kugou.com/share/zlist.html?global_collection_id=" + playlistID
	}
	if strings.HasPrefix(strings.ToLower(playlistID), "gcid_") {
		return "https://www.kugou.com/songlist/" + playlistID + "/"
	}
	return "https://www.kugou.com/yy/special/single/" + playlistID + ".html"
}
