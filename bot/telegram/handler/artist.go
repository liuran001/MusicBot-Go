package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

type artistDetailProvider interface {
	GetArtistDetails(ctx context.Context, artistID string) (*platform.Artist, int, error)
}

type ArtistHandler struct {
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
	Logger          interface {
		Warn(msg string, keysAndValues ...any)
	}
}

func (h *ArtistHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	_ = h.TryHandle(ctx, b, update)
}

func (h *ArtistHandler) TryHandle(ctx context.Context, b *telego.Bot, update *telego.Update) bool {
	if update == nil || update.Message == nil || strings.TrimSpace(update.Message.Text) == "" {
		return false
	}
	message := update.Message
	text := message.Text
	args := commandArguments(text)
	if args != "" {
		text = args
	} else if strings.HasPrefix(strings.TrimSpace(text), "/") {
		return false
	}
	baseText, _, _ := parseTrailingOptions(text, h.PlatformManager)
	baseText = strings.TrimSpace(baseText)
	if baseText == "" {
		return false
	}
	platformName, artistID, ok := matchArtistURL(ctx, h.PlatformManager, baseText)
	if !ok {
		return false
	}
	if message.Chat.Type != "private" && !isAllowedGroupURLPlatform(platformName, h.PlatformManager) {
		return false
	}
	if h.PlatformManager == nil {
		sendText(ctx, b, message.Chat.ID, message.MessageID, noResults)
		return true
	}
	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		sendText(ctx, b, message.Chat.ID, message.MessageID, noResults)
		return true
	}

	artist, trackCount, err := h.fetchArtist(ctx, plat, artistID)
	if err != nil {
		sendText(ctx, b, message.Chat.ID, message.MessageID, userVisibleArtistError(err))
		return true
	}
	if artist == nil {
		sendText(ctx, b, message.Chat.ID, message.MessageID, noResults)
		return true
	}

	textOut := formatArtistMessage(h.PlatformManager, platformName, artist, trackCount)
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		MessageThreadID: message.MessageThreadID,
		Text:            textOut,
		ReplyParameters: buildReplyParams(message),
		LinkPreviewOptions: &telego.LinkPreviewOptions{
			IsDisabled: strings.TrimSpace(artist.AvatarURL) == "",
			URL:        strings.TrimSpace(artist.AvatarURL),
		},
	}
	if h.RateLimiter != nil {
		if _, err := telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params); err != nil && h.Logger != nil {
			h.Logger.Warn("failed to send artist message", "chatID", message.Chat.ID, "error", err)
		}
	} else {
		if _, err := b.SendMessage(ctx, params); err != nil && h.Logger != nil {
			h.Logger.Warn("failed to send artist message", "chatID", message.Chat.ID, "error", err)
		}
	}
	return true
}

func (h *ArtistHandler) fetchArtist(ctx context.Context, plat platform.Platform, artistID string) (*platform.Artist, int, error) {
	if provider, ok := plat.(artistDetailProvider); ok {
		return provider.GetArtistDetails(ctx, artistID)
	}
	artist, err := plat.GetArtist(ctx, artistID)
	return artist, 0, err
}

func formatArtistMessage(manager platform.Manager, platformName string, artist *platform.Artist, trackCount int) string {
	if artist == nil {
		return noResults
	}
	name := strings.TrimSpace(artist.Name)
	if name == "" {
		name = "未知歌手"
	}
	platformText := platformDisplayName(manager, platformName)
	var lines []string
	lines = append(lines, fmt.Sprintf("%s 歌手信息", platformEmoji(manager, platformName)))
	lines = append(lines, fmt.Sprintf("平台：%s", platformText))
	lines = append(lines, fmt.Sprintf("歌手：%s", name))
	if url := strings.TrimSpace(artist.URL); url != "" {
		lines = append(lines, fmt.Sprintf("链接：%s", url))
	}
	if trackCount > 0 {
		lines = append(lines, fmt.Sprintf("代表作品数：%d", trackCount))
	}
	if avatar := strings.TrimSpace(artist.AvatarURL); avatar != "" {
		lines = append(lines, fmt.Sprintf("头像：%s", avatar))
	}
	return strings.Join(lines, "\n")
}

func userVisibleArtistError(err error) string {
	if err == nil {
		return noResults
	}
	if errors.Is(err, platform.ErrNotFound) {
		return "未找到歌手"
	}
	if errors.Is(err, platform.ErrUnsupported) {
		return "当前平台暂不支持歌手信息解析"
	}
	if errors.Is(err, platform.ErrUnavailable) {
		return "歌手信息暂时不可用，请稍后重试"
	}
	return noResults
}
