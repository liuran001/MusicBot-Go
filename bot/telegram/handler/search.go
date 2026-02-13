package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// SearchHandler handles /search and private message search.
type SearchHandler struct {
	PlatformManager  platform.Manager
	Repo             botpkg.SongRepository
	RateLimiter      *telegram.RateLimiter
	DefaultPlatform  string
	FallbackPlatform string
	PageSize         int
	searchMu         sync.Mutex
	searchCache      map[int]*searchState
}

const (
	searchCacheTTL        = 10 * time.Minute
	searchCacheMaxEntries = 256
	defaultSearchLimit    = 48
	neteaseSearchLimit    = 48
)

type searchState struct {
	keyword          string
	platform         string
	quality          string
	requesterID      int64
	limit            int
	currentPage      int
	updatedAt        time.Time
	tracksByPlatform map[string][]platform.Track
}

func (s *searchState) setTracks(platformName string, tracks []platform.Track) {
	if s == nil {
		return
	}
	name := strings.TrimSpace(platformName)
	if name == "" || len(tracks) == 0 {
		return
	}
	if s.tracksByPlatform == nil {
		s.tracksByPlatform = make(map[string][]platform.Track)
	}
	copied := make([]platform.Track, len(tracks))
	copy(copied, tracks)
	s.tracksByPlatform[name] = copied
}

func (s *searchState) getTracks(platformName string) ([]platform.Track, bool) {
	if s == nil || s.tracksByPlatform == nil {
		return nil, false
	}
	name := strings.TrimSpace(platformName)
	if name == "" {
		return nil, false
	}
	tracks, ok := s.tracksByPlatform[name]
	if !ok || len(tracks) == 0 {
		return nil, false
	}
	return tracks, true
}

