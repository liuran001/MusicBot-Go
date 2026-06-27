package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	lyricpkg "github.com/liuran001/MusicBot-Go/bot/lyric"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// lyricRenderState captures everything needed to render a lyric document: the
// target format plus the translation/roma toggle choices. explicitFlags marks
// whether the toggles came from the user (vs. format defaults).
//
// showSettings controls the keyboard layout below the document. When false
// (the default for a fresh /lyric), only a single "更换歌词格式" entry button is
// shown. After the user taps it the full format grid + toggles are revealed.
// defaultFormat is the persisted per-scope default; the "保存为默认" button is
// offered whenever the chosen format differs from it, and disappears once they
// match (e.g. right after saving).
type lyricRenderState struct {
	format             string
	defaultFormat      string
	includeTranslation bool
	includeRoma        bool
	explicitFlags      bool
	showSettings       bool
}

// lyricFormatRows defines the format buttons shown under a lyric document,
// grouped into rows. Order favors the most-used formats first.
var lyricFormatRows = [][]string{
	{"lrc", "yrc", "qrc"},
	{"lys", "krc", "elrc"},
	{"spl", "ass", "ttml"},
	{"lqe", "amjson", "srt"},
	{"txt", "trans", "roma"},
}

// lyricFormatButtonLabels maps a format to its short button label.
var lyricFormatButtonLabels = map[string]string{
	"lrc": "LRC", "yrc": "YRC", "qrc": "QRC", "lys": "LYS", "krc": "KRC",
	"elrc": "ELRC", "spl": "SPL", "ass": "ASS", "ttml": "TTML",
	"lqe": "LQE", "amjson": "AM-JSON", "srt": "SRT", "txt": "TXT",
	"trans": "翻译", "roma": "罗马音",
}

// lyricFormatButtonLabel returns the localized short button label for a format.
// The translation/romaji labels are localized via the catalog; every other
// format keeps its language-neutral identifier from lyricFormatButtonLabels
// (shared with the /settings lyric-format menu), falling back to the uppercased
// token for unknown formats.
func lyricFormatButtonLabel(ctx context.Context, format string) string {
	switch format {
	case "trans":
		return tr(ctx, "lyr_trans")
	case "roma":
		return tr(ctx, "lyr_roma")
	}
	if label := lyricFormatButtonLabels[format]; label != "" {
		return label
	}
	return strings.ToUpper(format)
}

// lyricCallbackPayload holds the data needed to re-render a lyric in another
// format. It is stored in a TTL store and referenced by a short token, since
// platform+trackID+format+requester can exceed Telegram's 64-byte callback
// data limit.
//
// defaultFormat snapshots the saved per-scope default at render time, so the
// callback can decide whether the "保存为默认" button should appear without
// inflating the callback data.
type lyricCallbackPayload struct {
	platformName  string
	trackID       string
	requesterID   int64
	defaultFormat string
}

var lyricCallbackPayloads = newTTLStore[lyricCallbackPayload](30 * time.Minute)
var lyricCallbackTokenCounter uint64

func storeLyricCallbackPayload(payload lyricCallbackPayload) string {
	payload.platformName = strings.TrimSpace(payload.platformName)
	payload.trackID = strings.TrimSpace(payload.trackID)
	if payload.platformName == "" || payload.trackID == "" {
		return ""
	}
	token := strconv.FormatUint(uint64(time.Now().UnixNano()), 36) + strconv.FormatUint(atomic.AddUint64(&lyricCallbackTokenCounter, 1), 36)
	lyricCallbackPayloads.Store(token, payload)
	return token
}

