package handler

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

const lyricCaptionMaxChars = 1000

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

	sendParams := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		Text:            fetchingLyric,
		ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
	}
	var msgResult *telego.Message
	var err error
	if h.RateLimiter != nil {
		msgResult, err = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, sendParams)
	} else {
		msgResult, err = b.SendMessage(ctx, sendParams)
	}
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

	fileName := h.buildLyricFileName(ctx, plat, trackID)
	h.sendLyrics(ctx, b, msgResult, message, lyrics, fileName)
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

func (h *LyricHandler) sendLyrics(ctx context.Context, b *telego.Bot, msgResult *telego.Message, originalMsg *telego.Message, lyrics *platform.Lyrics, fileName string) {
	lrcText, plainText := buildLyricTexts(lyrics)
	if strings.TrimSpace(lrcText) == "" {
		lrcText = "暂无歌词信息\n"
	}

	deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msgResult.Chat.ID}, MessageID: msgResult.MessageID}
	if h.RateLimiter != nil {
		_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
	} else {
		_ = b.DeleteMessage(ctx, deleteParams)
	}

	filePath, err := writeLyricTempFile(lrcText)
	if err != nil {
		sendFallback := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: originalMsg.Chat.ID},
			Text:            getLrcFailed,
			ReplyParameters: &telego.ReplyParameters{MessageID: originalMsg.MessageID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, sendFallback)
		} else {
			_, _ = b.SendMessage(ctx, sendFallback)
		}
		return
	}
	defer os.Remove(filePath)

	file, err := os.Open(filePath)
	if err != nil {
		sendFallback := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: originalMsg.Chat.ID},
			Text:            getLrcFailed,
			ReplyParameters: &telego.ReplyParameters{MessageID: originalMsg.MessageID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, sendFallback)
		} else {
			_, _ = b.SendMessage(ctx, sendFallback)
		}
		return
	}
	defer file.Close()

	docParams := &telego.SendDocumentParams{
		ChatID:          telego.ChatID{ID: originalMsg.Chat.ID},
		Document:        telego.InputFile{File: telegoutil.NameReader(file, fileName)},
		ReplyParameters: &telego.ReplyParameters{MessageID: originalMsg.MessageID},
	}

	plainText = strings.TrimSpace(plainText)
	caption := ""
	if plainText != "" {
		escaped := html.EscapeString(plainText)
		candidate := fmt.Sprintf("<blockquote expandable>%s</blockquote>", escaped)
		if len([]rune(candidate)) <= lyricCaptionMaxChars {
			caption = candidate
		}
	}
	if caption != "" {
		docParams.Caption = caption
		docParams.ParseMode = telego.ModeHTML
	}

	var sendErr error
	if h.RateLimiter != nil {
		_, sendErr = telegram.SendDocumentWithRetry(ctx, h.RateLimiter, b, docParams)
	} else {
		_, sendErr = b.SendDocument(ctx, docParams)
	}

	if sendErr != nil && caption != "" {
		docParams.Caption = ""
		docParams.ParseMode = ""
		if h.RateLimiter != nil {
			_, _ = telegram.SendDocumentWithRetry(ctx, h.RateLimiter, b, docParams)
		} else {
			_, _ = b.SendDocument(ctx, docParams)
		}
	}
}

func buildLyricTexts(lyrics *platform.Lyrics) (lrcText, plainText string) {
	if lyrics == nil {
		return "", ""
	}

	if len(lyrics.Timestamped) > 0 {
		lrcLines := make([]string, 0, len(lyrics.Timestamped))
		plainLines := make([]string, 0, len(lyrics.Timestamped))
		for _, line := range lyrics.Timestamped {
			lineText := strings.TrimSpace(line.Text)
			if lineText == "" {
				continue
			}
			timestamp := formatDuration(line.Time)
			lrcLines = append(lrcLines, fmt.Sprintf("[%s] %s", timestamp, lineText))
			plainLines = append(plainLines, fmt.Sprintf("[%s] %s", timestamp, lineText))
		}
		lrcText = strings.Join(lrcLines, "\n")
		plainText = strings.Join(plainLines, "\n")
	}

	if strings.TrimSpace(lrcText) == "" && strings.TrimSpace(lyrics.Plain) != "" {
		lrcText = platform.NormalizeLRCTimestamps(lyrics.Plain)
	}
	if strings.TrimSpace(plainText) == "" && strings.TrimSpace(lyrics.Plain) != "" {
		plainText = lyrics.Plain
	}

	return strings.TrimSpace(lrcText), strings.TrimSpace(plainText)
}

func writeLyricTempFile(text string) (string, error) {
	tmpFile, err := os.CreateTemp("", "musicbot-lyrics-*.lrc")
	if err != nil {
		return "", err
	}
	path := tmpFile.Name()
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(text); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func (h *LyricHandler) buildLyricFileName(ctx context.Context, plat platform.Platform, trackID string) string {
	defaultName := "歌词.lrc"
	if plat == nil || strings.TrimSpace(trackID) == "" {
		return defaultName
	}

	track, err := plat.GetTrack(ctx, trackID)
	if err != nil || track == nil {
		return defaultName
	}

	artists := make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		name := strings.TrimSpace(artist.Name)
		if name != "" {
			artists = append(artists, name)
		}
	}
	artistJoined := strings.Join(artists, "/")
	title := strings.TrimSpace(track.Title)
	if artistJoined == "" && title == "" {
		return defaultName
	}
	if artistJoined == "" {
		return sanitizeFileName(title + ".lrc")
	}
	if title == "" {
		return sanitizeFileName(strings.ReplaceAll(artistJoined, "/", ",") + ".lrc")
	}
	return sanitizeFileName(fmt.Sprintf("%s - %s.lrc", strings.ReplaceAll(artistJoined, "/", ","), title))
}

func formatDuration(d time.Duration) string {
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	centis := int((d % time.Second) / (10 * time.Millisecond))
	return fmt.Sprintf("%02d:%02d.%02d", minutes, seconds, centis)
}
