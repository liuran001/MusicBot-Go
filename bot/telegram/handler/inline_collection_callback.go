package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// InlineCollectionCallbackHandler handles inline collection page callbacks.
type InlineCollectionCallbackHandler struct {
	Chosen      *ChosenInlineMusicHandler
	RateLimiter *telegram.RateLimiter
}

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
	state, ok := h.Chosen.getInlineCollectionState(token)
	if !ok || state == nil {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "列表已过期，请重新选择", ShowAlert: true})
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

	text, markup := h.Chosen.renderInlineCollectionPage(state, token, page)
	params := &telego.EditMessageTextParams{
		InlineMessageID:    query.InlineMessageID,
		Text:               text,
		ParseMode:          telego.ModeMarkdownV2,
		ReplyMarkup:        markup,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: fmt.Sprintf("第 %d 页", page)})
}
