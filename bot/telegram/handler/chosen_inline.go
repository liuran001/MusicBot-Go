package handler

import (
	"context"
	"fmt"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// ChosenInlineMusicHandler handles chosen inline results (requires inline feedback).
type ChosenInlineMusicHandler struct {
	Music       *MusicHandler
	RateLimiter *telegram.RateLimiter
}

func (h *ChosenInlineMusicHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if h == nil || h.Music == nil || b == nil || update == nil || update.ChosenInlineResult == nil {
		return
	}
	chosen := update.ChosenInlineResult
	if strings.TrimSpace(chosen.InlineMessageID) == "" {
		return
	}
	platformName, trackID, qualityValue, ok := parseInlinePendingResultID(chosen.ResultID)
	if !ok || strings.TrimSpace(platformName) == "" || strings.TrimSpace(trackID) == "" {
		return
	}

	setInlineText := func(text string) {
		params := &telego.EditMessageTextParams{InlineMessageID: chosen.InlineMessageID, Text: text}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
	}
	editInlineMedia := func(songInfo *botpkg.SongInfo) error {
		if songInfo == nil || strings.TrimSpace(songInfo.FileID) == "" {
			return fmt.Errorf("inline media requires file_id")
		}
		media := &telego.InputMediaAudio{
			Type:      telego.MediaTypeAudio,
			Media:     telego.InputFile{FileID: songInfo.FileID},
			Caption:   buildMusicCaption(h.Music.PlatformManager, songInfo, h.Music.BotName),
			ParseMode: telego.ModeHTML,
			Title:     songInfo.SongName,
			Performer: songInfo.SongArtists,
			Duration:  songInfo.Duration,
		}
		if strings.TrimSpace(songInfo.ThumbFileID) != "" {
			media.Thumbnail = &telego.InputFile{FileID: songInfo.ThumbFileID}
		}
		replyMarkup := buildForwardKeyboard(songInfo.TrackURL, songInfo.Platform, songInfo.TrackID)
		_, err := b.EditMessageMedia(ctx, &telego.EditMessageMediaParams{
			InlineMessageID: chosen.InlineMessageID,
			Media:           media,
			ReplyMarkup:     replyMarkup,
		})
		if err != nil && telegram.IsMessageNotModified(err) {
			return nil
		}
		return err
	}

	setInlineText(waitForDown)
	progress := func(text string) {
		setInlineText(text)
	}
	songInfo, err := h.Music.prepareInlineSong(ctx, b, chosen.From.ID, platformName, trackID, qualityValue, progress)
	if err != nil {
		if h.Music.Logger != nil {
			h.Music.Logger.Error("failed to prepare inline song", "platform", platformName, "trackID", trackID, "error", err)
		}
		setInlineText(buildMusicInfoText("", "", "", userVisibleDownloadError(err)))
		return
	}
	if err := editInlineMedia(songInfo); err != nil {
		if h.Music.Logger != nil {
			h.Music.Logger.Error("failed to edit inline media", "platform", platformName, "trackID", trackID, "error", err)
		}
		setInlineText(buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), userVisibleDownloadError(err)))
		return
	}
}
