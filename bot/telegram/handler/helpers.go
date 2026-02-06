package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

func extractPlatformTrack(ctx context.Context, message *telego.Message, manager platform.Manager) (platformName, trackID string, found bool) {
	if message == nil || message.Text == "" {
		return "", "", false
	}

	text := message.Text
	args := commandArguments(message.Text)
	if args != "" {
		text = args
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
	}

	return "", "", false
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

var urlMatcher = regexp.MustCompile(`https?://[^\s]+`)

func extractFirstURL(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	match := urlMatcher.FindString(text)
	match = strings.TrimRight(match, ".,!?)]}>")
	return strings.TrimSpace(match)
}

func resolveShortLinkText(ctx context.Context, manager platform.Manager, text string) string {
	urlStr := extractFirstURL(text)
	if urlStr == "" {
		return text
	}
	resolved, err := resolveShortURL(ctx, manager, urlStr)
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
	resolved, err := resolveShortURL(ctx, manager, urlStr)
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
			if strings.EqualFold(strings.TrimSpace(domain), host) {
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
	return fmt.Sprintf(buildMusicInfoText(songName, songAlbum, fileInfo, suffixFmt), args...)
}

func buildMusicCaption(manager platform.Manager, songInfo *botpkg.SongInfo, botName string) string {
	if songInfo == nil {
		return ""
	}

	songNameHTML := songInfo.SongName
	artistsHTML := songInfo.SongArtists
	albumHTML := songInfo.SongAlbum
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
		songNameHTML = fmt.Sprintf("<a href=\"%s\">%s</a>", songInfo.TrackURL, songInfo.SongName)
	}

	if strings.TrimSpace(songInfo.SongArtistsURLs) != "" {
		artistURLs := strings.Split(songInfo.SongArtistsURLs, ",")
		artists := strings.Split(songInfo.SongArtists, "/")
		var parts []string
		for i, artist := range artists {
			artist = strings.TrimSpace(artist)
			if i < len(artistURLs) && strings.TrimSpace(artistURLs[i]) != "" {
				parts = append(parts, fmt.Sprintf("<a href=\"%s\">%s</a>", strings.TrimSpace(artistURLs[i]), artist))
				continue
			}
			parts = append(parts, artist)
		}
		artistsHTML = strings.Join(parts, " / ")
	}

	if strings.TrimSpace(songInfo.AlbumURL) != "" {
		albumHTML = fmt.Sprintf("<a href=\"%s\">%s</a>", songInfo.AlbumURL, songInfo.SongAlbum)
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
	return text, true
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
