package handler

import (
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestDetectCollectionType(t *testing.T) {
	tests := []struct {
		name       string
		rawID      string
		url        string
		expectedTy string
	}{
		{name: "album by prefixed id", rawID: "album:3411281", expectedTy: collectionTypeAlbum},
		{name: "album by url", rawID: "3411281", url: "https://music.163.com/album?id=3411281", expectedTy: collectionTypeAlbum},
		{name: "playlist default", rawID: "19723756", url: "https://music.163.com/playlist?id=19723756", expectedTy: collectionTypePlaylist},
		{name: "explicit type", rawID: collectionTypeAlbum, expectedTy: collectionTypeAlbum},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectCollectionType(tt.rawID, tt.url)
			if got != tt.expectedTy {
				t.Fatalf("detectCollectionType()=%q, want=%q", got, tt.expectedTy)
			}
		})
	}
}

func TestFormatExpandableQuote(t *testing.T) {
	quote := formatExpandableQuote("第一行\n第二行")
	if !strings.HasPrefix(quote, ">简介\n") {
		t.Fatalf("quote should start with intro line, got %q", quote)
	}
	if !strings.Contains(quote, ">第一行\n>第二行||") {
		t.Fatalf("quote should contain expandable marker, got %q", quote)
	}
}

func TestFormatPlaylistInfoUsesCollectionLabelAndQuote(t *testing.T) {
	info := formatPlaylistInfo(&platform.Playlist{
		Title:       "测试专辑",
		Description: "这是第一行\n这是第二行",
		TrackCount:  12,
		URL:         "https://music.163.com/album?id=3411281",
	}, "专辑")

	if !strings.Contains(info, "专辑: [测试专辑](https://music.163.com/album?id=3411281)") {
		t.Fatalf("expected album label in playlist info, got %q", info)
	}
	if !strings.Contains(info, ">简介") {
		t.Fatalf("expected quote intro in playlist info, got %q", info)
	}
	if !strings.Contains(info, "||") {
		t.Fatalf("expected expandable marker in playlist info, got %q", info)
	}
}
