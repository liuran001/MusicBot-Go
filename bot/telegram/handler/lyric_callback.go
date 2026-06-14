package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	lyricpkg "github.com/liuran001/MusicBot-Go/bot/lyric"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// lyricRenderState captures everything needed to render a lyric document: the
// target format plus the translation/roma toggle choices. explicitFlags marks
// whether the toggles came from the user (vs. format defaults).
type lyricRenderState struct {
	format             string
	includeTranslation bool
	includeRoma        bool
	explicitFlags      bool
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

// lyricCallbackPayload holds the data needed to re-render a lyric in another
// format. It is stored in a TTL store and referenced by a short token, since
// platform+trackID+format+requester can exceed Telegram's 64-byte callback
// data limit.
type lyricCallbackPayload struct {
	platformName string
	trackID      string
	requesterID  int64
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

// buildLyricFormatKeyboard builds the format-switch keyboard plus translation/
// roma toggle buttons. The current format is marked with "•". Callback data is
// "lyric f <fmt> <flags> <token>" where flags is a 2-char on/off pair for
// translation and roma (e.g. "10"). Switching format preserves the toggles;
// the toggle buttons flip their own flag and re-render the current format.
func buildLyricFormatKeyboard(platformName, trackID string, state lyricRenderState, requesterID int64) *telego.InlineKeyboardMarkup {
	token := storeLyricCallbackPayload(lyricCallbackPayload{platformName: platformName, trackID: trackID, requesterID: requesterID})
	if token == "" {
		return nil
	}
	current := lyricpkg.NormalizeFormat(state.format)
	flags := encodeLyricFlags(state.includeTranslation, state.includeRoma)

	rows := make([][]telego.InlineKeyboardButton, 0, len(lyricFormatRows)+1)
	for _, row := range lyricFormatRows {
		buttons := make([]telego.InlineKeyboardButton, 0, len(row))
		for _, format := range row {
			label := lyricFormatButtonLabels[format]
			if label == "" {
				label = strings.ToUpper(format)
			}
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
	if lyricFormatSupportsSideTracks(current) {
		toggles := make([]telego.InlineKeyboardButton, 0, 2)
		transLabel := "翻译: 关"
		if state.includeTranslation {
			transLabel = "翻译: 开"
		}
		transData := fmt.Sprintf("lyric f %s %s %s", current, encodeLyricFlags(!state.includeTranslation, state.includeRoma), token)
		if len(transData) <= 64 {
			toggles = append(toggles, telego.InlineKeyboardButton{Text: transLabel, CallbackData: transData})
		}
		romaLabel := "罗马音: 关"
		if state.includeRoma {
			romaLabel = "罗马音: 开"
		}
		romaData := fmt.Sprintf("lyric f %s %s %s", current, encodeLyricFlags(state.includeTranslation, !state.includeRoma), token)
		if len(romaData) <= 64 {
			toggles = append(toggles, telego.InlineKeyboardButton{Text: romaLabel, CallbackData: romaData})
		}
		if len(toggles) > 0 {
			rows = append(rows, toggles)
		}
	}

	if len(rows) == 0 {
		return nil
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
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
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
}

func (h *LyricCallbackHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	args := strings.Fields(query.Data)
	// Expected: "lyric f <format> <flags> <token>".
	if len(args) < 5 || args[0] != "lyric" || args[1] != "f" {
		h.answer(ctx, b, query.ID, "")
		return
	}
	format := lyricpkg.NormalizeFormat(args[2])
	includeTranslation, includeRoma, flagsOK := decodeLyricFlags(args[3])
	token := args[4]

	payload, ok := lyricCallbackPayloads.Load(token)
	if !ok {
		h.answer(ctx, b, query.ID, "按钮已过期，请重新发送 /lyric")
		return
	}

	// Restrict switching to the original requester when known.
	if payload.requesterID != 0 && query.From.ID != payload.requesterID {
		h.answer(ctx, b, query.ID, "只有发起者可以切换格式")
		return
	}

	state := lyricRenderState{format: format, includeTranslation: includeTranslation, includeRoma: includeRoma, explicitFlags: flagsOK}

	chatID, messageID, replyToID, ok := lyricCallbackMessageTarget(query)
	if !ok {
		h.answer(ctx, b, query.ID, "")
		return
	}

	// Guard against duplicate concurrent processing of the same message.
	guardKey := fmt.Sprintf("lyricfmt:%d:%d", chatID, messageID)
	release, acquired := tryAcquireCallbackInFlight(guardKey, 10*time.Second)
	if !acquired {
		h.answer(ctx, b, query.ID, "处理中，请稍候")
		return
	}
	defer release()

	if h.PlatformManager == nil {
		h.answer(ctx, b, query.ID, getLrcFailed)
		return
	}
	plat := h.PlatformManager.Get(payload.platformName)
	if plat == nil || !plat.SupportsLyrics() {
		h.answer(ctx, b, query.ID, "此平台不支持获取歌词")
		return
	}

	h.answer(ctx, b, query.ID, "正在生成 "+lyricFormatDisplayName(format))

	lyrics, err := plat.GetLyrics(ctx, payload.trackID)
	if err != nil || lyrics == nil {
		h.sendError(ctx, b, chatID, replyToID, getLrcFailed)
		return
	}

	lh := &LyricHandler{PlatformManager: h.PlatformManager, RateLimiter: h.RateLimiter}
	baseName := lh.buildLyricBaseName(ctx, plat, payload.trackID)
	lh.editLyricDocumentState(ctx, b, chatID, messageID, replyToID, lyrics, baseName, payload.platformName, payload.trackID, state, payload.requesterID)
}

// lyricCallbackMessageTarget resolves where to update the lyric document. It
// returns the document message itself (chatID + messageID) to edit in place,
// plus replyToID — the document's own reply target (the original command) used
// only when an in-place edit fails and the document must be deleted and resent.
func lyricCallbackMessageTarget(query *telego.CallbackQuery) (chatID int64, messageID, replyToID int, ok bool) {
	if query == nil || query.Message == nil {
		return 0, 0, 0, false
	}
	msg := query.Message.Message()
	if msg == nil {
		return 0, 0, 0, false
	}
	replyToID = 0
	if msg.ReplyToMessage != nil {
		replyToID = msg.ReplyToMessage.MessageID
	}
	return msg.Chat.ID, msg.MessageID, replyToID, true
}

func (h *LyricCallbackHandler) answer(ctx context.Context, b *telego.Bot, callbackQueryID, text string) {
	params := &telego.AnswerCallbackQueryParams{CallbackQueryID: callbackQueryID}
	if text != "" {
		params.Text = text
	}
	_ = b.AnswerCallbackQuery(ctx, params)
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
