package handler

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// AboutHandler handles /about command.
type AboutHandler struct {
	RuntimeVer string
	BinVersion string
	CommitSHA  string
	BuildTime  string
	BuildArch  string
}

func (h *AboutHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}
	msg := fmt.Sprintf(aboutText, h.RuntimeVer, h.BinVersion, h.CommitSHA, h.BuildTime, h.BuildArch)
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          update.Message.Chat.ID,
		Text:            msg,
		ParseMode:       models.ParseModeMarkdown,
		ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
	})
}
