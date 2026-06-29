package handler

import (
	"context"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
)

// When a channel post is automatically forwarded into the channel's linked
// discussion group, Telegram strips the inline keyboard from the copy. For a
// song the bot posted to the channel (recognizable by its "via @bot" caption),
// that means the lyrics / favorite / group-favorite buttons vanish in the
// comment thread. This per-group setting controls whether the bot re-posts
// those buttons as a reply in the comment thread. GroupOnly: it only makes
// sense in the linked discussion group, never in a per-user menu.
const (
	CommentButtonsPlugin = "telegram"
	CommentButtonsKey    = "comment_buttons"
	CommentButtonsOn     = "on"
	CommentButtonsOff    = "off"
)

func CommentButtonsSettingDefinition() botpkg.PluginSettingDefinition {
	return botpkg.PluginSettingDefinition{
		Plugin:         CommentButtonsPlugin,
		Key:            CommentButtonsKey,
		Title:          "频道评论区补充按钮",
		TitleKey:       "set_pdef_comment_buttons_title",
		Description:    "频道歌曲转发到关联讨论群时，在评论区补发歌词/收藏按钮",
		DescriptionKey: "set_pdef_comment_buttons_desc",
		DefaultGroup:   CommentButtonsOn,
		DefaultUser:    CommentButtonsOff,
		GroupOnly:      true,
		Order:          122,
		Options: []botpkg.PluginSettingOption{
			{Value: CommentButtonsOn, Label: "开", LabelKey: "set_state_on"},
			{Value: CommentButtonsOff, Label: "关", LabelKey: "set_state_off"},
		},
	}
}

// resolveCommentButtonsEnabled returns whether the comment-thread button reposting
// is enabled for a discussion group, defaulting to on when unset.
func resolveCommentButtonsEnabled(ctx context.Context, repo botpkg.SongRepository, chatID int64) bool {
	if repo == nil || chatID == 0 {
		return false
	}
	enabled := true
	if val, err := repo.GetPluginSetting(ctx, botpkg.PluginScopeGroup, chatID, CommentButtonsPlugin, CommentButtonsKey); err == nil && strings.TrimSpace(val) != "" {
		enabled = strings.TrimSpace(strings.ToLower(val)) == CommentButtonsOn
	}
	return enabled
}
