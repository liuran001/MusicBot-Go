package handler

import (
	"context"
	"errors"
	"strings"
	"testing"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
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

func TestUserVisibleDownloadErrorMappings(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: "处理超时，请稍后重试"},
		{name: "context canceled", err: context.Canceled, want: "任务已取消，请稍后重试"},
		{name: "download overloaded", err: errDownloadQueueOverloaded, want: "当前下载任务过多，请稍后再试"},
		{name: "upload queue full text", err: errors.New("upload queue is full"), want: "当前发送任务过多，请稍后再试"},
		{name: "rate limited", err: platform.ErrRateLimited, want: "请求过于频繁，请稍后重试"},
		{name: "auth required", err: platform.ErrAuthRequired, want: "平台认证已失效，请联系管理员更新凭据"},
		{name: "unavailable", err: platform.ErrUnavailable, want: "当前歌曲暂不可用，请稍后再试"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := userVisibleDownloadError(tt.err)
			if got != tt.want {
				t.Fatalf("unexpected message: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestIsTelegramFileIDInvalid(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "wrong file identifier", err: errors.New("Bad Request: wrong file identifier/HTTP URL specified"), want: true},
		{name: "file_id_invalid", err: errors.New("400 FILE_ID_INVALID"), want: true},
		{name: "invalid file id", err: errors.New("invalid file id"), want: true},
		{name: "other error", err: errors.New("network timeout"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTelegramFileIDInvalid(tt.err); got != tt.want {
				t.Fatalf("unexpected result: got %v want %v", got, tt.want)
			}
		})
	}
}
