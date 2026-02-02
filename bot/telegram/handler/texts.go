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
Github: https://github.com/liuran001/MusicBot\-Go

Multi\-platform music bot supporting NetEase Cloud Music and more\.

\[编译环境] %s
\[编译版本] %s
\[编译哈希] %s
\[编译日期] %s
\[运行环境] %s`
	musicInfoMsg = `%s
专辑: %s
%s %.2fMB
`
	musicInfo = `「%s」- %s
专辑: %s
#网易云音乐 #%s %.2fMB %.2fkbps
via @%s`
	uploadFailed     = "下载/发送失败\n%v"
	hitCache         = "命中缓存, 正在发送中..."
	noCache          = "歌曲未缓存"
	rmcacheReport    = "清除 [%s] 缓存成功"
	inputIDorKeyword = "请输入歌曲ID或歌曲关键词"
	tapToDownload    = "点击上方按钮缓存歌曲"
	tapMeToDown      = "点我缓存歌曲"
	sendMeTo         = "Send me to..."
	waitForDown      = "等待下载中..."
	fetchInfo        = "正在获取歌曲信息..."
	fetchInfoFailed  = "获取歌曲信息失败"
	getUrlFailed     = "获取歌曲下载链接失败"
	downloading      = "下载中..."
	redownloading    = "下载失败，尝试重新下载中..."
	uploading        = "下载完成, 发送中...（若文件较大可能需要更久）"
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
`
	callbackText   = "Success"
	callbackDenied = "仅发起人或管理员可操作"
)
