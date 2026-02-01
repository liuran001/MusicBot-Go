package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// RmCacheHandler handles /rmcache command.
type RmCacheHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
}

func (h *RmCacheHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil || h.Repo == nil {
		return
	}
	message := update.Message
	args := commandArguments(message.Text)
	if args == "" {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          message.Chat.ID,
			Text:            inputIDorKeyword,
			ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
		})
		return
	}

	parts := strings.Fields(args)

	if len(parts) >= 2 {
		platformName := parts[0]
		trackID := parts[1]

		if h.PlatformManager != nil {
			plat := h.PlatformManager.Get(platformName)
			if plat != nil {
				err := h.Repo.DeleteByPlatformTrackID(ctx, platformName, trackID)
				if err != nil {
					_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
						ChatID:          message.Chat.ID,
						Text:            "清除缓存失败",
						ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
					})
					return
				}
				_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:          message.Chat.ID,
					Text:            fmt.Sprintf("已清除平台 %s 歌曲 %s 的缓存", platformName, trackID),
					ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
				})
				return
			}
		}
	}

	musicID := parseMusicID(args)
	if musicID == 0 {
		musicID = parseProgramID(args)
		if musicID != 0 {
			musicID = getProgramRealID(musicID)
		}
	}
	if musicID == 0 {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          message.Chat.ID,
			Text:            "请输入有效的歌曲ID或URL，或使用格式: /rmcache <platform> <trackID>",
			ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
		})
		return
	}

	songInfo, err := h.Repo.FindByMusicID(ctx, musicID)
	if err == nil && songInfo != nil {
		_ = h.Repo.Delete(ctx, musicID)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          message.Chat.ID,
			Text:            fmt.Sprintf(rmcacheReport, songInfo.SongName),
			ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
		})
		return
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          message.Chat.ID,
		Text:            noCache,
		ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
	})
}
