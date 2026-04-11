package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

func extractPlatformTrack(ctx context.Context, message *telego.Message, manager platform.Manager) (platformName, trackID string, found bool) {
	if message == nil || message.Text == "" {
		return "", "", false
	}

	text := message.Text
	if looksLikeCookiePayload(text) {
		return "", "", false
	}
	args := commandArguments(message.Text)
	if args != "" {
		text = args
		if looksLikeCookiePayload(text) {
			return "", "", false
		}
		fields := strings.Fields(args)
		if len(fields) >= 3 {
			if _, err := platform.ParseQuality(fields[2]); err == nil {
				return fields[0], fields[1], true
			}
		}
	}
	text, _, _ = parseTrailingOptions(text, manager)

	if manager != nil {
		if _, _, hasSuffix := parseSearchKeywordPlatform(text, manager); hasSuffix {
			return "", "", false
		}
		if _, _, matched := matchPlaylistURL(ctx, manager, text); matched {
			return "", "", false
		}
		resolvedText := resolveShortLinkText(ctx, manager, text)
		if plat, id, matched := manager.MatchText(resolvedText); matched {
			return plat, id, true
		}
		if plat, id, matched := manager.MatchURL(resolvedText); matched {
			return plat, id, true
		}
		if extractedURL := extractFirstURL(resolvedText); extractedURL != "" && extractedURL != resolvedText {
			if plat, id, matched := manager.MatchURL(extractedURL); matched {
				return plat, id, true
			}
			if plat, id, matched := manager.MatchText(extractedURL); matched {
				return plat, id, true
			}
		}
	}

	return "", "", false
}

func looksLikeCookiePayload(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	if strings.Count(text, "=") >= 3 && strings.Count(text, ";") >= 2 {
		return true
	}
	markers := []string{
		"sessionid=",
		"sessionid_ss=",
		"sid_tt=",
		"sid_guard=",
		"uid_tt=",
		"passport_csrf_token=",
		"cookie=",
		"ttwid=",
		"odin_tt=",
		"uifid=",
	}
	matched := 0
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			matched++
		}
		if matched >= 2 {
			return true
		}
	}
	return false
}

func extractQualityOverride(message *telego.Message, manager platform.Manager) string {
	if message == nil || message.Text == "" {
		return ""
	}
	args := commandArguments(message.Text)
	if args == "" {
		args = message.Text
	}
	_, _, quality := parseTrailingOptions(args, manager)
	return quality
}

func commandArguments(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	parts := strings.SplitN(text, " ", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func commandName(text, botName string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	parts := strings.SplitN(text, " ", 2)
	command := strings.TrimPrefix(parts[0], "/")
	if command == "" {
		return ""
	}
	if strings.Contains(command, "@") {
		seg := strings.SplitN(command, "@", 2)
		command = seg[0]
		if botName != "" && len(seg) > 1 && seg[1] != "" && seg[1] != botName {
			return ""
		}
	}
	return command
}

func parseTrailingOptions(text string, manager platform.Manager) (baseText, platformName, quality string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", "", ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", "", ""
	}
	lastIdx := len(fields) - 1
	qualityToken := normalizeQualityToken(strings.ToLower(fields[lastIdx]))
	if qualityToken != "" {
		if _, err := platform.ParseQuality(qualityToken); err == nil {
			quality = qualityToken
			fields = fields[:lastIdx]
		}
	}
	if len(fields) > 0 {
		last := normalizePlatformToken(strings.ToLower(fields[len(fields)-1]))
		if mapped, ok := resolvePlatformAlias(manager, last); ok {
			platformName = mapped
			fields = fields[:len(fields)-1]
		}
	}
	baseText = strings.TrimSpace(strings.Join(fields, " "))
	return baseText, platformName, quality
}

type searchLimitFunc func(platformName string) int

// searchTracksWithFallback runs search on primary platform and optionally falls back
// to another searchable platform when primary fails (or returns empty, when enabled).
// Returns tracks, matched platform name, whether fallback was used, and terminal error.
func searchTracksWithFallback(ctx context.Context, manager platform.Manager, primaryPlatform, fallbackPlatform, keyword string, limitFn searchLimitFunc, fallbackOnEmpty bool) ([]platform.Track, string, bool, error) {
	primaryPlatform = strings.TrimSpace(primaryPlatform)
	fallbackPlatform = strings.TrimSpace(fallbackPlatform)
	if manager == nil {
		return nil, primaryPlatform, false, platform.ErrUnavailable
	}
	plat := manager.Get(primaryPlatform)
	if plat == nil || !plat.SupportsSearch() {
		if fallbackPlatform != "" && fallbackPlatform != primaryPlatform {
			fallbackPlat := manager.Get(fallbackPlatform)
			if fallbackPlat != nil && fallbackPlat.SupportsSearch() {
				fallbackLimit := defaultSearchLimit
				if limitFn != nil {
					if v := limitFn(fallbackPlatform); v > 0 {
						fallbackLimit = v
					}
				}
				fallbackTracks, fallbackErr := fallbackPlat.Search(ctx, keyword, fallbackLimit)
				if fallbackErr == nil && len(fallbackTracks) > 0 {
					return fallbackTracks, fallbackPlatform, true, nil
				}
				if fallbackErr != nil {
					return fallbackTracks, fallbackPlatform, true, fallbackErr
				}
			}
		}
		return nil, primaryPlatform, false, platform.ErrUnsupported
	}

	limit := defaultSearchLimit
	if limitFn != nil {
		if v := limitFn(primaryPlatform); v > 0 {
			limit = v
		}
	}
	tracks, err := plat.Search(ctx, keyword, limit)

	shouldFallback := err != nil || (fallbackOnEmpty && len(tracks) == 0)
	if shouldFallback && fallbackPlatform != "" && fallbackPlatform != primaryPlatform {
		fallbackPlat := manager.Get(fallbackPlatform)
		if fallbackPlat != nil && fallbackPlat.SupportsSearch() {
			fallbackLimit := limit
			if limitFn != nil {
				if v := limitFn(fallbackPlatform); v > 0 {
					fallbackLimit = v
				}
			}
			fallbackTracks, fallbackErr := fallbackPlat.Search(ctx, keyword, fallbackLimit)
			if fallbackErr == nil && len(fallbackTracks) > 0 {
				return fallbackTracks, fallbackPlatform, true, nil
			}
		}
	}

	if err != nil {
		return tracks, primaryPlatform, false, err
	}
	return tracks, primaryPlatform, false, nil
}

var urlMatcher = regexp.MustCompile(`https?://[^\s\x{00A0}\x{2000}-\x{200D}\x{202F}\x{205F}\x{3000}<>"'()（）\[\]{}【】《》「」『』]+`)

func extractFirstURL(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	match := urlMatcher.FindString(text)
	return trimURLCandidate(match)
}

func extractURLs(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	matches := urlMatcher.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		cleaned := trimURLCandidate(match)
		if cleaned != "" {
			urls = append(urls, cleaned)
		}
	}
	return urls
}

