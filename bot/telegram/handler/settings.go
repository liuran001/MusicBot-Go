package handler

import (
	"context"
	"fmt"
	"sort"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
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
				Text:   "❌ 仅管理员可修改群组设置",
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
			Text:   "❌ 获取设置失败，请稍后重试",
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

	sb.WriteString("⚙️ 设置中心\n\n")

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
	sb.WriteString(fmt.Sprintf("🎵 默认平台: %s %s\n", platformEmoji, h.getPlatformDisplayName(platformName)))

	qualityEmoji := h.getQualityEmoji(qualityValue)
	sb.WriteString(fmt.Sprintf("🎧 默认音质: %s %s\n", qualityEmoji, h.getQualityDisplayName(qualityValue)))
	autoDeleteEnabled := h.resolveAutoDeleteList(chatType, settings, groupSettings)
	autoDeleteText := "关闭"
	if autoDeleteEnabled {
		autoDeleteText = "开启"
	}
	autoLinkDetectEnabled := h.resolveAutoLinkDetect(chatType, settings, groupSettings)
	autoLinkDetectText := "关闭"
	if autoLinkDetectEnabled {
		autoLinkDetectText = "开启"
	}
	sb.WriteString(fmt.Sprintf("🧹 点歌后自动删除列表消息: %s\n", autoDeleteText))
	sb.WriteString(fmt.Sprintf("🔗 会话内链接自动识别: %s\n", autoLinkDetectText))

	for _, def := range h.sortedPluginSettingDefinitions() {
		if !h.shouldShowPluginSetting(def, autoLinkDetectEnabled) {
			continue
		}
		value := h.resolvePluginSettingValue(ctx, chatType, settings, groupSettings, def)
		sb.WriteString(fmt.Sprintf("🔌 %s: %s\n", def.Title, def.LabelOf(value)))
	}
	sb.WriteString("\n")

	if len(platforms) > 1 {
		sb.WriteString("💡 可用平台: ")
		var platformNames []string
		for _, p := range platforms {
			platformNames = append(platformNames, h.getPlatformDisplayName(p))
		}
		sb.WriteString(strings.Join(platformNames, ", "))
		sb.WriteString("\n")
	}

	sb.WriteString("\n点击下方按钮修改设置")

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
				text = "✅ " + platformSearchShortName(p)
			}

			platformButtons = append(platformButtons, telego.InlineKeyboardButton{
				Text:         text,
				CallbackData: callbackData,
			})
		}

		rows = append(rows, platformButtons)
	}

	qualityButtons := []telego.InlineKeyboardButton{
		{
			Text:         h.formatQualityButton("standard", qualityValue == "standard"),
			CallbackData: "settings quality standard",
		},
		{
			Text:         h.formatQualityButton("high", qualityValue == "high"),
			CallbackData: "settings quality high",
		},
		{
			Text:         h.formatQualityButton("lossless", qualityValue == "lossless"),
			CallbackData: "settings quality lossless",
		},
		{
			Text:         h.formatQualityButton("hires", qualityValue == "hires"),
			CallbackData: "settings quality hires",
		},
	}
	rows = append(rows, qualityButtons)

	autoDeleteEnabled := h.resolveAutoDeleteList(chatType, settings, groupSettings)
	autoLinkDetectEnabled := h.resolveAutoLinkDetect(chatType, settings, groupSettings)
	rows = append(rows, []telego.InlineKeyboardButton{
		{
			Text:         h.formatToggleButton("自动删列表", autoDeleteEnabled),
			CallbackData: fmt.Sprintf("settings autodelete %s", h.toggleValue(autoDeleteEnabled)),
		},
		{
			Text:         h.formatToggleButton("自动识别链接", autoLinkDetectEnabled),
			CallbackData: fmt.Sprintf("settings autolink %s", h.toggleValue(autoLinkDetectEnabled)),
		},
	})
	for _, def := range h.sortedPluginSettingDefinitions() {
		if !h.shouldShowPluginSetting(def, autoLinkDetectEnabled) {
			continue
		}
		if len(def.Options) == 0 {
			continue
		}
		current := h.resolvePluginSettingValue(ctx, chatType, settings, groupSettings, def)
		rows = append(rows, []telego.InlineKeyboardButton{{
			Text:         fmt.Sprintf("%s: %s", def.Title, def.LabelOf(current)),
			CallbackData: fmt.Sprintf("settings pcycle %s %s", def.Plugin, def.Key),
		}})
	}
	rows = append(rows, []telego.InlineKeyboardButton{{Text: "关闭", CallbackData: "settings close"}})

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

