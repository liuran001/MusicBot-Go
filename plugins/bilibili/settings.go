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

	SearchFilterKey = "search_filter_kichiku"
	SearchFilterOn  = "on"
	SearchFilterOff = "off"
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

func SearchFilterDefinition() botpkg.PluginSettingDefinition {
	return botpkg.PluginSettingDefinition{
		Plugin:                "bilibili",
		Key:                   SearchFilterKey,
		Title:                 "B站搜索默认筛选音乐/鬼畜区内容",
		Description:           "在Bilibili搜索时，是否仅搜寻音乐/鬼畜相关分区（如关闭则搜索所有）",
		DefaultUser:           SearchFilterOn,
		DefaultGroup:          SearchFilterOn,
		RequireAutoLinkDetect: false,
		Order:                 110,
		Options: []botpkg.PluginSettingOption{
			{Value: SearchFilterOn, Label: "开"},
			{Value: SearchFilterOff, Label: "关"},
		},
	}
}
