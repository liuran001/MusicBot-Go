package handler

import (
	"context"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

// MentionRouter handles "<keyword> @bot" messages in chats the bot HAS joined.
//
// In guest mode (bot not a member) the same "@bot ..." phrasing is delivered as
// a GuestMessage and handled by GuestModeHandler. But once the bot is a member,
// the message arrives as a normal Message and none of the command/URL/keyword
// routes match a plain "<keyword> @bot", so it was silently ignored. This router
// restores parity: it detects an @mention of the bot, strips it, and dispatches
// the remaining content using the SAME keyword classification as guest mode —
// lyric / recognize / song-or-search — but replying with normal messages instead
// of inline edits.
//
// Telegram clients trigger inline mode when "@bot" leads a message, so users
// usually put the mention LAST ("晴天 @bot"). stripBotMention removes the mention
// at any position, so both orders work.
type MentionRouter struct {
	Music           MessageHandler
	Search          *SearchHandler
	Lyric           MessageHandler
	Recognize       MessageHandler
	Favorites       *FavoritesHandler
	PlatformManager platform.Manager
	BotName         string
}

// mentionsBot reports whether message @mentions this bot, returning the message
// content with the mention removed. It mirrors stripBotMention's matching but
// additionally tells the caller whether a mention was actually present, so a
// message that merely contains text is not hijacked.
func (r *MentionRouter) mentionsBot(message *telego.Message) (content string, ok bool) {
	if message == nil || strings.TrimSpace(message.Text) == "" {
		return "", false
	}
	botName := strings.TrimPrefix(strings.TrimSpace(r.BotName), "@")
	if botName == "" {
		return "", false
	}
	// Confirm a real mention before stripping: stripBotMention returns the text
	// unchanged when nothing matched, which is indistinguishable from a no-op
	// strip, so check the token's presence explicitly.
	if !mentionTokenPresent(message.Text, botName) {
		return "", false
	}
	stripped := stripBotMention(message.Text, message.Entities, botName)
	return strings.TrimSpace(stripped), true
}

// mentionTokenPresent reports whether "@botname" appears as a standalone token in
// text (case-insensitive). Used to confirm a mention even when stripping happens
// to leave the trimmed text identical (e.g. a trailing mention with no payload).
func mentionTokenPresent(text, botName string) bool {
	mentionLower := "@" + strings.ToLower(strings.TrimPrefix(botName, "@"))
	lower := strings.ToLower(text)
	idx := strings.Index(lower, mentionLower)
	for idx >= 0 {
		end := idx + len(mentionLower)
		if end >= len(text) || !isUsernameByte(text[end]) {
			return true
		}
		next := strings.Index(lower[end:], mentionLower)
		if next < 0 {
			break
		}
		idx = end + next
	}
	return false
}

// Handle dispatches a "<keyword> @bot" message to the matching feature handler,
// classifying exactly like GuestModeHandler.Handle: voice-reply / 识曲 → recognize,
// 歌词 → lyric, otherwise → song-or-search.
func (r *MentionRouter) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil {
		return
	}
	message := update.Message
	content, ok := r.mentionsBot(message)
	if !ok {
		return
	}

	// Replying to a voice with just "@bot" (no payload) triggers recognition,
	// same as guest mode.
	if content == "" && message.ReplyToMessage != nil && message.ReplyToMessage.Voice != nil {
		r.dispatchRecognize(ctx, b, message)
		return
	}
	if isShazamKeyword(content) {
		r.dispatchRecognize(ctx, b, message)
		return
	}

	// With no payload, fall back to the replied message's link/text/caption.
	if content == "" {
		content = repliedMessageQuery(message.ReplyToMessage)
	}
	if strings.TrimSpace(content) == "" {
		// A bare "@bot" with nothing to act on shows the favorites list (group
		// favorites by default in a group), matching the guest-mode behavior.
		if r.Favorites != nil {
			requesterID := int64(0)
			if message.From != nil {
				requesterID = message.From.ID
			}
			groupChatID := int64(0)
			isGroupChat := false
			if message.Chat.Type != "private" {
				groupChatID = message.Chat.ID
				isGroupChat = true
			}
			view := r.Favorites.defaultListView(ctx, groupChatID, isGroupChat)
			r.Favorites.sendListMessage(ctx, b, message.Chat.ID, message.MessageID, requesterID, groupChatID, isGroupChat, view)
			return
		}
		sendText(ctx, b, message.Chat.ID, message.MessageID, inputContent)
		return
	}

	// A mention buried mid-sentence ("aaa @bot bbb", with query text on BOTH
	// sides) is usually conversational, not addressed to the bot. Act on it only
	// when it carries a track/playlist link or ID (then download it); otherwise
	// stay silent instead of running a search. Leading/trailing mentions
	// ("@bot 晴天" / "晴天 @bot" / "晴天 @bot qq") are unaffected.
	if r.mentionSurrounded(message) {
		if r.isDirectDownload(ctx, content) {
			r.dispatchMusic(ctx, b, message, content)
		}
		return
	}

	if isLyricKeyword(content) {
		keyword := strings.TrimSpace(strings.Replace(content, "歌词", "", 1))
		if keyword == "" {
			keyword = repliedMessageQuery(message.ReplyToMessage)
		}
		r.dispatchLyric(ctx, b, message, keyword)
		return
	}

	r.dispatchSong(ctx, b, message, content)
}

