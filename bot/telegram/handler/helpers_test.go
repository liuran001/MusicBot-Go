package handler

import (
	"strings"
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
	caption := buildMusicCaption(nil, info, "botname")
	if caption == "" {
		t.Fatalf("expected caption")
	}
	if !strings.Contains(caption, "专辑: Album") {
		t.Fatalf("expected caption contains album line")
	}
}

func TestBuildMusicCaptionHidesAlbumLineWhenEmpty(t *testing.T) {
	info := &botpkg.SongInfo{
		SongName:    "Song",
		SongArtists: "Artist",
		SongAlbum:   "",
		FileExt:     "mp3",
		MusicSize:   1024,
		BitRate:     320000,
	}
	caption := buildMusicCaption(nil, info, "botname")
	if strings.Contains(caption, "专辑:") {
		t.Fatalf("expected caption to hide album line when album is empty, got %q", caption)
	}
}

func TestBuildMusicInfoTextHideAlbumLineWhenEmpty(t *testing.T) {
	text := buildMusicInfoText("Song", "", "mp3 1MB", "下载中...")
	if strings.Contains(text, "专辑:") {
		t.Fatalf("expected status text to hide album line when album is empty, got %q", text)
	}
	if !strings.Contains(text, "Song\nmp3 1MB\n下载中...") {
		t.Fatalf("unexpected status text: %q", text)
	}
}

func TestBuildMusicInfoTextKeepAlbumLine(t *testing.T) {
	text := buildMusicInfoText("Song", "Album", "mp3 1MB", "下载中...")
	if !strings.Contains(text, "专辑: Album") {
		t.Fatalf("expected status text contains album line, got %q", text)
	}
}
