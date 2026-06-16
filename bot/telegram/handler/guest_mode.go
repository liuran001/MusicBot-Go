package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/recognize"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// GuestModeHandler handles guest messages (bot channel posts).
type GuestModeHandler struct {
	PlatformManager  platform.Manager
	MusicHandler     *MusicHandler
	LyricHandler     *LyricHandler
	SearchHandler    *SearchHandler
	RateLimiter      *telegram.RateLimiter
	RecognizeService recognize.Service
	BotName          string
}

func (h *GuestModeHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.GuestMessage == nil {
		return
	}
	message := update.GuestMessage
	guestQueryID := strings.TrimSpace(message.GuestQueryID)
	if guestQueryID == "" {
		return
	}

	text := strings.TrimSpace(message.Text)
	if text == "" {
		return
	}

	// Detect bot @mention. The message may or may not start with @botName.
	content := h.stripBotMention(text)
	if content == "" {
		return
	}

	// Route based on content type.
	switch {
	case isShazamKeyword(content):
		h.handleGuestRecognize(ctx, b, message, guestQueryID)
	case isLyricKeyword(content):
		h.handleGuestLyric(ctx, b, message, content, guestQueryID)
	default:
		h.handleGuestSong(ctx, b, message, content, guestQueryID)
	}
}

// stripBotMention removes the leading @botName mention if present.
// Returns the remaining text after the mention, or the full text if no mention.
func (h *GuestModeHandler) stripBotMention(text string) string {
	botName := strings.TrimSpace(h.BotName)
	if botName == "" {
		return text
	}
	// The message may start with "@botname " or "@botname\n"
	prefix := "@" + botName
	if strings.HasPrefix(text, prefix) {
		rest := strings.TrimPrefix(text, prefix)
		rest = strings.TrimLeft(rest, " \t\n")
		return rest
	}
	return text
}

// handleGuestSong resolves the content as a song link/ID or search keyword and
// responds via AnswerGuestQuery.
func (h *GuestModeHandler) handleGuestSong(ctx context.Context, b *telego.Bot, message *telego.Message, content, guestQueryID string) {
	if h.PlatformManager == nil {
		h.answerGuest(ctx, b, guestQueryID, "服务不可用")
		return
	}

	// Try to match as a direct track URL/ID first.
	resolvedText := resolveShortLinkText(ctx, h.PlatformManager, content)
	if platformName, trackID, matched := h.PlatformManager.MatchText(resolvedText); matched {
		h.sendGuestTrackResult(ctx, b, guestQueryID, platformName, trackID)
		return
	}
	if platformName, trackID, matched := h.PlatformManager.MatchURL(resolvedText); matched {
		h.sendGuestTrackResult(ctx, b, guestQueryID, platformName, trackID)
		return
	}

	// Not a direct link — search for the keyword and respond with the first result.
	h.handleGuestSearch(ctx, b, message, content, guestQueryID)
}

// sendGuestTrackResult answers the guest query with a track article.
func (h *GuestModeHandler) sendGuestTrackResult(ctx context.Context, b *telego.Bot, guestQueryID, platformName, trackID string) {
	title := platformDisplayName(h.PlatformManager, platformName)
	resultText := fmt.Sprintf("🎵 正在获取: %s\n%s", title, trackID)

	article := &telego.InlineQueryResultArticle{
		Type: telego.ResultTypeArticle,
		ID:   fmt.Sprintf("guest_%s_%s", platformName, trackID),
		Title: title,
		Description: fmt.Sprintf("平台: %s · ID: %s", platformName, trackID),
		InputMessageContent: &telego.InputTextMessageContent{MessageText: resultText},
	}
	_, _ = b.AnswerGuestQuery(ctx, &telego.AnswerGuestQueryParams{
		GuestQueryID: guestQueryID,
		Result:       article,
	})
}