func trimURLCandidate(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	for candidate != "" {
		r, size := utf8.DecodeLastRuneInString(candidate)
		if r == utf8.RuneError && size == 1 {
			break
		}
		if !isURLTrailingRune(r) {
			break
		}
		candidate = candidate[:len(candidate)-size]
	}
	return strings.TrimSpace(candidate)
}

func isURLTrailingRune(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	switch r {
	case '.', ',', '!', '?', ';', ':', ')', ']', '}', '>', '"', '\'',
		'，', '。', '！', '？', '；', '：', '）', '】', '》', '」', '』', '、':
		return true
	default:
		return false
	}
}

var shortURLResolver = resolveShortURL

func resolveShortLinkText(ctx context.Context, manager platform.Manager, text string) string {
	urlStr := extractFirstURL(text)
	if urlStr == "" {
		return text
	}
	resolved, err := shortURLResolver(ctx, manager, urlStr)
	if err != nil || resolved == "" || resolved == urlStr {
		return text
	}
	return strings.Replace(text, urlStr, resolved, 1)
}

func extractResolvedURL(ctx context.Context, manager platform.Manager, text string) string {
	urlStr := extractFirstURL(text)
	if urlStr == "" {
		return ""
	}
	resolved, err := shortURLResolver(ctx, manager, urlStr)
	if err != nil || resolved == "" {
		return urlStr
	}
	return resolved
}

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"

func applyBrowserHeaders(req *http.Request) {
	if req == nil {
		return
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
}

func resolveShortURL(ctx context.Context, manager platform.Manager, urlStr string) (string, error) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return urlStr, err
	}
	if !shouldResolveHost(parsed.Hostname(), manager) {
		return urlStr, nil
	}
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return urlStr, err
	}
	applyBrowserHeaders(req)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location := strings.TrimSpace(resp.Header.Get("Location"))
			if location != "" {
				if resolved, err := parsed.Parse(location); err == nil {
					return resolved.String(), nil
				}
				return location, nil
			}
		}
		if resp.Request != nil && resp.Request.URL != nil {
			return resp.Request.URL.String(), nil
		}
	}
	client = &http.Client{Timeout: 8 * time.Second}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return urlStr, err
	}
	applyBrowserHeaders(req)
	req.Header.Set("Range", "bytes=0-0")
	resp, err = client.Do(req)
	if err != nil {
		return urlStr, err
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String(), nil
	}
	return urlStr, nil
}

func shouldResolveHost(host string, manager platform.Manager) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || manager == nil {
		return false
	}
	for _, name := range manager.List() {
		plat := manager.Get(name)
		if plat == nil {
			continue
		}
		provider, ok := plat.(platform.ShortLinkProvider)
		if !ok {
			continue
		}
		for _, domain := range provider.ShortLinkHosts() {
			domain = strings.ToLower(strings.TrimSpace(domain))
			if domain == "" {
				continue
			}
			if host == domain || strings.HasSuffix(host, "."+domain) {
				return true
			}
		}
	}
	return false
}

func matchPlaylistURL(ctx context.Context, manager platform.Manager, text string) (platformName, playlistID string, matched bool) {
	if manager == nil {
		return "", "", false
	}
	urlStr := extractResolvedURL(ctx, manager, text)
	if urlStr == "" {
		return "", "", false
	}
	for _, name := range manager.List() {
		plat := manager.Get(name)
		if plat == nil {
			continue
		}
		if matcher, ok := plat.(platform.PlaylistURLMatcher); ok {
			if id, ok := matcher.MatchPlaylistURL(urlStr); ok {
				return name, id, true
			}
		}
	}
	return "", "", false
}

func matchArtistURL(ctx context.Context, manager platform.Manager, text string) (platformName, artistID string, matched bool) {
	if manager == nil {
		return "", "", false
	}
	urlStr := extractResolvedURL(ctx, manager, text)
	if urlStr == "" {
		return "", "", false
	}
	for _, name := range manager.List() {
		plat := manager.Get(name)
		if plat == nil {
			continue
		}
		if matcher, ok := plat.(platform.ArtistURLMatcher); ok {
			if id, ok := matcher.MatchArtistURL(urlStr); ok {
				return name, id, true
			}
		}
	}
	return "", "", false
}

func normalizeQualityToken(token string) string {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "low":
		return "standard"
	case "standard", "high", "lossless", "hires":
		return strings.ToLower(strings.TrimSpace(token))
	default:
		return ""
	}
}

func normalizePlatformToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	return strings.TrimPrefix(token, "@")
}

func isLikelyIDToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	hasDigit := false
	for _, ch := range token {
		switch {
		case ch >= '0' && ch <= '9':
			hasDigit = true
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		default:
			return false
		}
	}
	return hasDigit
}

