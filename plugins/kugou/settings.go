package kugou

import botpkg "github.com/liuran001/MusicBot-Go/bot"

const (
	NoHiResWhenDefaultKey = "no_hires_when_default"
	NoHiResWhenDefaultOn  = "on"
	NoHiResWhenDefaultOff = "off"
)

func NoHiResWhenDefaultDefinition() botpkg.PluginSettingDefinition {
	return botpkg.PluginSettingDefinition{
		Plugin:       "kugou",
		Key:          NoHiResWhenDefaultKey,
		Title:        "酷狗不返回 Hi-Res",
		Description:  "开启后，当默认音质是 Hi-Res 且并非用户显式指定时，酷狗自动降级为无损返回",
		DefaultUser:  NoHiResWhenDefaultOn,
		DefaultGroup: NoHiResWhenDefaultOn,
		Order:        100,
		Options: []botpkg.PluginSettingOption{
			{Value: NoHiResWhenDefaultOn, Label: "开"},
			{Value: NoHiResWhenDefaultOff, Label: "关"},
		},
	}
}