// buildLyricFormatKeyboard builds the keyboard shown under a lyric document. It
// has three layouts driven by lyricRenderState:
//
//   - Collapsed (showSettings == false): a single "🎼 更换歌词格式" entry button
//     that, when tapped, only swaps the keyboard for the full grid (the document
//     above is untouched). Callback data: "lyric o <fmt> <flags> <token>".
//   - Expanded (showSettings == true): the full format grid (current marked "•")
//     plus translation/roma toggles for side-track formats. Format buttons emit
//     "lyric f ..."; toggles emit "lyric t ..." so toggling does not by itself
//     count as a format change.
//   - Expanded + changed: when the displayed format differs from the saved
//     default, an extra "💾 保存为默认歌词格式" button is appended, emitting
//     "lyric d <fmt> <flags> <token>". After saving (default == current) it
//     disappears.
//
// flags is a 2-char on/off pair for translation and roma (e.g. "10"). The token
// references a stored payload snapshotting platform/track/requester plus the
// saved default format, so the callback can derive the next state within
// Telegram's 64-byte callback-data limit.
func buildLyricFormatKeyboard(ctx context.Context, platformName, trackID string, state lyricRenderState, requesterID int64) *telego.InlineKeyboardMarkup {
	current := lyricpkg.NormalizeFormat(state.format)
	defaultFormat := lyricpkg.NormalizeFormat(state.defaultFormat)
	token := storeLyricCallbackPayload(lyricCallbackPayload{
		platformName:  platformName,
		trackID:       trackID,
		requesterID:   requesterID,
		defaultFormat: defaultFormat,
	})
	if token == "" {
		return nil
	}
	flags := encodeLyricFlags(state.includeTranslation, state.includeRoma)

	// Collapsed: a single entry button that expands into the format grid.
	if !state.showSettings {
		data := fmt.Sprintf("lyric o %s %s %s", current, flags, token)
		if len(data) > 64 {
			return nil
		}
		return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{
			{Text: tr(ctx, "lyr_change_format"), CallbackData: data},
		}}}
	}

	rows := make([][]telego.InlineKeyboardButton, 0, len(lyricFormatRows)+2)
	for _, row := range lyricFormatRows {
		buttons := make([]telego.InlineKeyboardButton, 0, len(row))
		for _, format := range row {
			label := lyricFormatButtonLabel(ctx, format)
			data := fmt.Sprintf("lyric f %s %s %s", format, flags, token)
			if format == current {
				label = "• " + label
			}
			if len(data) > 64 {
				continue
			}
			buttons = append(buttons, telego.InlineKeyboardButton{Text: label, CallbackData: data})
		}
		if len(buttons) > 0 {
			rows = append(rows, buttons)
		}
	}

	// Translation/roma toggles, shown only for formats that can carry side tracks.
	// Toggles use the "t" verb (distinct from format switches) and re-render the
	// same format, so the save-default button follows the format, not the toggle.
	if lyricFormatSupportsSideTracks(current) {
		toggles := make([]telego.InlineKeyboardButton, 0, 2)
		transLabel := tr(ctx, "lyr_toggle_trans_off")
		if state.includeTranslation {
			transLabel = tr(ctx, "lyr_toggle_trans_on")
		}
		transData := fmt.Sprintf("lyric t %s %s %s", current, encodeLyricFlags(!state.includeTranslation, state.includeRoma), token)
		if len(transData) <= 64 {
			toggles = append(toggles, telego.InlineKeyboardButton{Text: transLabel, CallbackData: transData})
		}
		romaLabel := tr(ctx, "lyr_toggle_roma_off")
		if state.includeRoma {
			romaLabel = tr(ctx, "lyr_toggle_roma_on")
		}
		romaData := fmt.Sprintf("lyric t %s %s %s", current, encodeLyricFlags(state.includeTranslation, !state.includeRoma), token)
		if len(romaData) <= 64 {
			toggles = append(toggles, telego.InlineKeyboardButton{Text: romaLabel, CallbackData: romaData})
		}
		if len(toggles) > 0 {
			rows = append(rows, toggles)
		}
	}

	// Save-as-default button: shown only once the displayed format differs from
	// the saved default. Tapping it persists the default; the next render has
	// default == current, so the button disappears.
	if lyricFormatShowSaveDefault(current, defaultFormat) {
		data := fmt.Sprintf("lyric d %s %s %s", current, flags, token)
		if len(data) <= 64 {
			rows = append(rows, []telego.InlineKeyboardButton{
				{Text: tr(ctx, "lyr_save_default"), CallbackData: data},
			})
		}
	}

	if len(rows) == 0 {
		return nil
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// lyricFormatShowSaveDefault reports whether the "保存为默认" button should appear:
// when the displayed format differs from the saved default. When the default is
// unknown (empty), assume it differs so the user can still save.
func lyricFormatShowSaveDefault(current, defaultFormat string) bool {
	if defaultFormat == "" {
		return current != ""
	}
	return current != defaultFormat
}

// lyricFormatSupportsSideTracks reports whether a format can merge translation/
// roma side-tracks (and therefore should show the toggle buttons).
func lyricFormatSupportsSideTracks(format string) bool {
	switch lyricpkg.NormalizeFormat(format) {
	case "lrc", "spl", "ass", "lqe", "ttml", "amjson":
		return true
	}
	return false
}

// encodeLyricFlags packs the translation/roma toggles into a 2-char string.
func encodeLyricFlags(includeTranslation, includeRoma bool) string {
	b := []byte{'0', '0'}
	if includeTranslation {
		b[0] = '1'
	}
	if includeRoma {
		b[1] = '1'
	}
	return string(b)
}

// decodeLyricFlags unpacks a 2-char flags string. ok is false when the string
// is not exactly two 0/1 chars (so callers can fall back to format defaults).
func decodeLyricFlags(s string) (includeTranslation, includeRoma, ok bool) {
	if len(s) != 2 || (s[0] != '0' && s[0] != '1') || (s[1] != '0' && s[1] != '1') {
		return false, false, false
	}
	return s[0] == '1', s[1] == '1', true
}

// LyricCallbackHandler handles the lyric format-switch buttons.
type LyricCallbackHandler struct {
	PlatformManager  platform.Manager
	RateLimiter      *telegram.RateLimiter
	Repo             botpkg.SongRepository
	DefaultPlatform  string
	FallbackPlatform string
	// InlineUploadChatID and UploadBot are needed to re-render the lyric document
	// for format switches on inline messages (guest mode), where the new file
	// must be uploaded first to obtain a file_id before EditMessageMedia.
	InlineUploadChatID int64
	UploadBot          *telego.Bot
}

func (h *LyricCallbackHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	args := strings.Fields(query.Data)
	if len(args) < 2 || args[0] != "lyric" {
		h.answer(ctx, b, query.ID, "")
		return
	}
	verb := args[1]
	switch verb {
	case "o", "f", "t", "d":
		// Format-switch / toggle / save-default callbacks.
		h.handleFormatCallback(ctx, b, query, args)
	default:
		// Search-result lyric callback: "lyric <platform> <trackID> <quality> <requesterID>"
		h.handleSearchResultCallback(ctx, b, query, args)
	}
}

// handleSearchResultCallback handles a search-result lyric button click.
// Format: "lyric <platform> <trackID> <quality> <requesterID>"
func (h *LyricCallbackHandler) handleSearchResultCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	// args[0]="lyric", args[1]=platform, args[2]=trackID, args[3]=quality, args[4]=requesterID
	if len(args) < 4 {
		h.answer(ctx, b, query.ID, "")
		return
	}
	platformName := strings.TrimSpace(args[1])
	trackID := strings.TrimSpace(args[2])
	requesterID := int64(0)
	if len(args) >= 5 {
		requesterID, _ = strconv.ParseInt(strings.TrimSpace(args[4]), 10, 64)
	}
	if requesterID != 0 && query.From.ID != requesterID {
		h.answer(ctx, b, query.ID, tr(ctx, "guest_lyric_only_requester_get"))
		return
	}
	if h.PlatformManager == nil {
		h.answer(ctx, b, query.ID, "")
		return
	}
	plat := h.PlatformManager.Get(platformName)
	if plat == nil || !plat.SupportsLyrics() {
		h.answer(ctx, b, query.ID, tr(ctx, "guest_platform_no_lyrics"))
		return
	}

	chatID, messageID, replyToID, inlineMessageID, ok := lyricCallbackMessageTarget(query)
	if !ok {
		h.answer(ctx, b, query.ID, "")
		return
	}

	h.answer(ctx, b, query.ID, tr(ctx, "guest_lyric_fetching"))

	lyrics, err := plat.GetLyrics(ctx, trackID)
	if err != nil || lyrics == nil {
		if inlineMessageID != "" {
			h.editInlineError(ctx, b, inlineMessageID, tr(ctx, "get_lrc_failed"))
		} else {
			h.sendError(ctx, b, chatID, replyToID, tr(ctx, "get_lrc_failed"))
		}
		return
	}

	lh := h.newLyricHandler()
	baseName := lh.buildLyricBaseName(ctx, plat, trackID)
	defaultFormat := lh.resolveDefaultLyricFormat(ctx, &telego.Message{Chat: telego.Chat{ID: chatID}, From: &telego.User{ID: requesterID}})
	format := defaultFormat
	state := lyricRenderState{
		format:             format,
		defaultFormat:      defaultFormat,
		includeTranslation: lyricFormatDefaultTranslation(format),
		includeRoma:        false,
		showSettings:       false,
	}

	if inlineMessageID != "" {
		lh.editLyricDocumentInlineState(ctx, b, inlineMessageID, lyrics, baseName, platformName, trackID, state, requesterID)
		return
	}
	lh.sendLyricDocumentState(ctx, b, chatID, messageID, lyrics, baseName, platformName, trackID, state, requesterID)
}

