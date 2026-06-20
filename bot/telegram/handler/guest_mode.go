package handler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf16"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/recognize"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// GuestModeHandler handles guest messages: a bot that is NOT a member of a chat
// can still be @mentioned there and reply via answerGuestQuery (Bot API 10.0).
//
// API shape that drives the whole design:
//   - A guest update carries a Message with a GuestQueryID.
//   - The bot replies with answerGuestQuery(GuestQueryID, ONE InlineQueryResult)
//     and gets back a SentGuestMessage{InlineMessageID}.
//   - That inline_message_id can then be edited (EditMessageText / Media /
//     ReplyMarkup) exactly like an inline-mode message.
//
// So guest mode reuses the existing inline machinery: we answer with a single
// placeholder article, capture its inline_message_id, then drive the SAME
// search-menu / download-and-edit-to-audio flow used by inline/chosen-inline —
// instead of inventing a separate plain-text reply format.
type GuestModeHandler struct {
	PlatformManager  platform.Manager
	Music            *MusicHandler
	LyricHandler     *LyricHandler
	SearchHandler    *SearchHandler
	RateLimiter      *telegram.RateLimiter
	RecognizeService recognize.Service
	CacheDir         string
	DownloadBot      *telego.Bot
	BotName          string
	DefaultPlatform  string
	FallbackPlatform string
	DefaultQuality   string

	search *guestSearchStore
	// searchOnce 保证 search store 懒初始化在并发回调下只发生一次，避免多个
	// goroutine 各建一个 store 互相覆盖（丢状态 + 指针 race）。
	searchOnce sync.Once
}

func (h *GuestModeHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.GuestMessage == nil {
		return
	}
	message := update.GuestMessage
	guestQueryID := strings.TrimSpace(message.GuestQueryID)
	if guestQueryID == "" {
		return
	}

	// Strip the @bot mention precisely using entities (UTF-16 offsets), so the
	// username never leaks into the search keyword.
	content := strings.TrimSpace(stripBotMention(message.Text, message.Entities, h.BotName))

	// When the mention carries no payload, fall back to the replied message's
	// embedded link / text / caption: "回复一条消息 + @bot" should act on that
	// message (e.g. replying to a bot-sent song message links to its track URL).
	if content == "" {
		content = strings.TrimSpace(repliedMessageQuery(message.ReplyToMessage))
	}

	// Replying to a voice message with @bot (no text) triggers recognition
	// directly, without needing a "识曲" keyword.
	if content == "" && message.ReplyToMessage != nil && message.ReplyToMessage.Voice != nil {
		h.handleGuestRecognize(ctx, b, message, guestQueryID)
		return
	}

	// Recognition is keyed off the literal keyword and needs the replied voice,
	// so route it before the empty-content guard.
	if isShazamKeyword(content) {
		h.handleGuestRecognize(ctx, b, message, guestQueryID)
		return
	}

	if content == "" {
		h.answerGuest(ctx, b, guestQueryID, "请输入歌曲名或链接")
		return
	}

	switch {
	case isLyricKeyword(content):
		h.handleGuestLyric(ctx, b, message, content, guestQueryID)
	default:
		h.handleGuestSong(ctx, b, message, content, guestQueryID)
	}
}

// stripBotMention removes the bot's @mention from text. It prefers Telegram's
// message entities (authoritative, UTF-16 offsets) and falls back to a
// case-insensitive string search when entities are unavailable. Matching is
// case-insensitive because Telegram usernames are.
func stripBotMention(text string, entities []telego.MessageEntity, botName string) string {
	if text == "" {
		return text
	}
	botName = strings.TrimPrefix(strings.TrimSpace(botName), "@")
	if botName == "" {
		return text
	}
	mentionLower := "@" + strings.ToLower(botName)

	// Entity-based precise removal.
	units := utf16.Encode([]rune(text))
	for _, entity := range entities {
		if entity.Type != telego.EntityTypeMention {
			continue
		}
		if entity.Offset < 0 || entity.Length <= 0 || entity.Offset+entity.Length > len(units) {
			continue
		}
		seg := string(utf16.Decode(units[entity.Offset : entity.Offset+entity.Length]))
		if strings.ToLower(strings.TrimSpace(seg)) != mentionLower {
			continue
		}
		before := string(utf16.Decode(units[:entity.Offset]))
		after := string(utf16.Decode(units[entity.Offset+entity.Length:]))
		return strings.TrimSpace(joinMentionGap(before, after))
	}

	// Fallback: case-insensitive search for "@botname" as a whole token.
	lower := strings.ToLower(text)
	idx := strings.Index(lower, mentionLower)
	for idx >= 0 {
		end := idx + len(mentionLower)
		// Ensure the next char isn't a username continuation (so "@bot2" of a
		// different bot isn't matched as "@bot").
		if end >= len(text) || !isUsernameByte(text[end]) {
			return strings.TrimSpace(joinMentionGap(text[:idx], text[end:]))
		}
		next := strings.Index(lower[end:], mentionLower)
		if next < 0 {
			break
		}
		idx = end + next
	}
	return text
}

