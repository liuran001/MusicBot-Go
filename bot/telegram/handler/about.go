package handler

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
)

// AboutHandler handles /about command.
type AboutHandler struct {
	RuntimeVer  string
	BinVersion  string
	CommitSHA   string
	BuildTime   string
	BuildArch   string
	RateLimiter *telegram.RateLimiter
}

func (h *AboutHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}
	msg := fmt.Sprintf(aboutText, h.RuntimeVer, h.BinVersion, h.CommitSHA, h.BuildTime, h.BuildArch)
	params := &bot.SendMessageParams{
		ChatID:          update.Message.Chat.ID,
		Text:            msg,
		ParseMode:       models.ParseModeMarkdown,
		ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}
