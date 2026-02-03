package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
)

// SearchHandler handles /search and private message search.
type SearchHandler struct {
	PlatformManager  platform.Manager
	Repo             botpkg.SongRepository
	RateLimiter      *telegram.RateLimiter
	DefaultPlatform  string
	FallbackPlatform string
	searchMu         sync.Mutex
	searchCache      map[int]*searchState
}

const (
	searchPageSize     = 8
	searchCacheTTL     = 10 * time.Minute
	defaultSearchLimit = 10
	neteaseSearchLimit = 48
)

var searchPlatformAliases = map[string]string{
	"wy":      "netease",
	"163":     "netease",
	"netease": "netease",
	"qq":      "tencent",
	"tencent": "tencent",
	"qqmusic": "tencent",
}

type searchState struct {
	keyword     string
	platform    string
	quality     string
	requesterID int64
	limit       int
	updatedAt   time.Time
}

func (h *SearchHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}

	message := update.Message
	threadID := message.MessageThreadID
	replyParams := buildReplyParams(message)
	keyword := commandArguments(message.Text)
	if keyword == "" && message.Chat.Type == "private" {
		if !strings.HasPrefix(strings.TrimSpace(message.Text), "/") {
			keyword = message.Text
		}
	}
	if strings.TrimSpace(keyword) == "" {
		params := &bot.SendMessageParams{
			ChatID:          message.Chat.ID,
			Text:            inputKeyword,
			ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}

	msgResult, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          message.Chat.ID,
		MessageThreadID: threadID,
		Text:            searching,
		ReplyParameters: replyParams,
	})
	if err != nil {
		return
	}
	if h.PlatformManager == nil {
		params := &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      noResults,
		}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	keyword, requestedPlatform, hasPlatformSuffix := parseSearchKeywordPlatform(keyword)
	// Get user's default platform from settings
	platformName := h.DefaultPlatform
	if platformName == "" {
		platformName = "netease"
	}
	fallbackPlatform := h.FallbackPlatform
	if fallbackPlatform == "" {
		fallbackPlatform = "netease"
	}
	if h.Repo != nil {
		if message.Chat.Type != "private" {
			if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
				platformName = settings.DefaultPlatform
			}
		}
	}
	var userID int64
	if message.From != nil {
		userID = message.From.ID
		if h.Repo != nil {
			if message.Chat.Type == "private" {
				if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
					platformName = settings.DefaultPlatform
				}
			} else if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
				platformName = settings.DefaultPlatform
			}
		}
	}
	if hasPlatformSuffix {
		platformName = requestedPlatform
		fallbackPlatform = ""
	}

	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		params := &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      noResults,
		}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	if !plat.SupportsSearch() {
		params := &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      "此平台不支持搜索功能",
		}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	searchLimit := h.searchLimit(platformName)
	tracks, err := plat.Search(ctx, keyword, searchLimit)
	usedFallback := false
	if err != nil {
		if fallbackPlatform != "" && platformName != fallbackPlatform {
			fallbackPlat := h.PlatformManager.Get(fallbackPlatform)
			if fallbackPlat != nil && fallbackPlat.SupportsSearch() {
				fallbackLimit := h.searchLimit(fallbackPlatform)
				tracks, err = fallbackPlat.Search(ctx, keyword, fallbackLimit)
				if err == nil && len(tracks) > 0 {
					platformName = fallbackPlatform
					plat = fallbackPlat
					usedFallback = true
					searchLimit = fallbackLimit
				}
			}
		}

		if err != nil {
			errorText := noResults
			if errors.Is(err, platform.ErrUnsupported) {
				errorText = "此平台不支持搜索功能"
			} else if errors.Is(err, platform.ErrRateLimited) {
				errorText = "请求过于频繁，请稍后再试"
			} else if errors.Is(err, platform.ErrUnavailable) {
				errorText = "搜索服务暂时不可用"
			}
			params := &bot.EditMessageTextParams{
				ChatID:    msgResult.Chat.ID,
				MessageID: msgResult.ID,
				Text:      errorText,
			}
			if h.RateLimiter != nil {
				_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.EditMessageText(ctx, params)
			}
			return
		}
	}

	if len(tracks) == 0 {
		params := &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      noResults,
		}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	var textMessage strings.Builder

	platformEmoji := platformEmoji(platformName)
	displayName := platformDisplayName(platformName)

	if usedFallback {
		textMessage.WriteString(fmt.Sprintf("⚠️ 默认平台搜索失败，已切换到%s\n\n", displayName))
	}

	textMessage.WriteString(fmt.Sprintf("%s *%s* 搜索结果\n\n", platformEmoji, mdV2Replacer.Replace(displayName)))

	requesterID := int64(0)
	if message.From != nil {
		requesterID = message.From.ID
	}

	qualityValue := h.resolveDefaultQuality(ctx, message, userID)
	pageText, keyboard := h.buildSearchPage(tracks, platformName, keyword, qualityValue, requesterID, msgResult.ID, 1)
	textMessage.WriteString(pageText)
	disablePreview := true
	params := &bot.EditMessageTextParams{
		ChatID:             msgResult.Chat.ID,
		MessageID:          msgResult.ID,
		Text:               textMessage.String(),
		ParseMode:          models.ParseModeMarkdown,
		ReplyMarkup:        keyboard,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: &disablePreview},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
	h.storeSearchState(msgResult.ID, &searchState{
		keyword:     keyword,
		platform:    platformName,
		quality:     qualityValue,
		requesterID: requesterID,
		limit:       searchLimit,
		updatedAt:   time.Now(),
	})
}

