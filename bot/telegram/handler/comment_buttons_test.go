package handler

import (
	"context"
	"testing"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/mymmrac/telego"
)

func TestCommentButtons_captionFromBot(t *testing.T) {
	h := &CommentButtonsHandler{BotName: "@MyMusicBot"}
	tests := []struct {
		name    string
		caption string
		want    bool
	}{
		{"matches via signature", "「Song」- Artist\nvia @MyMusicBot", true},
		{"case-insensitive", "「Song」- Artist\nvia @mymusicbot", true},
		{"different bot", "「Song」- Artist\nvia @SomeoneElse", false},
		{"no signature", "just a random audio caption", false},
		{"empty caption", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &telego.Message{Caption: tt.caption}
			if got := h.captionFromBot(msg); got != tt.want {
				t.Fatalf("captionFromBot(%q) = %v, want %v", tt.caption, got, tt.want)
			}
		})
	}
}

func TestCommentButtons_captionFromBot_emptyBotName(t *testing.T) {
	h := &CommentButtonsHandler{BotName: ""}
	msg := &telego.Message{Caption: "via @anything"}
	if h.captionFromBot(msg) {
		t.Fatal("expected false when bot name is unset")
	}
}

func TestCommentButtons_captionURLCandidates(t *testing.T) {
	msg := &telego.Message{
		Caption: "「Song」- Artist\nhttps://bare.example/x",
		CaptionEntities: []telego.MessageEntity{
			{Type: "bold", Offset: 0, Length: 4},
			{Type: "text_link", URL: "https://music.example/track/123"},
			{Type: "text_link", URL: "https://music.example/track/123"}, // duplicate
			{Type: "text_link", URL: ""},                                 // empty ignored
		},
	}
	got := captionURLCandidates(msg)
	// text_link URLs first (deduped), then bare URLs.
	want := []string{"https://music.example/track/123", "https://bare.example/x"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCommentButtons_resolveTrack_byFileID(t *testing.T) {
	repo := newStubRepo()
	_ = repo.Create(context.Background(), &botpkg.SongInfo{
		Platform: "netease", TrackID: "42", Quality: "lossless",
		TrackURL: "https://music.example/song/42", FileID: "FILE42",
	})
	h := &CommentButtonsHandler{Repo: repo, PlatformManager: newStubManager()}

	msg := &telego.Message{Audio: &telego.Audio{FileID: "FILE42"}}
	plat, id, url, quality := h.resolveTrack(context.Background(), msg)
	if plat != "netease" || id != "42" || url != "https://music.example/song/42" || quality != "lossless" {
		t.Fatalf("file-id lookup = (%q,%q,%q,%q)", plat, id, url, quality)
	}
}

func TestCommentButtons_resolveTrack_byCaptionURL(t *testing.T) {
	mgr := newStubManager()
	mgr.AddURLRule("https://music.example/track/123", "qqmusic", "123")
	h := &CommentButtonsHandler{Repo: newStubRepo(), PlatformManager: mgr}

	msg := &telego.Message{
		Audio:   &telego.Audio{FileID: "UNKNOWN"},
		Caption: "「Song」- Artist\nvia @bot",
		CaptionEntities: []telego.MessageEntity{
			{Type: "text_link", URL: "https://music.example/track/123"},
		},
	}
	plat, id, url, quality := h.resolveTrack(context.Background(), msg)
	if plat != "qqmusic" || id != "123" || url != "https://music.example/track/123" || quality != "" {
		t.Fatalf("caption-url lookup = (%q,%q,%q,%q)", plat, id, url, quality)
	}
}

func TestCommentButtons_resolveTrack_noMatch(t *testing.T) {
	h := &CommentButtonsHandler{Repo: newStubRepo(), PlatformManager: newStubManager()}
	msg := &telego.Message{
		Audio:   &telego.Audio{FileID: "UNKNOWN"},
		Caption: "no links here",
	}
	plat, id, _, _ := h.resolveTrack(context.Background(), msg)
	if plat != "" || id != "" {
		t.Fatalf("expected no match, got (%q,%q)", plat, id)
	}
}

func TestResolveCommentButtonsEnabled(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	// Unset → default on.
	if !resolveCommentButtonsEnabled(ctx, repo, 100) {
		t.Fatal("expected default enabled when unset")
	}

	_ = repo.SetPluginSetting(ctx, botpkg.PluginScopeGroup, 100, CommentButtonsPlugin, CommentButtonsKey, CommentButtonsOff)
	if resolveCommentButtonsEnabled(ctx, repo, 100) {
		t.Fatal("expected disabled after setting off")
	}

	_ = repo.SetPluginSetting(ctx, botpkg.PluginScopeGroup, 100, CommentButtonsPlugin, CommentButtonsKey, CommentButtonsOn)
	if !resolveCommentButtonsEnabled(ctx, repo, 100) {
		t.Fatal("expected enabled after setting on")
	}

	// Nil repo / zero chat → false (cannot resolve).
	if resolveCommentButtonsEnabled(ctx, nil, 100) {
		t.Fatal("expected false for nil repo")
	}
	if resolveCommentButtonsEnabled(ctx, repo, 0) {
		t.Fatal("expected false for zero chat id")
	}
}
