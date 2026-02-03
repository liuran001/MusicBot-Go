package handler

import "strings"

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
	helpText = "欢迎使用 MusicBot\\-Go \\!\n" +
		"这是一个强大的音乐下载机器人，支持网易云/QQ音乐歌曲的搜索与下载\n" +
		"直接发送 链接/歌曲名/ID 即可下载对应歌曲\n\n" +
		"使用方法:\n" +
		"`/music` \\<链接\\|ID\\|关键词\\> \\- 下载歌曲\n" +
		"`/search` \\<关键词\\> \\<\\(可选\\)搜索平台: 163/qq\\> \\- 搜索歌曲\n" +
		"`/lyric` \\<链接\\|ID\\|关键词\\> \\- 获取歌词\n" +
		"`/recognize` \\- 听歌识曲 \\(回复一条语音消息\\)\n" +
		"`/settings` \\- 默认音质/搜索平台设置\n\n" +
		"示例:\n" +
		"`/music https://music.163.com/song/1859603835`\n" +
		"`/music 周杰伦`\n" +
		"`/search 周杰伦 qq`"
	musicInfoMsg = `%s
专辑: %s
%s
`
	musicInfo = `「%s」- %s
专辑: %s
%s
via @%s`
	uploadFailed     = "下载/发送失败\n%v"
	hitCache         = "命中缓存, 正在发送中..."
	noCache          = "歌曲未缓存"
	rmcacheReport    = "清除 [%s] 缓存成功"
	inputIDorKeyword = "请输入歌曲ID或歌曲关键词，或使用 /rmcache all 清空所有缓存"
	tapToDownload    = "点击上方按钮缓存歌曲"
	tapMeToDown      = "点我缓存歌曲"
	sendMeTo         = "Send me to..."
	waitForDown      = "等待下载中..."
	fetchInfo        = "正在获取歌曲信息..."
	fetchInfoFailed  = "获取歌曲信息失败"
	getUrlFailed     = "获取歌曲下载链接失败"
	downloading      = "下载中..."
	redownloading    = "下载失败，尝试重新下载中..."
	uploading        = "下载完成, 发送中..."
	downloadStatus   = " %s\n%.2fMB/%.2fMB %d%%"
	md5VerFailed     = "MD5校验失败"
	retryLater       = "请稍后重试"
	downloadTimeout  = "下载超时"
	inputKeyword     = "请输入搜索关键词"
	inputContent     = "请输入歌曲关键词/歌曲分享链接/歌曲ID"
	searching        = "搜索中..."
	fetchingLyric    = "正在获取歌词中"
	noResults        = "未找到结果"
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
