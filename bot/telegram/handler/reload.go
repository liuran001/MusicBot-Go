package handler

import (
	"context"

	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// ReloadHandler handles /reload command for dynamic plugins.
type ReloadHandler struct {
	Reload      func(ctx context.Context) error
	RateLimiter *telegram.RateLimiter
	Logger      *logpkg.Logger
	AdminIDs    map[int64]struct{}
}

func (h *ReloadHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}
	message := update.Message

	if !isBotAdmin(h.AdminIDs, message.From.ID) {
		return
	}

	if h.Reload == nil {
		params := &telego.SendMessageParams{
			ChatID: telego.ChatID{ID: message.Chat.ID},
			Text:   "❌ 重载未启用",
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}

	if err := h.Reload(ctx); err != nil {
		if h.Logger != nil {
			h.Logger.Error("reload failed", "error", err)
		}
		params := &telego.SendMessageParams{
			ChatID: telego.ChatID{ID: message.Chat.ID},
			Text:   "❌ 重载失败: " + err.Error(),
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}

	params := &telego.SendMessageParams{
		ChatID: telego.ChatID{ID: message.Chat.ID},
		Text:   "✅ 动态插件已重载",
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}
