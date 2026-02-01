package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type SettingsHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
}

func (h *SettingsHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}

	message := update.Message
	userID := message.From.ID

	settings, err := h.Repo.GetUserSettings(ctx, userID)
	if err != nil {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: message.Chat.ID,
			Text:   "❌ 获取设置失败，请稍后重试",
		})
		return
	}

	platforms := h.PlatformManager.List()

	text := h.buildSettingsText(settings, platforms)
	keyboard := h.buildSettingsKeyboard(settings, platforms)

	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      message.Chat.ID,
		Text:        text,
		ReplyMarkup: keyboard,
	})
}

func (h *SettingsHandler) buildSettingsText(settings *botpkg.UserSettings, platforms []string) string {
	var sb strings.Builder

	sb.WriteString("⚙️ 设置中心\n\n")

	platformName := settings.DefaultPlatform
	platformEmoji := h.getPlatformEmoji(platformName)
	sb.WriteString(fmt.Sprintf("🎵 默认平台: %s %s\n", platformEmoji, h.getPlatformDisplayName(platformName)))

	qualityEmoji := h.getQualityEmoji(settings.DefaultQuality)
	sb.WriteString(fmt.Sprintf("🎧 默认音质: %s %s\n\n", qualityEmoji, h.getQualityDisplayName(settings.DefaultQuality)))

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

func (h *SettingsHandler) buildSettingsKeyboard(settings *botpkg.UserSettings, platforms []string) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton

	if len(platforms) > 1 {
		var platformButtons []models.InlineKeyboardButton
		for _, p := range platforms {
			emoji := h.getPlatformEmoji(p)
			displayName := h.getPlatformDisplayName(p)
			callbackData := fmt.Sprintf("settings platform %s", p)

			text := fmt.Sprintf("%s %s", emoji, displayName)
			if p == settings.DefaultPlatform {
				text = "✓ " + text
			}

			platformButtons = append(platformButtons, models.InlineKeyboardButton{
				Text:         text,
				CallbackData: callbackData,
			})
		}

		for i := 0; i < len(platformButtons); i += 2 {
			if i+1 < len(platformButtons) {
				rows = append(rows, []models.InlineKeyboardButton{platformButtons[i], platformButtons[i+1]})
			} else {
				rows = append(rows, []models.InlineKeyboardButton{platformButtons[i]})
			}
		}
	}

	qualityButtons := []models.InlineKeyboardButton{
		{
			Text:         h.formatQualityButton("standard", settings.DefaultQuality == "standard"),
			CallbackData: "settings quality standard",
		},
		{
			Text:         h.formatQualityButton("high", settings.DefaultQuality == "high"),
			CallbackData: "settings quality high",
		},
	}
	rows = append(rows, qualityButtons)

	qualityButtons2 := []models.InlineKeyboardButton{
		{
			Text:         h.formatQualityButton("lossless", settings.DefaultQuality == "lossless"),
			CallbackData: "settings quality lossless",
		},
		{
			Text:         h.formatQualityButton("hires", settings.DefaultQuality == "hires"),
			CallbackData: "settings quality hires",
		},
	}
	rows = append(rows, qualityButtons2)

	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (h *SettingsHandler) formatQualityButton(quality string, isSelected bool) string {
	emoji := h.getQualityEmoji(quality)
	name := h.getQualityDisplayName(quality)
	if isSelected {
		return fmt.Sprintf("✓ %s %s", emoji, name)
	}
	return fmt.Sprintf("%s %s", emoji, name)
}

func (h *SettingsHandler) getPlatformEmoji(platform string) string {
	switch platform {
	case "netease":
		return "🎵"
	case "spotify":
		return "🎧"
	case "qqmusic":
		return "🎶"
	default:
		return "🎵"
	}
}

func (h *SettingsHandler) getPlatformDisplayName(platform string) string {
	switch platform {
	case "netease":
		return "网易云音乐"
	case "spotify":
		return "Spotify"
	case "qqmusic":
		return "QQ音乐"
	default:
		return platform
	}
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
}

func (h *SettingsCallbackHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}

	query := update.CallbackQuery
	args := strings.Split(query.Data, " ")

	if len(args) < 3 {
		return
	}

	userID := query.From.ID
	settingType := args[1]
	settingValue := args[2]

	settings, err := h.Repo.GetUserSettings(ctx, userID)
	if err != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
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

		if validPlatform && settings.DefaultPlatform != settingValue {
			settings.DefaultPlatform = settingValue
			changed = true
			responseText = fmt.Sprintf("✅ 已切换到 %s", h.SettingsHandler.getPlatformDisplayName(settingValue))
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

		if validQuality && settings.DefaultQuality != settingValue {
			settings.DefaultQuality = settingValue
			changed = true
			responseText = fmt.Sprintf("✅ 音质已设置为 %s", h.SettingsHandler.getQualityDisplayName(settingValue))
		}
	}

	if changed {
		if err := h.Repo.UpdateUserSettings(ctx, settings); err != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
				CallbackQueryID: query.ID,
				Text:            "❌ 保存设置失败",
				ShowAlert:       true,
			})
			return
		}

		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            responseText,
		})

		msg := query.Message.Message
		if msg != nil {
			platforms := h.PlatformManager.List()
			text := h.SettingsHandler.buildSettingsText(settings, platforms)
			keyboard := h.SettingsHandler.buildSettingsKeyboard(settings, platforms)

			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:      msg.Chat.ID,
				MessageID:   msg.ID,
				Text:        text,
				ReplyMarkup: keyboard,
			})
		}
	} else {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
		})
	}
}