func isNumeric(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func ensureDir(path string) {
	_, err := os.Stat(path)
	if err == nil {
		return
	}
	if os.IsNotExist(err) {
		_ = os.MkdirAll(path, os.ModePerm)
	}
}

func sanitizeFileName(name string) string {
	replacer := strings.NewReplacer("/", " ", "?", " ", "*", " ", ":", " ", "|", " ", "\\", " ", "<", " ", ">", " ", "\"", " ")
	return replacer.Replace(name)
}

func cleanupFiles(paths ...string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		_ = os.RemoveAll(p)
	}
}

func buildMusicInfoText(songName, songAlbum, fileInfo, suffix string) string {
	songName = strings.TrimSpace(songName)
	fileInfo = strings.TrimSpace(fileInfo)
	songAlbum = strings.TrimSpace(songAlbum)

	var builder strings.Builder
	builder.WriteString(songName)
	builder.WriteString("\n")
	if songAlbum != "" {
		builder.WriteString("专辑: ")
		builder.WriteString(songAlbum)
		builder.WriteString("\n")
	}
	builder.WriteString(fileInfo)
	builder.WriteString("\n")
	builder.WriteString(suffix)
	return builder.String()
}

func buildMusicInfoTextf(songName, songAlbum, fileInfo, suffixFmt string, args ...interface{}) string {
	suffix := suffixFmt
	if len(args) > 0 {
		suffix = fmt.Sprintf(suffixFmt, args...)
	}
	return buildMusicInfoText(songName, songAlbum, fileInfo, suffix)
}

func userVisibleDownloadError(err error) string {
	if err != nil {
		errText := fmt.Sprintf("%v", err)
		errLower := strings.ToLower(errText)
		if strings.Contains(errText, md5VerFailed) {
			return md5VerFailed
		}
		if strings.Contains(errText, downloadTimeout) {
			return downloadTimeout
		}
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(errLower, "context deadline exceeded") {
			return "处理超时，请稍后重试"
		}
		if errors.Is(err, context.Canceled) || strings.Contains(errLower, "context canceled") {
			return "任务已取消，请稍后重试"
		}
		if errors.Is(err, errDownloadQueueOverloaded) || strings.Contains(errText, "download queue overloaded") {
			return "当前下载任务过多，请稍后再试"
		}
		if strings.Contains(errText, "upload queue is full") {
			return "当前发送任务过多，请稍后再试"
		}
		if errors.Is(err, platform.ErrRateLimited) || strings.Contains(errText, "Too Many Requests") || strings.Contains(errLower, "retry after") {
			return "请求过于频繁，请稍后重试"
		}
		if errors.Is(err, platform.ErrAuthRequired) {
			return "平台认证已失效，请联系管理员更新凭据"
		}
		if errors.Is(err, platform.ErrUnavailable) {
			return "当前歌曲暂不可用，请稍后再试"
		}
	}
	return "下载/发送失败，请稍后重试"
}

func userVisibleSearchError(err error, unavailableText string) string {
	if err == nil {
		return noResults
	}
	if errors.Is(err, platform.ErrUnsupported) {
		return "此平台不支持搜索功能"
	}
	if errors.Is(err, platform.ErrRateLimited) {
		return "请求过于频繁，请稍后再试"
	}
	if errors.Is(err, platform.ErrUnavailable) {
		if strings.TrimSpace(unavailableText) != "" {
			return unavailableText
		}
		return "搜索服务暂时不可用"
	}
	return noResults
}

func userVisiblePlaylistError(err error) string {
	if err == nil {
		return noResults
	}
	if errors.Is(err, platform.ErrUnsupported) {
		return "此平台不支持获取歌单"
	}
	if errors.Is(err, platform.ErrRateLimited) {
		return "请求过于频繁，请稍后再试"
	}
	if errors.Is(err, platform.ErrUnavailable) {
		return "歌单服务暂时不可用"
	}
	if errors.Is(err, platform.ErrNotFound) {
		return "未找到歌单"
	}
	return noResults
}

func isTelegramFileIDInvalid(err error) bool {
	if err == nil {
		return false
	}
	errText := strings.ToLower(fmt.Sprintf("%v", err))
	return strings.Contains(errText, "wrong file identifier") ||
		strings.Contains(errText, "file_id_invalid") ||
		strings.Contains(errText, "invalid file id") ||
		strings.Contains(errText, "wrong remote file identifier")
}

func buildMusicCaption(manager platform.Manager, songInfo *botpkg.SongInfo, botName string) string {
	if songInfo == nil {
		return ""
	}

	songNameText := html.EscapeString(songInfo.SongName)
	artistsText := html.EscapeString(songInfo.SongArtists)
	albumText := html.EscapeString(songInfo.SongAlbum)
	songNameHTML := songNameText
	artistsHTML := artistsText
	albumHTML := albumText
	infoParts := make([]string, 0, 2)
	if sizeText := formatFileSize(songInfo.MusicSize + songInfo.EmbPicSize); sizeText != "" {
		infoParts = append(infoParts, sizeText)
	}
	if bitrateText := formatBitrate(songInfo.BitRate); bitrateText != "" {
		infoParts = append(infoParts, bitrateText)
	}
	infoLine := strings.Join(infoParts, " ")
	if infoLine != "" {
		infoLine += "\n"
	}
	tags := strings.Join(formatInfoTags(manager, songInfo.Platform, songInfo.FileExt), " ")

	if strings.TrimSpace(songInfo.TrackURL) != "" {
		songNameHTML = fmt.Sprintf("<a href=\"%s\">%s</a>", html.EscapeString(songInfo.TrackURL), songNameText)
	}

	if strings.TrimSpace(songInfo.SongArtistsURLs) != "" {
		artistURLs := strings.Split(songInfo.SongArtistsURLs, ",")
		artists := strings.Split(songInfo.SongArtists, "/")
		var parts []string
		for i, artist := range artists {
			artist = html.EscapeString(strings.TrimSpace(artist))
			if i < len(artistURLs) && strings.TrimSpace(artistURLs[i]) != "" {
				parts = append(parts, fmt.Sprintf("<a href=\"%s\">%s</a>", html.EscapeString(strings.TrimSpace(artistURLs[i])), artist))
				continue
			}
			parts = append(parts, artist)
		}
		artistsHTML = strings.Join(parts, " / ")
	}

	if strings.TrimSpace(songInfo.AlbumURL) != "" {
		albumHTML = fmt.Sprintf("<a href=\"%s\">%s</a>", html.EscapeString(songInfo.AlbumURL), albumText)
	}
	albumLine := ""
	if strings.TrimSpace(songInfo.SongAlbum) != "" {
		albumLine = fmt.Sprintf("专辑: %s\n", albumHTML)
	}

	return fmt.Sprintf("<b>「%s」- %s</b>\n%s<blockquote>%s%s\n</blockquote>via @%s",
		songNameHTML,
		artistsHTML,
		albumLine,
		infoLine,
		tags,
		botName,
	)
}

