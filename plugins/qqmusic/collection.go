package qqmusic

import "github.com/liuran001/MusicBot-Go/bot/platform"

func encodeAlbumCollectionID(albumID string) string {
	return platform.EncodeAlbumCollectionID(albumID)
}

func parseCollectionID(rawID string) (isAlbum bool, id string) {
	return platform.ParseAlbumCollectionID(rawID)
}
