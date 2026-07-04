package handler

import (
	"context"
	"errors"
	"testing"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

func TestLyricUnavailableResult(t *testing.T) {
	if !lyricUnavailableResult(nil, nil) {
		t.Fatalf("nil lyrics with nil error should be treated as unavailable")
	}
	if !lyricUnavailableResult(nil, platform.NewUnavailableError("netease", "lyrics", "1")) {
		t.Fatalf("ErrUnavailable should be treated as unavailable")
	}
	if !lyricUnavailableResult(nil, platform.NewNotFoundError("netease", "lyrics", "1")) {
		t.Fatalf("ErrNotFound should be treated as unavailable")
	}
	if lyricUnavailableResult(nil, platform.ErrRateLimited) {
		t.Fatalf("rate limit should not be cached as unavailable")
	}
	if lyricUnavailableResult(nil, errors.New("temporary network error")) {
		t.Fatalf("generic errors should not be cached as unavailable")
	}
	if lyricUnavailableResult(&platform.Lyrics{Plain: "la"}, nil) {
		t.Fatalf("lyrics with nil error should be available")
	}
}

func TestRemoveSongLyricsButtons(t *testing.T) {
	deepLink := "https://t.me/MyBot?start=lyric_netease_12345"
	keyboard, changed := removeSongLyricsButtons(&telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{{Text: "send", SwitchInlineQuery: ptrString("https://example.test/song")}},
			{
				{Text: "lyrics", CallbackData: "lyric netease 12345 hires 99"},
				{Text: "favorite", CallbackData: "fav t u netease 12345"},
			},
			{
				{Text: "lyrics", URL: deepLink},
				{Text: "other", CallbackData: "noop"},
			},
		},
	})
	if !changed {
		t.Fatalf("expected keyboard to change")
	}
	if keyboardHasSongLyricButton(keyboard) {
		t.Fatalf("lyrics button should be removed")
	}
	if !keyboardHasFavoriteButton(keyboard) {
		t.Fatalf("favorite button should remain")
	}
	if len(keyboard.InlineKeyboard) != 3 {
		t.Fatalf("expected non-empty rows to remain, got %d", len(keyboard.InlineKeyboard))
	}
}

func TestMarkSongLyricsUnavailableUpdatesCachedQualities(t *testing.T) {
	ctx := context.Background()
	repo := newStubRepo()
	for _, quality := range []string{"high", "hires", "lossless"} {
		if err := repo.Create(ctx, &botpkg.SongInfo{
			Platform: "netease",
			TrackID:  "12345",
			Quality:  quality,
			FileID:   "file-" + quality,
			SongName: "Song",
		}); err != nil {
			t.Fatalf("create %s: %v", quality, err)
		}
	}
	if err := repo.Create(ctx, &botpkg.SongInfo{
		Platform: "netease",
		TrackID:  "67890",
		Quality:  "high",
		FileID:   "other",
		SongName: "Other",
	}); err != nil {
		t.Fatalf("create other: %v", err)
	}

	h := &LyricCallbackHandler{Repo: repo}
	h.markSongLyricsUnavailable(ctx, "netease", "12345", "high")

	for _, quality := range []string{"high", "hires", "lossless"} {
		song, err := repo.FindByPlatformTrackID(ctx, "netease", "12345", quality)
		if err != nil || song == nil {
			t.Fatalf("find %s: %v", quality, err)
		}
		if song.LyricsAvailable == nil || *song.LyricsAvailable {
			t.Fatalf("%s LyricsAvailable = %v, want false", quality, song.LyricsAvailable)
		}
	}
	other, err := repo.FindByPlatformTrackID(ctx, "netease", "67890", "high")
	if err != nil || other == nil {
		t.Fatalf("find other: %v", err)
	}
	if other.LyricsAvailable != nil {
		t.Fatalf("other track should be unchanged, got %v", other.LyricsAvailable)
	}
}

func ptrString(value string) *string {
	return &value
}
