package handler

import (
	"testing"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
)

func TestSanitizeFileName(t *testing.T) {
	name := "a/b:c*?d|e\\f\"g"
	safe := sanitizeFileName(name)
	if safe == name {
		t.Fatalf("expected sanitized name")
	}
}

func TestBuildMusicCaption(t *testing.T) {
	info := &botpkg.SongInfo{
		MusicID:     1,
		SongName:    "Song",
		SongArtists: "Artist",
		SongAlbum:   "Album",
		FileExt:     "mp3",
		MusicSize:   1024,
		BitRate:     320000,
	}
	caption := buildMusicCaption(info, "botname")
	if caption == "" {
		t.Fatalf("expected caption")
	}
}
