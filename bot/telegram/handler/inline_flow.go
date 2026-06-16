package handler

import (
	"context"
	"fmt"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// inlineMediaFlowDeps carries the dependencies needed to turn an inline message
// (identified by inline_message_id) into a music audio result in place. It is
// the shared core behind three entry points that all do the same thing:
//
//   - inline "点击发送" button callback (CallbackMusicHandler.runInlineDownloadFlow)
//   - chosen inline result (ChosenInlineMusicHandler.handleChosenTrack)
//   - guest mode track selection (GuestModeHandler)
//
// All three receive an inline_message_id and must: show progress text, hit the
// cache or download+upload, then EditMessageMedia the message into an audio with
// cover thumbnail, HTML caption and an optional forward button.
type inlineMediaFlowDeps struct {
	Music       *MusicHandler
	RateLimiter *telegram.RateLimiter
}

// runInlineMediaFlow downloads (or pulls from cache) the requested track and
// edits the inline message identified by inlineMessageID into an audio result.
// It is safe to call from any handler that holds an inline_message_id.
//
// userID/userName identify the requester (used for per-user cache/quality and
// the forward-button setting). The whole operation is serialized per
// inline_message_id via withInlineMessageLock so concurrent taps don't race.
func runInlineMediaFlow(ctx context.Context, b *telego.Bot, deps inlineMediaFlowDeps, inlineMessageID string, userID int64, userName, platformName, trackID, qualityOverride string) {
	music := deps.Music
	if music == nil || b == nil || strings.TrimSpace(inlineMessageID) == "" {
		return
	}
	rl := deps.RateLimiter
	withInlineMessageLock(inlineMessageID, func() {
		lastInlineText := ""
		setInlineText := func(text string, markup *telego.InlineKeyboardMarkup) {
			text = strings.TrimSpace(text)
			if text == "" || text == lastInlineText {
				return
			}
			params := &telego.EditMessageTextParams{InlineMessageID: inlineMessageID, Text: text, ReplyMarkup: markup}
			if markup != nil {
				if rl != nil {
					_, _ = telegram.EditMessageTextWithRetry(ctx, rl, b, params)
				} else {
					_, _ = b.EditMessageText(ctx, params)
				}
				lastInlineText = text
				return
			}
			if rl != nil {
				_, _ = telegram.EditMessageTextBestEffort(ctx, rl, b, params)
			} else {
				_, _ = b.EditMessageText(ctx, params)
			}
			lastInlineText = text
		}
		clearInlineReplyMarkup := func() {
			params := &telego.EditMessageReplyMarkupParams{InlineMessageID: inlineMessageID}
			if rl != nil {
				_, _ = telegram.EditMessageReplyMarkupWithRetry(ctx, rl, b, params)
			} else {
				_, _ = b.EditMessageReplyMarkup(ctx, params)
			}
		}
		retryMarkup := buildInlineSendKeyboard(platformName, trackID, qualityOverride, userID)
		editInlineMedia := func(songInfo *botpkg.SongInfo) (bool, error) {
			if songInfo == nil || strings.TrimSpace(songInfo.FileID) == "" {
				return false, fmt.Errorf("inline media requires file_id")
			}
			media := &telego.InputMediaAudio{
				Type:      telego.MediaTypeAudio,
				Media:     telego.InputFile{FileID: songInfo.FileID},
				Caption:   buildMusicCaption(music.PlatformManager, songInfo, music.BotName),
				ParseMode: telego.ModeHTML,
				Title:     songInfo.SongName,
				Performer: songInfo.SongArtists,
				Duration:  songInfo.Duration,
			}
			if strings.TrimSpace(songInfo.ThumbFileID) != "" {
				media.Thumbnail = &telego.InputFile{FileID: songInfo.ThumbFileID}
			}
			var replyMarkup *telego.InlineKeyboardMarkup
			if resolveForwardButtonEnabledForUser(ctx, music.Repo, userID) {
				replyMarkup = buildForwardKeyboard(songInfo.TrackURL, songInfo.Platform, songInfo.TrackID)
			}
			params := &telego.EditMessageMediaParams{
				InlineMessageID: inlineMessageID,
				Media:           media,
				ReplyMarkup:     replyMarkup,
			}
			var err error
			if rl != nil {
				_, err = telegram.EditMessageMediaWithRetry(ctx, rl, b, params)
			} else {
				_, err = b.EditMessageMedia(ctx, params)
			}
			if err != nil && telegram.IsMessageNotModified(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return true, nil
		}

		progress := func(text string) {
			setInlineText(text, nil)
		}
		if cachedSong, _, cacheErr := music.findInlineCachedSong(ctx, userID, platformName, trackID, qualityOverride); cacheErr == nil && cachedSong != nil {
			modified, err := editInlineMedia(cachedSong)
			if err == nil {
				if modified && music.Repo != nil {
					if err := music.Repo.IncrementSendCount(ctx); err != nil && music.Logger != nil {
						music.Logger.Error("failed to update send count", "error", err)
					}
				}
				return
			}
			if music.Logger != nil {
				music.Logger.Warn("failed to edit cached inline media, fallback to prepare", "platform", platformName, "trackID", trackID, "error", err)
			}
		}
		clearInlineReplyMarkup()
		setInlineText(waitForDown, nil)
		songInfo, err := music.prepareInlineSongWithTimeout(ctx, b, userID, userName, platformName, trackID, qualityOverride, progress)
		if err != nil {
			if music.Logger != nil {
				music.Logger.Error("failed to prepare inline song", "platform", platformName, "trackID", trackID, "error", err)
			}
			setInlineText(buildMusicInfoText("", "", "", userVisibleDownloadError(err)), retryMarkup)
			return
		}
		modified, err := editInlineMedia(songInfo)
		if err != nil {
			if music.Logger != nil {
				music.Logger.Error("failed to edit inline media", "platform", platformName, "trackID", trackID, "error", err)
			}
			setInlineText(buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), userVisibleDownloadError(err)), retryMarkup)
			return
		}
		if modified && music.Repo != nil {
			if err := music.Repo.IncrementSendCount(ctx); err != nil && music.Logger != nil {
				music.Logger.Error("failed to update send count", "error", err)
			}
		}
	})
}