// handleFormatCallback handles the format-switch/toggle/save-default callbacks.
// Expected: "lyric <verb> <format> <flags> <token>"
func (h *LyricCallbackHandler) handleFormatCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	if len(args) < 5 {
		h.answer(ctx, b, query.ID, "")
		return
	}
	verb := args[1]
	format := lyricpkg.NormalizeFormat(args[2])
	includeTranslation, includeRoma, flagsOK := decodeLyricFlags(args[3])
	token := args[4]

	payload, ok := lyricCallbackPayloads.Load(token)
	if !ok {
		h.answer(ctx, b, query.ID, tr(ctx, "guest_lyric_button_expired"))
		return
	}

	// Restrict switching to the original requester when known.
	if payload.requesterID != 0 && query.From.ID != payload.requesterID {
		h.answer(ctx, b, query.ID, tr(ctx, "guest_lyric_only_requester_switch"))
		return
	}

	chatID, messageID, replyToID, inlineMessageID, ok := lyricCallbackMessageTarget(query)
	if !ok {
		h.answer(ctx, b, query.ID, "")
		return
	}

	defaultFormat := lyricpkg.NormalizeFormat(payload.defaultFormat)
	if defaultFormat == "" {
		defaultFormat = h.resolveDefaultLyricFormat(ctx, query)
	}

	state := lyricRenderState{
		format:             format,
		defaultFormat:      defaultFormat,
		includeTranslation: includeTranslation,
		includeRoma:        includeRoma,
		explicitFlags:      flagsOK,
		showSettings:       true,
	}

	// Guard against duplicate concurrent processing of the same message. Inline
	// messages key off inline_message_id; normal messages off chat+message.
	guardKey := fmt.Sprintf("lyricfmt:%d:%d", chatID, messageID)
	if inlineMessageID != "" {
		guardKey = "lyricfmt:inline:" + inlineMessageID
	}
	release, acquired := tryAcquireCallbackInFlight(guardKey, 10*time.Second)
	if !acquired {
		h.answer(ctx, b, query.ID, tr(ctx, "guest_lyric_processing"))
		return
	}
	defer release()

	switch verb {
	case "o":
		// Expand the keyboard only — the document above is untouched. Toggles
		// start from the format's defaults unless the user already set them.
		if !flagsOK {
			state.includeTranslation = lyricFormatDefaultTranslation(format)
			state.includeRoma = false
		}
		if inlineMessageID != "" {
			h.replaceInlineKeyboard(ctx, b, inlineMessageID, payload.platformName, payload.trackID, state, payload.requesterID)
		} else {
			h.replaceKeyboard(ctx, b, chatID, messageID, payload.platformName, payload.trackID, state, payload.requesterID)
		}
		h.answer(ctx, b, query.ID, "")
		return
	case "d":
		if inlineMessageID != "" {
			// Saving a per-scope default needs the chat context, which an inline
			// message doesn't carry; just acknowledge without persisting.
			h.answer(ctx, b, query.ID, "")
			return
		}
		h.handleSaveDefault(ctx, b, query, chatID, messageID, payload, state)
		return
	}

	// "f" / "t": re-render the document in the new format/toggle state.
	if h.PlatformManager == nil {
		h.answer(ctx, b, query.ID, tr(ctx, "get_lrc_failed"))
		return
	}
	plat := h.PlatformManager.Get(payload.platformName)
	if plat == nil || !plat.SupportsLyrics() {
		h.answer(ctx, b, query.ID, tr(ctx, "guest_platform_no_lyrics"))
		return
	}

	h.answer(ctx, b, query.ID, tr(ctx, "guest_lyric_generating", map[string]any{"Format": lyricFormatDisplayName(ctx, format)}))

	lyrics, err := plat.GetLyrics(ctx, payload.trackID)
	if err != nil || lyrics == nil {
		if inlineMessageID != "" {
			h.editInlineError(ctx, b, inlineMessageID, tr(ctx, "get_lrc_failed"))
		} else {
			h.sendError(ctx, b, chatID, replyToID, tr(ctx, "get_lrc_failed"))
		}
		return
	}

	lh := h.newLyricHandler()
	baseName := lh.buildLyricBaseName(ctx, plat, payload.trackID)
	if inlineMessageID != "" {
		lh.editLyricDocumentInlineState(ctx, b, inlineMessageID, lyrics, baseName, payload.platformName, payload.trackID, state, payload.requesterID)
		return
	}
	lh.editLyricDocumentState(ctx, b, chatID, messageID, replyToID, lyrics, baseName, payload.platformName, payload.trackID, state, payload.requesterID)
}

