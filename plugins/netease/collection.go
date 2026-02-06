package netease

import "strings"

const albumCollectionPrefix = "album:"

func encodeAlbumCollectionID(albumID string) string {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return ""
	}
	return albumCollectionPrefix + albumID
}

func parseCollectionID(rawID string) (isAlbum bool, id string) {
	rawID = strings.TrimSpace(rawID)
	if rawID == "" {
		return false, ""
	}
	if strings.HasPrefix(rawID, albumCollectionPrefix) {
		id = strings.TrimSpace(strings.TrimPrefix(rawID, albumCollectionPrefix))
		return true, id
	}
	return false, rawID
}
