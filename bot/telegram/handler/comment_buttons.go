package handler

import (
	"context"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// CommentButtonsHandler reposts the song action buttons (lyrics / favorite /
// group favorite) into a channel's linked discussion group.
//
// Background: when a channel post carrying an inline keyboard is automatically
// forwarded into the channel's linked discussion group, Telegram strips the
// keyboard from the forwarded copy. So a song the bot posted to the channel
// loses its buttons in the comment thread. This handler detects that forwarded
// copy (IsAutomaticForward + an audio caption ending in "via @<bot>"), recovers
// the track, and replies in the comment thread with a fresh keyboard.
//
// The buttons run in an inline-style context: the lyrics button deep-links to
// the bot's private chat (the bot generally is not configured to download a new
// lyrics message into a discussion group), while favorites work via callback.
type CommentButtonsHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
	BotName         string
	Logger          botpkg.Logger
}

func (h *CommentButtonsHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if h == nil || update == nil || update.Message == nil {
		return
	}
	message := update.Message

	// Only the auto-forwarded copy in a discussion group is relevant.
	if !message.IsAutomaticForward {
		return
	}
	if message.Chat.Type != "group" && message.Chat.Type != "supergroup" {
		return
	}
	if message.Audio == nil {
		return
	}

	// The caption must look like one the bot produced ("via @<bot>"), otherwise
	// this is someone else's audio post and we must not touch it.
	if !h.captionFromBot(message) {
		return
	}

	chatID := message.Chat.ID
	if !resolveCommentButtonsEnabled(ctx, h.Repo, chatID) {
		return
	}

	platformName, trackID, trackURL, quality := h.resolveTrack(ctx, message)
	if platformName == "" || trackID == "" {
		// Reverse lookup failed (no recoverable URL / file id). Silently skip:
		// reposting an empty or partial button row would be worse than nothing.
		return
	}

	opts := songButtonOptions{
		platformName:  platformName,
		trackID:       trackID,
		trackURL:      trackURL,
		quality:       quality,
		botName:       h.BotName,
		inlineContext: true,
		chatID:        chatID,
		isGroup:       true,
	}
	keyboard := buildSongBottomKeyboard(ctx, h.Repo, opts)
	if keyboard == nil || len(keyboard.InlineKeyboard) == 0 {
		return
	}

	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: chatID},
		Text:            tr(ctx, "comment_buttons_text"),
		ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
		ReplyMarkup:     keyboard,
	}
	var err error
	if h.RateLimiter != nil {
		_, err = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, err = b.SendMessage(ctx, params)
	}
	if err != nil && h.Logger != nil {
		h.Logger.Warn("failed to repost comment buttons", "chat_id", chatID, "error", err)
	}
}

// captionFromBot reports whether the message caption carries this bot's
// "via @<bot>" signature appended by buildMusicCaption.
func (h *CommentButtonsHandler) captionFromBot(message *telego.Message) bool {
	name := strings.TrimPrefix(strings.TrimSpace(h.BotName), "@")
	if name == "" {
		return false
	}
	caption := message.Caption
	if strings.TrimSpace(caption) == "" {
		return false
	}
	needle := "via @" + name
	return strings.Contains(strings.ToLower(caption), strings.ToLower(needle))
}

// resolveTrack recovers the platform, track ID, canonical URL, and quality for
// the forwarded song. It prefers an exact cache hit by audio file ID (which
// yields full metadata including quality); failing that it falls back to the
// track URL embedded as a text_link entity in the caption (the song title is a
// clickable link to TrackURL in buildMusicCaption).
func (h *CommentButtonsHandler) resolveTrack(ctx context.Context, message *telego.Message) (platformName, trackID, trackURL, quality string) {
	if h.Repo != nil && message.Audio != nil && message.Audio.FileID != "" {
		if song, err := h.Repo.FindByFileID(ctx, message.Audio.FileID); err == nil && song != nil {
			return song.Platform, song.TrackID, song.TrackURL, song.Quality
		}
	}

	if h.PlatformManager == nil {
		return "", "", "", ""
	}
	for _, candidate := range captionURLCandidates(message) {
		if plat, id, matched := h.PlatformManager.MatchURL(candidate); matched {
			return plat, id, candidate, ""
		}
	}
	return "", "", "", ""
}

// captionURLCandidates returns URLs found in the caption: text_link entity URLs
// first (the title/artist/album links produced by buildMusicCaption), then any
// bare URLs in the caption text.
func captionURLCandidates(message *telego.Message) []string {
	var urls []string
	seen := make(map[string]struct{})
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	for _, entity := range message.CaptionEntities {
		if entity.Type == "text_link" && entity.URL != "" {
			add(entity.URL)
		}
	}
	for _, u := range extractURLs(message.Caption) {
		add(u)
	}
	return urls
}
