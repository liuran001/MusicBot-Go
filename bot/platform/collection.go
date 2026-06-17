package platform

import "strings"

// AlbumCollectionPrefix is the sentinel prefix used to mark a collection ID as
// an album (as opposed to a playlist) across music platform plugins.
const AlbumCollectionPrefix = "album:"

// EncodeAlbumCollectionID wraps a raw album ID with AlbumCollectionPrefix so it
// can be distinguished from a playlist ID. Returns "" for blank input.
func EncodeAlbumCollectionID(albumID string) string {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return ""
	}
	return AlbumCollectionPrefix + albumID
}

// ParseAlbumCollectionID reports whether rawID is an album collection ID and
// returns the underlying ID. A non-prefixed value is treated as a playlist ID
// and returned unchanged with isAlbum=false.
func ParseAlbumCollectionID(rawID string) (isAlbum bool, id string) {
	rawID = strings.TrimSpace(rawID)
	if rawID == "" {
		return false, ""
	}
	if strings.HasPrefix(rawID, AlbumCollectionPrefix) {
		return true, strings.TrimSpace(strings.TrimPrefix(rawID, AlbumCollectionPrefix))
	}
	return false, rawID
}
