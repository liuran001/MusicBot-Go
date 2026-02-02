package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
)

// RmCacheHandler handles /rmcache command.
type RmCacheHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
}

func (h *RmCacheHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil || h.Repo == nil {
		return
	}
	message := update.Message
	args := commandArguments(message.Text)
	if args == "" {
		params := &bot.SendMessageParams{
			ChatID:          message.Chat.ID,
			Text:            inputIDorKeyword,
			ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}

	parts := strings.Fields(args)

	if len(parts) >= 2 {
		platformName := parts[0]
		trackID := parts[1]

		if h.PlatformManager != nil {
			plat := h.PlatformManager.Get(platformName)
			if plat != nil {
				err := h.Repo.DeleteAllQualitiesByPlatformTrackID(ctx, platformName, trackID)
				if err != nil {
					params := &bot.SendMessageParams{
						ChatID:          message.Chat.ID,
						Text:            "清除缓存失败",
						ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
					}
					if h.RateLimiter != nil {
						_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
					} else {
						_, _ = b.SendMessage(ctx, params)
					}
					return
				}
				params := &bot.SendMessageParams{
					ChatID:          message.Chat.ID,
					Text:            fmt.Sprintf("已清除平台 %s 歌曲 %s 的缓存", platformName, trackID),
					ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
				}
				if h.RateLimiter != nil {
					_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
				} else {
					_, _ = b.SendMessage(ctx, params)
				}
				return
			}
		}
	}
	if h.PlatformManager != nil {
		if platformName, trackID, matched := h.PlatformManager.MatchText(args); matched {
			if err := h.Repo.DeleteAllQualitiesByPlatformTrackID(ctx, platformName, trackID); err == nil {
				params := &bot.SendMessageParams{
					ChatID:          message.Chat.ID,
					Text:            fmt.Sprintf("已清除平台 %s 歌曲 %s 的缓存", platformName, trackID),
					ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
				}
				if h.RateLimiter != nil {
					_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
				} else {
					_, _ = b.SendMessage(ctx, params)
				}
				return
			}
			params := &bot.SendMessageParams{
				ChatID:          message.Chat.ID,
				Text:            "清除缓存失败",
				ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
			}
			if h.RateLimiter != nil {
				_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.SendMessage(ctx, params)
			}
			return
		}
		if platformName, trackID, matched := h.PlatformManager.MatchURL(args); matched {
			if err := h.Repo.DeleteAllQualitiesByPlatformTrackID(ctx, platformName, trackID); err == nil {
				params := &bot.SendMessageParams{
					ChatID:          message.Chat.ID,
					Text:            fmt.Sprintf("已清除平台 %s 歌曲 %s 的缓存", platformName, trackID),
					ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
				}
				if h.RateLimiter != nil {
					_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
				} else {
					_, _ = b.SendMessage(ctx, params)
				}
				return
			}
			params := &bot.SendMessageParams{
				ChatID:          message.Chat.ID,
				Text:            "清除缓存失败",
				ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
			}
			if h.RateLimiter != nil {
				_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.SendMessage(ctx, params)
			}
			return
		}
	}
	params := &bot.SendMessageParams{
		ChatID:          message.Chat.ID,
		Text:            "请输入有效的歌曲ID或URL，或使用格式: /rmcache <platform> <trackID>",
		ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
	return
}
