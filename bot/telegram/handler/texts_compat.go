package handler

// TEMPORARY migration shim. These package vars hold the original Chinese strings
// so the package keeps compiling while individual handler files are migrated to
// ctx-aware catalog lookups (tr(ctx, "key")). Each is unused once its last call
// site is migrated; Go permits unused package-level vars, so the build stays
// green throughout. DELETE this file once all references are gone.
//
// Migration mapping (var -> catalog key):
//
//	hitCache          -> hit_cache
//	inputIDorKeyword  -> input_id_or_keyword
//	inlineTapToSend   -> inline_tap_to_send
//	sendMeTo          -> send_me_to
//	waitForDown       -> wait_for_down
//	fetchInfo         -> fetch_info
//	fetchInfoFailed   -> fetch_info_failed
//	downloading       -> downloading
//	uploading         -> uploading
//	md5VerFailed      -> md5_ver_failed
//	downloadTimeout   -> download_timeout
//	inputKeyword      -> input_keyword
//	inputContent      -> input_content
//	inputLyricContent -> input_lyric_content
//	searching         -> searching
//	fetchingPlaylist  -> fetching_playlist
//	fetchingLyric     -> fetching_lyric
//	noResults         -> no_results
//	playlistEmpty     -> playlist_empty
//	getLrcFailed      -> get_lrc_failed
//	callbackDenied    -> callback_denied
var (
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
	callbackDenied    = "仅发起人或管理员可操作"
)
