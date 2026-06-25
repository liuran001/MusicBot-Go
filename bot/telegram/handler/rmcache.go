package handler

import (
	"context"
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
	AdminIDs        *AdminSet
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
			Text:            tr(ctx, "input_id_or_keyword"),
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
				Text:            tr(ctx, "adm_rmcache_failed"),
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
			Text:            tr(ctx, "adm_rmcache_all_done"),
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
		platformName := strings.TrimSpace(parts[0])
		target := strings.TrimSpace(strings.Join(parts[1:], " "))

		if h.PlatformManager != nil {
			if resolved, ok := h.PlatformManager.ResolveAlias(platformName); ok {
				platformName = resolved
			}
			plat := h.PlatformManager.Get(platformName)
			if plat != nil {
				if strings.EqualFold(strings.TrimSpace(target), "all") {
					err := h.Repo.DeleteAllByPlatform(ctx, platformName)
					if err != nil {
						params := &telego.SendMessageParams{
							ChatID:          telego.ChatID{ID: message.Chat.ID},
							Text:            tr(ctx, "adm_rmcache_failed"),
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
						Text:            tr(ctx, "adm_rmcache_platform_all_done", map[string]any{"Platform": platformName}),
						ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
					}
					if h.RateLimiter != nil {
						_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
					} else {
						_, _ = b.SendMessage(ctx, params)
					}
					return
				}

				resolvedTarget := resolveShortLinkText(ctx, h.PlatformManager, target)
				trackID := strings.TrimSpace(resolvedTarget)
				if matcher, ok := plat.(platform.URLMatcher); ok {
					if matchedTrackID, matched := matcher.MatchURL(resolvedTarget); matched {
						trackID = strings.TrimSpace(matchedTrackID)
					}
				}
				if platformFromURL, matchedTrackID, matched := h.PlatformManager.MatchURL(resolvedTarget); matched && platformFromURL == platformName {
					trackID = strings.TrimSpace(matchedTrackID)
				}
				if trackID == "" {
					params := &telego.SendMessageParams{
						ChatID:          telego.ChatID{ID: message.Chat.ID},
						Text:            tr(ctx, "adm_rmcache_invalid"),
						ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
					}
					if h.RateLimiter != nil {
						_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
					} else {
						_, _ = b.SendMessage(ctx, params)
					}
					return
				}

				err := h.Repo.DeleteAllQualitiesByPlatformTrackID(ctx, platformName, trackID)
				if err != nil {
					params := &telego.SendMessageParams{
						ChatID:          telego.ChatID{ID: message.Chat.ID},
						Text:            tr(ctx, "adm_rmcache_failed"),
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
					Text:            tr(ctx, "adm_rmcache_track_done", map[string]any{"Platform": platformName, "TrackID": trackID}),
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
					Text:            tr(ctx, "adm_rmcache_track_done", map[string]any{"Platform": platformName, "TrackID": trackID}),
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
				Text:            tr(ctx, "adm_rmcache_failed"),
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
					Text:            tr(ctx, "adm_rmcache_track_done", map[string]any{"Platform": platformName, "TrackID": trackID}),
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
				Text:            tr(ctx, "adm_rmcache_failed"),
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
		Text:            tr(ctx, "adm_rmcache_invalid"),
		ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}
