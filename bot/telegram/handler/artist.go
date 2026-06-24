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
		sendText(ctx, b, message.Chat.ID, message.MessageID, userVisibleArtistError(ctx, err))
		return true
	}
	if artist == nil {
		sendText(ctx, b, message.Chat.ID, message.MessageID, noResults)
		return true
	}

	textOut := formatArtistMessage(ctx, h.PlatformManager, platformName, artist, trackCount)
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

func formatArtistMessage(ctx context.Context, manager platform.Manager, platformName string, artist *platform.Artist, trackCount int) string {
	if artist == nil {
		return noResults
	}
	name := strings.TrimSpace(artist.Name)
	if name == "" {
		name = tr(ctx, "guest_artist_unknown")
	}
	platformText := platformDisplayName(manager, platformName)
	var lines []string
	lines = append(lines, fmt.Sprintf("%s %s", platformEmoji(manager, platformName), tr(ctx, "guest_artist_info")))
	lines = append(lines, tr(ctx, "guest_artist_platform_label", map[string]any{"Platform": platformText}))
	lines = append(lines, tr(ctx, "guest_artist_name_label", map[string]any{"Name": name}))
	if url := strings.TrimSpace(artist.URL); url != "" {
		lines = append(lines, tr(ctx, "guest_artist_link_label", map[string]any{"URL": url}))
	}
	if trackCount > 0 {
		lines = append(lines, tr(ctx, "guest_artist_track_count_label", map[string]any{"Count": trackCount}))
	}
	if avatar := strings.TrimSpace(artist.AvatarURL); avatar != "" {
		lines = append(lines, tr(ctx, "guest_artist_avatar_label", map[string]any{"URL": avatar}))
	}
	return strings.Join(lines, "\n")
}

func userVisibleArtistError(ctx context.Context, err error) string {
	if err == nil {
		return noResults
	}
	if errors.Is(err, platform.ErrNotFound) {
		return tr(ctx, "guest_artist_not_found")
	}
	if errors.Is(err, platform.ErrUnsupported) {
		return tr(ctx, "guest_artist_unsupported")
	}
	if errors.Is(err, platform.ErrUnavailable) {
		return tr(ctx, "guest_artist_unavailable")
	}
	return noResults
}