func buildForwardQuery(trackURL, platformName, trackID string) string {
	if strings.TrimSpace(trackURL) != "" {
		return strings.TrimSpace(trackURL)
	}
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return ""
	}
	if strings.TrimSpace(platformName) == "qqmusic" {
		return "qqmusic:" + trackID
	}
	return trackID
}

func buildForwardKeyboard(trackURL, platformName, trackID string) *telego.InlineKeyboardMarkup {
	query := buildForwardQuery(trackURL, platformName, trackID)
	if query == "" {
		return nil
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{
		{{Text: sendMeTo, SwitchInlineQuery: &query}},
	}}
}

func buildForwardKeyboardWithEpisodes(trackURL, platformName, trackID, qualityValue string, requesterID int64) *telego.InlineKeyboardMarkup {
	return buildForwardKeyboard(trackURL, platformName, trackID)
}

func buildInlineSendKeyboard(platformName, trackID, qualityValue string, requesterID int64) *telego.InlineKeyboardMarkup {
	callbackData := buildInlineSendCallbackData(platformName, trackID, qualityValue, requesterID)
	if callbackData == "" {
		return nil
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{
		{Text: inlineTapToSend, CallbackData: callbackData},
	}}}
}

func buildInlineCollectionOpenKeyboard(token string, requesterID int64) *telego.InlineKeyboardMarkup {
	token = strings.TrimSpace(token)
	if token == "" || requesterID == 0 || !isInlineStartToken(token) {
		return nil
	}
	callbackData := fmt.Sprintf("ipl %s open %d", token, requesterID)
	if len(callbackData) > 64 {
		return nil
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{
		{Text: "如果没反应点此刷新", CallbackData: callbackData},
	}}}
}

func buildInlineRandomSendKeyboard(requesterID int64) *telego.InlineKeyboardMarkup {
	if requesterID == 0 {
		return nil
	}
	callbackData := fmt.Sprintf("music i random %d", requesterID)
	if len(callbackData) > 64 {
		return nil
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{
		{Text: inlineTapToSend, CallbackData: callbackData},
	}}}
}

func buildInlineSendCallbackData(platformName, trackID, qualityValue string, requesterID int64) string {
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	qualityValue = strings.TrimSpace(qualityValue)
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if requesterID == 0 || !isInlineStartToken(platformName) || !isInlineStartToken(trackID) || !isInlineStartToken(qualityValue) {
		return ""
	}
	data := fmt.Sprintf("music i %s %s %s %d", platformName, trackID, qualityValue, requesterID)
	if len(data) <= 64 {
		return data
	}
	if token := storeInlineCallbackPayload(inlineCallbackPayload{platformName: platformName, trackID: trackID, qualityValue: qualityValue, requesterID: requesterID}); token != "" {
		data = fmt.Sprintf("music it %s", token)
		if len(data) <= 64 {
			return data
		}
	}
	return ""
}

func parseEpisodeTrackID(manager platform.Manager, platformName, trackID string) (baseTrackID string, page int, hasExplicitPage bool, ok bool) {
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	if manager == nil || platformName == "" || trackID == "" {
		return "", 0, false, false
	}
	plat := manager.Get(platformName)
	if plat == nil {
		return "", 0, false, false
	}
	resolver, ok := plat.(platform.EpisodeTrackIDResolver)
	if !ok {
		return "", 0, false, false
	}
	baseTrackID, page, hasExplicitPage = resolver.ParseEpisodeTrackID(trackID)
	baseTrackID = strings.TrimSpace(baseTrackID)
	if baseTrackID == "" {
		return "", 0, false, false
	}
	if page <= 0 {
		page = 1
	}
	return baseTrackID, page, hasExplicitPage, true
}

func buildEpisodeTrackID(manager platform.Manager, platformName, baseTrackID string, page int, explicit bool) string {
	platformName = strings.TrimSpace(platformName)
	baseTrackID = strings.TrimSpace(baseTrackID)
	if manager == nil || platformName == "" || baseTrackID == "" {
		return ""
	}
	plat := manager.Get(platformName)
	if plat == nil {
		return ""
	}
	resolver, ok := plat.(platform.EpisodeTrackIDResolver)
	if !ok {
		return ""
	}
	return strings.TrimSpace(resolver.BuildEpisodeTrackID(baseTrackID, page, explicit))
}

func buildEpisodeCollectionID(manager platform.Manager, platformName, baseTrackID string) string {
	platformName = strings.TrimSpace(platformName)
	baseTrackID = strings.TrimSpace(baseTrackID)
	if manager == nil || platformName == "" || baseTrackID == "" {
		return ""
	}
	plat := manager.Get(platformName)
	if plat == nil {
		return ""
	}
	provider, ok := plat.(platform.EpisodeCollectionProvider)
	if !ok {
		return ""
	}
	return strings.TrimSpace(provider.BuildEpisodeCollectionID(baseTrackID))
}

