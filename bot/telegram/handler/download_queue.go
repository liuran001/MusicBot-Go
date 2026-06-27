package handler

import (
	"context"

	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// downloadQueueCallbackData is the callback payload for the "view queue" button
// shown on a queued download's status message. It carries no per-task state —
// tapping it just reports the current global queue counters — so a single
// constant suffices and there is nothing to expire.
const downloadQueueCallbackData = "dlq show"

// downloadQueueButton builds the one-button inline keyboard attached to the
// static "queued" status. The label is localized for the request language.
func downloadQueueButton(ctx context.Context) *telego.InlineKeyboardMarkup {
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{{
			{Text: tr(ctx, "download_queue_button"), CallbackData: downloadQueueCallbackData},
		}},
	}
}

// DownloadQueueCallbackHandler answers taps on the "view queue" button with a
// popup showing the live download/queue counts. It holds only a reference to the
// MusicHandler that owns the counters.
type DownloadQueueCallbackHandler struct {
	Music       *MusicHandler
	RateLimiter *telegram.RateLimiter
}

// Handle answers the callback query with an alert popup of the current counts.
func (h *DownloadQueueCallbackHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if h == nil || b == nil || update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	waiting, running, limit := 0, 0, 0
	if h.Music != nil {
		waiting, running, limit = h.Music.DownloadQueueStats()
	}
	var text string
	if limit > 0 {
		text = tr(ctx, "download_queue_status_limited", map[string]any{
			"Running": running, "Waiting": waiting, "Limit": limit,
		})
	} else {
		text = tr(ctx, "download_queue_status", map[string]any{
			"Running": running, "Waiting": waiting,
		})
	}
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
		Text:            text,
		ShowAlert:       true,
	})
}
