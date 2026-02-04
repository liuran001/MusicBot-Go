package handler

import (
	"context"
	"fmt"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// RmCacheHandler handles /rmcache command.
type RmCacheHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
	AdminIDs        map[int64]struct{}
}

func (h *RmCacheHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil || h.Repo == nil {
		return
	}
	message := update.Message
	if message.From == nil || !isBotAdmin(h.AdminIDs, message.From.ID) {
		return
	}
	args := commandArguments(message.Text)
	if args == "" {
		params := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: message.Chat.ID},
			Text:            inputIDorKeyword,
			ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}
	if strings.EqualFold(strings.TrimSpace(args), "all") {
		if err := h.Repo.DeleteAll(ctx); err != nil {
			params := &telego.SendMessageParams{
				ChatID:          telego.ChatID{ID: message.Chat.ID},
				Text:            "清除缓存失败",
				ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
			}
			if h.RateLimiter != nil {
				_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.SendMessage(ctx, params)
			}
			return
		}
		params := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: message.Chat.ID},
			Text:            "已清空所有缓存",
			ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
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
					params := &telego.SendMessageParams{
						ChatID:          telego.ChatID{ID: message.Chat.ID},
						Text:            "清除缓存失败",
						ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
					}
					if h.RateLimiter != nil {
						_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
					} else {
						_, _ = b.SendMessage(ctx, params)
					}
					return
				}
				params := &telego.SendMessageParams{
					ChatID:          telego.ChatID{ID: message.Chat.ID},
					Text:            fmt.Sprintf("已清除平台 %s 歌曲 %s 的缓存", platformName, trackID),
					ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
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
		resolvedArgs := resolveShortLinkText(ctx, h.PlatformManager, args)
		if platformName, trackID, matched := h.PlatformManager.MatchText(resolvedArgs); matched {
			if err := h.Repo.DeleteAllQualitiesByPlatformTrackID(ctx, platformName, trackID); err == nil {
				params := &telego.SendMessageParams{
					ChatID:          telego.ChatID{ID: message.Chat.ID},
					Text:            fmt.Sprintf("已清除平台 %s 歌曲 %s 的缓存", platformName, trackID),
					ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
				}
				if h.RateLimiter != nil {
					_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
				} else {
					_, _ = b.SendMessage(ctx, params)
				}
				return
			}
			params := &telego.SendMessageParams{
				ChatID:          telego.ChatID{ID: message.Chat.ID},
				Text:            "清除缓存失败",
				ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
			}
			if h.RateLimiter != nil {
				_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.SendMessage(ctx, params)
			}
			return
		}
		if platformName, trackID, matched := h.PlatformManager.MatchURL(resolvedArgs); matched {
			if err := h.Repo.DeleteAllQualitiesByPlatformTrackID(ctx, platformName, trackID); err == nil {
				params := &telego.SendMessageParams{
					ChatID:          telego.ChatID{ID: message.Chat.ID},
					Text:            fmt.Sprintf("已清除平台 %s 歌曲 %s 的缓存", platformName, trackID),
					ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
				}
				if h.RateLimiter != nil {
					_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
				} else {
					_, _ = b.SendMessage(ctx, params)
				}
				return
			}
			params := &telego.SendMessageParams{
				ChatID:          telego.ChatID{ID: message.Chat.ID},
				Text:            "清除缓存失败",
				ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
			}
			if h.RateLimiter != nil {
				_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.SendMessage(ctx, params)
			}
			return
		}
	}
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		Text:            "请输入有效的歌曲ID或URL，或使用格式: /rmcache <platform> <trackID>",
		ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}
