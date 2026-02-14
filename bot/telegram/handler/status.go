package handler

import (
	"context"
	"fmt"
	"sort"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// StatusHandler handles /status command.
type StatusHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
}

func (h *StatusHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil || h.Repo == nil {
		return
	}
	message := update.Message

	fromCount, _ := h.Repo.Count(ctx)
	chatCount, _ := h.Repo.CountByChatID(ctx, message.Chat.ID)
	chatInfo := mdV2Replacer.Replace(message.Chat.Title)
	if message.Chat.Username != "" && message.Chat.Title == "" {
		chatInfo = fmt.Sprintf("[%s](tg://user?id=%d)", mdV2Replacer.Replace(message.Chat.Username), message.Chat.ID)
	} else if message.Chat.Username != "" {
		chatInfo = fmt.Sprintf("[%s](https://t.me/%s)", mdV2Replacer.Replace(message.Chat.Title), message.Chat.Username)
	}

	userID := int64(0)
	userCount := int64(0)
	if message.From != nil {
		userID = message.From.ID
		userCount, _ = h.Repo.CountByUserID(ctx, userID)
	}

	sendCount, _ := h.Repo.GetSendCount(ctx)
	msgText := fmt.Sprintf(statusInfo, fromCount, chatInfo, chatCount, userID, userID, userCount, sendCount)

	if platformCounts, err := h.Repo.CountByPlatform(ctx); err == nil && len(platformCounts) > 0 {
		platformNames := make([]string, 0, len(platformCounts))
		for name := range platformCounts {
			platformNames = append(platformNames, name)
		}
		sort.Strings(platformNames)
		lines := make([]string, 0, len(platformNames))
		for _, name := range platformNames {
			display := mdV2Replacer.Replace(platformDisplayName(h.PlatformManager, name))
			lines = append(lines, fmt.Sprintf("%s: %d", display, platformCounts[name]))
		}
		msgText += "\n缓存平台统计:\n" + strings.Join(lines, "\n")
	}

	if h.PlatformManager != nil {
		platforms := h.PlatformManager.List()
		if len(platforms) > 0 {
			displayNames := make([]string, 0, len(platforms))
			for _, name := range platforms {
				displayNames = append(displayNames, platformDisplayName(h.PlatformManager, name))
			}
			platformsEscaped := mdV2Replacer.Replace(strings.Join(displayNames, ", "))
			msgText += fmt.Sprintf("\n\n📱 可用平台: %s", platformsEscaped)
		}
	}

	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		Text:            msgText,
		ParseMode:       telego.ModeMarkdownV2,
		ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}