func (h *SearchHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
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
		params := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: message.Chat.ID},
			Text:            inputKeyword,
			ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}

	sendParams := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		MessageThreadID: threadID,
		Text:            searching,
		ReplyParameters: replyParams,
	}
	var msgResult *telego.Message
	var err error
	if h.RateLimiter != nil {
		msgResult, err = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, sendParams)
	} else {
		msgResult, err = b.SendMessage(ctx, sendParams)
	}
	if err != nil {
		return
	}
	if h.PlatformManager == nil {
		params := &telego.EditMessageTextParams{
			ChatID:    telego.ChatID{ID: msgResult.Chat.ID},
			MessageID: msgResult.MessageID,
			Text:      noResults,
		}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	keyword, requestedPlatform, qualityOverride := parseTrailingOptions(keyword, h.PlatformManager)
	hasPlatformSuffix := strings.TrimSpace(requestedPlatform) != ""
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
		params := &telego.EditMessageTextParams{
			ChatID:    telego.ChatID{ID: msgResult.Chat.ID},
			MessageID: msgResult.MessageID,
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
		params := &telego.EditMessageTextParams{
			ChatID:    telego.ChatID{ID: msgResult.Chat.ID},
			MessageID: msgResult.MessageID,
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
			params := &telego.EditMessageTextParams{
				ChatID:    telego.ChatID{ID: msgResult.Chat.ID},
				MessageID: msgResult.MessageID,
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
		params := &telego.EditMessageTextParams{
			ChatID:    telego.ChatID{ID: msgResult.Chat.ID},
			MessageID: msgResult.MessageID,
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

	platformEmoji := platformEmoji(h.PlatformManager, platformName)
	displayName := platformDisplayName(h.PlatformManager, platformName)

	if usedFallback {
		textMessage.WriteString(fmt.Sprintf("⚠️ 默认平台搜索失败，已切换到%s\n\n", displayName))
	}

	textMessage.WriteString(fmt.Sprintf("%s *%s* 搜索结果\n\n", platformEmoji, mdV2Replacer.Replace(displayName)))

	requesterID := int64(0)
	if message.From != nil {
		requesterID = message.From.ID
	}

	qualityValue := h.resolveDefaultQuality(ctx, message, userID)
	if strings.TrimSpace(qualityOverride) != "" {
		qualityValue = qualityOverride
	}
	pageText, keyboard := h.buildSearchPage(tracks, platformName, keyword, qualityValue, requesterID, msgResult.MessageID, 1)
	textMessage.WriteString(pageText)
	disablePreview := true
	params := &telego.EditMessageTextParams{
		ChatID:             telego.ChatID{ID: msgResult.Chat.ID},
		MessageID:          msgResult.MessageID,
		Text:               textMessage.String(),
		ParseMode:          telego.ModeMarkdownV2,
		ReplyMarkup:        keyboard,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: disablePreview},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
	state := &searchState{
		keyword:     keyword,
		platform:    platformName,
		quality:     qualityValue,
		requesterID: requesterID,
		limit:       searchLimit,
		currentPage: 1,
		updatedAt:   time.Now(),
	}
	state.setTracks(platformName, tracks)
	h.storeSearchState(msgResult.MessageID, state)
}

type SearchCallbackHandler struct {
	Search      *SearchHandler
	RateLimiter *telegram.RateLimiter
}

func (h *SearchCallbackHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
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
	if query.Message == nil {
		return
	}
	msg := query.Message.Message()
	if msg == nil {
		return
	}
	if msg.Chat.Type != "private" {
		if !isRequesterOrAdmin(ctx, b, msg.Chat.ID, query.From.ID, requesterID) {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
				CallbackQueryID: query.ID,
				Text:            callbackDenied,
				ShowAlert:       true,
			})
			return
		}
	}
	state, ok := h.Search.getSearchState(messageID)
	if !ok {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "搜索结果已过期，请重新搜索",
		})
		return
	}
	if action == "close" {
		deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
		if h.RateLimiter != nil {
			_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
		} else {
			_ = b.DeleteMessage(ctx, deleteParams)
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
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
	if action != "platform" && state.currentPage == page {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: fmt.Sprintf("已是第 %d 页", page)})
		return
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
	pageSize := h.Search.pageSize()
	if limit <= 0 {
		limit = page * pageSize
	}
	tracks, ok := state.getTracks(state.platform)
	if !ok {
		tracks, err = plat.Search(ctx, state.keyword, limit)
		if err != nil {
			params := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID, Text: "搜索失败，请稍后重试"}
			if h.RateLimiter != nil {
				_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.EditMessageText(ctx, params)
			}
			return
		}
		if len(tracks) == 0 {
			params := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID, Text: noResults}
			if h.RateLimiter != nil {
				_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.EditMessageText(ctx, params)
			}
			return
		}
		state.setTracks(state.platform, tracks)
	}
	manager := h.Search.PlatformManager
	textHeader := fmt.Sprintf("%s *%s* 搜索结果\n\n", platformEmoji(manager, state.platform), mdV2Replacer.Replace(platformDisplayName(manager, state.platform)))
	pageText, keyboard := h.Search.buildSearchPage(tracks, state.platform, state.keyword, state.quality, state.requesterID, messageID, page)
	text := textHeader + pageText
	disablePreview := true
	params := &telego.EditMessageTextParams{
		ChatID:             telego.ChatID{ID: msg.Chat.ID},
		MessageID:          msg.MessageID,
		Text:               text,
		ParseMode:          telego.ModeMarkdownV2,
		ReplyMarkup:        keyboard,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: disablePreview},
	}
	if h.RateLimiter != nil {
		_, err = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, err = b.EditMessageText(ctx, params)
	}
	if err != nil {
		return
	}
	state.currentPage = page
	h.Search.storeSearchState(messageID, state)
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
}

func (h *SearchHandler) searchLimit(platformName string) int {
	if strings.TrimSpace(platformName) == "netease" {
		return neteaseSearchLimit
	}
	return defaultSearchLimit
}