func parseEpisodeCollectionID(manager platform.Manager, platformName, collectionID string) (baseTrackID string, ok bool) {
	platformName = strings.TrimSpace(platformName)
	collectionID = strings.TrimSpace(collectionID)
	if manager == nil || platformName == "" || collectionID == "" {
		return "", false
	}
	plat := manager.Get(platformName)
	if plat == nil {
		return "", false
	}
	provider, ok := plat.(platform.EpisodeCollectionProvider)
	if !ok {
		return "", false
	}
	baseTrackID, ok = provider.ParseEpisodeCollectionID(collectionID)
	baseTrackID = strings.TrimSpace(baseTrackID)
	return baseTrackID, ok && baseTrackID != ""
}

func getSearchFilterProvider(manager platform.Manager, platformName string) (platform.SearchFilterProvider, bool) {
	platformName = strings.TrimSpace(platformName)
	if manager == nil || platformName == "" {
		return nil, false
	}
	plat := manager.Get(platformName)
	if plat == nil {
		return nil, false
	}
	provider, ok := plat.(platform.SearchFilterProvider)
	return provider, ok
}

func resolveSearchFilterEnabled(ctx context.Context, manager platform.Manager, repo botpkg.SongRepository, platformName string, scopeType string, scopeID int64) (enabled bool, supported bool, label string) {
	provider, ok := getSearchFilterProvider(manager, platformName)
	if !ok {
		return false, false, ""
	}
	enabled = provider.SearchFilterDefaultEnabled()
	label = strings.TrimSpace(provider.SearchFilterButtonLabel())
	if repo != nil && scopeID != 0 {
		if val, err := repo.GetPluginSetting(ctx, scopeType, scopeID, strings.TrimSpace(platformName), provider.SearchFilterSettingKey()); err == nil && val != "" {
			enabled = strings.TrimSpace(strings.ToLower(val)) == "on"
		}
	}
	return enabled, true, label
}

func withSearchFilterContext(ctx context.Context, manager platform.Manager, platformName string, enabled bool) context.Context {
	provider, ok := getSearchFilterProvider(manager, platformName)
	if !ok {
		return ctx
	}
	return provider.WithSearchFilter(ctx, enabled)
}

func buildInlineEpisodeShowCallbackData(platformName, trackID, qualityValue string, requesterID int64, page int) string {
	return buildInlineEpisodeCallbackData("s", platformName, trackID, qualityValue, requesterID, page)
}

func buildInlineEpisodeSelectCallbackData(platformName, trackID, qualityValue string, requesterID int64, page int) string {
	return buildInlineEpisodeCallbackData("p", platformName, trackID, qualityValue, requesterID, page)
}

func buildInlineEpisodeNavCallbackData(platformName, trackID, qualityValue string, requesterID int64, page int) string {
	return buildInlineEpisodeCallbackData("n", platformName, trackID, qualityValue, requesterID, page)
}

func buildInlineEpisodeCallbackData(action, platformName, trackID, qualityValue string, requesterID int64, page int) string {
	action = strings.TrimSpace(strings.ToLower(action))
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	qualityValue = strings.TrimSpace(qualityValue)
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if page <= 0 {
		page = 1
	}
	if requesterID == 0 || !isInlineStartToken(action) || !isInlineStartToken(platformName) || !isInlineStartToken(trackID) || !isInlineStartToken(qualityValue) {
		return ""
	}
	data := fmt.Sprintf("music iep %s %s %s %s %d %d", action, platformName, trackID, qualityValue, requesterID, page)
	if len(data) <= 64 {
		return data
	}
	if token := storeInlineCallbackPayload(inlineCallbackPayload{action: action, platformName: platformName, trackID: trackID, qualityValue: qualityValue, requesterID: requesterID, page: page}); token != "" {
		data = fmt.Sprintf("music iet %s", token)
		if len(data) <= 64 {
			return data
		}
	}
	return ""
}

type inlineCallbackPayload struct {
	platformName string
	trackID      string
	qualityValue string
	requesterID  int64
	action       string
	page         int
	storedAt     time.Time
}

type inlineResultPayload struct {
	platformName string
	trackID      string
	qualityValue string
	collectionID string
	storedAt     time.Time
}

type inlineMessageLockEntry struct {
	mu   sync.Mutex
	refs int
}

var inlineMessageLockStore = struct {
	mu    sync.Mutex
	locks map[string]*inlineMessageLockEntry
}{locks: make(map[string]*inlineMessageLockEntry)}

var callbackInFlightStore = struct {
	mu      sync.Mutex
	entries map[string]time.Time
}{entries: make(map[string]time.Time)}

var inlineCallbackPayloadStore = struct {
	mu      sync.Mutex
	entries map[string]inlineCallbackPayload
}{entries: make(map[string]inlineCallbackPayload)}

var inlineResultPayloadStore = struct {
	mu      sync.Mutex
	entries map[string]inlineResultPayload
}{entries: make(map[string]inlineResultPayload)}

var inlineCallbackTokenCounter uint64
var inlineResultTokenCounter uint64

const inlineCallbackPayloadTTL = 30 * time.Minute

func withInlineMessageLock(inlineMessageID string, fn func()) {
	if fn == nil {
		return
	}
	inlineMessageID = strings.TrimSpace(inlineMessageID)
	if inlineMessageID == "" {
		fn()
		return
	}

	inlineMessageLockStore.mu.Lock()
	entry := inlineMessageLockStore.locks[inlineMessageID]
	if entry == nil {
		entry = &inlineMessageLockEntry{}
		inlineMessageLockStore.locks[inlineMessageID] = entry
	}
	entry.refs++
	inlineMessageLockStore.mu.Unlock()

	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		inlineMessageLockStore.mu.Lock()
		entry.refs--
		if entry.refs <= 0 {
			delete(inlineMessageLockStore.locks, inlineMessageID)
		}
		inlineMessageLockStore.mu.Unlock()
	}()

	fn()
}