type SearchCallbackHandler struct {
	Search      *SearchHandler
	RateLimiter *telegram.RateLimiter
}

func (h *SearchCallbackHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.CallbackQuery == nil || h.Search == nil {
		return
	}
	query := update.CallbackQuery
	parts := strings.Fields(query.Data)
	if len(parts) < 4 || parts[0] != "search" {
		return
	}
	messageID, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}
	action := parts[2]
	page := 0
	if action == "page" {
		page, err = strconv.Atoi(parts[3])
		if err != nil {
			return
		}
	}
	requesterIDIndex := 3
	if action == "page" {
		requesterIDIndex = 4
	}
	if len(parts) <= requesterIDIndex {
		return
	}
	requesterID, _ := strconv.ParseInt(parts[requesterIDIndex], 10, 64)
	if query.Message.Message == nil {
		return
	}
	msg := query.Message.Message
	if msg.Chat.Type != "private" {
		if !isRequesterOrAdmin(ctx, b, msg.Chat.ID, query.From.ID, requesterID) {
			_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
				CallbackQueryID: query.ID,
				Text:            callbackDenied,
				ShowAlert:       true,
			})
			return
		}
	}
	state, ok := h.Search.getSearchState(messageID)
	if !ok {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "搜索结果已过期，请重新搜索",
		})
		return
	}
	if action == "close" {
		deleteParams := &bot.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID}
		if h.RateLimiter != nil {
			_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
		} else {
			_, _ = b.DeleteMessage(ctx, deleteParams)
		}
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
		return
	}
	if action == "platform" {
		if len(parts) < 5 {
			return
		}
		state.platform = strings.TrimSpace(parts[3])
		page = 1
		state.limit = h.Search.searchLimit(state.platform)
	}
	if action == "home" {
		page = 1
	}
	if page < 1 {
		page = 1
	}
	if h.Search.PlatformManager == nil {
		return
	}
	plat := h.Search.PlatformManager.Get(state.platform)
	if plat == nil {
		return
	}
	if !plat.SupportsSearch() {
		return
	}
	limit := state.limit
	if limit <= 0 {
		limit = page * searchPageSize
	}
	tracks, err := plat.Search(ctx, state.keyword, limit)
	if err != nil {
		params := &bot.EditMessageTextParams{ChatID: msg.Chat.ID, MessageID: msg.ID, Text: "搜索失败，请稍后重试"}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}
	if len(tracks) == 0 {
		params := &bot.EditMessageTextParams{ChatID: msg.Chat.ID, MessageID: msg.ID, Text: noResults}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}
	textHeader := fmt.Sprintf("%s *%s* 搜索结果\n\n", platformEmoji(state.platform), mdV2Replacer.Replace(platformDisplayName(state.platform)))
	pageText, keyboard := h.Search.buildSearchPage(tracks, state.platform, state.keyword, state.quality, state.requesterID, messageID, page)
	text := textHeader + pageText
	disablePreview := true
	params := &bot.EditMessageTextParams{
		ChatID:             msg.Chat.ID,
		MessageID:          msg.ID,
		Text:               text,
		ParseMode:          models.ParseModeMarkdown,
		ReplyMarkup:        keyboard,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: &disablePreview},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
	h.Search.storeSearchState(messageID, state)
	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
}

