package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// CallbackMusicHandler handles callback queries for music buttons.
type CallbackMusicHandler struct {
	Music       *MusicHandler
	BotName     string
	RateLimiter *telegram.RateLimiter
}

type parsedMusicCallback struct {
	platformName    string
	trackID         string
	qualityOverride string
	requesterID     int64
	ok              bool
}

func (h *CallbackMusicHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	args := strings.Split(query.Data, " ")
	if len(args) < 2 {
		return
	}
	if len(args) >= 3 && args[1] == "i" {
		h.handleInlineCallback(ctx, b, query, args)
		return
	}

	parsed := parseMusicCallbackDataV2(args)
	if !parsed.ok {
		return
	}

	platformName := parsed.platformName
	trackID := parsed.trackID
	requesterID := parsed.requesterID
	qualityOverride := parsed.qualityOverride
	if qualityOverride != "" {
		if _, err := platform.ParseQuality(qualityOverride); err != nil {
			qualityOverride = ""
		}
	}

	if query.Message == nil {
		return
	}
	msg := query.Message.Message()
	if msg == nil {
		return
	}
	chatType := msg.Chat.Type

	msgToUse := msg
	if msg.ReplyToMessage != nil {
		msgToUse = msg.ReplyToMessage
	}

	if chatType == "private" {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
		if h.Music != nil {
			h.Music.dispatch(withForceNonSilent(ctx), b, msgToUse, platformName, trackID, qualityOverride)
		}
		if h.shouldAutoDeleteListMessage(ctx, msg, query.From.ID, nil, nil) {
			deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
			if h.RateLimiter != nil {
				_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
			} else {
				_ = b.DeleteMessage(ctx, deleteParams)
			}
		}
		return
	}

	if !isRequesterOrAdmin(ctx, b, msg.Chat.ID, query.From.ID, requesterID) {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            callbackDenied,
			ShowAlert:       true,
		})
		return
	}

	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
	autoDelete := h.shouldAutoDeleteListMessage(ctx, msg, query.From.ID, nil, nil)
	if h.Music != nil {
		h.Music.dispatch(withForceNonSilent(ctx), b, msgToUse, platformName, trackID, qualityOverride)
	}
	if autoDelete {
		deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
		if h.RateLimiter != nil {
			_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
		} else {
			_ = b.DeleteMessage(ctx, deleteParams)
		}
	}
}

func (h *CallbackMusicHandler) handleInlineCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	if query == nil || h == nil || h.Music == nil || b == nil {
		return
	}
	if query.InlineMessageID == "" {
		return
	}
	if len(args) >= 4 && strings.TrimSpace(args[2]) == "random" {
		requesterID, _ := strconv.ParseInt(strings.TrimSpace(args[len(args)-1]), 10, 64)
		if requesterID != 0 && requesterID != query.From.ID {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackDenied, ShowAlert: true})
			return
		}
		platformName, trackID, qualityValue, ok := h.resolveInlineRandomTrack(ctx)
		if !ok {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "暂无可随机歌曲", ShowAlert: true})
			return
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
		go h.runInlineDownloadFlow(detachContext(ctx), b, query.InlineMessageID, query.From.ID, query.From.Username, platformName, trackID, qualityValue)
		return
	}
	if len(args) < 5 {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
		return
	}
	platformName := strings.TrimSpace(args[2])
	trackID := strings.TrimSpace(args[3])
	requesterID, _ := strconv.ParseInt(args[len(args)-1], 10, 64)
	qualityOverride := ""
	if len(args) >= 6 {
		qualityOverride = strings.TrimSpace(args[4])
	}
	if platformName == "" || trackID == "" {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
		return
	}
	if requesterID != 0 && requesterID != query.From.ID {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackDenied, ShowAlert: true})
		return
	}
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})

	go h.runInlineDownloadFlow(detachContext(ctx), b, query.InlineMessageID, query.From.ID, query.From.Username, platformName, trackID, qualityOverride)
}

func (h *CallbackMusicHandler) resolveInlineRandomTrack(ctx context.Context) (platformName, trackID, qualityValue string, ok bool) {
	if h == nil || h.Music == nil || h.Music.Repo == nil {
		return "", "", "", false
	}
	info, err := h.Music.Repo.FindRandomCachedSong(ctx)
	if err != nil || info == nil {
		return "", "", "", false
	}
	platformName = strings.TrimSpace(info.Platform)
	if platformName == "" {
		platformName = "netease"
	}
	trackID = strings.TrimSpace(info.TrackID)
	if trackID == "" && info.MusicID > 0 {
		trackID = strconv.Itoa(info.MusicID)
	}
	if trackID == "" {
		return "", "", "", false
	}
	qualityValue = strings.TrimSpace(info.Quality)
	if qualityValue == "" {
		qualityValue = "hires"
	}
	return platformName, trackID, qualityValue, true
}