// handleGuestSearch searches for a keyword and responds with the first result.
func (h *GuestModeHandler) handleGuestSearch(ctx context.Context, b *telego.Bot, message *telego.Message, keyword, guestQueryID string) {
	if h.SearchHandler == nil || h.SearchHandler.PlatformManager == nil {
		h.answerGuest(ctx, b, guestQueryID, "搜索服务不可用")
		return
	}

	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		h.answerGuest(ctx, b, guestQueryID, "")
		return
	}

	// Parse trailing platform/quality options.
	keyword, requestedPlatform, _ := parseTrailingOptions(keyword, h.SearchHandler.PlatformManager)
	if strings.TrimSpace(keyword) == "" {
		h.answerGuest(ctx, b, guestQueryID, "")
		return
	}

	platformName := h.SearchHandler.DefaultPlatform
	if platformName == "" {
		platformName = "netease"
	}
	fallbackPlatform := h.SearchHandler.FallbackPlatform
	if fallbackPlatform == "" {
		fallbackPlatform = "netease"
	}
	if strings.TrimSpace(requestedPlatform) != "" {
		platformName = requestedPlatform
		fallbackPlatform = ""
	}

	tracks, matchedPlatform, _, err := searchTracksWithFallback(ctx, h.SearchHandler.PlatformManager, platformName, fallbackPlatform, keyword, nil, true)
	if err != nil || len(tracks) == 0 {
		h.answerGuest(ctx, b, guestQueryID, "未找到相关歌曲")
		return
	}

	first := tracks[0]
	resolvedPlatform := strings.TrimSpace(first.Platform)
	if resolvedPlatform == "" {
		resolvedPlatform = matchedPlatform
	}
	if resolvedPlatform == "" || strings.TrimSpace(first.ID) == "" {
		h.answerGuest(ctx, b, guestQueryID, "未找到相关歌曲")
		return
	}

	artists := trackArtistsLabel(first.Artists)
	title := strings.TrimSpace(first.Title)
	if title == "" {
		title = first.ID
	}
	desc := fmt.Sprintf("%s - %s", title, artists)
	article := &telego.InlineQueryResultArticle{
		Type:        telego.ResultTypeArticle,
		ID:          fmt.Sprintf("guest_search_%s_%s", resolvedPlatform, first.ID),
		Title:       title,
		Description: desc,
		InputMessageContent: &telego.InputTextMessageContent{
			MessageText: fmt.Sprintf("🎵 %s\n%s", title, artists),
		},
	}
	_, _ = b.AnswerGuestQuery(ctx, &telego.AnswerGuestQueryParams{
		GuestQueryID: guestQueryID,
		Result:       article,
	})
}

// handleGuestLyric processes a lyric request from a guest message.
// The keyword may contain a trailing platform/quality option.
func (h *GuestModeHandler) handleGuestLyric(ctx context.Context, b *telego.Bot, message *telego.Message, content, guestQueryID string) {
	// Remove the leading "歌词" keyword.
	keyword := strings.TrimSpace(strings.TrimPrefix(content, "歌词"))
	if keyword == "" {
		h.answerGuest(ctx, b, guestQueryID, "请输入歌曲名或链接")
		return
	}
	if h.LyricHandler == nil {
		h.answerGuest(ctx, b, guestQueryID, "歌词服务不可用")
		return
	}
	if h.LyricHandler.PlatformManager == nil {
		h.answerGuest(ctx, b, guestQueryID, "歌词服务不可用")
		return
	}

	// Try to match as a direct track URL/ID.
	resolvedText := resolveShortLinkText(ctx, h.LyricHandler.PlatformManager, keyword)
	if platformName, trackID, matched := h.LyricHandler.PlatformManager.MatchText(resolvedText); matched {
		h.fetchGuestLyric(ctx, b, guestQueryID, platformName, trackID)
		return
	}
	if platformName, trackID, matched := h.LyricHandler.PlatformManager.MatchURL(resolvedText); matched {
		h.fetchGuestLyric(ctx, b, guestQueryID, platformName, trackID)
		return
	}

	// Not a direct link — search and use the first result.
	if h.SearchHandler != nil && h.SearchHandler.PlatformManager != nil {
		keyword, requestedPlatform, _ := parseTrailingOptions(keyword, h.SearchHandler.PlatformManager)
		platformName := h.LyricHandler.resolveDefaultPlatform(ctx, message)
		if strings.TrimSpace(requestedPlatform) != "" {
			platformName = requestedPlatform
		}
		fallbackPlatform := strings.TrimSpace(h.LyricHandler.FallbackPlatform)
		if fallbackPlatform == "" {
			fallbackPlatform = "netease"
		}
		tracks, matchedPlatform, _, err := searchTracksWithFallback(ctx, h.LyricHandler.PlatformManager, platformName, fallbackPlatform, keyword, nil, true)
		if err != nil || len(tracks) == 0 {
			h.answerGuest(ctx, b, guestQueryID, "未找到相关歌曲")
			return
		}
		first := tracks[0]
		resolvedPlatform := strings.TrimSpace(first.Platform)
		if resolvedPlatform == "" {
			resolvedPlatform = matchedPlatform
		}
		if resolvedPlatform == "" || strings.TrimSpace(first.ID) == "" {
			h.answerGuest(ctx, b, guestQueryID, "未找到相关歌曲")
			return
		}
		h.fetchGuestLyric(ctx, b, guestQueryID, resolvedPlatform, first.ID)
		return
	}

	// Fallback to searchFirstTrackForLyric.
	platformName, trackID, found := h.LyricHandler.searchFirstTrackForLyric(ctx, message, keyword)
	if !found {
		h.answerGuest(ctx, b, guestQueryID, "未找到相关歌曲")
		return
	}
	h.fetchGuestLyric(ctx, b, guestQueryID, platformName, trackID)
}

