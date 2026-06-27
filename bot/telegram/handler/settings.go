package handler

import (
	"context"
	"fmt"
	"sort"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	lyricpkg "github.com/liuran001/MusicBot-Go/bot/lyric"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

type SettingsHandler struct {
	Repo                     botpkg.SongRepository
	PlatformManager          platform.Manager
	RateLimiter              *telegram.RateLimiter
	DefaultPlatform          string
	DefaultQuality           string
	DefaultLyricFormat       string
	PluginSettingDefinitions []botpkg.PluginSettingDefinition
}

func (h *SettingsHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}

	message := update.Message
	userID := message.From.ID
	var settings *botpkg.UserSettings
	var groupSettings *botpkg.GroupSettings
	var err error
	if message.Chat.Type != "private" {
		if !isRequesterOrAdmin(ctx, b, message.Chat.ID, message.From.ID, 0) {
			params := &telego.SendMessageParams{
				ChatID: telego.ChatID{ID: message.Chat.ID},
				Text:   "❌ " + tr(ctx, "set_admin_only"),
			}
			if h.RateLimiter != nil {
				_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.SendMessage(ctx, params)
			}
			return
		}
		groupSettings, err = h.Repo.GetGroupSettings(ctx, message.Chat.ID)
	} else {
		settings, err = h.Repo.GetUserSettings(ctx, userID)
	}
	if err != nil {
		params := &telego.SendMessageParams{
			ChatID: telego.ChatID{ID: message.Chat.ID},
			Text:   "❌ " + tr(ctx, "set_err_load_retry"),
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}

	platforms := h.PlatformManager.List()

	chatType := string(message.Chat.Type)
	text := h.buildSettingsText(ctx, chatType, settings, groupSettings, platforms)
	keyboard := h.buildSettingsKeyboard(ctx, chatType, settings, groupSettings, platforms)

	params := &telego.SendMessageParams{
		ChatID:      telego.ChatID{ID: message.Chat.ID},
		Text:        text,
		ReplyMarkup: keyboard,
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}

func (h *SettingsHandler) buildSettingsText(ctx context.Context, chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings, platforms []string) string {
	var sb strings.Builder

	sb.WriteString("⚙️ " + tr(ctx, "set_title") + "\n\n")

	platformName := h.DefaultPlatform
	qualityValue := h.DefaultQuality
	if platformName == "" {
		platformName = "netease"
	}
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if chatType != "private" {
		if groupSettings != nil {
			platformName = groupSettings.DefaultPlatform
			qualityValue = groupSettings.DefaultQuality
		}
	} else if settings != nil {
		platformName = settings.DefaultPlatform
		qualityValue = settings.DefaultQuality
	}
	platformEmoji := h.getPlatformEmoji(platformName)
	sb.WriteString(fmt.Sprintf("🎵 %s：%s %s\n", tr(ctx, "set_platform_label"), platformEmoji, h.getPlatformDisplayName(platformName)))

	qualityEmoji := h.getQualityEmoji(qualityValue)
	sb.WriteString(fmt.Sprintf("🎧 %s：%s %s\n", tr(ctx, "set_quality_label"), qualityEmoji, h.getQualityDisplayName(ctx, qualityValue)))

	lyricFormat := h.resolveDefaultLyricFormat(chatType, settings, groupSettings)
	lyricSummary := lyricFormatDisplayName(ctx, lyricFormat)
	if lyricFormatSupportsSideTracks(lyricFormat) {
		includeTranslation, includeRoma := h.resolveDefaultLyricFlags(chatType, settings, groupSettings, lyricFormat)
		var extras []string
		if includeTranslation {
			extras = append(extras, tr(ctx, "set_label_translation"))
		}
		if includeRoma {
			extras = append(extras, tr(ctx, "set_label_roma"))
		}
		if len(extras) > 0 {
			lyricSummary += " · " + tr(ctx, "set_lyric_include_prefix") + strings.Join(extras, "/")
		}
	}
	sb.WriteString(fmt.Sprintf("🎤 %s：%s\n", tr(ctx, "set_lyric_label"), lyricSummary))

	autoDeleteEnabled := h.resolveAutoDeleteList(chatType, settings, groupSettings)
	autoDeleteText := tr(ctx, "set_state_off_full")
	if autoDeleteEnabled {
		autoDeleteText = tr(ctx, "set_state_on_full")
	}
	autoLinkDetectEnabled := h.resolveAutoLinkDetect(chatType, settings, groupSettings)
	autoLinkDetectText := tr(ctx, "set_state_off_full")
	if autoLinkDetectEnabled {
		autoLinkDetectText = tr(ctx, "set_state_on_full")
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("🧹 %s：%s\n", tr(ctx, "set_autodelete_summary_label"), autoDeleteText))
	sb.WriteString(fmt.Sprintf("🔗 %s：%s\n", tr(ctx, "set_autolink_summary_label"), autoLinkDetectText))

	for _, def := range h.sortedPluginSettingDefinitions() {
		if !h.shouldShowPluginSetting(def, autoLinkDetectEnabled, chatType != "private") {
			continue
		}
		value := h.resolvePluginSettingValue(ctx, chatType, settings, groupSettings, def)
		resolve := func(key string) string { return tr(ctx, key) }
		sb.WriteString(fmt.Sprintf("🔌 %s：%s\n", def.TitleLocalized(resolve), def.LabelOfLocalized(resolve, value)))
	}

	if len(platforms) > 1 {
		sb.WriteString("\n💡 " + tr(ctx, "set_available_platforms") + "\n")
		var platformNames []string
		for _, p := range platforms {
			platformNames = append(platformNames, h.getPlatformDisplayName(p))
		}
		sb.WriteString(strings.Join(platformNames, " / "))
		sb.WriteString("\n")
	}

	sb.WriteString("\n" + tr(ctx, "set_tap_to_modify"))

	return sb.String()
}

func (h *SettingsHandler) buildSettingsKeyboard(ctx context.Context, chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings, platforms []string) *telego.InlineKeyboardMarkup {
	var rows [][]telego.InlineKeyboardButton
	platformValue := h.DefaultPlatform
	qualityValue := h.DefaultQuality
	if platformValue == "" {
		platformValue = "netease"
	}
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if chatType != "private" {
		if groupSettings != nil {
			platformValue = groupSettings.DefaultPlatform
			qualityValue = groupSettings.DefaultQuality
		}
	} else if settings != nil {
		platformValue = settings.DefaultPlatform
		qualityValue = settings.DefaultQuality
	}

	if len(platforms) > 1 {
		var platformButtons []telego.InlineKeyboardButton
		for _, p := range platforms {
			displayName := h.getPlatformDisplayName(p)
			callbackData := fmt.Sprintf("settings platform %s", p)

			text := displayName
			if p == platformValue {
				text = "✅ " + displayName
			}

			platformButtons = append(platformButtons, telego.InlineKeyboardButton{
				Text:         text,
				CallbackData: callbackData,
			})
		}

		// Show at most 3 platform buttons per row, wrapping to new rows beyond
		// that (avoids an unreadably long single row when many platforms exist).
		const platformsPerRow = 3
		for i := 0; i < len(platformButtons); i += platformsPerRow {
			end := i + platformsPerRow
			if end > len(platformButtons) {
				end = len(platformButtons)
			}
			rows = append(rows, platformButtons[i:end])
		}
	}

	qualityButtons := []telego.InlineKeyboardButton{
		{
			Text:         h.formatQualityButton(ctx, "standard", qualityValue == "standard"),
			CallbackData: "settings quality standard",
		},
		{
			Text:         h.formatQualityButton(ctx, "high", qualityValue == "high"),
			CallbackData: "settings quality high",
		},
		{
			Text:         h.formatQualityButton(ctx, "lossless", qualityValue == "lossless"),
			CallbackData: "settings quality lossless",
		},
		{
			Text:         h.formatQualityButton(ctx, "hires", qualityValue == "hires"),
			CallbackData: "settings quality hires",
		},
	}
	rows = append(rows, qualityButtons)

	autoDeleteEnabled := h.resolveAutoDeleteList(chatType, settings, groupSettings)
	autoLinkDetectEnabled := h.resolveAutoLinkDetect(chatType, settings, groupSettings)
	rows = append(rows, []telego.InlineKeyboardButton{
		{
			Text:         h.formatToggleButton(ctx, "🧹 "+tr(ctx, "set_btn_autodelete"), autoDeleteEnabled),
			CallbackData: fmt.Sprintf("settings autodelete %s", h.toggleValue(autoDeleteEnabled)),
		},
		{
			Text:         h.formatToggleButton(ctx, "🔗 "+tr(ctx, "set_btn_autolink"), autoLinkDetectEnabled),
			CallbackData: fmt.Sprintf("settings autolink %s", h.toggleValue(autoLinkDetectEnabled)),
		},
	})

	lyricFormat := h.resolveDefaultLyricFormat(chatType, settings, groupSettings)
	rows = append(rows, []telego.InlineKeyboardButton{{
		Text:         fmt.Sprintf("🎤 %s：%s", tr(ctx, "set_lyric_label"), lyricFormatDisplayName(ctx, lyricFormat)),
		CallbackData: "settings lyricmenu",
	}})
	for _, def := range h.sortedPluginSettingDefinitions() {
		if !h.shouldShowPluginSetting(def, autoLinkDetectEnabled, chatType != "private") {
			continue
		}
		if len(def.Options) == 0 {
			continue
		}
		current := h.resolvePluginSettingValue(ctx, chatType, settings, groupSettings, def)
		resolve := func(key string) string { return tr(ctx, key) }
		rows = append(rows, []telego.InlineKeyboardButton{{
			Text:         fmt.Sprintf("🔌 %s：%s", def.TitleLocalized(resolve), def.LabelOfLocalized(resolve, current)),
			CallbackData: fmt.Sprintf("settings pcycle %s %s", def.Plugin, def.Key),
		}})
	}
	rows = append(rows, []telego.InlineKeyboardButton{{
		Text:         "🌐 " + tr(ctx, "set_language_label") + "：" + languageDisplayName(ctx, h.resolveLanguage(chatType, settings, groupSettings)),
		CallbackData: "settings langmenu",
	}})
	rows = append(rows, []telego.InlineKeyboardButton{{Text: "✖️ " + tr(ctx, "set_btn_close"), CallbackData: "settings close"}})

	return &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (h *SettingsHandler) resolveAutoDeleteList(chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) bool {
	if chatType != "private" {
		if groupSettings != nil {
			return groupSettings.AutoDeleteList
		}
		return true
	}
	if settings != nil {
		return settings.AutoDeleteList
	}
	return false
}

func (h *SettingsHandler) resolveAutoLinkDetect(chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) bool {
	if chatType != "private" {
		if groupSettings != nil {
			return groupSettings.AutoLinkDetect
		}
		return true
	}
	if settings != nil {
		return settings.AutoLinkDetect
	}
	return true
}

// resolveDefaultLyricFormat resolves the effective default lyric format for the
// current scope, falling back to the configured default and finally "lrc".
func (h *SettingsHandler) resolveDefaultLyricFormat(chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) string {
	format := strings.TrimSpace(h.DefaultLyricFormat)
	if format == "" {
		format = "lrc"
	}
	if chatType != "private" {
		if groupSettings != nil && strings.TrimSpace(groupSettings.DefaultLyricFormat) != "" {
			format = groupSettings.DefaultLyricFormat
		}
	} else if settings != nil && strings.TrimSpace(settings.DefaultLyricFormat) != "" {
		format = settings.DefaultLyricFormat
	}
	return lyricpkg.NormalizeFormat(format)
}

// resolveDefaultLyricFlags resolves the persisted translation/roma side-track
// defaults for the current scope. A nil stored pointer means "unset", in which
// case the per-format default applies (document formats default translation on;
// roma always defaults off). Only meaningful for side-track-capable formats.
func (h *SettingsHandler) resolveDefaultLyricFlags(chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings, format string) (includeTranslation, includeRoma bool) {
	includeTranslation = lyricFormatDefaultTranslation(format)
	includeRoma = false
	var transPtr, romaPtr *bool
	if chatType != "private" {
		if groupSettings != nil {
			transPtr = groupSettings.DefaultLyricIncludeTranslation
			romaPtr = groupSettings.DefaultLyricIncludeRoma
		}
	} else if settings != nil {
		transPtr = settings.DefaultLyricIncludeTranslation
		romaPtr = settings.DefaultLyricIncludeRoma
	}
	if transPtr != nil {
		includeTranslation = *transPtr
	}
	if romaPtr != nil {
		includeRoma = *romaPtr
	}
	return includeTranslation, includeRoma
}

// settingsLyricFormatRows groups the selectable default lyric formats for the
// submenu, mirroring the /lyric format grid.
var settingsLyricFormatRows = [][]string{
	{"lrc", "yrc", "qrc"},
	{"lys", "krc", "elrc"},
	{"spl", "ass", "ttml"},
	{"lqe", "amjson", "srt"},
	{"txt", "trans", "roma"},
}

// buildLyricFormatMenuKeyboard builds the default-lyric-format submenu: a grid
// of formats (current marked "✅") plus a back button to the main settings.
func (h *SettingsHandler) buildLyricFormatMenuKeyboard(ctx context.Context, chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) *telego.InlineKeyboardMarkup {
	current := h.resolveDefaultLyricFormat(chatType, settings, groupSettings)
	rows := make([][]telego.InlineKeyboardButton, 0, len(settingsLyricFormatRows)+1)
	for _, row := range settingsLyricFormatRows {
		buttons := make([]telego.InlineKeyboardButton, 0, len(row))
		for _, format := range row {
			label := lyricFormatButtonLabel(ctx, format)
			if format == current {
				label = "✅ " + label
			}
			buttons = append(buttons, telego.InlineKeyboardButton{
				Text:         label,
				CallbackData: fmt.Sprintf("settings lyricfmt %s", format),
			})
		}
		if len(buttons) > 0 {
			rows = append(rows, buttons)
		}
	}
	// Translation/roma side-track toggles, shown only when the chosen default
	// format can carry them. They persist independently of the format pick.
	if lyricFormatSupportsSideTracks(current) {
		includeTranslation, includeRoma := h.resolveDefaultLyricFlags(chatType, settings, groupSettings, current)
		rows = append(rows, []telego.InlineKeyboardButton{
			{
				Text:         h.formatToggleButton(ctx, "🌐 "+tr(ctx, "set_label_translation"), includeTranslation),
				CallbackData: fmt.Sprintf("settings lyrictrans %s", h.toggleValue(includeTranslation)),
			},
			{
				Text:         h.formatToggleButton(ctx, "🔤 "+tr(ctx, "set_label_roma"), includeRoma),
				CallbackData: fmt.Sprintf("settings lyricroma %s", h.toggleValue(includeRoma)),
			},
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{{Text: "⬅️ " + tr(ctx, "set_btn_back"), CallbackData: "settings lyricback"}})
	return &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// buildLyricFormatMenuText is the header text shown above the format submenu.
func (h *SettingsHandler) buildLyricFormatMenuText(ctx context.Context, chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) string {
	current := h.resolveDefaultLyricFormat(chatType, settings, groupSettings)
	var sb strings.Builder
	sb.WriteString("🎤 " + tr(ctx, "set_lyric_menu_title") + "\n\n")
	sb.WriteString(fmt.Sprintf("%s：%s\n", tr(ctx, "set_lyric_menu_current"), lyricFormatDisplayName(ctx, current)))
	if lyricFormatSupportsSideTracks(current) {
		includeTranslation, includeRoma := h.resolveDefaultLyricFlags(chatType, settings, groupSettings, current)
		sb.WriteString(fmt.Sprintf("%s：%s ｜ %s：%s\n", tr(ctx, "set_label_translation"), enabledText(ctx, includeTranslation), tr(ctx, "set_label_roma"), enabledText(ctx, includeRoma)))
	}
	sb.WriteString("\n" + tr(ctx, "set_lyric_menu_hint"))
	if lyricFormatSupportsSideTracks(current) {
		sb.WriteString("\n" + tr(ctx, "set_lyric_menu_sidetrack_hint"))
	}
	return sb.String()
}

// enabledText renders a bool as a 开/关 label for settings summaries.
func enabledText(ctx context.Context, enabled bool) string {
	if enabled {
		return tr(ctx, "set_state_on")
	}
	return tr(ctx, "set_state_off")
}

func (h *SettingsHandler) formatToggleButton(ctx context.Context, label string, enabled bool) string {
	state := tr(ctx, "set_state_off")
	if enabled {
		state = tr(ctx, "set_state_on")
	}
	return fmt.Sprintf("%s：%s", label, state)
}

func (h *SettingsHandler) sortedPluginSettingDefinitions() []botpkg.PluginSettingDefinition {
	defs := make([]botpkg.PluginSettingDefinition, 0, len(h.PluginSettingDefinitions))
	seen := make(map[string]struct{}, len(h.PluginSettingDefinitions))
	for _, def := range h.PluginSettingDefinitions {
		pluginName := strings.TrimSpace(def.Plugin)
		key := strings.TrimSpace(def.Key)
		if pluginName == "" || key == "" {
			continue
		}
		id := pluginName + ":" + key
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		defs = append(defs, def)
	}
	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i].Order != defs[j].Order {
			return defs[i].Order < defs[j].Order
		}
		if defs[i].Plugin != defs[j].Plugin {
			return defs[i].Plugin < defs[j].Plugin
		}
		return defs[i].Key < defs[j].Key
	})
	return defs
}

func (h *SettingsHandler) findPluginSettingDefinition(plugin string, key string) (botpkg.PluginSettingDefinition, bool) {
	plugin = strings.TrimSpace(plugin)
	key = strings.TrimSpace(key)
	for _, def := range h.sortedPluginSettingDefinitions() {
		if strings.TrimSpace(def.Plugin) == plugin && strings.TrimSpace(def.Key) == key {
			return def, true
		}
	}
	return botpkg.PluginSettingDefinition{}, false
}

func (h *SettingsHandler) resolvePluginSettingValue(ctx context.Context, chatType string, settings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings, def botpkg.PluginSettingDefinition) string {
	scopeType := botpkg.PluginScopeUser
	scopeID := int64(0)
	if chatType != "private" {
		scopeType = botpkg.PluginScopeGroup
		if groupSettings != nil {
			scopeID = groupSettings.ChatID
		}
	} else if settings != nil {
		scopeID = settings.UserID
	}

	value := ""
	if h.Repo != nil && scopeID != 0 {
		if stored, err := h.Repo.GetPluginSetting(ctx, scopeType, scopeID, def.Plugin, def.Key); err == nil {
			value = strings.TrimSpace(stored)
		}
	}
	if value == "" {
		value = def.DefaultForScope(scopeType)
	}
	if !def.Validate(value) {
		value = def.DefaultForScope(scopeType)
	}
	return value
}

func (h *SettingsHandler) toggleValue(enabled bool) string {
	if enabled {
		return "off"
	}
	return "on"
}

func (h *SettingsHandler) shouldShowPluginSetting(def botpkg.PluginSettingDefinition, autoLinkDetectEnabled bool, isGroup bool) bool {
	if def.GroupOnly && !isGroup {
		return false
	}
	if def.RequireAutoLinkDetect {
		return autoLinkDetectEnabled
	}
	return true
}

func (h *SettingsHandler) formatQualityButton(ctx context.Context, quality string, isSelected bool) string {
	name := h.getQualityDisplayName(ctx, quality)
	if isSelected {
		return fmt.Sprintf("✅ %s", name)
	}
	return name
}

func (h *SettingsHandler) getPlatformEmoji(platform string) string {
	return platformEmoji(h.PlatformManager, platform)
}

func (h *SettingsHandler) getPlatformDisplayName(platform string) string {
	return platformDisplayName(h.PlatformManager, platform)
}

func (h *SettingsHandler) getQualityEmoji(quality string) string {
	switch quality {
	case "standard":
		return "🔉"
	case "high":
		return "🔊"
	case "lossless":
		return "💎"
	case "hires":
		return "👑"
	default:
		return "🔊"
	}
}

func (h *SettingsHandler) getQualityDisplayName(ctx context.Context, quality string) string {
	switch quality {
	case "standard":
		return tr(ctx, "set_quality_standard")
	case "high":
		return tr(ctx, "set_quality_high")
	case "lossless":
		return tr(ctx, "set_quality_lossless")
	case "hires":
		return tr(ctx, "set_quality_hires")
	default:
		return quality
	}
}

type SettingsCallbackHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	SettingsHandler *SettingsHandler
	RateLimiter     *telegram.RateLimiter
}

// handleLyricMenuNavigation swaps the message between the main settings view and
// the default-lyric-format submenu. enterMenu picks the direction. It only edits
// text+keyboard; persistence happens via the "lyricfmt" case. In groups, opening
// the submenu still requires admin (mirroring the rest of group settings).
func (h *SettingsCallbackHandler) handleLyricMenuNavigation(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, msg *telego.Message, enterMenu bool) {
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
	var text string
	var keyboard *telego.InlineKeyboardMarkup
	if enterMenu {
		text = h.SettingsHandler.buildLyricFormatMenuText(ctx, chatType, settings, groupSettings)
		keyboard = h.SettingsHandler.buildLyricFormatMenuKeyboard(ctx, chatType, settings, groupSettings)
	} else {
		platforms := h.PlatformManager.List()
		text = h.SettingsHandler.buildSettingsText(ctx, chatType, settings, groupSettings, platforms)
		keyboard = h.SettingsHandler.buildSettingsKeyboard(ctx, chatType, settings, groupSettings, platforms)
	}

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

func (h *SettingsCallbackHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	args := strings.Split(query.Data, " ")

	if len(args) < 2 {
		return
	}
	if query.Message == nil {
		return
	}
	msg := query.Message.Message()
	if msg == nil {
		return
	}

	if args[1] == "close" {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
		deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
		if h.RateLimiter != nil {
			_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
		} else {
			_ = b.DeleteMessage(ctx, deleteParams)
		}
		return
	}

	// Navigate into / back out of the default-lyric-format submenu. These only
	// swap the keyboard+text; no setting changes here.
	if args[1] == "lyricmenu" || args[1] == "lyricback" {
		h.handleLyricMenuNavigation(ctx, b, query, msg, args[1] == "lyricmenu")
		return
	}

	// Open the language submenu (swaps keyboard+text; no change yet).
	if args[1] == "langmenu" {
		h.handleLanguageMenuNavigation(ctx, b, query, msg)
		return
	}
	// Persist a language pick: "settings langset <auto|en|zh|ja>".
	if args[1] == "langset" {
		if len(args) < 3 {
			return
		}
		h.applyLanguageSelection(ctx, b, query, msg, args[2])
		return
	}

	if len(args) < 3 {
		return
	}

	userID := query.From.ID
	settingType := args[1]
	settingValue := args[2]

	var settings *botpkg.UserSettings
	var groupSettings *botpkg.GroupSettings
	var err error
	if msg.Chat.Type != "private" {
		if !isRequesterOrAdmin(ctx, b, msg.Chat.ID, query.From.ID, 0) {
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

	changed := false
	var responseText string

	switch settingType {
	case "platform":
		platforms := h.PlatformManager.List()
		validPlatform := false
		for _, p := range platforms {
			if p == settingValue {
				validPlatform = true
				break
			}
		}

		if validPlatform {
			if msg != nil && msg.Chat.Type != "private" {
				if groupSettings != nil && groupSettings.DefaultPlatform != settingValue {
					groupSettings.DefaultPlatform = settingValue
					changed = true
					responseText = "✅ " + tr(ctx, "set_resp_platform_switched", map[string]any{"Name": h.SettingsHandler.getPlatformDisplayName(settingValue)})
				}
			} else if settings != nil && settings.DefaultPlatform != settingValue {
				settings.DefaultPlatform = settingValue
				changed = true
				responseText = "✅ " + tr(ctx, "set_resp_platform_switched", map[string]any{"Name": h.SettingsHandler.getPlatformDisplayName(settingValue)})
			}
		}

	case "quality":
		validQualities := []string{"standard", "high", "lossless", "hires"}
		validQuality := false
		for _, q := range validQualities {
			if q == settingValue {
				validQuality = true
				break
			}
		}

		if validQuality {
			if msg != nil && msg.Chat.Type != "private" {
				if groupSettings != nil && groupSettings.DefaultQuality != settingValue {
					groupSettings.DefaultQuality = settingValue
					changed = true
					responseText = "✅ " + tr(ctx, "set_resp_quality_set", map[string]any{"Name": h.SettingsHandler.getQualityDisplayName(ctx, settingValue)})
				}
			} else if settings != nil && settings.DefaultQuality != settingValue {
				settings.DefaultQuality = settingValue
				changed = true
				responseText = "✅ " + tr(ctx, "set_resp_quality_set", map[string]any{"Name": h.SettingsHandler.getQualityDisplayName(ctx, settingValue)})
			}
		}
	case "autodelete":
		if settingValue != "on" && settingValue != "off" {
			break
		}
		enabled := settingValue == "on"
		if msg != nil && msg.Chat.Type != "private" {
			if groupSettings != nil && groupSettings.AutoDeleteList != enabled {
				groupSettings.AutoDeleteList = enabled
				changed = true
				if enabled {
					responseText = "✅ " + tr(ctx, "set_resp_autodelete_on")
				} else {
					responseText = "✅ " + tr(ctx, "set_resp_autodelete_off")
				}
			}
		} else if settings != nil && settings.AutoDeleteList != enabled {
			settings.AutoDeleteList = enabled
			changed = true
			if enabled {
				responseText = "✅ " + tr(ctx, "set_resp_autodelete_on")
			} else {
				responseText = "✅ " + tr(ctx, "set_resp_autodelete_off")
			}
		}
	case "autolink":
		if settingValue != "on" && settingValue != "off" {
			break
		}
		enabled := settingValue == "on"
		if msg != nil && msg.Chat.Type != "private" {
			if groupSettings != nil && groupSettings.AutoLinkDetect != enabled {
				groupSettings.AutoLinkDetect = enabled
				changed = true
				if enabled {
					responseText = "✅ " + tr(ctx, "set_resp_autolink_on")
				} else {
					responseText = "✅ " + tr(ctx, "set_resp_autolink_off")
				}
			}
		} else if settings != nil && settings.AutoLinkDetect != enabled {
			settings.AutoLinkDetect = enabled
			changed = true
			if enabled {
				responseText = "✅ " + tr(ctx, "set_resp_autolink_on")
			} else {
				responseText = "✅ " + tr(ctx, "set_resp_autolink_off")
			}
		}
	case "pset":
		if len(args) < 5 {
			break
		}
		pluginName := strings.TrimSpace(args[2])
		pluginKey := strings.TrimSpace(args[3])
		pluginValue := strings.TrimSpace(args[4])
		def, ok := h.SettingsHandler.findPluginSettingDefinition(pluginName, pluginKey)
		if !ok || !def.Validate(pluginValue) {
			break
		}
		scopeType := botpkg.PluginScopeUser
		scopeID := userID
		if msg != nil && msg.Chat.Type != "private" {
			scopeType = botpkg.PluginScopeGroup
			scopeID = msg.Chat.ID
		}
		stored, _ := h.Repo.GetPluginSetting(ctx, scopeType, scopeID, pluginName, pluginKey)
		if strings.TrimSpace(stored) != pluginValue {
			if err := h.Repo.SetPluginSetting(ctx, scopeType, scopeID, pluginName, pluginKey, pluginValue); err != nil {
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
					CallbackQueryID: query.ID,
					Text:            "❌ " + tr(ctx, "set_err_save"),
					ShowAlert:       true,
				})
				return
			}
			changed = true
			responseText = "✅ " + tr(ctx, "set_resp_plugin_set", map[string]any{"Title": def.TitleLocalized(func(k string) string { return tr(ctx, k) }), "Label": def.LabelOfLocalized(func(k string) string { return tr(ctx, k) }, pluginValue)})
		}
	case "pcycle":
		if len(args) < 4 {
			break
		}
		pluginName := strings.TrimSpace(args[2])
		pluginKey := strings.TrimSpace(args[3])
		def, ok := h.SettingsHandler.findPluginSettingDefinition(pluginName, pluginKey)
		if !ok || len(def.Options) == 0 {
			break
		}
		scopeType := botpkg.PluginScopeUser
		scopeID := userID
		if msg != nil && msg.Chat.Type != "private" {
			scopeType = botpkg.PluginScopeGroup
			scopeID = msg.Chat.ID
		}
		current := h.SettingsHandler.resolvePluginSettingValue(ctx, string(msg.Chat.Type), settings, groupSettings, def)
		next := ""
		for i, opt := range def.Options {
			if strings.TrimSpace(opt.Value) == strings.TrimSpace(current) {
				next = def.Options[(i+1)%len(def.Options)].Value
				break
			}
		}
		if next == "" {
			next = def.Options[0].Value
		}
		if err := h.Repo.SetPluginSetting(ctx, scopeType, scopeID, pluginName, pluginKey, next); err != nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
				CallbackQueryID: query.ID,
				Text:            "❌ " + tr(ctx, "set_err_save"),
				ShowAlert:       true,
			})
			return
		}
		changed = true
		responseText = "✅ " + tr(ctx, "set_resp_plugin_set", map[string]any{"Title": def.TitleLocalized(func(k string) string { return tr(ctx, k) }), "Label": def.LabelOfLocalized(func(k string) string { return tr(ctx, k) }, next)})
	case "lyricfmt":
		if !isKnownLyricFormat(settingValue) {
			break
		}
		resolved := lyricpkg.NormalizeFormat(settingValue)
		if msg != nil && msg.Chat.Type != "private" {
			if groupSettings != nil && groupSettings.DefaultLyricFormat != resolved {
				groupSettings.DefaultLyricFormat = resolved
				changed = true
				responseText = "✅ " + tr(ctx, "set_resp_lyricfmt_set", map[string]any{"Name": lyricFormatDisplayName(ctx, resolved)})
			}
		} else if settings != nil && settings.DefaultLyricFormat != resolved {
			settings.DefaultLyricFormat = resolved
			changed = true
			responseText = "✅ " + tr(ctx, "set_resp_lyricfmt_set", map[string]any{"Name": lyricFormatDisplayName(ctx, resolved)})
		}
	case "lyrictrans", "lyricroma":
		if settingValue != "on" && settingValue != "off" {
			break
		}
		enabled := settingValue == "on"
		label := tr(ctx, "set_label_translation")
		if settingType == "lyricroma" {
			label = tr(ctx, "set_label_roma")
		}
		if msg != nil && msg.Chat.Type != "private" {
			if groupSettings != nil {
				if settingType == "lyrictrans" {
					groupSettings.DefaultLyricIncludeTranslation = &enabled
				} else {
					groupSettings.DefaultLyricIncludeRoma = &enabled
				}
				changed = true
			}
		} else if settings != nil {
			if settingType == "lyrictrans" {
				settings.DefaultLyricIncludeTranslation = &enabled
			} else {
				settings.DefaultLyricIncludeRoma = &enabled
			}
			changed = true
		}
		if changed {
			state := tr(ctx, "set_state_off_full")
			if enabled {
				state = tr(ctx, "set_state_on_full")
			}
			responseText = "✅ " + tr(ctx, "set_resp_lyric_sidetrack", map[string]any{"Label": label, "State": state})
		}
	}

	if changed {
		if settingType != "pset" {
			if msg != nil && msg.Chat.Type != "private" {
				if err := h.Repo.UpdateGroupSettings(ctx, groupSettings); err != nil {
					_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
						CallbackQueryID: query.ID,
						Text:            "❌ " + tr(ctx, "set_err_save"),
						ShowAlert:       true,
					})
					return
				}
			} else if err := h.Repo.UpdateUserSettings(ctx, settings); err != nil {
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
					CallbackQueryID: query.ID,
					Text:            "❌ " + tr(ctx, "set_err_save"),
					ShowAlert:       true,
				})
				return
			}
		}

		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            responseText,
		})

		if msg != nil {
			chatType := string(msg.Chat.Type)
			var text string
			var keyboard *telego.InlineKeyboardMarkup
			if settingType == "lyricfmt" || settingType == "lyrictrans" || settingType == "lyricroma" {
				// Stay in the lyric-format submenu so the ✅/toggle state updates.
				text = h.SettingsHandler.buildLyricFormatMenuText(ctx, chatType, settings, groupSettings)
				keyboard = h.SettingsHandler.buildLyricFormatMenuKeyboard(ctx, chatType, settings, groupSettings)
			} else {
				platforms := h.PlatformManager.List()
				text = h.SettingsHandler.buildSettingsText(ctx, chatType, settings, groupSettings, platforms)
				keyboard = h.SettingsHandler.buildSettingsKeyboard(ctx, chatType, settings, groupSettings, platforms)
			}

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
	} else {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
		})
	}
}
