package handler

import (
	"context"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// CallbackMusicHandler handles callback queries for music buttons.
type CallbackMusicHandler struct {
	Music   *MusicHandler
	BotName string
}

func (h *CallbackMusicHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	args := strings.Split(query.Data, " ")
	if len(args) < 2 {
		return
	}

	// Parse callback data: "music <platform> <trackID> <requesterID>" (new format)
	// OR "music <musicID> <requesterID>" (old format for backward compatibility)
	var platformName string
	var trackID string
	var requesterID int64

	if len(args) >= 4 {
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

	msg := query.Message.Message
	if msg == nil {
		return
	}
	chatType := msg.Chat.Type

	if chatType == "private" {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
		if h.Music != nil {
			_ = h.Music.processMusic(ctx, b, msg, platformName, trackID)
		}
		return
	}

	if !isRequesterOrAdmin(ctx, b, msg.Chat.ID, query.From.ID, requesterID) {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            callbackDenied,
			ShowAlert:       true,
		})
		return
	}

	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
	_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID})
	if h.Music != nil {
		_ = h.Music.processMusic(ctx, b, msg, platformName, trackID)
	}
}

func isRequesterOrAdmin(ctx context.Context, b *bot.Bot, chatID int64, userID int64, requesterID int64) bool {
	if requesterID != 0 && requesterID == userID {
		return true
	}
	if b == nil {
		return false
	}
	member, err := b.GetChatMember(ctx, &bot.GetChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil || member == nil {
		return false
	}
	return member.Type == models.ChatMemberTypeOwner || member.Type == models.ChatMemberTypeAdministrator
}
