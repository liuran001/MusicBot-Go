package bilibili

import (
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
)

const (
	ParseModeKey          = "parse_mode"
	ParseModeOn           = "on"
	ParseModeMusicKichiku = "music_kichiku"
	ParseModeOff          = "off"
)

func ParseModeDefinition() botpkg.PluginSettingDefinition {
	return botpkg.PluginSettingDefinition{
		Plugin:                "bilibili",
		Key:                   ParseModeKey,
		Title:                 "B站链接自动解析",
		Description:           "控制哔哩哔哩链接自动解析行为",
		DefaultUser:           ParseModeOn,
		DefaultGroup:          ParseModeMusicKichiku,
		RequireAutoLinkDetect: true,
		Order:                 100,
		Options: []botpkg.PluginSettingOption{
			{Value: ParseModeOn, Label: "开"},
			{Value: ParseModeMusicKichiku, Label: "仅音乐/鬼畜区"},
			{Value: ParseModeOff, Label: "关"},
		},
	}
}

func normalizeParseMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ParseModeOn, ParseModeMusicKichiku, ParseModeOff:
		return strings.TrimSpace(mode)
	default:
		return ParseModeOn
	}
}
