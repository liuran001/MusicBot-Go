package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// guestInlineChat maps an inline_message_id to the group chat it was summoned in.
// Guest-mode sends edit an inline message into audio, but the callback that
// triggers the send (e.g. a search-result tap, which reuses the generic
// "music i" inline callback, or a retry button) carries no chat context. So when
// a guest inline message is created we remember its chat here, and the media
// flow recovers it to attach the group-favorite button. Regular inline mode never
// stores anything (it has no chat ID), so it correctly gets no group favorites.
type guestInlineChat struct {
	chatID  int64
	isGroup bool
}

var guestInlineChatStore = newTTLStore[guestInlineChat](2 * time.Hour)

func rememberInlineChat(inlineMessageID string, chatID int64, isGroup bool) {
	inlineMessageID = strings.TrimSpace(inlineMessageID)
	if inlineMessageID == "" || chatID == 0 {
		return
	}
	guestInlineChatStore.Store(inlineMessageID, guestInlineChat{chatID: chatID, isGroup: isGroup})
}

func lookupInlineChat(inlineMessageID string) (int64, bool) {
	inlineMessageID = strings.TrimSpace(inlineMessageID)
	if inlineMessageID == "" {
		return 0, false
	}
	if v, ok := guestInlineChatStore.Load(inlineMessageID); ok {
		return v.chatID, v.isGroup
	}
	return 0, false
}

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
// the forward-button setting). chatID/isGroup carry the originating chat for
// guest mode (where Message.Chat.ID is known); inline mode passes 0/false. The
// whole operation is serialized per inline_message_id via withInlineMessageLock
// so concurrent taps don't race.
func runInlineMediaFlow(ctx context.Context, b *telego.Bot, deps inlineMediaFlowDeps, inlineMessageID string, userID int64, userName, platformName, trackID, qualityOverride string, chatID int64, isGroup bool) {
	music := deps.Music
	if music == nil || b == nil || strings.TrimSpace(inlineMessageID) == "" {
		return
	}
	// Remember the chat for this inline message (so later callbacks on it — a
	// retry tap, a search-result selection — can recover it), or recover it when
	// the caller had no chat context (the generic inline "music i" callback).
	if chatID != 0 {
		rememberInlineChat(inlineMessageID, chatID, isGroup)
	} else if cid, grp := lookupInlineChat(inlineMessageID); cid != 0 {
		chatID, isGroup = cid, grp
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
		retryMarkup := buildInlineSendKeyboard(ctx, platformName, trackID, qualityOverride, userID)
		editInlineMedia := func(songInfo *botpkg.SongInfo) (bool, error) {
			if songInfo == nil || strings.TrimSpace(songInfo.FileID) == "" {
				return false, fmt.Errorf("inline media requires file_id")
			}
			media := &telego.InputMediaAudio{
				Type:      telego.MediaTypeAudio,
				Media:     telego.InputFile{FileID: songInfo.FileID},
				Caption:   buildMusicCaption(ctx, music.PlatformManager, songInfo, music.BotName),
				ParseMode: telego.ModeHTML,
				Title:     songInfo.SongName,
				Performer: songInfo.SongArtists,
				Duration:  songInfo.Duration,
			}
			if strings.TrimSpace(songInfo.ThumbFileID) != "" {
				media.Thumbnail = &telego.InputFile{FileID: songInfo.ThumbFileID}
			}
			var replyMarkup *telego.InlineKeyboardMarkup
			if resolveShowBottomButtons(ctx, music.Repo, userID, chatID, isGroup) {
				replyMarkup = buildSongBottomKeyboard(ctx, music.Repo, songButtonOptions{
					platformName:  songInfo.Platform,
					trackID:       songInfo.TrackID,
					trackURL:      songInfo.TrackURL,
					quality:       qualityOverride,
					requesterID:   userID,
					botName:       music.BotName,
					inlineContext: true,
					chatID:        chatID,
					isGroup:       isGroup,
				})
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
		setInlineText(tr(ctx, "wait_for_down"), nil)
		songInfo, err := music.prepareInlineSongWithTimeout(ctx, b, userID, userName, platformName, trackID, qualityOverride, progress)
		if err != nil {
			if music.Logger != nil {
				music.Logger.Error("failed to prepare inline song", "platform", platformName, "trackID", trackID, "error", err)
			}
			setInlineText(buildMusicInfoText(ctx, "", "", "", userVisibleDownloadError(ctx, err)), retryMarkup)
			return
		}
		modified, err := editInlineMedia(songInfo)
		if err != nil {
			if music.Logger != nil {
				music.Logger.Error("failed to edit inline media", "platform", platformName, "trackID", trackID, "error", err)
			}
			setInlineText(buildMusicInfoText(ctx, songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), userVisibleDownloadError(ctx, err)), retryMarkup)
			return
		}
		if modified && music.Repo != nil {
			if err := music.Repo.IncrementSendCount(ctx); err != nil && music.Logger != nil {
				music.Logger.Error("failed to update send count", "error", err)
			}
		}
	})
}
