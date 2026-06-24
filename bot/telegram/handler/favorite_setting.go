package handler

import (
	"context"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
)

// Group-favorites is a per-group, three-state setting controlling whether a
// shared group favorites list is available and who may add to it:
//
//	on    – anyone in the group may add group favorites (default)
//	admin – only group admins may add; everyone may view
//	off   – group favorites disabled entirely
//
// It is GroupOnly, so it never appears in the per-user settings menu. It is read
// by group chat ID, which guest mode also has (Message.Chat.ID) but inline mode
// does not — so group favorites are unavailable on the inline path by design.
const (
	GroupFavPlugin = "telegram"
	GroupFavKey    = "group_favorites"
	GroupFavOn     = "on"
	GroupFavAdmin  = "admin"
	GroupFavOff    = "off"
)

func GroupFavoritesSettingDefinition() botpkg.PluginSettingDefinition {
	return botpkg.PluginSettingDefinition{
		Plugin:         GroupFavPlugin,
		Key:            GroupFavKey,
		Title:          "启用群聊收藏",
		TitleKey:       "set_pdef_groupfav_title",
		Description:    "群内是否提供共享的群聊收藏",
		DescriptionKey: "set_pdef_groupfav_desc",
		DefaultGroup:   GroupFavOn,
		DefaultUser:    GroupFavOff,
		GroupOnly:      true,
		Order:          121,
		Options: []botpkg.PluginSettingOption{
			{Value: GroupFavOn, Label: "开", LabelKey: "set_state_on"},
			{Value: GroupFavAdmin, Label: "仅管理员收藏", LabelKey: "set_pdef_groupfav_admin"},
			{Value: GroupFavOff, Label: "关", LabelKey: "set_state_off"},
		},
	}
}

// resolveGroupFavoritesMode returns the group-favorites mode for a chat, falling
// back to the default ("on") when unset or invalid. chatID must be a real group
// chat ID; pass 0 (e.g. inline mode, no chat ID) to get GroupFavOff.
func resolveGroupFavoritesMode(ctx context.Context, repo botpkg.SongRepository, chatID int64) string {
	if repo == nil || chatID == 0 {
		return GroupFavOff
	}
	mode := GroupFavOn
	if val, err := repo.GetPluginSetting(ctx, botpkg.PluginScopeGroup, chatID, GroupFavPlugin, GroupFavKey); err == nil {
		switch strings.TrimSpace(strings.ToLower(val)) {
		case GroupFavOn:
			mode = GroupFavOn
		case GroupFavAdmin:
			mode = GroupFavAdmin
		case GroupFavOff:
			mode = GroupFavOff
		}
	}
	return mode
}

// groupFavoritesAvailable reports whether group favorites are usable at all
// (mode is on or admin) for a chat.
func groupFavoritesAvailable(mode string) bool {
	return mode == GroupFavOn || mode == GroupFavAdmin
}
