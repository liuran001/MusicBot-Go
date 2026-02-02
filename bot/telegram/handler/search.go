package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// SearchHandler handles /search and private message search.
type SearchHandler struct {
	PlatformManager platform.Manager
	Repo            botpkg.SongRepository
}

func (h *SearchHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}

	message := update.Message
	threadID := message.MessageThreadID
	replyParams := buildReplyParams(message)
	keyword := commandArguments(message.Text)
	if keyword == "" && message.Chat.Type == "private" {
		if !strings.HasPrefix(strings.TrimSpace(message.Text), "/") {
			keyword = message.Text
		}
	}
	if strings.TrimSpace(keyword) == "" {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          message.Chat.ID,
			Text:            inputKeyword,
			ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
		})
		return
	}

	msgResult, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          message.Chat.ID,
		MessageThreadID: threadID,
		Text:            searching,
		ReplyParameters: replyParams,
	})
	if err != nil {
		return
	}
	if h.PlatformManager == nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      noResults,
		})
		return
	}

	// Get user's default platform from settings
	platformName := "netease" // fallback default
	if h.Repo != nil {
		if message.Chat.Type != "private" {
			if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
				platformName = settings.DefaultPlatform
			}
		}
	}
	var userID int64
	if message.From != nil {
		userID = message.From.ID
		if h.Repo != nil {
			if message.Chat.Type == "private" {
				if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
					platformName = settings.DefaultPlatform
				}
			} else if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
				platformName = settings.DefaultPlatform
			}
		}
	}

	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      noResults,
		})
		return
	}

	if !plat.SupportsSearch() {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      "此平台不支持搜索功能",
		})
		return
	}

	tracks, err := plat.Search(ctx, keyword, 10)
	usedFallback := false
	if err != nil {
		if platformName != "netease" {
			fallbackPlat := h.PlatformManager.Get("netease")
			if fallbackPlat != nil && fallbackPlat.SupportsSearch() {
				tracks, err = fallbackPlat.Search(ctx, keyword, 10)
				if err == nil && len(tracks) > 0 {
					platformName = "netease"
					plat = fallbackPlat
					usedFallback = true
				}
			}
		}

		if err != nil {
			errorText := noResults
			if errors.Is(err, platform.ErrUnsupported) {
				errorText = "此平台不支持搜索功能"
			} else if errors.Is(err, platform.ErrRateLimited) {
				errorText = "请求过于频繁，请稍后再试"
			} else if errors.Is(err, platform.ErrUnavailable) {
				errorText = "搜索服务暂时不可用"
			}
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    msgResult.Chat.ID,
				MessageID: msgResult.ID,
				Text:      errorText,
			})
			return
		}
	}

	if len(tracks) == 0 {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      noResults,
		})
		return
	}

	var buttons []models.InlineKeyboardButton
	var textMessage strings.Builder

	platformEmoji := "🎵"
	platformDisplayName := "网易云音乐"
	if platformName == "netease" {
		platformEmoji = "🎵"
		platformDisplayName = "网易云音乐"
	}

	if usedFallback {
		textMessage.WriteString("⚠️ 默认平台搜索失败，已切换到网易云\n\n")
	}

	textMessage.WriteString(fmt.Sprintf("%s *%s* 搜索结果\n\n", platformEmoji, mdV2Replacer.Replace(platformDisplayName)))

	requesterID := int64(0)
	if message.From != nil {
		requesterID = message.From.ID
	}

	maxResults := len(tracks)
	if maxResults > 8 {
		maxResults = 8
	}

	for i := 0; i < maxResults; i++ {
		track := tracks[i]
		escapedTitle := mdV2Replacer.Replace(track.Title)

		var trackLink string
		if platformName == "netease" && track.ID != "" {
			trackLink = fmt.Sprintf("[%s](https://music.163.com/song?id=%s)", escapedTitle, track.ID)
		} else {
			trackLink = escapedTitle
		}

		var artistParts []string
		for _, artist := range track.Artists {
			escapedArtist := mdV2Replacer.Replace(artist.Name)
			if platformName == "netease" && artist.ID != "" {
				artistLink := fmt.Sprintf("[%s](https://music.163.com/artist?id=%s)", escapedArtist, artist.ID)
				artistParts = append(artistParts, artistLink)
			} else {
				artistParts = append(artistParts, escapedArtist)
			}
		}
		songArtists := strings.Join(artistParts, " / ")

		textMessage.WriteString(fmt.Sprintf("%d\\. 「%s」 \\- %s\n", i+1, trackLink, songArtists))

		qualityValue := "hires"
		if h.Repo != nil {
			if message.Chat.Type != "private" {
				if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
					qualityValue = settings.DefaultQuality
				}
			} else if userID != 0 {
				if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
					qualityValue = settings.DefaultQuality
				}
			}
		}
		callbackData := fmt.Sprintf("music %s %s %s %d", platformName, track.ID, qualityValue, requesterID)
		buttons = append(buttons, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: callbackData,
		})
	}

	keyboard := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{buttons}}
	disablePreview := true
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:             msgResult.Chat.ID,
		MessageID:          msgResult.ID,
		Text:               textMessage.String(),
		ParseMode:          models.ParseModeMarkdown,
		ReplyMarkup:        keyboard,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: &disablePreview},
	})
}
