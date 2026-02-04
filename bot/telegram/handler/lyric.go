package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// LyricHandler handles /lyric command.
type LyricHandler struct {
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
}

func (h *LyricHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil {
		return
	}
	message := update.Message

	args := commandArguments(message.Text)
	if args == "" && message.ReplyToMessage == nil {
		params := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: message.Chat.ID},
			Text:            inputContent,
			ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}

	if args == "" && message.ReplyToMessage != nil {
		args = message.ReplyToMessage.Text
		if args == "" {
			return
		}
	}

	msgResult, err := b.SendMessage(ctx, &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		Text:            fetchingLyric,
		ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
	})
	if err != nil {
		return
	}

	if h.PlatformManager == nil {
		params := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: msgResult.Chat.ID}, MessageID: msgResult.MessageID, Text: getLrcFailed}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	platformName, trackID, found := extractPlatformTrackFromMessage(ctx, args, h.PlatformManager)
	if !found {
		params := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: msgResult.Chat.ID}, MessageID: msgResult.MessageID, Text: noResults}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		params := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: msgResult.Chat.ID}, MessageID: msgResult.MessageID, Text: getLrcFailed}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	if !plat.SupportsLyrics() {
		params := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: msgResult.Chat.ID}, MessageID: msgResult.MessageID, Text: "此平台不支持获取歌词"}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	lyrics, err := plat.GetLyrics(ctx, trackID)
	if err != nil {
		errText := h.formatLyricsError(err)
		params := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: msgResult.Chat.ID}, MessageID: msgResult.MessageID, Text: errText}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		return
	}

	h.sendLyrics(ctx, b, msgResult, message, lyrics)
}

func extractPlatformTrackFromMessage(ctx context.Context, messageText string, mgr platform.Manager) (platformName, trackID string, found bool) {
	if messageText == "" {
		return "", "", false
	}
	if mgr != nil {
		resolvedText := resolveShortLinkText(ctx, mgr, messageText)
		if _, _, matched := matchPlaylistURL(ctx, mgr, resolvedText); matched {
			return "", "", false
		}
		if platformName, trackID, matched := mgr.MatchText(resolvedText); matched {
			return platformName, trackID, true
		}
		if platformName, trackID, matched := mgr.MatchURL(resolvedText); matched {
			return platformName, trackID, true
		}
	}
	return "", "", false
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

func (h *LyricHandler) sendLyrics(ctx context.Context, b *telego.Bot, msgResult *telego.Message, originalMsg *telego.Message, lyrics *platform.Lyrics) {
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

	deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msgResult.Chat.ID}, MessageID: msgResult.MessageID}
	if h.RateLimiter != nil {
		_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
	} else {
		_ = b.DeleteMessage(ctx, deleteParams)
	}

	sendParams := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: originalMsg.Chat.ID},
		Text:            text,
		ReplyParameters: &telego.ReplyParameters{MessageID: originalMsg.MessageID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, sendParams)
	} else {
		_, _ = b.SendMessage(ctx, sendParams)
	}
}

func formatDuration(d time.Duration) string {
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
