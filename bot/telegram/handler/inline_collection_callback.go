package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// InlineCollectionCallbackHandler handles inline collection page callbacks.
type InlineCollectionCallbackHandler struct {
	Chosen      *ChosenInlineMusicHandler
	RateLimiter *telegram.RateLimiter

	lastPageMu        sync.Mutex
	lastPage          map[string]inlineCollectionLastPageEntry
	lastPageCleanupAt time.Time
}

type inlineCollectionLastPageEntry struct {
	page         int
	updatedAt    time.Time
	lastAccessAt time.Time
}

const (
	inlineCollectionLastPageTTL             = 30 * time.Minute
	inlineCollectionLastPageCleanupInterval = 1 * time.Minute
	inlineCollectionLastPageMaxEntries      = 2000
)

func (h *InlineCollectionCallbackHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if h == nil || h.Chosen == nil || b == nil || update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	if strings.TrimSpace(query.InlineMessageID) == "" {
		return
	}
	parts := strings.Fields(query.Data)
	if len(parts) < 4 || parts[0] != "ipl" {
		return
	}
	token := strings.TrimSpace(parts[1])
	action := strings.TrimSpace(parts[2])
	if token == "" {
		return
	}
	requesterID, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || requesterID == 0 {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
		return
	}
	if query.From.ID != requesterID {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackDenied, ShowAlert: true})
		return
	}
	page := 1
	switch action {
	case "open":
		page = 1
	case "home":
		page = 1
	case "page":
		if len(parts) < 5 {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
			return
		}
		page, err = strconv.Atoi(parts[3])
		if err != nil || page < 1 {
			page = 1
		}
	default:
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
		return
	}

	withInlineMessageLock(query.InlineMessageID, func() {
		state, ok := h.Chosen.getInlineCollectionState(token)
		if !ok || state == nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "列表已过期，请重新选择", ShowAlert: true})
			return
		}

		targetPage := normalizeInlineCollectionPage(h.Chosen, state, page)
		if lastPage, ok := h.getLastInlineCollectionPage(query.InlineMessageID); ok && lastPage == targetPage {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: fmt.Sprintf("已是第 %d 页", targetPage)})
			return
		}
		if err := h.Chosen.ensureInlineCollectionChunk(ctx, state, targetPage); err != nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "加载失败，请重试", ShowAlert: true})
			return
		}

		text, markup := h.Chosen.renderInlineCollectionPage(state, token, targetPage)
		params := &telego.EditMessageTextParams{
			InlineMessageID:    query.InlineMessageID,
			Text:               text,
			ParseMode:          telego.ModeMarkdownV2,
			ReplyMarkup:        markup,
			LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
		}
		var editErr error
		if h.RateLimiter != nil {
			_, editErr = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, editErr = b.EditMessageText(ctx, params)
		}
		if editErr == nil {
			h.setLastInlineCollectionPage(query.InlineMessageID, targetPage)
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: fmt.Sprintf("第 %d 页", targetPage)})
	})
}

func normalizeInlineCollectionPage(chosen *ChosenInlineMusicHandler, state *inlineCollectionState, page int) int {
	if state == nil {
		return 1
	}
	pageSize := 8
	if chosen != nil {
		pageSize = chosen.inlineCollectionPageSize()
	}
	pageCount := 1
	if state.totalTracks > 0 {
		pageCount = (state.totalTracks-1)/pageSize + 1
	}
	if page < 1 {
		return 1
	}
	if page > pageCount {
		return pageCount
	}
	return page
}

func (h *InlineCollectionCallbackHandler) getLastInlineCollectionPage(inlineMessageID string) (int, bool) {
	if h == nil {
		return 0, false
	}
	inlineMessageID = strings.TrimSpace(inlineMessageID)
	if inlineMessageID == "" {
		return 0, false
	}
	h.lastPageMu.Lock()
	defer h.lastPageMu.Unlock()
	if h.lastPage == nil {
		return 0, false
	}
	entry, ok := h.lastPage[inlineMessageID]
	if !ok {
		return 0, false
	}
	if time.Since(entry.updatedAt) > inlineCollectionLastPageTTL {
		delete(h.lastPage, inlineMessageID)
		return 0, false
	}
	entry.lastAccessAt = time.Now()
	h.lastPage[inlineMessageID] = entry
	return entry.page, true
}

func (h *InlineCollectionCallbackHandler) setLastInlineCollectionPage(inlineMessageID string, page int) {
	if h == nil {
		return
	}
	inlineMessageID = strings.TrimSpace(inlineMessageID)
	if inlineMessageID == "" {
		return
	}
	h.lastPageMu.Lock()
	defer h.lastPageMu.Unlock()
	now := time.Now()
	if h.lastPage == nil {
		h.lastPage = make(map[string]inlineCollectionLastPageEntry)
	}
	if h.lastPageCleanupAt.IsZero() || now.Sub(h.lastPageCleanupAt) >= inlineCollectionLastPageCleanupInterval {
		for key, entry := range h.lastPage {
			if now.Sub(entry.updatedAt) > inlineCollectionLastPageTTL {
				delete(h.lastPage, key)
			}
		}
		h.lastPageCleanupAt = now
	}
	h.lastPage[inlineMessageID] = inlineCollectionLastPageEntry{page: page, updatedAt: now, lastAccessAt: now}

	for len(h.lastPage) > inlineCollectionLastPageMaxEntries {
		oldestKey := ""
		oldestAt := now
		initialized := false
		for key, entry := range h.lastPage {
			accessAt := entry.lastAccessAt
			if accessAt.IsZero() {
				accessAt = entry.updatedAt
			}
			if !initialized || accessAt.Before(oldestAt) {
				oldestAt = accessAt
				oldestKey = key
				initialized = true
			}
		}
		if !initialized || oldestKey == "" {
			break
		}
		delete(h.lastPage, oldestKey)
	}
}
