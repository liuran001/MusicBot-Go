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
	aboutText = `*MusicBot\-Go*
版本: %s
源码: https://github\.com/liuran001/MusicBot\-Go

\[编译环境\] %s
\[编译日期\] %s
\[运行环境\] %s
%s`
	uploadFailed     = "下载/发送失败\n%v"
	hitCache         = "命中缓存, 正在发送中..."
	inputIDorKeyword = "请输入歌曲ID或歌曲关键词，或使用 /rmcache all 清空所有缓存"
	tapToDownload    = "点我缓存歌曲"
	inlineCacheHint  = "如果搜索结果不符可以点击上方缓存"
	inlineTapToSend  = "如果没反应点此刷新"
	sendMeTo         = "发送到聊天..."
	waitForDown      = "等待下载中..."
	fetchInfo        = "正在获取歌曲信息..."
	fetchInfoFailed  = "获取歌曲信息失败"
	downloading      = "下载中..."
	uploading        = "下载完成, 发送中..."
	md5VerFailed     = "MD5校验失败"
	downloadTimeout  = "下载超时"
	inputKeyword     = "请输入搜索关键词"
	inputContent     = "请输入歌曲关键词/歌曲或专辑分享链接/歌曲ID"
	searching        = "搜索中..."
	fetchingPlaylist = "正在获取歌单..."
	fetchingLyric    = "正在获取歌词中"
	noResults        = "未找到结果"
	playlistEmpty    = "歌单为空"
	getLrcFailed     = "获取歌词失败, 歌曲可能不存在或为纯音乐"
	statusInfo       = `*\[统计信息\]*
数据库中总缓存歌曲数量: %d
当前对话 \[%s\] 缓存歌曲数量: %d
当前用户 \[[%d](tg://user?id=%d)\] 缓存歌曲数量: %d
成功发送音乐次数: %d
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
	text := "欢迎使用 MusicBot\\-Go \\!\n" +
		"这是一个强大的音乐下载机器人，支持多平台歌曲的搜索与下载\n"
	if isPrivateChat {
		text += "直接发送 链接/歌曲名/ID 即可下载对应歌曲\n"
	}
	text += "\n使用方法:\n" +
		"`/music` \\<链接\\|ID\\|关键词\\> \\<\\(可选\\)搜索平台\\> \\<\\(可选\\)音质\\> \\- 下载歌曲\n" +
		"`/search` \\<关键词\\> \\<\\(可选\\)搜索平台\\> \\<\\(可选\\)音质\\> \\- 搜索歌曲\n" +
		"`/lyric` \\<链接\\|ID\\|关键词\\> \\<\\(可选\\)搜索平台\\> \\- 获取歌词\n"
	if recognizeEnabled {
		text += "`/recognize` \\- 听歌识曲 \\(回复一条语音消息\\)\n"
	}
	text += "`/settings` \\- 默认音质/搜索平台设置\n\n" +
		"平台参数:\n" + aliasText + "\n" +
		"音质参数: `low` / `high` / `lossless` / `hires`\n\n" +
		"支持平台: " + platformText + "\n" +
		"示例:\n" +
		"`/music https://music.163.com/song/1859603835`\n" +
		"`/music 周杰伦`\n" +
		"`/search 周杰伦 qq`"
	adminText := buildAdminHelp(adminCommands)
	if isAdmin && adminText != "" {
		text += "\n\n管理员命令:\n" + adminText
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
