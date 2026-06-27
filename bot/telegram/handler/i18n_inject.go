package handler

import (
	"context"

	"github.com/liuran001/MusicBot-Go/bot/i18n"
	"github.com/mymmrac/telego"
)

// tr resolves a plain (non-MarkdownV2) localized string for the request language
// carried by ctx. It is the terse call-site form of i18n.From(ctx).T, used
// pervasively across handlers so migrated code stays readable.
func tr(ctx context.Context, id string, args ...map[string]any) string {
	return i18n.From(ctx).T(id, args...)
}

// trMd resolves a localized string and escapes it for Telegram MarkdownV2. Use
// for any text sent with ParseMode=MarkdownV2.
func trMd(ctx context.Context, id string, args ...map[string]any) string {
	return i18n.From(ctx).Tmd(id, args...)
}

// trn resolves a pluralized localized string for the request language.
func trn(ctx context.Context, id string, count int, args ...map[string]any) string {
	return i18n.From(ctx).Tn(id, count, args...)
}

// clientLanguageTag extracts the Telegram client UI language (an IETF tag like
// "zh-hans") from whichever From field the update carries. Returns "" when the
// update has no associated user (the resolver then falls back to default).
func clientLanguageTag(update *telego.Update) string {
	if update == nil {
		return ""
	}
	switch {
	case update.Message != nil && update.Message.From != nil:
		return update.Message.From.LanguageCode
	case update.CallbackQuery != nil:
		return update.CallbackQuery.From.LanguageCode
	case update.InlineQuery != nil:
		return update.InlineQuery.From.LanguageCode
	case update.ChosenInlineResult != nil:
		return update.ChosenInlineResult.From.LanguageCode
	case update.GuestMessage != nil && update.GuestMessage.From != nil:
		return update.GuestMessage.From.LanguageCode
	}
	return ""
}

// localize resolves the request language and injects the matching Localizer into
// ctx. This is the single chokepoint: every handler downstream reads the result
// via i18n.From(ctx), so handlers never resolve language themselves.
//
// override is a persisted per-user/group language preference (empty until the
// settings field is wired). When present and supported it wins over the client
// tag; this keeps the auto-detect + manual-override contract in one place.
func (r *Router) localize(ctx context.Context, update *telego.Update) context.Context {
	override := r.languageOverride(ctx, update)
	lang := i18n.Resolve(override, clientLanguageTag(update))
	return i18n.WithLocalizer(ctx, i18n.For(lang))
}

// languageOverride returns the persisted language preference for the update's
// scope (group setting in groups, user setting in private chats), or "" when no
// repository is wired or no preference is stored. Failures degrade silently to
// auto-detection.
func (r *Router) languageOverride(ctx context.Context, update *telego.Update) string {
	if r == nil || r.Repo == nil || update == nil {
		return ""
	}
	chatID, userID, isGroup, ok := updateScope(update)
	if !ok {
		return ""
	}
	if isGroup {
		if gs, err := r.Repo.GetGroupSettings(ctx, chatID); err == nil && gs != nil {
			return gs.Language
		}
		return ""
	}
	if userID != 0 {
		if us, err := r.Repo.GetUserSettings(ctx, userID); err == nil && us != nil {
			return us.Language
		}
	}
	return ""
}

// updateScope extracts the chat/user identity and group-ness from any update
// type, for resolving the persisted language override.
func updateScope(update *telego.Update) (chatID, userID int64, isGroup, ok bool) {
	switch {
	case update.Message != nil:
		chatID = update.Message.Chat.ID
		if update.Message.From != nil {
			userID = update.Message.From.ID
		}
		isGroup = update.Message.Chat.Type == "group" || update.Message.Chat.Type == "supergroup"
		return chatID, userID, isGroup, true
	case update.CallbackQuery != nil:
		userID = update.CallbackQuery.From.ID
		if update.CallbackQuery.Message != nil {
			if msg := update.CallbackQuery.Message.Message(); msg != nil {
				chatID = msg.Chat.ID
				isGroup = msg.Chat.Type == "group" || msg.Chat.Type == "supergroup"
			}
		}
		return chatID, userID, isGroup, true
	case update.InlineQuery != nil:
		userID = update.InlineQuery.From.ID
		return userID, userID, false, true
	case update.ChosenInlineResult != nil:
		userID = update.ChosenInlineResult.From.ID
		return userID, userID, false, true
	case update.GuestMessage != nil:
		chatID = update.GuestMessage.Chat.ID
		if update.GuestMessage.From != nil {
			userID = update.GuestMessage.From.ID
		}
		isGroup = update.GuestMessage.Chat.Type == "group" || update.GuestMessage.Chat.Type == "supergroup"
		return chatID, userID, isGroup, true
	}
	return 0, 0, false, false
}
