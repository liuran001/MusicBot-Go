package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
)

// StatusHandler handles /status command.
type StatusHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
}

var statLimiter = make(chan struct{}, 1)

func (h *StatusHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil || h.Repo == nil {
		return
	}
	message := update.Message

	statLimiter <- struct{}{}
	defer func() {
		time.Sleep(500 * time.Millisecond)
		<-statLimiter
	}()

	fromCount, _ := h.Repo.Count(ctx)
	chatCount, _ := h.Repo.CountByChatID(ctx, message.Chat.ID)
	chatInfo := message.Chat.Title
	if message.Chat.Username != "" && message.Chat.Title == "" {
		chatInfo = fmt.Sprintf("[%s](tg://user?id=%d)", mdV2Replacer.Replace(message.Chat.Username), message.Chat.ID)
	} else if message.Chat.Username != "" {
		chatInfo = fmt.Sprintf("[%s](https://t.me/%s)", mdV2Replacer.Replace(message.Chat.Title), message.Chat.Username)
	} else {
		chatInfo = fmt.Sprintf("%s", mdV2Replacer.Replace(message.Chat.Title))
	}

	userID := int64(0)
	userCount := int64(0)
	if message.From != nil {
		userID = message.From.ID
		userCount, _ = h.Repo.CountByUserID(ctx, userID)
	}

	msgText := fmt.Sprintf(statusInfo, fromCount, chatInfo, chatCount, userID, userID, userCount)

	if h.PlatformManager != nil {
		platforms := h.PlatformManager.List()
		if len(platforms) > 0 {
			platformsEscaped := mdV2Replacer.Replace(strings.Join(platforms, ", "))
			msgText += fmt.Sprintf("\n\n📱 Available Platforms: %s", platformsEscaped)
		}
	}

	params := &bot.SendMessageParams{
		ChatID:          message.Chat.ID,
		Text:            msgText,
		ParseMode:       models.ParseModeMarkdown,
		ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}