func tryAcquireCallbackInFlight(key string, ttl time.Duration) (release func(), ok bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return func() {}, true
	}
	if ttl <= 0 {
		ttl = 3 * time.Second
	}
	now := time.Now()
	until := now.Add(ttl)

	callbackInFlightStore.mu.Lock()
	for k, exp := range callbackInFlightStore.entries {
		if !exp.After(now) {
			delete(callbackInFlightStore.entries, k)
		}
	}
	if exp, exists := callbackInFlightStore.entries[key]; exists && exp.After(now) {
		callbackInFlightStore.mu.Unlock()
		return nil, false
	}
	callbackInFlightStore.entries[key] = until
	callbackInFlightStore.mu.Unlock()

	released := false
	return func() {
		callbackInFlightStore.mu.Lock()
		defer callbackInFlightStore.mu.Unlock()
		if released {
			return
		}
		released = true
		delete(callbackInFlightStore.entries, key)
	}, true
}

func storeInlineCallbackPayload(payload inlineCallbackPayload) string {
	payload.platformName = strings.TrimSpace(payload.platformName)
	payload.trackID = strings.TrimSpace(payload.trackID)
	payload.qualityValue = strings.TrimSpace(payload.qualityValue)
	payload.action = strings.TrimSpace(strings.ToLower(payload.action))
	if payload.qualityValue == "" {
		payload.qualityValue = "hires"
	}
	if payload.page <= 0 {
		payload.page = 1
	}
	if payload.platformName == "" || payload.trackID == "" || payload.requesterID == 0 {
		return ""
	}
	payload.storedAt = time.Now()
	token := strconv.FormatUint(uint64(time.Now().UnixNano()), 36) + strconv.FormatUint(atomic.AddUint64(&inlineCallbackTokenCounter, 1), 36)
	inlineCallbackPayloadStore.mu.Lock()
	defer inlineCallbackPayloadStore.mu.Unlock()
	for key, entry := range inlineCallbackPayloadStore.entries {
		if payload.storedAt.Sub(entry.storedAt) > inlineCallbackPayloadTTL {
			delete(inlineCallbackPayloadStore.entries, key)
		}
	}
	inlineCallbackPayloadStore.entries[token] = payload
	return token
}

func storeInlineResultPayload(payload inlineResultPayload) string {
	payload.platformName = strings.TrimSpace(payload.platformName)
	payload.trackID = strings.TrimSpace(payload.trackID)
	payload.qualityValue = strings.TrimSpace(payload.qualityValue)
	payload.collectionID = strings.TrimSpace(payload.collectionID)
	if payload.qualityValue == "" {
		payload.qualityValue = "hires"
	}
	if payload.platformName == "" || (payload.trackID == "" && payload.collectionID == "") {
		return ""
	}
	payload.storedAt = time.Now()
	token := strconv.FormatUint(uint64(time.Now().UnixNano()), 36) + strconv.FormatUint(atomic.AddUint64(&inlineResultTokenCounter, 1), 36)
	inlineResultPayloadStore.mu.Lock()
	defer inlineResultPayloadStore.mu.Unlock()
	for key, entry := range inlineResultPayloadStore.entries {
		if payload.storedAt.Sub(entry.storedAt) > inlineCallbackPayloadTTL {
			delete(inlineResultPayloadStore.entries, key)
		}
	}
	inlineResultPayloadStore.entries[token] = payload
	return token
}

func getInlineCallbackPayload(token string) (inlineCallbackPayload, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return inlineCallbackPayload{}, false
	}
	now := time.Now()
	inlineCallbackPayloadStore.mu.Lock()
	defer inlineCallbackPayloadStore.mu.Unlock()
	for key, entry := range inlineCallbackPayloadStore.entries {
		if now.Sub(entry.storedAt) > inlineCallbackPayloadTTL {
			delete(inlineCallbackPayloadStore.entries, key)
		}
	}
	payload, ok := inlineCallbackPayloadStore.entries[token]
	if !ok {
		return inlineCallbackPayload{}, false
	}
	return payload, true
}

func getInlineResultPayload(token string) (inlineResultPayload, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return inlineResultPayload{}, false
	}
	now := time.Now()
	inlineResultPayloadStore.mu.Lock()
	defer inlineResultPayloadStore.mu.Unlock()
	for key, entry := range inlineResultPayloadStore.entries {
		if now.Sub(entry.storedAt) > inlineCallbackPayloadTTL {
			delete(inlineResultPayloadStore.entries, key)
		}
	}
	payload, ok := inlineResultPayloadStore.entries[token]
	if !ok {
		return inlineResultPayload{}, false
	}
	return payload, true
}

func buildInlineMusicCommand(platformName, trackID, qualityValue string) string {
	trackID = strings.TrimSpace(trackID)
	platformName = strings.TrimSpace(platformName)
	qualityValue = strings.TrimSpace(qualityValue)
	parts := []string{"/music"}
	if trackID != "" {
		parts = append(parts, trackID)
	}
	if platformName != "" {
		parts = append(parts, platformName)
	}
	if qualityValue != "" {
		parts = append(parts, qualityValue)
	}
	return strings.Join(parts, " ")
}

func isAutoLinkDetectEnabled(ctx context.Context, repo botpkg.SongRepository, message *telego.Message) bool {
	if message == nil {
		return true
	}
	if isCommandMessage(message) {
		return true
	}
	if repo == nil {
		return true
	}
	if message.Chat.Type != "private" {
		settings, err := repo.GetGroupSettings(ctx, message.Chat.ID)
		if err != nil || settings == nil {
			return true
		}
		return settings.AutoLinkDetect
	}
	if message.From == nil {
		return true
	}
	settings, err := repo.GetUserSettings(ctx, message.From.ID)
	if err != nil || settings == nil {
		return true
	}
	return settings.AutoLinkDetect
}