func (h *SearchHandler) pageSize() int {
	if h == nil {
		return 8
	}
	if h.PageSize > 0 {
		return h.PageSize
	}
	return 8
}

func (h *SearchHandler) resolveDefaultQuality(ctx context.Context, message *telego.Message, userID int64) string {
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

func (h *SearchHandler) buildSearchPage(tracks []platform.Track, platformName, keyword, qualityValue string, requesterID int64, messageID int, page int) (string, *telego.InlineKeyboardMarkup) {
	pageSize := h.pageSize()
	if page < 1 {
		page = 1
	}
	pageCount := 1
	if len(tracks) > 0 {
		pageCount = (len(tracks)-1)/pageSize + 1
	}
	if page > pageCount {
		page = pageCount
	}
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	end := start + pageSize
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
	buttons := make([]telego.InlineKeyboardButton, 0, pageSize)
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
		buttons = append(buttons, telego.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i-start+1),
			CallbackData: callbackData,
		})
	}

	var rows [][]telego.InlineKeyboardButton
	if len(buttons) > 0 {
		rows = append(rows, buttons)
	}
	if pageCount > 1 {
		navRow := make([]telego.InlineKeyboardButton, 0, 2)
		if page == 1 {
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "关闭", CallbackData: fmt.Sprintf("search %d close %d", messageID, requesterID)})
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "下一页", CallbackData: fmt.Sprintf("search %d page %d %d", messageID, page+1, requesterID)})
			rows = append(rows, navRow)
		} else if page == pageCount {
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "上一页", CallbackData: fmt.Sprintf("search %d page %d %d", messageID, page-1, requesterID)})
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "回到首页", CallbackData: fmt.Sprintf("search %d home %d", messageID, requesterID)})
			rows = append(rows, navRow)
		} else {
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "上一页", CallbackData: fmt.Sprintf("search %d page %d %d", messageID, page-1, requesterID)})
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "下一页", CallbackData: fmt.Sprintf("search %d page %d %d", messageID, page+1, requesterID)})
			rows = append(rows, navRow)
			homeRow := []telego.InlineKeyboardButton{{Text: "回到首页", CallbackData: fmt.Sprintf("search %d home %d", messageID, requesterID)}}
			rows = append(rows, homeRow)
		}
	} else if page == 1 {
		rows = append(rows, []telego.InlineKeyboardButton{{Text: "关闭", CallbackData: fmt.Sprintf("search %d close %d", messageID, requesterID)}})
	}

	platforms := h.searchPlatforms()
	if len(platforms) > 1 {
		switchRow := make([]telego.InlineKeyboardButton, 0, len(platforms))
		for _, name := range platforms {
			text := fmt.Sprintf("%s %s", platformEmoji(h.PlatformManager, name), platformDisplayName(h.PlatformManager, name))
			if name == platformName {
				text = "✅ " + text
			}
			switchRow = append(switchRow, telego.InlineKeyboardButton{
				Text:         text,
				CallbackData: fmt.Sprintf("search %d platform %s %d", messageID, name, requesterID),
			})
		}
		rows = append(rows, switchRow)
	}
	keyboard := &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
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

func parseSearchKeywordPlatform(keyword string, manager platform.Manager) (string, string, bool) {
	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return trimmed, "", false
	}
	last := normalizePlatformToken(strings.ToLower(parts[len(parts)-1]))
	platformName, ok := resolvePlatformAlias(manager, last)
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
	for len(h.searchCache) > searchCacheMaxEntries {
		oldestKey := 0
		oldestTime := time.Now()
		first := true
		for key, state := range h.searchCache {
			updatedAt := time.Time{}
			if state != nil {
				updatedAt = state.updatedAt
			}
			if first || updatedAt.Before(oldestTime) {
				first = false
				oldestKey = key
				oldestTime = updatedAt
			}
		}
		delete(h.searchCache, oldestKey)
	}
}