func (h *SettingsHandler) formatToggleButton(label string, enabled bool) string {
	state := "关闭"
	if enabled {
		state = "开启"
	}
	return fmt.Sprintf("%s: %s", label, state)
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

func (h *SettingsHandler) shouldShowPluginSetting(def botpkg.PluginSettingDefinition, autoLinkDetectEnabled bool) bool {
	if def.RequireAutoLinkDetect {
		return autoLinkDetectEnabled
	}
	return true
}

func (h *SettingsHandler) formatQualityButton(quality string, isSelected bool) string {
	name := h.getQualityDisplayName(quality)
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

func (h *SettingsHandler) getQualityDisplayName(quality string) string {
	switch quality {
	case "standard":
		return "标准"
	case "high":
		return "高品质"
	case "lossless":
		return "无损"
	case "hires":
		return "Hi-Res"
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
				Text:            "❌ 仅管理员可修改群组设置",
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
			Text:            "❌ 获取设置失败",
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
					responseText = fmt.Sprintf("✅ 已切换到 %s", h.SettingsHandler.getPlatformDisplayName(settingValue))
				}
			} else if settings != nil && settings.DefaultPlatform != settingValue {
				settings.DefaultPlatform = settingValue
				changed = true
				responseText = fmt.Sprintf("✅ 已切换到 %s", h.SettingsHandler.getPlatformDisplayName(settingValue))
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
					responseText = fmt.Sprintf("✅ 音质已设置为 %s", h.SettingsHandler.getQualityDisplayName(settingValue))
				}
			} else if settings != nil && settings.DefaultQuality != settingValue {
				settings.DefaultQuality = settingValue
				changed = true
				responseText = fmt.Sprintf("✅ 音质已设置为 %s", h.SettingsHandler.getQualityDisplayName(settingValue))
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
					responseText = "✅ 已开启自动删除列表"
				} else {
					responseText = "✅ 已关闭自动删除列表"
				}
			}
		} else if settings != nil && settings.AutoDeleteList != enabled {
			settings.AutoDeleteList = enabled
			changed = true
			if enabled {
				responseText = "✅ 已开启自动删除列表"
			} else {
				responseText = "✅ 已关闭自动删除列表"
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
					responseText = "✅ 已开启会话内链接自动识别"
				} else {
					responseText = "✅ 已关闭会话内链接自动识别"
				}
			}
		} else if settings != nil && settings.AutoLinkDetect != enabled {
			settings.AutoLinkDetect = enabled
			changed = true
			if enabled {
				responseText = "✅ 已开启会话内链接自动识别"
			} else {
				responseText = "✅ 已关闭会话内链接自动识别"
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
					Text:            "❌ 保存设置失败",
					ShowAlert:       true,
				})
				return
			}
			changed = true
			responseText = fmt.Sprintf("✅ %s 已设置为: %s", def.Title, def.LabelOf(pluginValue))
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
				Text:            "❌ 保存设置失败",
				ShowAlert:       true,
			})
			return
		}
		changed = true
		responseText = fmt.Sprintf("✅ %s 已设置为: %s", def.Title, def.LabelOf(next))
	}

	if changed {
		if settingType != "pset" {
			if msg != nil && msg.Chat.Type != "private" {
				if err := h.Repo.UpdateGroupSettings(ctx, groupSettings); err != nil {
					_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
						CallbackQueryID: query.ID,
						Text:            "❌ 保存设置失败",
						ShowAlert:       true,
					})
					return
				}
			} else if err := h.Repo.UpdateUserSettings(ctx, settings); err != nil {
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
					CallbackQueryID: query.ID,
					Text:            "❌ 保存设置失败",
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
			platforms := h.PlatformManager.List()
			chatType := string(msg.Chat.Type)
			text := h.SettingsHandler.buildSettingsText(ctx, chatType, settings, groupSettings, platforms)
			keyboard := h.SettingsHandler.buildSettingsKeyboard(ctx, chatType, settings, groupSettings, platforms)

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