// fetchGuestLyric fetches lyrics for a track and returns the text via AnswerGuestQuery.
func (h *GuestModeHandler) fetchGuestLyric(ctx context.Context, b *telego.Bot, guestQueryID, platformName, trackID string) {
	if h.LyricHandler == nil || h.LyricHandler.PlatformManager == nil {
		h.answerGuest(ctx, b, guestQueryID, "歌词服务不可用")
		return
	}
	plat := h.LyricHandler.PlatformManager.Get(platformName)
	if plat == nil || !plat.SupportsLyrics() {
		h.answerGuest(ctx, b, guestQueryID, "此平台不支持获取歌词")
		return
	}

	lyrics, err := plat.GetLyrics(ctx, trackID)
	if err != nil || lyrics == nil {
		h.answerGuest(ctx, b, guestQueryID, "获取歌词失败")
		return
	}

	content := lyrics.Plain
	if strings.TrimSpace(content) == "" {
		content = "暂无歌词信息"
	}
	if len(content) > 4000 {
		content = content[:4000] + "\n...(歌词过长已截断)"
	}

	title := platformDisplayName(h.LyricHandler.PlatformManager, platformName)
	article := &telego.InlineQueryResultArticle{
		Type:        telego.ResultTypeArticle,
		ID:          fmt.Sprintf("guest_lyric_%s_%s", platformName, trackID),
		Title:       fmt.Sprintf("歌词: %s", title),
		Description: "点击查看歌词",
		InputMessageContent: &telego.InputTextMessageContent{MessageText: content},
	}
	_, _ = b.AnswerGuestQuery(ctx, &telego.AnswerGuestQueryParams{
		GuestQueryID: guestQueryID,
		Result:       article,
	})
}

// handleGuestRecognize triggers audio recognition for a guest voice message.
func (h *GuestModeHandler) handleGuestRecognize(ctx context.Context, b *telego.Bot, message *telego.Message, guestQueryID string) {
	if h.RecognizeService == nil {
		h.answerGuest(ctx, b, guestQueryID, "识别服务未启动")
		return
	}
	if message.ReplyToMessage == nil || message.ReplyToMessage.Voice == nil {
		h.answerGuest(ctx, b, guestQueryID, "请回复一条语音消息")
		return
	}
	// Guest mode cannot download files directly — inform the user to use
	// the bot's private chat for recognition.
	h.answerGuest(ctx, b, guestQueryID, "访客模式暂不支持听歌识曲，请私聊使用")
}

// answerGuest sends a simple text response via AnswerGuestQuery.
func (h *GuestModeHandler) answerGuest(ctx context.Context, b *telego.Bot, guestQueryID, text string) {
	if strings.TrimSpace(text) == "" {
		text = "MusicBot-Go"
	}
	article := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  "guest_response",
		Title:               text,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: text},
	}
	_, _ = b.AnswerGuestQuery(ctx, &telego.AnswerGuestQueryParams{
		GuestQueryID: guestQueryID,
		Result:       article,
	})
}

func isShazamKeyword(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == "听歌识曲" || trimmed == "识曲"
}

func isLyricKeyword(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "歌词")
}

// trackArtistsLabel builds a comma-separated artist list from a track's artists.
func trackArtistsLabel(artists []platform.Artist) string {
	if len(artists) == 0 {
		return ""
	}
	names := make([]string, 0, len(artists))
	for _, a := range artists {
		if n := strings.TrimSpace(a.Name); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}
