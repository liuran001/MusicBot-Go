package handler

import (
	"context"
	"fmt"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/i18n"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// languageAuto is the sentinel persisted value meaning "no explicit override —
// follow the Telegram client language". It is the empty string so an unset
// settings row naturally means auto.
const languageAuto = ""

// resolveLanguage returns the persisted UI-language override for the active
// scope (group setting in groups, user setting in private chats). Empty means
// auto-detect.
func (h *SettingsHandler) resolveLanguage(chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) string {
	if chatType != "private" {
		if groupSettings != nil {
			return groupSettings.Language
		}
		return languageAuto
	}
	if settings != nil {
		return settings.Language
	}
	return languageAuto
}

// languageDisplayName renders a language code as its native display name for the
// settings UI. The empty code renders as the localized "Auto" label.
func languageDisplayName(ctx context.Context, lang string) string {
	if lang == languageAuto {
		return tr(ctx, "set_language_auto")
	}
	switch lang {
	case "en":
		return tr(ctx, "set_lang_name_en")
	case "zh":
		return tr(ctx, "set_lang_name_zh")
	case "ja":
		return tr(ctx, "set_lang_name_ja")
	default:
		return lang
	}
}

// buildLanguageMenuText is the header shown above the language submenu.
func (h *SettingsHandler) buildLanguageMenuText(ctx context.Context, chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) string {
	current := h.resolveLanguage(chatType, settings, groupSettings)
	return tr(ctx, "set_language_menu_title") + "\n\n" +
		tr(ctx, "set_language_menu_current", map[string]any{"Name": languageDisplayName(ctx, current)}) + "\n\n" +
		tr(ctx, "set_language_menu_hint")
}

// buildLanguageMenuKeyboard renders the language submenu: an "Auto" option plus
// one button per supported language, the current pick marked, then a back button.
func (h *SettingsHandler) buildLanguageMenuKeyboard(ctx context.Context, chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) *telego.InlineKeyboardMarkup {
	current := h.resolveLanguage(chatType, settings, groupSettings)
	mark := func(code, label string) string {
		if code == current {
			return "✅ " + label
		}
		return label
	}
	rows := [][]telego.InlineKeyboardButton{
		{{Text: mark(languageAuto, tr(ctx, "set_language_auto")), CallbackData: "settings langset auto"}},
	}
	for _, code := range i18n.SupportedLanguages {
		rows = append(rows, []telego.InlineKeyboardButton{{
			Text:         mark(code, languageDisplayName(ctx, code)),
			CallbackData: fmt.Sprintf("settings langset %s", code),
		}})
	}
	rows = append(rows, []telego.InlineKeyboardButton{{Text: "⬅️ " + tr(ctx, "set_btn_back"), CallbackData: "settings lyricback"}})
	return &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// handleLanguageMenuNavigation swaps the message into the language submenu. Only
// edits text+keyboard; persistence happens via the "langset" case. In groups,
// opening the submenu still requires admin (mirrors the rest of group settings).
func (h *SettingsCallbackHandler) handleLanguageMenuNavigation(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, msg *telego.Message) {
	userID := query.From.ID
	var settings *botpkg.UserSettings
	var groupSettings *botpkg.GroupSettings
	var err error
	if msg.Chat.Type != "private" {
		if !isRequesterOrAdmin(ctx, b, msg.Chat.ID, userID, 0) {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
				CallbackQueryID: query.ID,
				Text:            "❌ " + tr(ctx, "set_admin_only"),
				ShowAlert:       true,
			})
			return
		}
		groupSettings, err = h.Repo.GetGroupSettings(ctx, msg.Chat.ID)
	} else {
		settings, err = h.Repo.GetUserSettings(ctx, userID)
	}
	if err != nil {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "❌ " + tr(ctx, "set_err_load"),
			ShowAlert:       true,
		})
		return
	}
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID})

	chatType := string(msg.Chat.Type)
	text := h.SettingsHandler.buildLanguageMenuText(ctx, chatType, settings, groupSettings)
	keyboard := h.SettingsHandler.buildLanguageMenuKeyboard(ctx, chatType, settings, groupSettings)
	params := &telego.EditMessageTextParams{
		ChatID:      telego.ChatID{ID: msg.Chat.ID},
		MessageID:   msg.MessageID,
		Text:        text,
		ReplyMarkup: keyboard,
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
}

// applyLanguageSelection persists a language pick ("auto" or a supported code)
// and re-renders the language submenu in the NEWLY selected language so the
// change is immediately visible. Returns true when a change was saved.
func (h *SettingsCallbackHandler) applyLanguageSelection(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, msg *telego.Message, value string) {
	userID := query.From.ID
	lang := languageAuto
	if value != "auto" && i18n.IsSupported(value) {
		lang = i18n.NormalizeLang(value)
	}

	var settings *botpkg.UserSettings
	var groupSettings *botpkg.GroupSettings
	var err error
	isGroup := msg.Chat.Type != "private"
	if isGroup {
		if !isRequesterOrAdmin(ctx, b, msg.Chat.ID, userID, 0) {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
				CallbackQueryID: query.ID,
				Text:            "❌ " + tr(ctx, "set_admin_only"),
				ShowAlert:       true,
			})
			return
		}
		groupSettings, err = h.Repo.GetGroupSettings(ctx, msg.Chat.ID)
	} else {
		settings, err = h.Repo.GetUserSettings(ctx, userID)
	}
	if err != nil {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID, Text: "❌ " + tr(ctx, "set_err_load"), ShowAlert: true,
		})
		return
	}

	if isGroup {
		if groupSettings == nil || groupSettings.Language == lang {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
			return
		}
		groupSettings.Language = lang
		if err := h.Repo.UpdateGroupSettings(ctx, groupSettings); err != nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "❌ " + tr(ctx, "set_err_save"), ShowAlert: true})
			return
		}
	} else {
		if settings == nil || settings.Language == lang {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
			return
		}
		settings.Language = lang
		if err := h.Repo.UpdateUserSettings(ctx, settings); err != nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "❌ " + tr(ctx, "set_err_save"), ShowAlert: true})
			return
		}
	}

	// Re-render in the freshly chosen language: resolve a localizer for the
	// effective language (the override if set, else the client tag) and inject it
	// so the submenu and toast both reflect the new pick immediately.
	effective := lang
	if effective == languageAuto {
		effective = i18n.Resolve("", query.From.LanguageCode)
	}
	newCtx := i18n.WithLocalizer(ctx, i18n.For(effective))

	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
		Text:            tr(newCtx, "set_resp_language_set", map[string]any{"Name": languageDisplayName(newCtx, lang)}),
	})

	chatType := string(msg.Chat.Type)
	text := h.SettingsHandler.buildLanguageMenuText(newCtx, chatType, settings, groupSettings)
	keyboard := h.SettingsHandler.buildLanguageMenuKeyboard(newCtx, chatType, settings, groupSettings)
	params := &telego.EditMessageTextParams{
		ChatID:      telego.ChatID{ID: msg.Chat.ID},
		MessageID:   msg.MessageID,
		Text:        text,
		ReplyMarkup: keyboard,
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
}