// replaceKeyboard swaps just the inline keyboard under the lyric document for
// the one implied by state, leaving the document content untouched. Used by the
// collapsed→expanded transition and after saving a default.
func (h *LyricCallbackHandler) replaceKeyboard(ctx context.Context, b *telego.Bot, chatID int64, messageID int, platformName, trackID string, state lyricRenderState, requesterID int64) {
	keyboard := buildLyricFormatKeyboard(ctx, platformName, trackID, state, requesterID)
	params := &telego.EditMessageReplyMarkupParams{
		ChatID:      telego.ChatID{ID: chatID},
		MessageID:   messageID,
		ReplyMarkup: keyboard,
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageReplyMarkupWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageReplyMarkup(ctx, params)
	}
}

// newLyricHandler builds a LyricHandler carrying the same platform/rate-limit/
// repo plus the inline-upload context, so document edits work for both normal
// and inline (guest) messages.
func (h *LyricCallbackHandler) newLyricHandler() *LyricHandler {
	return &LyricHandler{
		PlatformManager:    h.PlatformManager,
		RateLimiter:        h.RateLimiter,
		Repo:               h.Repo,
		InlineUploadChatID: h.InlineUploadChatID,
		UploadBot:          h.UploadBot,
	}
}

// replaceInlineKeyboard swaps just the inline keyboard under an inline lyric
// document (guest mode), leaving the document content untouched.
func (h *LyricCallbackHandler) replaceInlineKeyboard(ctx context.Context, b *telego.Bot, inlineMessageID, platformName, trackID string, state lyricRenderState, requesterID int64) {
	keyboard := buildLyricFormatKeyboard(ctx, platformName, trackID, state, requesterID)
	params := &telego.EditMessageReplyMarkupParams{
		InlineMessageID: inlineMessageID,
		ReplyMarkup:     keyboard,
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageReplyMarkupWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageReplyMarkup(ctx, params)
	}
}