func (h *SearchHandler) searchLimit(platformName string) int {
	if strings.TrimSpace(platformName) == "netease" {
		return neteaseSearchLimit
	}
	return defaultSearchLimit
}

func (h *SearchHandler) resolveDefaultQuality(ctx context.Context, message *models.Message, userID int64) string {
	qualityValue := "hires"
	if h.Repo == nil {
		return qualityValue
	}
	if message != nil && message.Chat.Type != "private" {
		if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultQuality) != "" {
				qualityValue = settings.DefaultQuality
			}
		}
		return qualityValue
	}
	if userID != 0 {
		if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultQuality) != "" {
				qualityValue = settings.DefaultQuality
			}
		}
	}
	return qualityValue
}

func (h *SearchHandler) buildSearchPage(tracks []platform.Track, platformName, keyword, qualityValue string, requesterID int64, messageID int, page int) (string, *models.InlineKeyboardMarkup) {
	if page < 1 {
		page = 1
	}
	pageCount := 1
	if len(tracks) > 0 {
		pageCount = (len(tracks)-1)/searchPageSize + 1
	}
	if page > pageCount {
		page = pageCount
	}
	start := (page - 1) * searchPageSize
	if start < 0 {
		start = 0
	}
	end := start + searchPageSize
	if end > len(tracks) {
		end = len(tracks)
	}
	var textMessage strings.Builder
	if strings.TrimSpace(keyword) != "" {
		textMessage.WriteString(fmt.Sprintf("关键词: %s\n", mdV2Replacer.Replace(keyword)))
	}
	if pageCount > 1 {
		textMessage.WriteString(fmt.Sprintf("第 %d/%d 页\n\n", page, pageCount))
	} else {
		textMessage.WriteString("\n")
	}
	buttons := make([]models.InlineKeyboardButton, 0, searchPageSize)
	for i := start; i < end; i++ {
		track := tracks[i]
		escapedTitle := mdV2Replacer.Replace(track.Title)
		trackLink := escapedTitle
		if strings.TrimSpace(track.URL) != "" {
			trackLink = fmt.Sprintf("[%s](%s)", escapedTitle, track.URL)
		}
		var artistParts []string
		for _, artist := range track.Artists {
			escapedArtist := mdV2Replacer.Replace(artist.Name)
			if strings.TrimSpace(artist.URL) != "" {
				artistParts = append(artistParts, fmt.Sprintf("[%s](%s)", escapedArtist, artist.URL))
			} else {
				artistParts = append(artistParts, escapedArtist)
			}
		}
		songArtists := strings.Join(artistParts, " / ")
		textMessage.WriteString(fmt.Sprintf("%d\\. 「%s」 \\- %s\n", i-start+1, trackLink, songArtists))
		callbackData := fmt.Sprintf("music %s %s %s %d", platformName, track.ID, qualityValue, requesterID)
		buttons = append(buttons, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i-start+1),
			CallbackData: callbackData,
		})
	}

	var rows [][]models.InlineKeyboardButton
	if len(buttons) > 0 {
		rows = append(rows, buttons)
	}
	if pageCount > 1 {
		navRow := make([]models.InlineKeyboardButton, 0, 2)
		if page == 1 {
			navRow = append(navRow, models.InlineKeyboardButton{Text: "关闭", CallbackData: fmt.Sprintf("search %d close %d", messageID, requesterID)})
			navRow = append(navRow, models.InlineKeyboardButton{Text: "下一页", CallbackData: fmt.Sprintf("search %d page %d %d", messageID, page+1, requesterID)})
		} else if page == pageCount {
			navRow = append(navRow, models.InlineKeyboardButton{Text: "上一页", CallbackData: fmt.Sprintf("search %d page %d %d", messageID, page-1, requesterID)})
			navRow = append(navRow, models.InlineKeyboardButton{Text: "回到首页", CallbackData: fmt.Sprintf("search %d home %d", messageID, requesterID)})
		} else {
			navRow = append(navRow, models.InlineKeyboardButton{Text: "上一页", CallbackData: fmt.Sprintf("search %d page %d %d", messageID, page-1, requesterID)})
			navRow = append(navRow, models.InlineKeyboardButton{Text: "下一页", CallbackData: fmt.Sprintf("search %d page %d %d", messageID, page+1, requesterID)})
		}
		rows = append(rows, navRow)
	} else if page == 1 {
		rows = append(rows, []models.InlineKeyboardButton{{Text: "关闭", CallbackData: fmt.Sprintf("search %d close %d", messageID, requesterID)}})
	}

	platforms := h.searchPlatforms()
	if len(platforms) > 1 {
		switchRow := make([]models.InlineKeyboardButton, 0, len(platforms))
		for _, name := range platforms {
			text := fmt.Sprintf("%s %s", platformEmoji(name), platformDisplayName(name))
			if name == platformName {
				text = "✅ " + text
			}
			switchRow = append(switchRow, models.InlineKeyboardButton{
				Text:         text,
				CallbackData: fmt.Sprintf("search %d platform %s %d", messageID, name, requesterID),
			})
		}
		rows = append(rows, switchRow)
	}
	keyboard := &models.InlineKeyboardMarkup{InlineKeyboard: rows}
	return textMessage.String(), keyboard
}