// mentionSurrounded reports whether the bot @mention sits mid-sentence — i.e.
// there is query text on BOTH sides of it. A trailing segment that is only
// platform/quality options (e.g. "晴天 @bot qq") counts as trailing, not
// surrounding, so such searches still work.
func (r *MentionRouter) mentionSurrounded(message *telego.Message) bool {
	if message == nil {
		return false
	}
	before, after, found := splitAroundMention(message.Text, message.Entities, r.BotName)
	if !found {
		return false
	}
	if strings.TrimSpace(before) == "" || strings.TrimSpace(after) == "" {
		return false
	}
	if r.PlatformManager != nil {
		if base, _, _ := parseTrailingOptions(strings.TrimSpace(after), r.PlatformManager); strings.TrimSpace(base) == "" {
			return false
		}
	}
	return true
}

// isDirectDownload reports whether content resolves to a track/playlist URL or a
// direct ID (i.e. dispatchSong would download rather than search). Used to let a
// mid-sentence mention through only when it carries a download link.
func (r *MentionRouter) isDirectDownload(ctx context.Context, content string) bool {
	if r.PlatformManager == nil {
		return false
	}
	baseText, _, _ := parseTrailingOptions(content, r.PlatformManager)
	baseText = strings.TrimSpace(baseText)
	if baseText == "" {
		return false
	}
	resolvedText := resolveShortLinkText(ctx, r.PlatformManager, baseText)
	if _, _, matched := matchPlaylistURL(ctx, r.PlatformManager, resolvedText); matched {
		return true
	}
	if urlStr := extractFirstURL(resolvedText); urlStr != "" {
		if _, _, matched := r.PlatformManager.MatchURL(urlStr); matched {
			return true
		}
	}
	if _, _, matched := r.PlatformManager.MatchText(resolvedText); matched {
		return true
	}
	if _, _, matched := r.PlatformManager.MatchURL(resolvedText); matched {
		return true
	}
	return false
}

// dispatchRecognize forwards the original message to the recognize handler, which
// reads the replied voice itself.
func (r *MentionRouter) dispatchRecognize(ctx context.Context, b *telego.Bot, message *telego.Message) {
	if r.Recognize == nil {
		sendText(ctx, b, message.Chat.ID, message.MessageID, tr(ctx, "guest_recognize_service_unavailable"))
		return
	}
	r.Recognize.Handle(ctx, b, &telego.Update{Message: message})
}

// dispatchLyric rewrites the message into "/lyric <keyword>" and forwards it to
// the lyric handler, reusing its full URL/ID/search-delegation flow.
func (r *MentionRouter) dispatchLyric(ctx context.Context, b *telego.Bot, message *telego.Message, keyword string) {
	if r.Lyric == nil {
		return
	}
	r.Lyric.Handle(ctx, b, &telego.Update{Message: rewriteCommandMessage(message, "/lyric", keyword)})
}

// dispatchSong matches the guest song flow: a URL/ID resolves directly to a
// download (via the music handler), otherwise the keyword goes to a search list.
func (r *MentionRouter) dispatchSong(ctx context.Context, b *telego.Bot, message *telego.Message, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		sendText(ctx, b, message.Chat.ID, message.MessageID, inputContent)
		return
	}
	if r.PlatformManager != nil {
		baseText, _, _ := parseTrailingOptions(content, r.PlatformManager)
		resolvedText := resolveShortLinkText(ctx, r.PlatformManager, baseText)
		if _, _, matched := r.PlatformManager.MatchText(resolvedText); matched {
			r.dispatchMusic(ctx, b, message, content)
			return
		}
		if _, _, matched := r.PlatformManager.MatchURL(resolvedText); matched {
			r.dispatchMusic(ctx, b, message, content)
			return
		}
	}
	if r.Search != nil {
		r.Search.runSearch(ctx, b, message, content, "music")
		return
	}
	r.dispatchMusic(ctx, b, message, content)
}

// dispatchMusic rewrites the message into "/music <content>" and forwards it to
// the music handler.
func (r *MentionRouter) dispatchMusic(ctx context.Context, b *telego.Bot, message *telego.Message, content string) {
	if r.Music == nil {
		return
	}
	r.Music.Handle(ctx, b, &telego.Update{Message: rewriteCommandMessage(message, "/music", content)})
}

// rewriteCommandMessage returns a shallow copy of message whose Text is rewritten
// to "<command> <args>" (with no bot_command entity, since downstream handlers
// parse the leading slash by string). The copy avoids mutating the original
// update that other routes may still inspect.
func rewriteCommandMessage(message *telego.Message, command, args string) *telego.Message {
	if message == nil {
		return nil
	}
	clone := *message
	text := command
	if strings.TrimSpace(args) != "" {
		text = command + " " + strings.TrimSpace(args)
	}
	clone.Text = text
	// The rewritten text is a synthetic slash command; the original entities
	// (mention offsets) no longer apply and would mislead commandArguments.
	clone.Entities = nil
	return &clone
}
