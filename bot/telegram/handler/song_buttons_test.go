package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

func TestBuildSongBottomKeyboardKeepsLyricsWhenAvailabilityUnknown(t *testing.T) {
	keyboard := buildSongBottomKeyboard(context.Background(), nil, songButtonOptions{
		platformName: "netease",
		trackID:      "12345",
		quality:      "hires",
		requesterID:  99,
		botName:      "MyBot",
	})

	if !keyboardHasSongLyricButton(keyboard) {
		t.Fatalf("expected lyrics button when availability is unknown")
	}
}

func TestBuildSongBottomKeyboardLyricsButtonUsesCallbackOnlyInPrivateChat(t *testing.T) {
	privateKeyboard := buildSongBottomKeyboard(context.Background(), nil, songButtonOptions{
		platformName: "netease",
		trackID:      "12345",
		quality:      "hires",
		requesterID:  99,
		botName:      "MyBot",
	})
	privateButton := findSongLyricButton(privateKeyboard)
	if privateButton == nil {
		t.Fatalf("expected private lyric button")
	}
	if privateButton.CallbackData != "lyric netease 12345 hires 99" || privateButton.URL != "" {
		t.Fatalf("private lyric button should use callback, got callback=%q url=%q", privateButton.CallbackData, privateButton.URL)
	}

	groupKeyboard := buildSongBottomKeyboard(context.Background(), nil, songButtonOptions{
		platformName: "netease",
		trackID:      "12345",
		quality:      "hires",
		requesterID:  99,
		botName:      "MyBot",
		chatID:       -1001,
		isGroup:      true,
	})
	groupButton := findSongLyricButton(groupKeyboard)
	if groupButton == nil {
		t.Fatalf("expected group lyric button")
	}
	if groupButton.CallbackData != "" || groupButton.URL != "https://t.me/MyBot?start=lyric_netease_12345" {
		t.Fatalf("group lyric button should use deep link, got callback=%q url=%q", groupButton.CallbackData, groupButton.URL)
	}

	inlineKeyboard := buildSongBottomKeyboard(context.Background(), nil, songButtonOptions{
		platformName:  "netease",
		trackID:       "12345",
		quality:       "hires",
		requesterID:   99,
		botName:       "MyBot",
		inlineContext: true,
	})
	inlineButton := findSongLyricButton(inlineKeyboard)
	if inlineButton == nil {
		t.Fatalf("expected inline lyric button")
	}
	if inlineButton.CallbackData != "" || inlineButton.URL != "https://t.me/MyBot?start=lyric_netease_12345" {
		t.Fatalf("inline lyric button should use deep link, got callback=%q url=%q", inlineButton.CallbackData, inlineButton.URL)
	}
}

func TestBuildSongBottomKeyboardOmitsLyricsWhenTrackHasNoLyrics(t *testing.T) {
	noLyrics := false
	keyboard := buildSongBottomKeyboard(context.Background(), nil, songButtonOptions{
		platformName:    "amazonmusic",
		trackID:         "B0TEST",
		quality:         "hires",
		requesterID:     99,
		botName:         "MyBot",
		lyricsAvailable: &noLyrics,
	})

	if keyboardHasSongLyricButton(keyboard) {
		t.Fatalf("expected no lyrics button when track explicitly has no lyrics")
	}
	if !keyboardHasFavoriteButton(keyboard) {
		t.Fatalf("expected favorite button to remain")
	}
}

func TestBuildSongBottomKeyboardOmitsLyricsWhenPlatformDoesNotSupportLyrics(t *testing.T) {
	manager := platform.NewManager()
	manager.Register(stubSearchPlatform{name: "soda"})

	keyboard := buildSongBottomKeyboard(context.Background(), nil, songButtonOptions{
		platformName:    "soda",
		trackID:         "12345",
		quality:         "hires",
		requesterID:     99,
		botName:         "MyBot",
		platformManager: manager,
	})

	if keyboardHasSongLyricButton(keyboard) {
		t.Fatalf("expected no lyrics button for platform without lyrics support")
	}
	if !keyboardHasFavoriteButton(keyboard) {
		t.Fatalf("expected favorite button to remain")
	}
}

func keyboardHasSongLyricButton(keyboard *telego.InlineKeyboardMarkup) bool {
	return findSongLyricButton(keyboard) != nil
}

func findSongLyricButton(keyboard *telego.InlineKeyboardMarkup) *telego.InlineKeyboardButton {
	if keyboard == nil {
		return nil
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if strings.HasPrefix(button.CallbackData, "lyric ") || strings.Contains(button.URL, "start=lyric_") {
				return &button
			}
		}
	}
	return nil
}

func keyboardHasFavoriteButton(keyboard *telego.InlineKeyboardMarkup) bool {
	if keyboard == nil {
		return false
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if strings.HasPrefix(button.CallbackData, "fav ") {
				return true
			}
		}
	}
	return false
}