// editInlineError replaces an inline message with a plain-text error. Inline
// messages have no reply target, so there is nothing to delete-and-resend.
func (h *LyricCallbackHandler) editInlineError(ctx context.Context, b *telego.Bot, inlineMessageID, text string) {
	lh := h.newLyricHandler()
	lh.editInlineLyricError(ctx, b, inlineMessageID, text)
}

// handleSaveDefault persists the currently-shown format as the per-scope default
// (user settings in private chats, group settings in groups, gated by admin),
// then hides the save button by re-rendering the keyboard without it.
func (h *LyricCallbackHandler) handleSaveDefault(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, chatID int64, messageID int, payload lyricCallbackPayload, state lyricRenderState) {
	if h.Repo == nil {
		h.answer(ctx, b, query.ID, tr(ctx, "guest_lyric_save_default_failed_nosave"))
		return
	}
	msg := query.Message.Message()
	if msg == nil {
		h.answer(ctx, b, query.ID, "")
		return
	}
	format := lyricpkg.NormalizeFormat(state.format)

	if msg.Chat.Type != "private" {
		if !isRequesterOrAdmin(ctx, b, chatID, query.From.ID, 0) {
			h.answerAlert(ctx, b, query.ID, tr(ctx, "guest_lyric_admin_only_group_default"))
			return
		}
		settings, err := h.Repo.GetGroupSettings(ctx, chatID)
		if err != nil || settings == nil {
			h.answerAlert(ctx, b, query.ID, tr(ctx, "guest_lyric_save_default_failed"))
			return
		}
		if settings.DefaultLyricFormat != format {
			settings.DefaultLyricFormat = format
			if err := h.Repo.UpdateGroupSettings(ctx, settings); err != nil {
				h.answerAlert(ctx, b, query.ID, tr(ctx, "guest_lyric_save_default_failed"))
				return
			}
		}
	} else {
		settings, err := h.Repo.GetUserSettings(ctx, query.From.ID)
		if err != nil || settings == nil {
			h.answerAlert(ctx, b, query.ID, tr(ctx, "guest_lyric_save_default_failed"))
			return
		}
		if settings.DefaultLyricFormat != format {
			settings.DefaultLyricFormat = format
			if err := h.Repo.UpdateUserSettings(ctx, settings); err != nil {
				h.answerAlert(ctx, b, query.ID, tr(ctx, "guest_lyric_save_default_failed"))
				return
			}
		}
	}

	// The chosen format is now the default, so the save button disappears.
	state.defaultFormat = format
	h.replaceKeyboard(ctx, b, chatID, messageID, payload.platformName, payload.trackID, state, payload.requesterID)
	h.answer(ctx, b, query.ID, tr(ctx, "guest_lyric_saved_default", map[string]any{"Format": lyricFormatDisplayName(ctx, format)}))
}

