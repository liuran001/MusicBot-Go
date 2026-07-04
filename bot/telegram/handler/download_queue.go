package handler

import (
	"context"
	"strconv"

	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// downloadQueueCallbackData is the callback payload for the "view queue" button
// shown on a queued download's status message. It carries no per-task state —
// tapping it just reports the current global queue counters — so a single
// constant suffices and there is nothing to expire.
const downloadQueueCallbackData = "dlq show"

const telegramCallbackTextLimit = 200

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
	snapshot := DownloadQueueSnapshot{}
	if h.Music != nil {
		snapshot = h.Music.DownloadQueueSnapshot()
	}
	text := truncateCallbackText(formatDownloadQueueCallback(ctx, snapshot))
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
		Text:            text,
		ShowAlert:       true,
	})
}

func formatDownloadQueueCallback(ctx context.Context, s DownloadQueueSnapshot) string {
	var text string
	if s.WaitLimit > 0 {
		text = tr(ctx, "download_queue_status_limited", map[string]any{
			"Running": s.Running,
			"Waiting": s.Waiting,
			"Limit":   s.WaitLimit,
		})
	} else {
		text = tr(ctx, "download_queue_status", map[string]any{
			"Running": s.Running,
			"Waiting": s.Waiting,
		})
	}
	if s.ActiveLimit > 0 {
		text += "\n" + tr(ctx, "download_queue_active_limited", map[string]any{
			"Active": s.Active,
			"Limit":  s.ActiveLimit,
		})
	}
	return text
}

func truncateCallbackText(text string) string {
	runes := []rune(text)
	if len(runes) <= telegramCallbackTextLimit {
		return text
	}
	return string(runes[:telegramCallbackTextLimit-3]) + "..."
}

type DownloadQueueCommandHandler struct {
	Music       *MusicHandler
	RateLimiter *telegram.RateLimiter
}

func (h *DownloadQueueCommandHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if h == nil || b == nil || update == nil || update.Message == nil {
		return
	}
	snapshot := DownloadQueueSnapshot{}
	if h.Music != nil {
		snapshot = h.Music.DownloadQueueSnapshot()
	}
	var sendStats *telegramQueueStats
	if h.RateLimiter != nil {
		waiting, running, capacity, workers := h.RateLimiter.QueueStats()
		sendStats = &telegramQueueStats{waiting: waiting, running: running, capacity: capacity, workers: workers}
	}
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: update.Message.Chat.ID},
		MessageThreadID: update.Message.MessageThreadID,
		Text:            formatDownloadQueueSnapshot(ctx, snapshot, sendStats),
		ReplyParameters: buildReplyParams(update.Message),
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		return
	}
	_, _ = b.SendMessage(ctx, params)
}

type telegramQueueStats struct {
	waiting  int
	running  int
	capacity int
	workers  int
}

func formatDownloadQueueSnapshot(ctx context.Context, s DownloadQueueSnapshot, send *telegramQueueStats) string {
	waitLimit := formatOptionalLimit(s.WaitLimit)
	activeLimit := formatOptionalLimit(s.ActiveLimit)
	uploadQueueLimit := formatOptionalLimit(s.UploadQueueLimit)
	uploadLimit := formatOptionalLimit(s.UploadLimit)
	text := tr(ctx, "download_queue_status_detail", map[string]any{
		"Running":          s.Running,
		"Waiting":          s.Waiting,
		"WaitLimit":        waitLimit,
		"Active":           s.Active,
		"ActiveLimit":      activeLimit,
		"PerUserLimit":     formatLimitValue(s.PerUserLimit),
		"PerChatLimit":     formatLimitValue(s.PerChatLimit),
		"UploadWaiting":    s.UploadWaiting,
		"UploadQueueLimit": uploadQueueLimit,
		"UploadRunning":    s.UploadRunning,
		"UploadLimit":      uploadLimit,
	})
	if send == nil || send.capacity <= 0 {
		return text
	}
	return text + "\n" + tr(ctx, "telegram_send_queue_status", map[string]any{
		"Running": send.running,
		"Waiting": send.waiting,
		"Limit":   formatOptionalLimit(send.capacity),
		"Workers": send.workers,
	})
}

func formatOptionalLimit(limit int) string {
	if limit <= 0 {
		return ""
	}
	return " / " + formatLimitValue(limit)
}

func formatLimitValue(limit int) string {
	if limit <= 0 {
		return "-"
	}
	return strconv.Itoa(limit)
}
