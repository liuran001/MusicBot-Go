package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// LyricHandler handles /lyric command.
type LyricHandler struct {
	PlatformManager platform.Manager
}

func (h *LyricHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}
	message := update.Message

	args := commandArguments(message.Text)
	if args == "" && message.ReplyToMessage == nil {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          message.Chat.ID,
			Text:            inputContent,
			ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
		})
		return
	}

	if args == "" && message.ReplyToMessage != nil {
		args = message.ReplyToMessage.Text
		if args == "" {
			return
		}
	}

	msgResult, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          message.Chat.ID,
		Text:            fetchingLyric,
		ReplyParameters: &models.ReplyParameters{MessageID: message.ID},
	})
	if err != nil {
		return
	}

	if h.PlatformManager == nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID, Text: getLrcFailed})
		return
	}

	platformName, trackID, found := extractPlatformTrackFromMessage(args, h.PlatformManager)
	if !found {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID, Text: noResults})
		return
	}

	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID, Text: getLrcFailed})
		return
	}

	if !plat.SupportsLyrics() {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID, Text: "此平台不支持获取歌词"})
		return
	}

	lyrics, err := plat.GetLyrics(ctx, trackID)
	if err != nil {
		errText := h.formatLyricsError(err)
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID, Text: errText})
		return
	}

	h.sendLyrics(ctx, b, msgResult, message, lyrics)
}

func extractPlatformTrackFromMessage(messageText string, mgr platform.Manager) (platformName, trackID string, found bool) {
	if messageText == "" {
		return "", "", false
	}

	platformName, trackID, matched := mgr.MatchURL(messageText)
	if matched {
		return platformName, trackID, true
	}

	parts := strings.Fields(strings.TrimSpace(messageText))
	if len(parts) > 0 {
		if _, err := parseStringToInt(parts[0]); err == nil {
			return "netease", parts[0], true
		}
		return "netease", messageText, true
	}

	return "", "", false
}

func parseStringToInt(s string) (int, error) {
	var result int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		result = result*10 + int(c-'0')
	}
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	return result, nil
}

func (h *LyricHandler) formatLyricsError(err error) string {
	if err == nil {
		return getLrcFailed
	}

	if errors.Is(err, platform.ErrNotFound) {
		return "未找到歌曲或歌词"
	}
	if errors.Is(err, platform.ErrUnavailable) {
		return "此歌曲无法获取歌词"
	}
	if errors.Is(err, platform.ErrUnsupported) {
		return "此平台不支持获取歌词"
	}

	return getLrcFailed
}

func (h *LyricHandler) sendLyrics(ctx context.Context, b *bot.Bot, msgResult *models.Message, originalMsg *models.Message, lyrics *platform.Lyrics) {
	var text string

	if len(lyrics.Timestamped) > 0 {
		var lines []string
		for _, line := range lyrics.Timestamped {
			timestamp := formatDuration(line.Time)
			lines = append(lines, fmt.Sprintf("[%s] %s", timestamp, line.Text))
		}
		text = strings.Join(lines, "\n")
	} else if lyrics.Plain != "" {
		text = lyrics.Plain
	} else {
		text = "暂无歌词信息"
	}

	if len(text) > 4000 {
		text = text[:4000] + "\n\n... (已截断)"
	}

	_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID})
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          originalMsg.Chat.ID,
		Text:            text,
		ReplyParameters: &models.ReplyParameters{MessageID: originalMsg.ID},
	})
}

func formatDuration(d time.Duration) string {
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