func buildInlineStartParameter(platformName, trackID, qualityValue string) string {
	if !isInlineStartToken(platformName) || !isInlineStartToken(trackID) {
		return ""
	}
	qualityValue = strings.TrimSpace(qualityValue)
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if !isInlineStartToken(qualityValue) {
		return ""
	}
	param := fmt.Sprintf("cache_%s_%s_%s", platformName, trackID, qualityValue)
	if len(param) > 64 {
		return ""
	}
	return param
}

func buildInlineKeywordStartParameter(keyword string) string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return ""
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(keyword))
	if encoded == "" {
		return ""
	}
	param := "search_" + encoded
	if len(param) > 64 {
		return ""
	}
	return param
}

func buildInlinePendingResultID(platformName, trackID, qualityValue string) string {
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	qualityValue = strings.TrimSpace(qualityValue)
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if !isInlineStartToken(platformName) || !isInlineStartToken(trackID) || !isInlineStartToken(qualityValue) {
		if token := storeInlineResultPayload(inlineResultPayload{platformName: platformName, trackID: trackID, qualityValue: qualityValue}); token != "" {
			return "pt_" + token
		}
		return fmt.Sprintf("r_%d", time.Now().UnixMicro())
	}
	payload := platformName + "|" + trackID + "|" + qualityValue
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	if encoded == "" {
		if token := storeInlineResultPayload(inlineResultPayload{platformName: platformName, trackID: trackID, qualityValue: qualityValue}); token != "" {
			return "pt_" + token
		}
		return fmt.Sprintf("r_%d", time.Now().UnixMicro())
	}
	id := "p_" + encoded
	if len(id) > 64 {
		if token := storeInlineResultPayload(inlineResultPayload{platformName: platformName, trackID: trackID, qualityValue: qualityValue}); token != "" {
			return "pt_" + token
		}
		return fmt.Sprintf("r_%d", time.Now().UnixMicro())
	}
	return id
}

func buildInlineCachedResultID(platformName, trackID, qualityValue string) string {
	pendingID := buildInlinePendingResultID(platformName, trackID, qualityValue)
	if strings.HasPrefix(pendingID, "p_") {
		return "c_" + strings.TrimPrefix(pendingID, "p_")
	}
	if strings.HasPrefix(pendingID, "r_") {
		return "c_" + strings.TrimPrefix(pendingID, "r_")
	}
	return "c_" + pendingID
}

func buildInlineCollectionResultID(platformName, collectionID, qualityValue string) string {
	platformName = strings.TrimSpace(platformName)
	collectionID = strings.TrimSpace(collectionID)
	qualityValue = strings.TrimSpace(qualityValue)
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if platformName == "" || collectionID == "" {
		return fmt.Sprintf("l_%d", time.Now().UnixMicro())
	}
	payload := platformName + "|" + collectionID + "|" + qualityValue
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	if encoded == "" {
		if token := storeInlineResultPayload(inlineResultPayload{platformName: platformName, collectionID: collectionID, qualityValue: qualityValue}); token != "" {
			return "lt_" + token
		}
		return fmt.Sprintf("l_%d", time.Now().UnixMicro())
	}
	id := "l_" + encoded
	if len(id) > 64 {
		if token := storeInlineResultPayload(inlineResultPayload{platformName: platformName, collectionID: collectionID, qualityValue: qualityValue}); token != "" {
			return "lt_" + token
		}
		return fmt.Sprintf("l_%d", time.Now().UnixMicro())
	}
	return id
}

func parseInlinePendingResultID(resultID string) (platformName, trackID, qualityValue string, ok bool) {
	resultID = strings.TrimSpace(resultID)
	if strings.HasPrefix(resultID, "pt_") {
		payload, found := getInlineResultPayload(strings.TrimPrefix(resultID, "pt_"))
		if !found {
			return "", "", "", false
		}
		qualityValue = strings.TrimSpace(payload.qualityValue)
		if qualityValue == "" {
			qualityValue = "hires"
		}
		platformName = strings.TrimSpace(payload.platformName)
		trackID = strings.TrimSpace(payload.trackID)
		if platformName == "" || trackID == "" {
			return "", "", "", false
		}
		return platformName, trackID, qualityValue, true
	}
	if !strings.HasPrefix(resultID, "p_") {
		return "", "", "", false
	}
	encoded := strings.TrimPrefix(resultID, "p_")
	if encoded == "" {
		return "", "", "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", "", false
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) < 2 {
		return "", "", "", false
	}
	platformName = strings.TrimSpace(parts[0])
	trackID = strings.TrimSpace(parts[1])
	if len(parts) >= 3 {
		qualityValue = strings.TrimSpace(parts[2])
	}
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if !isInlineStartToken(platformName) || !isInlineStartToken(trackID) || !isInlineStartToken(qualityValue) {
		return "", "", "", false
	}
	return platformName, trackID, qualityValue, true
}