// resolveDefaultLyricFormat resolves the per-scope default lyric format for a
// callback query: group settings in groups, user settings in private chats,
// falling back to the configured default and finally "lrc".
func (h *LyricCallbackHandler) resolveDefaultLyricFormat(ctx context.Context, query *telego.CallbackQuery) string {
	format := "lrc"
	if h.Repo == nil || query == nil || query.Message == nil {
		return format
	}
	msg := query.Message.Message()
	if msg == nil {
		return format
	}
	if msg.Chat.Type != "private" {
		if settings, err := h.Repo.GetGroupSettings(ctx, msg.Chat.ID); err == nil && settings != nil {
			if f := strings.TrimSpace(settings.DefaultLyricFormat); f != "" {
				format = f
			}
		}
		return lyricpkg.NormalizeFormat(format)
	}
	if settings, err := h.Repo.GetUserSettings(ctx, query.From.ID); err == nil && settings != nil {
		if f := strings.TrimSpace(settings.DefaultLyricFormat); f != "" {
			format = f
		}
	}
	return lyricpkg.NormalizeFormat(format)
}

// lyricCallbackMessageTarget resolves where to update the lyric document. For a
// normal message it returns the document message itself (chatID + messageID) to
// edit in place, plus replyToID — the document's own reply target (the original
// command) used only when an in-place edit fails and the document must be deleted
// and resent. For an inline message (guest mode) only inlineMessageID is set;
// chatID/messageID/replyToID are zero and edits go through EditMessageMedia by
// inline_message_id instead.
func lyricCallbackMessageTarget(query *telego.CallbackQuery) (chatID int64, messageID, replyToID int, inlineMessageID string, ok bool) {
	if query == nil {
		return 0, 0, 0, "", false
	}
	if id := strings.TrimSpace(query.InlineMessageID); id != "" {
		return 0, 0, 0, id, true
	}
	if query.Message == nil {
		return 0, 0, 0, "", false
	}
	msg := query.Message.Message()
	if msg == nil {
		return 0, 0, 0, "", false
	}
	replyToID = 0
	if msg.ReplyToMessage != nil {
		replyToID = msg.ReplyToMessage.MessageID
	}
	return msg.Chat.ID, msg.MessageID, replyToID, "", true
}

func (h *LyricCallbackHandler) answer(ctx context.Context, b *telego.Bot, callbackQueryID, text string) {
	params := &telego.AnswerCallbackQueryParams{CallbackQueryID: callbackQueryID}
	if text != "" {
		params.Text = text
	}
	_ = b.AnswerCallbackQuery(ctx, params)
}

func (h *LyricCallbackHandler) answerAlert(ctx context.Context, b *telego.Bot, callbackQueryID, text string) {
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: callbackQueryID,
		Text:            text,
		ShowAlert:       true,
	})
}

func (h *LyricCallbackHandler) sendError(ctx context.Context, b *telego.Bot, chatID int64, replyToID int, text string) {
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: chatID},
		Text:            text,
		ReplyParameters: &telego.ReplyParameters{MessageID: replyToID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}
