package handler

import (
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

func mentionEntity(offset, length int) telego.MessageEntity {
	return telego.MessageEntity{Type: telego.EntityTypeMention, Offset: offset, Length: length}
}

func TestStripBotMention(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		entities []telego.MessageEntity
		botName  string
		want     string
	}{
		{
			name:     "leading mention with space",
			text:     "@MusicBot 晴天",
			entities: []telego.MessageEntity{mentionEntity(0, 9)},
			botName:  "MusicBot",
			want:     "晴天",
		},
		{
			name:     "leading mention case-insensitive",
			text:     "@musicbot 晴天",
			entities: []telego.MessageEntity{mentionEntity(0, 9)},
			botName:  "MusicBot",
			want:     "晴天",
		},
		{
			name:     "trailing mention",
			text:     "晴天 @MusicBot",
			entities: []telego.MessageEntity{mentionEntity(3, 9)},
			botName:  "MusicBot",
			want:     "晴天",
		},
		{
			name:     "mention after CJK uses utf16 offset",
			text:     "周杰伦 @MusicBot 晴天",
			entities: []telego.MessageEntity{mentionEntity(4, 9)},
			botName:  "MusicBot",
			want:     "周杰伦 晴天",
		},
		{
			name:     "mention after emoji uses utf16 offset",
			text:     "🎵 @MusicBot 晴天",
			entities: []telego.MessageEntity{mentionEntity(3, 9)},
			botName:  "MusicBot",
			want:     "🎵 晴天",
		},
		{
			name:     "no entities fallback string match",
			text:     "@MusicBot 晴天",
			entities: nil,
			botName:  "MusicBot",
			want:     "晴天",
		},
		{
			name:     "fallback does not match different bot prefix",
			text:     "@MusicBot2 晴天",
			entities: nil,
			botName:  "MusicBot",
			want:     "@MusicBot2 晴天",
		},
		{
			name:     "botName with leading @ still works",
			text:     "@MusicBot 晴天",
			entities: []telego.MessageEntity{mentionEntity(0, 9)},
			botName:  "@MusicBot",
			want:     "晴天",
		},
		{
			name:     "empty botName returns text unchanged",
			text:     "@MusicBot 晴天",
			entities: []telego.MessageEntity{mentionEntity(0, 9)},
			botName:  "",
			want:     "@MusicBot 晴天",
		},
		{
			name:     "mention of another bot is ignored",
			text:     "@OtherBot 晴天",
			entities: []telego.MessageEntity{mentionEntity(0, 9)},
			botName:  "MusicBot",
			want:     "@OtherBot 晴天",
		},
		{
			name:     "only mention yields empty",
			text:     "@MusicBot",
			entities: []telego.MessageEntity{mentionEntity(0, 9)},
			botName:  "MusicBot",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripBotMention(tt.text, tt.entities, tt.botName)
			if got != tt.want {
				t.Fatalf("stripBotMention(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestRepliedMessageText(t *testing.T) {
	tests := []struct {
		name  string
		reply *telego.Message
		want  string
	}{
		{name: "nil reply", reply: nil, want: ""},
		{name: "text reply", reply: &telego.Message{Text: " 晴天 "}, want: "晴天"},
		{name: "caption fallback", reply: &telego.Message{Caption: " 周杰伦 - 晴天 "}, want: "周杰伦 - 晴天"},
		{name: "text preferred over caption", reply: &telego.Message{Text: "稻香", Caption: "ignored"}, want: "稻香"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repliedMessageText(tt.reply)
			if got != tt.want {
				t.Fatalf("repliedMessageText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGuestSearchStore_StoreGetDelete(t *testing.T) {
	store := newGuestSearchStore()
	state := &searchState{keyword: "晴天", platform: "netease", requesterID: 12345}
	token := store.store(state)
	if token == "" {
		t.Fatal("store returned empty token")
	}
	got, ok := store.get(token)
	if !ok || got == nil {
		t.Fatal("get returned no state")
	}
	if got.keyword != "晴天" {
		t.Fatalf("got keyword %q, want 晴天", got.keyword)
	}
	store.delete(token)
	if _, ok := store.get(token); ok {
		t.Fatal("state still present after delete")
	}
}

func TestRenderGuestSearchPage_SelectButtonsUseInlineFlow(t *testing.T) {
	h := &GuestModeHandler{PlatformManager: nil, SearchHandler: &SearchHandler{}}
	state := &searchState{
		keyword:     "晴天",
		platform:    "netease",
		quality:     "hires",
		requesterID: 12345,
		limit:       48,
		currentPage: 1,
		action:      "music",
	}
	state.setTracks("netease", []platform.Track{{ID: "33894312", Title: "晴天", Artists: []platform.Artist{{Name: "周杰伦"}}}})
	token := h.guestSearchStore().store(state)

	text, keyboard := h.renderGuestSearchPage(state, token, 1)
	if text == "" {
		t.Fatal("empty page text")
	}
	if keyboard == nil || len(keyboard.InlineKeyboard) == 0 {
		t.Fatal("no keyboard rows")
	}
	// First row should be the result-select buttons, whose callback enters the
	// inline download flow ("music i ...") so guest reuses the normal pipeline.
	found := false
	for _, row := range keyboard.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != "" && len(btn.CallbackData) >= 7 && btn.CallbackData[:7] == "music i" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected a result button with 'music i' inline-send callback")
	}
}