// joinMentionGap rejoins the text around a removed mention, collapsing the
// surrounding whitespace into a single space when both sides are non-empty.
func joinMentionGap(before, after string) string {
	before = strings.TrimRight(before, " \t\n\r")
	after = strings.TrimLeft(after, " \t\n\r")
	if before == "" {
		return after
	}
	if after == "" {
		return before
	}
	return before + " " + after
}

func isUsernameByte(c byte) bool {
	return c == '_' ||
		(c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z')
}

// repliedMessageText extracts usable text from a replied-to message: its text,
// or its caption when the message is media.
func repliedMessageText(reply *telego.Message) string {
	if reply == nil {
		return ""
	}
	if t := strings.TrimSpace(reply.Text); t != "" {
		return t
	}
	return strings.TrimSpace(reply.Caption)
}

// repliedMessageQuery resolves a music/lyric query from a replied-to message. It
// prefers an embedded link from the message's entities (a bot-sent song message
// hyperlinks the title to the track URL, which is far more precise than its
// plain-text caption "「title」- artist"), then a Telegram auto-detected URL
// entity, falling back to the visible text or caption otherwise.
func repliedMessageQuery(reply *telego.Message) string {
	if reply == nil {
		return ""
	}
	entities := reply.Entities
	if len(entities) == 0 {
		entities = reply.CaptionEntities
	}
	// 1. Embedded link (entity carries an explicit URL, e.g. bot-sent song cards).
	for _, entity := range entities {
		if entity.Type == telego.EntityTypeTextLink {
			if u := strings.TrimSpace(entity.URL); u != "" {
				return u
			}
		}
	}
	// 2. Telegram auto-detected plain-text URL entity (e.g. a playlist link
	//    pasted in a group message).  Extract the URL substring so callers that
	//    match URLs don't receive surrounding chat text.
	text := repliedMessageText(reply)
	for _, entity := range entities {
		if entity.Type == telego.EntityTypeURL {
			if entity.Offset >= 0 && entity.Length > 0 {
				units := utf16.Encode([]rune(text))
				if end := entity.Offset + entity.Length; end <= len(units) {
					if u := strings.TrimSpace(string(utf16.Decode(units[entity.Offset:end]))); u != "" {
						return u
					}
				}
			}
		}
	}
	// 3. Fallback: full visible text / caption.
	return text
}

// answerGuest sends a single plain-text article in response to a guest query.
// Used for terminal states (errors, prompts) where there's nothing to download.
func (h *GuestModeHandler) answerGuest(ctx context.Context, b *telego.Bot, guestQueryID, text string) string {
	if strings.TrimSpace(text) == "" {
		text = "MusicBot-Go"
	}
	article := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  nextGuestResultID("resp"),
		Title:               text,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: text},
	}
	sent, err := b.AnswerGuestQuery(ctx, &telego.AnswerGuestQueryParams{
		GuestQueryID: guestQueryID,
		Result:       article,
	})
	if err != nil || sent == nil {
		return ""
	}
	return sent.InlineMessageID
}

// answerGuestPlaceholder answers the guest query with a placeholder article and
// returns the inline_message_id of the sent message, which can then be edited
// into a search menu or audio result.
func (h *GuestModeHandler) answerGuestPlaceholder(ctx context.Context, b *telego.Bot, guestQueryID, title string) string {
	if strings.TrimSpace(title) == "" {
		title = waitForDown
	}
	article := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  nextGuestResultID("pending"),
		Title:               title,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: title},
	}
	sent, err := b.AnswerGuestQuery(ctx, &telego.AnswerGuestQueryParams{
		GuestQueryID: guestQueryID,
		Result:       article,
	})
	if err != nil || sent == nil {
		return ""
	}
	return strings.TrimSpace(sent.InlineMessageID)
}

// guestResultIDCounter makes each answerGuestQuery result ID unique so that a
// bursty sequence of guest replies never reuses the same inline result ID.
var guestResultIDCounter uint64

func nextGuestResultID(prefix string) string {
	return fmt.Sprintf("guest_%s_%d", prefix, atomic.AddUint64(&guestResultIDCounter, 1))
}

func isShazamKeyword(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == "听歌识曲" || trimmed == "识曲"
}

func isLyricKeyword(text string) bool {
	return strings.Contains(strings.TrimSpace(text), "歌词")
}