func (h *CallbackMusicHandler) runInlineDownloadFlow(ctx context.Context, b *telego.Bot, inlineMessageID string, userID int64, userName, platformName, trackID, qualityOverride string) {
	if h == nil || h.Music == nil || b == nil || inlineMessageID == "" {
		return
	}
	withInlineMessageLock(inlineMessageID, func() {
		lastInlineText := ""
		setInlineText := func(text string, markup *telego.InlineKeyboardMarkup) {
			text = strings.TrimSpace(text)
			if text == "" || text == lastInlineText {
				return
			}
			if markup != nil {
				params := &telego.EditMessageTextParams{InlineMessageID: inlineMessageID, Text: text, ReplyMarkup: markup}
				if h.RateLimiter != nil {
					_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
				} else {
					_, _ = b.EditMessageText(ctx, params)
				}
				lastInlineText = text
				return
			}
			params := &telego.EditMessageTextParams{InlineMessageID: inlineMessageID, Text: text, ReplyMarkup: markup}
			if h.RateLimiter != nil {
				_, _ = telegram.EditMessageTextBestEffort(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.EditMessageText(ctx, params)
			}
			lastInlineText = text
		}
		clearInlineReplyMarkup := func() {
			params := &telego.EditMessageReplyMarkupParams{InlineMessageID: inlineMessageID}
			if h.RateLimiter != nil {
				_, _ = telegram.EditMessageReplyMarkupWithRetry(ctx, h.RateLimiter, b, params)
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
			params := &telego.EditMessageMediaParams{
				InlineMessageID: inlineMessageID,
				Media:           media,
				ReplyMarkup:     replyMarkup,
			}
			var err error
			if h.RateLimiter != nil {
				_, err = telegram.EditMessageMediaWithRetry(ctx, h.RateLimiter, b, params)
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
		if cachedSong, _, cacheErr := h.Music.findInlineCachedSong(ctx, userID, platformName, trackID, qualityOverride); cacheErr == nil && cachedSong != nil {
			modified, err := editInlineMedia(cachedSong)
			if err == nil {
				if modified && h.Music.Repo != nil {
					if err := h.Music.Repo.IncrementSendCount(ctx); err != nil && h.Music.Logger != nil {
						h.Music.Logger.Error("failed to update send count", "error", err)
					}
				}
				return
			}
			if h.Music.Logger != nil {
				h.Music.Logger.Warn("failed to edit cached inline media, fallback to prepare", "platform", platformName, "trackID", trackID, "error", err)
			}
		}
		clearInlineReplyMarkup()
		setInlineText(waitForDown, nil)
		songInfo, err := h.Music.prepareInlineSong(ctx, b, userID, userName, platformName, trackID, qualityOverride, progress)
		if err != nil {
			if h.Music.Logger != nil {
				h.Music.Logger.Error("failed to prepare inline song", "platform", platformName, "trackID", trackID, "error", err)
			}
			setInlineText(buildMusicInfoText("", "", "", userVisibleDownloadError(err)), retryMarkup)
			return
		}
		modified, err := editInlineMedia(songInfo)
		if err != nil {
			if h.Music.Logger != nil {
				h.Music.Logger.Error("failed to edit inline media", "platform", platformName, "trackID", trackID, "error", err)
			}
			setInlineText(buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), userVisibleDownloadError(err)), retryMarkup)
			return
		}
		if modified && h.Music.Repo != nil {
			if err := h.Music.Repo.IncrementSendCount(ctx); err != nil && h.Music.Logger != nil {
				h.Music.Logger.Error("failed to update send count", "error", err)
			}
		}
	})
}

func (h *CallbackMusicHandler) shouldAutoDeleteListMessage(ctx context.Context, msg *telego.Message, userID int64, userSettings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) bool {
	if msg == nil {
		return false
	}
	if msg.Chat.Type == "private" {
		if userSettings != nil {
			return userSettings.AutoDeleteList
		}
		if h != nil && h.Music != nil && h.Music.Repo != nil && userID != 0 {
			if settings, err := h.Music.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
				return settings.AutoDeleteList
			}
		}
		return false
	}
	if groupSettings != nil {
		return groupSettings.AutoDeleteList
	}
	if h != nil && h.Music != nil && h.Music.Repo != nil {
		if settings, err := h.Music.Repo.GetGroupSettings(ctx, msg.Chat.ID); err == nil && settings != nil {
			return settings.AutoDeleteList
		}
	}
	return true
}

func isRequesterOrAdmin(ctx context.Context, b *telego.Bot, chatID int64, userID int64, requesterID int64) bool {
	if requesterID != 0 && requesterID == userID {
		return true
	}
	if b == nil {
		return false
	}
	member, err := b.GetChatMember(ctx, &telego.GetChatMemberParams{ChatID: telego.ChatID{ID: chatID}, UserID: userID})
	if err == nil && member != nil {
		status := member.MemberStatus()
		if status == telego.MemberStatusCreator || status == telego.MemberStatusAdministrator {
			return true
		}
	}
	admins, err := b.GetChatAdministrators(ctx, &telego.GetChatAdministratorsParams{ChatID: telego.ChatID{ID: chatID}})
	if err != nil {
		return false
	}
	for _, admin := range admins {
		if admin.MemberUser().ID != userID {
			continue
		}
		status := admin.MemberStatus()
		return status == telego.MemberStatusCreator || status == telego.MemberStatusAdministrator
	}
	return false
}

func parseMusicCallbackDataV2(args []string) parsedMusicCallback {
	if len(args) < 2 {
		return parsedMusicCallback{}
	}
	parsed := parsedMusicCallback{ok: true}
	switch len(args) {
	case 2:
		parsed.platformName = "netease"
		parsed.trackID = args[1]
	case 3:
		if isNumeric(args[1]) && isNumeric(args[2]) {
			parsed.platformName = "netease"
			parsed.trackID = args[1]
			parsed.requesterID, _ = strconv.ParseInt(args[2], 10, 64)
		} else {
			parsed.platformName = args[1]
			parsed.trackID = args[2]
		}
	case 4:
		parsed.platformName = args[1]
		parsed.trackID = args[2]
		parsed.requesterID, _ = strconv.ParseInt(args[3], 10, 64)
	default:
		parsed.platformName = args[1]
		parsed.trackID = args[2]
		parsed.qualityOverride = args[3]
		parsed.requesterID, _ = strconv.ParseInt(args[4], 10, 64)
	}
	return parsed
}
