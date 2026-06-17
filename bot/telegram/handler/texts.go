package handler

import (
	"sort"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

var mdV2Replacer = strings.NewReplacer(
	"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(",
	"\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>",
	"#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|",
	"\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
)

var (
	aboutText = `*ℹ️ MusicBot\-Go*
版本：%s
源码：https://github\.com/liuran001/MusicBot\-Go

🧩 插件
%s

🛠 构建
编译环境：%s
编译日期：%s
运行环境：%s`
	hitCache          = "已命中缓存，正在发送…"
	inputIDorKeyword  = "请发送歌曲 ID 或关键词，或使用 /rmcache all 清空全部缓存"
	inlineTapToSend   = "没反应？点此刷新"
	sendMeTo          = "发送到聊天…"
	waitForDown       = "等待下载…"
	fetchInfo         = "正在获取歌曲信息…"
	fetchInfoFailed   = "获取歌曲信息失败"
	downloading       = "正在下载…"
	uploading         = "下载完成，正在发送…"
	md5VerFailed      = "MD5 校验失败"
	downloadTimeout   = "下载超时"
	inputKeyword      = "请发送搜索关键词\n\n示例：\n/search 周杰伦\n/search 起风了 qq"
	inputContent      = "请发送歌曲名、链接或 ID\n\n示例：\n/music 周杰伦\n/music https://music.163.com/song/1859603835"
	inputLyricContent = "请发送歌曲名、链接或 ID，或回复一条歌曲消息\n\n示例：\n/lyric 稻香\n/lyric 稻香 qrc"
	searching         = "正在搜索…"
	fetchingPlaylist  = "正在获取歌单…"
	fetchingLyric     = "正在获取歌词…"
	noResults         = "没有找到结果，换个关键词试试"
	playlistEmpty     = "歌单里没有歌曲"
	getLrcFailed      = "未找到歌词，可能是纯音乐或平台暂不支持"
	statusInfo        = `*📊 状态*

🎧 缓存
全部：%d 首
本聊天 \[%s\]：%d 首
你缓存 \[[%d](tg://user?id=%d)\]：%d 首
已发送：%d 次
`
	callbackText   = "Success"
	callbackDenied = "仅发起人或管理员可操作"
)

func buildHelpText(manager platform.Manager, isAdmin bool, adminCommands []admincmd.Command, recognizeEnabled bool, isPrivateChat bool) string {
	aliasText := buildAliasHint(manager)
	platformText := buildPlatformList(manager)
	if aliasText == "" {
		aliasText = "`163` / `qq`"
	}
	if platformText == "" {
		platformText = "网易云音乐, QQ音乐"
	}
	text := "*🎵 MusicBot\\-Go*\n\n" +
		"发送歌曲名、链接或 ID，即可搜索和下载音乐。\n"
	if isPrivateChat {
		text += "私聊中可直接发送内容，无需输入命令。\n"
	}
	text += "\n*🚀 常用命令*\n" +
		"`/music` 歌名\\|链接\\|ID \\[平台\\] \\[音质\\] \\- 下载歌曲\n" +
		"`/search` 关键词 \\[平台\\] \\[音质\\] \\- 搜索歌曲\n" +
		"`/lyric` 歌名\\|链接\\|ID \\[平台\\] \\- 获取歌词\n"
	if recognizeEnabled {
		text += "`/recognize` \\- 听歌识曲（回复一条语音）\n"
	}
	text += "`/settings` \\- 默认平台、音质与歌词格式\n" +
		"`/status` \\- 缓存与账号状态\n" +
		"`/about` \\- 版本与插件信息\n" +
		"\n*🎚 参数*\n" +
		"音质：`low` / `high` / `lossless` / `hires`\n" +
		"平台：\n" + aliasText + "\n" +
		"\n支持平台：" + platformText + "\n" +
		"\n*💡 示例*\n" +
		"`/music 周杰伦`\n" +
		"`/music https://music.163.com/song/1859603835`\n" +
		"`/search 起风了 qq`"
	adminText := buildAdminHelp(adminCommands)
	if isAdmin && adminText != "" {
		text += "\n\n*🛠 管理员命令*\n" + adminText
	}
	return text
}

func buildAdminHelp(adminCommands []admincmd.Command) string {
	if len(adminCommands) == 0 {
		return ""
	}
	items := make([]admincmd.Command, 0, len(adminCommands))
	for _, cmd := range adminCommands {
		if strings.TrimSpace(cmd.Name) == "" {
			continue
		}
		items = append(items, cmd)
	}
	if len(items) == 0 {
		return ""
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	lines := make([]string, 0, len(items))
	for _, cmd := range items {
		name := mdV2Replacer.Replace(cmd.Name)
		desc := mdV2Replacer.Replace(strings.TrimSpace(cmd.Description))
		if desc == "" {
			lines = append(lines, "`/"+name+"`")
			continue
		}
		lines = append(lines, "`/"+name+"` \\- "+desc)
	}
	return strings.Join(lines, "\n")
}

func buildAliasHint(manager platform.Manager) string {
	if manager == nil {
		return ""
	}
	metaList := manager.ListMeta()
	if len(metaList) == 0 {
		return ""
	}
	sort.Slice(metaList, func(i, j int) bool {
		left := strings.TrimSpace(metaList[i].DisplayName)
		if left == "" {
			left = strings.TrimSpace(metaList[i].Name)
		}
		right := strings.TrimSpace(metaList[j].DisplayName)
		if right == "" {
			right = strings.TrimSpace(metaList[j].Name)
		}
		if left == right {
			return strings.TrimSpace(metaList[i].Name) < strings.TrimSpace(metaList[j].Name)
		}
		return left < right
	})
	lines := make([]string, 0, len(metaList))
	for _, meta := range metaList {
		platformName := strings.TrimSpace(meta.Name)
		if platformName == "" {
			continue
		}
		aliases := meta.Aliases
		if len(aliases) == 0 {
			aliases = []string{platformName}
		}
		aliasSet := make(map[string]struct{})
		aliasItems := make([]string, 0, len(aliases))
		for _, alias := range aliases {
			key := platform.NormalizeAliasToken(alias)
			if key == "" {
				continue
			}
			if _, ok := aliasSet[key]; ok {
				continue
			}
			aliasSet[key] = struct{}{}
			aliasItems = append(aliasItems, key)
		}
		if len(aliasItems) == 0 {
			continue
		}
		sort.Strings(aliasItems)
		for i := range aliasItems {
			aliasItems[i] = "`" + mdV2Replacer.Replace(aliasItems[i]) + "`"
		}
		display := strings.TrimSpace(meta.DisplayName)
		if display == "" {
			display = platformName
		}
		lines = append(lines, "• "+mdV2Replacer.Replace(display)+": "+strings.Join(aliasItems, " / "))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func buildPlatformList(manager platform.Manager) string {
	if manager == nil {
		return ""
	}
	metaList := manager.ListMeta()
	if len(metaList) == 0 {
		return ""
	}
	items := make([]string, 0, len(metaList))
	for _, meta := range metaList {
		display := strings.TrimSpace(meta.DisplayName)
		if display == "" {
			display = meta.Name
		}
		if display == "" {
			continue
		}
		items = append(items, mdV2Replacer.Replace(display))
	}
	return strings.Join(items, ", ")
}
