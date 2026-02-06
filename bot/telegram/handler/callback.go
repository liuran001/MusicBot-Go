package handler

import (
	"context"
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

func (h *CallbackMusicHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	args := strings.Split(query.Data, " ")
	if len(args) < 2 {
		return
	}

	// Parse callback data: "music <platform> <trackID> <quality> <requesterID>" (new format)
	// OR "music <musicID> <requesterID>" (old format for backward compatibility)
	var platformName string
	var trackID string
	var requesterID int64
	var qualityOverride string

	if len(args) >= 5 {
		// New format: music <platform> <trackID> <quality> <requesterID>
		platformName = args[1]
		trackID = args[2]
		qualityOverride = args[3]
		requesterID, _ = strconv.ParseInt(args[4], 10, 64)
	} else if len(args) >= 4 {
		// New format: music <platform> <trackID> <requesterID>
		platformName = args[1]
		trackID = args[2]
		requesterID, _ = strconv.ParseInt(args[3], 10, 64)
	} else if len(args) >= 3 {
		// Could be new format without requester: music <platform> <trackID>
		// OR old format with requester: music <musicID> <requesterID>
		// Try to parse second arg as int to determine format
		if _, err := strconv.Atoi(args[1]); err == nil && isNumeric(args[2]) {
			// Old format: music <musicID> <requesterID>
			platformName = "netease"
			trackID = args[1]
			requesterID, _ = strconv.ParseInt(args[2], 10, 64)
		} else {
			// New format: music <platform> <trackID>
			platformName = args[1]
			trackID = args[2]
		}
	} else {
		// Old format: music <musicID>
		platformName = "netease"
		trackID = args[1]
	}
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
