package handler

import (
	"context"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/mymmrac/telego"
)

const (
	ForwardButtonPlugin = "telegram"
	ForwardButtonKey    = "show_forward_button"
	ForwardButtonOn     = "on"
	ForwardButtonOff    = "off"
)

func ForwardButtonSettingDefinition() botpkg.PluginSettingDefinition {
	return botpkg.PluginSettingDefinition{
		Plugin:                ForwardButtonPlugin,
		Key:                   ForwardButtonKey,
		Title:                 "展示 发送到聊天 按钮",
		Description:           "发送歌曲时是否显示“发送到聊天...”按钮",
		DefaultUser:           ForwardButtonOn,
		DefaultGroup:          ForwardButtonOn,
		RequireAutoLinkDetect: false,
		Order:                 120,
		Options: []botpkg.PluginSettingOption{
			{Value: ForwardButtonOn, Label: "开"},
			{Value: ForwardButtonOff, Label: "关"},
		},
	}
}

func resolveForwardButtonEnabled(ctx context.Context, repo botpkg.SongRepository, scopeType string, scopeID int64) bool {
	enabled := true
	if repo == nil || scopeID == 0 {
		return enabled
	}
	if val, err := repo.GetPluginSetting(ctx, scopeType, scopeID, ForwardButtonPlugin, ForwardButtonKey); err == nil && strings.TrimSpace(val) != "" {
		enabled = strings.TrimSpace(strings.ToLower(val)) == ForwardButtonOn
	}
	return enabled
}

func resolveForwardButtonEnabledForMessage(ctx context.Context, repo botpkg.SongRepository, message *telego.Message) bool {
	if message == nil {
		return true
	}
	if message.Chat.Type != "private" {
		return resolveForwardButtonEnabled(ctx, repo, botpkg.PluginScopeGroup, message.Chat.ID)
	}
	if message.From != nil {
		return resolveForwardButtonEnabled(ctx, repo, botpkg.PluginScopeUser, message.From.ID)
	}
	return true
}

func resolveForwardButtonEnabledForUser(ctx context.Context, repo botpkg.SongRepository, userID int64) bool {
	if userID == 0 {
		return true
	}
	return resolveForwardButtonEnabled(ctx, repo, botpkg.PluginScopeUser, userID)
}
