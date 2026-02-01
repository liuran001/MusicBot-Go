package id3

import (
	"context"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type TagData struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Year        string
	TrackNumber int
	DiscNumber  int
	Genre       string
	Comment     string
	CoverURL    string
	Lyrics      string
	Extra       map[string]any
}

type ID3TagProvider interface {
	GetTagData(ctx context.Context, track *platform.Track, info *platform.DownloadInfo) (*TagData, error)
}