func (h *SearchHandler) searchPlatforms() []string {
	if h == nil || h.PlatformManager == nil {
		return nil
	}
	names := h.PlatformManager.List()
	results := make([]string, 0, len(names))
	for _, name := range names {
		plat := h.PlatformManager.Get(name)
		if plat == nil || !plat.SupportsSearch() {
			continue
		}
		results = append(results, name)
	}
	return results
}

func parseSearchKeywordPlatform(keyword string) (string, string, bool) {
	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return trimmed, "", false
	}
	last := strings.ToLower(parts[len(parts)-1])
	platformName, ok := searchPlatformAliases[last]
	if !ok {
		return trimmed, "", false
	}
	mainKeyword := strings.Join(parts[:len(parts)-1], " ")
	if strings.TrimSpace(mainKeyword) == "" {
		return trimmed, "", false
	}
	return mainKeyword, platformName, true
}

func (h *SearchHandler) storeSearchState(messageID int, state *searchState) {
	if messageID == 0 || state == nil {
		return
	}
	h.searchMu.Lock()
	defer h.searchMu.Unlock()
	if h.searchCache == nil {
		h.searchCache = make(map[int]*searchState)
	}
	h.cleanupSearchStateLocked()
	h.searchCache[messageID] = state
}

func (h *SearchHandler) getSearchState(messageID int) (*searchState, bool) {
	h.searchMu.Lock()
	defer h.searchMu.Unlock()
	if h.searchCache == nil {
		return nil, false
	}
	h.cleanupSearchStateLocked()
	state, ok := h.searchCache[messageID]
	if ok && state != nil {
		state.updatedAt = time.Now()
	}
	return state, ok
}

func (h *SearchHandler) cleanupSearchStateLocked() {
	if h.searchCache == nil {
		return
	}
	cutoff := time.Now().Add(-searchCacheTTL)
	for key, state := range h.searchCache {
		if state == nil || state.updatedAt.Before(cutoff) {
			delete(h.searchCache, key)
		}
	}
}
