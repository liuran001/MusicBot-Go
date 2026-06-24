package handler

import (
	"testing"

	"github.com/mymmrac/telego"
)

func TestMentionSurrounded(t *testing.T) {
	manager := newStubManager()
	manager.Register(newStubPlatform("netease"))
	manager.Register(newStubPlatform("qqmusic"))
	manager.aliases["qq"] = "qqmusic"
	r := &MentionRouter{PlatformManager: manager, BotName: "MyBot"}

	cases := []struct {
		text string
		want bool
	}{
		{"@MyBot 晴天", false},    // leading mention
		{"晴天 @MyBot", false},    // trailing mention
		{"@MyBot", false},       // bare mention
		{"晴天 @MyBot 周杰伦", true}, // query text on both sides
		{"今天 @MyBot 在吗", true},  // conversational, mid-sentence
		{"晴天 @MyBot qq", false}, // trailing segment is only a platform option
	}
	for _, c := range cases {
		msg := &telego.Message{Text: c.text}
		if got := r.mentionSurrounded(msg); got != c.want {
			t.Errorf("mentionSurrounded(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