func parseInlineCollectionResultID(resultID string) (platformName, collectionID, qualityValue string, ok bool) {
	resultID = strings.TrimSpace(resultID)
	if strings.HasPrefix(resultID, "lt_") {
		payload, found := getInlineResultPayload(strings.TrimPrefix(resultID, "lt_"))
		if !found {
			return "", "", "", false
		}
		qualityValue = strings.TrimSpace(payload.qualityValue)
		if qualityValue == "" {
			qualityValue = "hires"
		}
		platformName = strings.TrimSpace(payload.platformName)
		collectionID = strings.TrimSpace(payload.collectionID)
		if platformName == "" || collectionID == "" {
			return "", "", "", false
		}
		return platformName, collectionID, qualityValue, true
	}
	if !strings.HasPrefix(resultID, "l_") {
		return "", "", "", false
	}
	encoded := strings.TrimPrefix(resultID, "l_")
	if encoded == "" {
		return "", "", "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", "", false
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) < 2 {
		return "", "", "", false
	}
	platformName = strings.TrimSpace(parts[0])
	collectionID = strings.TrimSpace(parts[1])
	if len(parts) >= 3 {
		qualityValue = strings.TrimSpace(parts[2])
	}
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if platformName == "" || collectionID == "" {
		return "", "", "", false
	}
	return platformName, collectionID, qualityValue, true
}

func formatInfoTags(manager platform.Manager, platformName, fileExt string) []string {
	tags := []string{"#" + platformTag(manager, platformName)}
	if strings.TrimSpace(fileExt) != "" {
		tags = append(tags, "#"+fileExt)
	}
	return tags
}

func formatFileSize(musicSize int) string {
	if musicSize <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2fMB", float64(musicSize)/1024/1024)
}

func formatBitrate(bitRate int) string {
	if bitRate <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2fkbps", float64(bitRate)/1000)
}

func formatFileInfo(fileExt string, musicSize int) string {
	if musicSize <= 0 || strings.TrimSpace(fileExt) == "" {
		return ""
	}
	return fmt.Sprintf("%s %.2fMB", fileExt, float64(musicSize)/1024/1024)
}

func platformEmoji(manager platform.Manager, platformName string) string {
	meta := resolvePlatformMeta(manager, platformName)
	if strings.TrimSpace(meta.Emoji) != "" {
		return meta.Emoji
	}
	return "🎵"
}

func platformDisplayName(manager platform.Manager, platformName string) string {
	meta := resolvePlatformMeta(manager, platformName)
	if strings.TrimSpace(meta.DisplayName) != "" {
		return meta.DisplayName
	}
	return platformName
}

func platformTag(manager platform.Manager, platformName string) string {
	display := strings.TrimSpace(platformDisplayName(manager, platformName))
	if display == "" {
		return "music"
	}
	return display
}

func resolvePlatformMeta(manager platform.Manager, platformName string) platform.Meta {
	trimmed := strings.TrimSpace(platformName)
	if trimmed == "" {
		return platform.Meta{Name: "", DisplayName: "", Emoji: "🎵"}
	}
	if manager == nil {
		return platform.Meta{Name: trimmed, DisplayName: trimmed, Emoji: "🎵"}
	}
	if meta, ok := manager.Meta(trimmed); ok {
		return meta
	}
	return platform.Meta{Name: trimmed, DisplayName: trimmed, Emoji: "🎵"}
}

func resolvePlatformAlias(manager platform.Manager, token string) (string, bool) {
	if manager == nil {
		return "", false
	}
	return manager.ResolveAlias(token)
}

func matchPlatformTrack(ctx context.Context, manager platform.Manager, platformName, text string) (string, bool) {
	if manager == nil {
		return "", false
	}
	platformName = strings.TrimSpace(platformName)
	text = strings.TrimSpace(text)
	if platformName == "" || text == "" {
		return "", false
	}
	if _, _, matched := matchPlaylistURL(ctx, manager, text); matched {
		return "", false
	}
	resolvedText := resolveShortLinkText(ctx, manager, text)
	text = strings.TrimSpace(resolvedText)
	plat := manager.Get(platformName)
	if plat == nil {
		return "", false
	}
	if matcher, ok := plat.(platform.URLMatcher); ok {
		if id, ok := matcher.MatchURL(text); ok {
			return id, true
		}
	}
	if matcher, ok := plat.(platform.TextMatcher); ok {
		if id, ok := matcher.MatchText(text); ok {
			return id, true
		}
	}
	if isLikelyIDToken(text) && len(text) >= 5 {
		return text, true
	}
	return "", false
}

func fillSongInfoFromTrack(songInfo *botpkg.SongInfo, track *platform.Track, platformName, trackID string, message *telego.Message) {
	songInfo.Platform = platformName
	songInfo.TrackID = trackID

	if platformName == "netease" {
		if id, err := strconv.Atoi(trackID); err == nil {
			songInfo.MusicID = id
		}
	} else {
		songInfo.MusicID = 0
	}

	songInfo.Duration = int(track.Duration.Seconds())
	songInfo.SongName = track.Title

	artistNames := make([]string, 0, len(track.Artists))
	artistIDs := make([]string, 0, len(track.Artists))
	artistURLs := make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		artistNames = append(artistNames, artist.Name)
		artistIDs = append(artistIDs, artist.ID)
		if strings.TrimSpace(artist.URL) != "" {
			artistURLs = append(artistURLs, artist.URL)
		} else {
			artistURLs = append(artistURLs, "")
		}
	}
	songInfo.SongArtists = strings.Join(artistNames, "/")
	songInfo.SongArtistsIDs = strings.Join(artistIDs, ",")
	songInfo.SongArtistsURLs = strings.Join(artistURLs, ",")

	if track.Album != nil {
		songInfo.SongAlbum = track.Album.Title
		if strings.TrimSpace(track.Album.URL) != "" {
			songInfo.AlbumURL = track.Album.URL
		}
		if id, err := strconv.Atoi(track.Album.ID); err == nil {
			songInfo.AlbumID = id
		}
	}
	if strings.TrimSpace(track.URL) != "" {
		songInfo.TrackURL = track.URL
	}

	if message != nil {
		songInfo.FromChatID = message.Chat.ID
		if message.Chat.Type == "private" {
			songInfo.FromChatName = message.Chat.Username
		} else {
			songInfo.FromChatName = message.Chat.Title
		}
		if message.From != nil {
			songInfo.FromUserID = message.From.ID
			songInfo.FromUserName = message.From.Username
		}
	}
}
