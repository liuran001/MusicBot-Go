package handler

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"strings"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	lyricpkg "github.com/liuran001/MusicBot-Go/bot/lyric"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

const lyricCaptionMaxChars = 1000

// LyricHandler handles /lyric command.
type LyricHandler struct {
	PlatformManager  platform.Manager
	RateLimiter      *telegram.RateLimiter
	Repo             botpkg.SongRepository
	DefaultPlatform  string
	FallbackPlatform string
	SearchHandler    *SearchHandler
	// InlineUploadChatID and UploadBot let the lyric document be rendered into an
	// inline message (guest mode / inline format switch). Telegram forbids
	// uploading a new file when editing an inline message, so the document is
	// first uploaded to InlineUploadChatID to obtain a reusable file_id.
	InlineUploadChatID int64
	UploadBot          *telego.Bot
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
			Text:            inputLyricContent,
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
		// Reply with bare "/lyric": use the replied message's embedded link
		// (e.g. a bot-sent song message links the title to its track URL) or its
		// text/caption as the lyric query.
		args = repliedMessageQuery(message.ReplyToMessage)
		if args == "" {
			return
		}
	}

	// A trailing token may request a specific lyric format, e.g. "/lyric <id> ttml".
	args, format, explicitFormat := parseTrailingLyricFormatExplicit(args)

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
		// Not a URL/ID — treat the argument as a song name.
		if h.SearchHandler != nil {
			// Delegate to SearchHandler with "lyric" action so the user gets
			// a full result list; the "fetching" message is no longer needed.
			deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msgResult.Chat.ID}, MessageID: msgResult.MessageID}
			if h.RateLimiter != nil {
				_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
			} else {
				_ = b.DeleteMessage(ctx, deleteParams)
			}
			h.SearchHandler.runSearch(ctx, b, message, args, "lyric")
			return
		}
		// Fallback: search the default platform and use the first result's lyrics.
		platformName, trackID, found = h.searchFirstTrackForLyric(ctx, message, args)
	}
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

	baseName := h.buildLyricBaseName(ctx, plat, trackID)

	// Delete the "fetching" status message before sending the document.
	deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msgResult.Chat.ID}, MessageID: msgResult.MessageID}
	if h.RateLimiter != nil {
		_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
	} else {
		_ = b.DeleteMessage(ctx, deleteParams)
	}

	requesterID := int64(0)
	if message.From != nil {
		requesterID = message.From.ID
	}
	// The per-scope default lyric format drives the initial render (when no
	// explicit trailing format was given) and the "保存为默认" comparison.
	defaultFormat := h.resolveDefaultLyricFormat(ctx, message)
	if !explicitFormat {
		format = defaultFormat
	}
	// Initial translation/roma toggles come from the persisted per-scope defaults,
	// falling back to the per-format default when unset.
	includeTranslation, includeRoma := h.resolveDefaultLyricFlags(ctx, message, format)
	h.sendLyricDocument(ctx, b, message.Chat.ID, message.MessageID, lyrics, baseName, platformName, trackID, format, defaultFormat, includeTranslation, includeRoma, requesterID)
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

// searchFirstTrackForLyric searches the default platform for the given song
// name and returns the first result's platform/trackID. It is the fallback for
// "/lyric <name>" where the argument is neither a URL nor a track ID.
func (h *LyricHandler) searchFirstTrackForLyric(ctx context.Context, message *telego.Message, query string) (platformName, trackID string, found bool) {
	if h.PlatformManager == nil {
		return "", "", false
	}
	keyword := strings.TrimSpace(query)
	if keyword == "" {
		return "", "", false
	}

	// Allow an explicit trailing platform/quality token, e.g. "lemon qq".
	keyword, requestedPlatform, _ := parseTrailingOptions(keyword, h.PlatformManager)
	if strings.TrimSpace(keyword) == "" {
		return "", "", false
	}

	primaryPlatform := h.resolveDefaultPlatform(ctx, message)
	fallbackPlatform := strings.TrimSpace(h.FallbackPlatform)
	if fallbackPlatform == "" {
		fallbackPlatform = "netease"
	}
	if strings.TrimSpace(requestedPlatform) != "" {
		primaryPlatform = requestedPlatform
		fallbackPlatform = ""
	}

	tracks, matchedPlatform, _, err := searchTracksWithFallback(ctx, h.PlatformManager, primaryPlatform, fallbackPlatform, keyword, nil, true)
	if err != nil || len(tracks) == 0 {
		return "", "", false
	}
	first := tracks[0]
	resolvedPlatform := strings.TrimSpace(first.Platform)
	if resolvedPlatform == "" {
		resolvedPlatform = matchedPlatform
	}
	if resolvedPlatform == "" || strings.TrimSpace(first.ID) == "" {
		return "", "", false
	}
	return resolvedPlatform, first.ID, true
}

// resolveDefaultPlatform resolves the lyric search platform: configured default,
// then group/user settings from the repository (mirroring the search handler).
func (h *LyricHandler) resolveDefaultPlatform(ctx context.Context, message *telego.Message) string {
	platformName := strings.TrimSpace(h.DefaultPlatform)
	if platformName == "" {
		platformName = "netease"
	}
	if h.Repo == nil || message == nil {
		return platformName
	}
	if message.Chat.Type != "private" {
		if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultPlatform) != "" {
				platformName = settings.DefaultPlatform
			}
		}
		return platformName
	}
	if message.From != nil {
		if settings, err := h.Repo.GetUserSettings(ctx, message.From.ID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultPlatform) != "" {
				platformName = settings.DefaultPlatform
			}
		}
	}
	return platformName
}

// parseTrailingLyricFormat strips a recognized trailing format token (e.g.
// "ttml", "yrc", "qrc") from the argument text, returning the remaining text
// and the resolved format. When no format token is present it returns the
// original text and "lrc".
func parseTrailingLyricFormat(text string) (rest, format string) {
	rest, format, _ = parseTrailingLyricFormatExplicit(text)
	return rest, format
}

// parseTrailingLyricFormatExplicit is parseTrailingLyricFormat plus an explicit
// flag reporting whether a format token was actually present. Callers use it to
// fall back to the per-scope default format when the user did not specify one.
func parseTrailingLyricFormatExplicit(text string) (rest, format string, explicit bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", "lrc", false
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 2 {
		return trimmed, "lrc", false
	}
	last := strings.ToLower(fields[len(fields)-1])
	if isKnownLyricFormat(last) {
		resolved := lyricpkg.NormalizeFormat(last)
		rest = strings.TrimSpace(strings.Join(fields[:len(fields)-1], " "))
		return rest, resolved, true
	}
	return trimmed, "lrc", false
}

// lyricFormatAliases maps user-typed format tokens to their canonical form.
// Only tokens here are accepted as a trailing /lyric format argument.
var lyricFormatAliases = map[string]bool{
	"lrc": true, "yrc": true, "qrc": true, "lys": true, "krc": true,
	"elrc": true, "lrcx": true, "alrc": true, "enhancedlrc": true,
	"spl": true, "ass": true, "lqe": true, "ttml": true,
	"amjson": true, "applemusicjson": true, "srt": true, "txt": true,
	"trans": true, "roma": true,
}

func isKnownLyricFormat(token string) bool {
	return lyricFormatAliases[strings.ToLower(strings.TrimSpace(token))]
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

// SendTrackLyrics fetches and sends a track's lyrics as a document to chatID,
// exactly like /lyric. It backs the inline/guest "歌词" button, which deep-links
// to the bot's private chat (?start=lyric_<platform>_<trackID>) because those
// contexts cannot post a new message to the originating chat. It is also the
// handler for the lyric_ /start deep link.
func (h *LyricHandler) SendTrackLyrics(ctx context.Context, b *telego.Bot, chatID int64, replyToMessageID int, platformName, trackID string, requesterID int64) {
	if h == nil || b == nil || h.PlatformManager == nil {
		return
	}
	plat := h.PlatformManager.Get(platformName)
	if plat == nil || !plat.SupportsLyrics() {
		sendText(ctx, b, chatID, replyToMessageID, "此平台不支持获取歌词")
		return
	}
	lyrics, err := plat.GetLyrics(ctx, trackID)
	if err != nil || lyrics == nil {
		sendText(ctx, b, chatID, replyToMessageID, getLrcFailed)
		return
	}
	baseName := h.buildLyricBaseName(ctx, plat, trackID)
	defaultFormat := h.resolveDefaultLyricFormat(ctx, &telego.Message{Chat: telego.Chat{ID: chatID}, From: &telego.User{ID: requesterID}})
	state := lyricRenderState{
		format:             defaultFormat,
		defaultFormat:      defaultFormat,
		includeTranslation: lyricFormatDefaultTranslation(defaultFormat),
		includeRoma:        false,
		showSettings:       false,
	}
	h.sendLyricDocumentState(ctx, b, chatID, replyToMessageID, lyrics, baseName, platformName, trackID, state, requesterID)
}

// sendLyricDocument is the initial entry from the /lyric command. It renders
// with a collapsed keyboard (only the "更换歌词格式" entry button). The initial
// translation/roma toggles are resolved by the caller (persisted per-scope
// defaults, falling back to per-format defaults). defaultFormat is the persisted
// per-scope default, carried so the format-switch keyboard can later decide when
// to offer "保存为默认".
func (h *LyricHandler) sendLyricDocument(ctx context.Context, b *telego.Bot, chatID int64, replyToMessageID int, lyrics *platform.Lyrics, baseName, platformName, trackID, format, defaultFormat string, includeTranslation, includeRoma bool, requesterID int64) {
	state := lyricRenderState{
		format:             lyricpkg.NormalizeFormat(format),
		defaultFormat:      lyricpkg.NormalizeFormat(defaultFormat),
		includeTranslation: includeTranslation,
		includeRoma:        includeRoma,
		showSettings:       false,
	}
	h.sendLyricDocumentState(ctx, b, chatID, replyToMessageID, lyrics, baseName, platformName, trackID, state, requesterID)
}

// resolveDefaultLyricFlags resolves the initial translation/roma toggles for a
// /lyric render: the persisted per-scope side-track defaults when set, otherwise
// the per-format defaults (document formats default translation on; roma always
// defaults off). A nil stored pointer means "unset".
func (h *LyricHandler) resolveDefaultLyricFlags(ctx context.Context, message *telego.Message, format string) (includeTranslation, includeRoma bool) {
	includeTranslation = lyricFormatDefaultTranslation(format)
	includeRoma = false
	if h.Repo == nil || message == nil {
		return includeTranslation, includeRoma
	}
	var transPtr, romaPtr *bool
	if message.Chat.Type != "private" {
		if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
			transPtr = settings.DefaultLyricIncludeTranslation
			romaPtr = settings.DefaultLyricIncludeRoma
		}
	} else if message.From != nil {
		if settings, err := h.Repo.GetUserSettings(ctx, message.From.ID); err == nil && settings != nil {
			transPtr = settings.DefaultLyricIncludeTranslation
			romaPtr = settings.DefaultLyricIncludeRoma
		}
	}
	if transPtr != nil {
		includeTranslation = *transPtr
	}
	if romaPtr != nil {
		includeRoma = *romaPtr
	}
	return includeTranslation, includeRoma
}

// resolveDefaultLyricFormat resolves the per-scope default lyric format: group
// settings in groups, user settings in private chats, falling back to "lrc".
func (h *LyricHandler) resolveDefaultLyricFormat(ctx context.Context, message *telego.Message) string {
	format := "lrc"
	if h.Repo == nil || message == nil {
		return format
	}
	if message.Chat.Type != "private" {
		if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
			if f := strings.TrimSpace(settings.DefaultLyricFormat); f != "" {
				format = f
			}
		}
		return lyricpkg.NormalizeFormat(format)
	}
	if message.From != nil {
		if settings, err := h.Repo.GetUserSettings(ctx, message.From.ID); err == nil && settings != nil {
			if f := strings.TrimSpace(settings.DefaultLyricFormat); f != "" {
				format = f
			}
		}
	}
	return lyricpkg.NormalizeFormat(format)
}

// lyricRenderedDoc holds the rendered artifacts for a lyric document: the temp
// file (owned by the caller, who must os.Remove it), its display name, the
// caption, and the format-switch keyboard.
type lyricRenderedDoc struct {
	filePath string
	fileName string
	caption  string
	keyboard *telego.InlineKeyboardMarkup
}

// renderLyricDocument converts the lyrics per the render state and writes a temp
// file, returning everything needed to send or edit the document message. The
// caller owns filePath and must os.Remove it. ok is false if writing failed.
func (h *LyricHandler) renderLyricDocument(lyrics *platform.Lyrics, baseName, platformName, trackID string, state lyricRenderState, requesterID int64) (lyricRenderedDoc, bool) {
	payload := lyricPayloadFrom(lyrics, platformName)
	resolved := lyricpkg.NormalizeFormat(state.format)

	includeTranslation := state.includeTranslation
	opts := lyricpkg.Options{
		IncludeTranslation: &includeTranslation,
		IncludeRoma:        state.includeRoma,
	}

	content := lyricpkg.Convert(payload, resolved, opts)
	if strings.TrimSpace(content) == "" {
		// Fall back to plain LRC if the requested format yielded nothing.
		resolved = "lrc"
		state.format = "lrc"
		content = lyricpkg.Convert(payload, "lrc", opts)
	}
	if strings.TrimSpace(content) == "" {
		content = "暂无歌词信息\n"
	}

	fileName := buildLyricFileNameForFormat(baseName, resolved)
	filePath, err := writeLyricTempFile(content, fileName)
	if err != nil {
		return lyricRenderedDoc{}, false
	}

	return lyricRenderedDoc{
		filePath: filePath,
		fileName: fileName,
		caption:  buildLyricCaption(payload, content, state),
		keyboard: buildLyricFormatKeyboard(platformName, trackID, state, requesterID),
	}, true
}

// sendLyricDocumentState converts the lyrics per the render state, sends the
// file with a caption showing the current format/toggles, and attaches the
// format + translation/roma toggle keyboard.
func (h *LyricHandler) sendLyricDocumentState(ctx context.Context, b *telego.Bot, chatID int64, replyToMessageID int, lyrics *platform.Lyrics, baseName, platformName, trackID string, state lyricRenderState, requesterID int64) {
	doc, ok := h.renderLyricDocument(lyrics, baseName, platformName, trackID, state, requesterID)
	if !ok {
		h.sendLyricFallbackError(ctx, b, chatID, replyToMessageID)
		return
	}
	defer os.Remove(doc.filePath)

	file, err := os.Open(doc.filePath)
	if err != nil {
		h.sendLyricFallbackError(ctx, b, chatID, replyToMessageID)
		return
	}
	defer file.Close()

	docParams := &telego.SendDocumentParams{
		ChatID:      telego.ChatID{ID: chatID},
		Document:    telego.InputFile{File: telegoutil.NameReader(file, doc.fileName)},
		ReplyMarkup: doc.keyboard,
	}
	if replyToMessageID > 0 {
		docParams.ReplyParameters = &telego.ReplyParameters{MessageID: replyToMessageID}
	}
	if doc.caption != "" {
		docParams.Caption = doc.caption
		docParams.ParseMode = telego.ModeHTML
	}

	var sendErr error
	if h.RateLimiter != nil {
		_, sendErr = telegram.SendDocumentWithRetry(ctx, h.RateLimiter, b, docParams)
	} else {
		_, sendErr = b.SendDocument(ctx, docParams)
	}

	if sendErr != nil && doc.caption != "" {
		docParams.Caption = ""
		docParams.ParseMode = ""
		if h.RateLimiter != nil {
			_, _ = telegram.SendDocumentWithRetry(ctx, h.RateLimiter, b, docParams)
		} else {
			_, _ = b.SendDocument(ctx, docParams)
		}
	}
}

// editLyricDocumentState re-renders the lyric document for a new format/toggle
// state and edits the existing message in place (file + caption + keyboard) via
// EditMessageMedia. When the edit fails for any reason other than a no-op
// "message is not modified", it deletes the old document and sends a fresh one
// so the user always ends up seeing the requested format.
func (h *LyricHandler) editLyricDocumentState(ctx context.Context, b *telego.Bot, chatID int64, messageID, fallbackReplyToID int, lyrics *platform.Lyrics, baseName, platformName, trackID string, state lyricRenderState, requesterID int64) {
	doc, ok := h.renderLyricDocument(lyrics, baseName, platformName, trackID, state, requesterID)
	if !ok {
		h.sendLyricFallbackError(ctx, b, chatID, fallbackReplyToID)
		return
	}
	defer os.Remove(doc.filePath)

	file, err := os.Open(doc.filePath)
	if err != nil {
		h.sendLyricFallbackError(ctx, b, chatID, fallbackReplyToID)
		return
	}
	defer file.Close()

	media := &telego.InputMediaDocument{
		Type:  telego.MediaTypeDocument,
		Media: telego.InputFile{File: telegoutil.NameReader(file, doc.fileName)},
	}
	if doc.caption != "" {
		media.Caption = doc.caption
		media.ParseMode = telego.ModeHTML
	}
	editParams := &telego.EditMessageMediaParams{
		ChatID:      telego.ChatID{ID: chatID},
		MessageID:   messageID,
		Media:       media,
		ReplyMarkup: doc.keyboard,
	}

	var editErr error
	if h.RateLimiter != nil {
		_, editErr = telegram.EditMessageMediaWithRetry(ctx, h.RateLimiter, b, editParams)
	} else {
		_, editErr = b.EditMessageMedia(ctx, editParams)
	}
	if editErr == nil || telegram.IsMessageNotModified(editErr) {
		return
	}

	// In-place edit failed — delete the old document and resend a new one so the
	// user still gets the requested format.
	deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: chatID}, MessageID: messageID}
	if h.RateLimiter != nil {
		_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
	} else {
		_ = b.DeleteMessage(ctx, deleteParams)
	}
	h.sendLyricDocumentState(ctx, b, chatID, fallbackReplyToID, lyrics, baseName, platformName, trackID, state, requesterID)
}

// uploadLyricDocumentForInline renders the lyric document and uploads it to the
// configured InlineUploadChatID to obtain a reusable file_id. Telegram forbids
// uploading a new file when editing an inline message, so an inline lyric edit
// must reference a previously-uploaded file by its file_id. Returns the file_id
// plus the caption/keyboard to attach, or ok=false on any failure. The temp file
// is removed before returning.
func (h *LyricHandler) uploadLyricDocumentForInline(ctx context.Context, b *telego.Bot, lyrics *platform.Lyrics, baseName, platformName, trackID string, state lyricRenderState, requesterID int64) (fileID, caption string, keyboard *telego.InlineKeyboardMarkup, ok bool) {
	if h.InlineUploadChatID == 0 {
		return "", "", nil, false
	}
	doc, rendered := h.renderLyricDocument(lyrics, baseName, platformName, trackID, state, requesterID)
	if !rendered {
		return "", "", nil, false
	}
	defer os.Remove(doc.filePath)

	file, err := os.Open(doc.filePath)
	if err != nil {
		return "", "", nil, false
	}
	defer file.Close()

	uploadBot := b
	if h.UploadBot != nil {
		uploadBot = h.UploadBot
	}
	params := &telego.SendDocumentParams{
		ChatID:              telego.ChatID{ID: h.InlineUploadChatID},
		Document:            telego.InputFile{File: telegoutil.NameReader(file, doc.fileName)},
		DisableNotification: true,
	}
	if doc.caption != "" {
		params.Caption = doc.caption
		params.ParseMode = telego.ModeHTML
	}
	var uploaded *telego.Message
	if h.RateLimiter != nil {
		uploaded, err = telegram.SendDocumentWithRetry(ctx, h.RateLimiter, uploadBot, params)
	} else {
		uploaded, err = uploadBot.SendDocument(ctx, params)
	}
	if err != nil || uploaded == nil || uploaded.Document == nil || strings.TrimSpace(uploaded.Document.FileID) == "" {
		return "", "", nil, false
	}
	return uploaded.Document.FileID, doc.caption, doc.keyboard, true
}

// editLyricDocumentInlineState renders the lyric document for the given state and
// edits the inline message (identified by inlineMessageID) into a document with
// caption + format-switch keyboard. Because inline messages can't carry a freshly
// uploaded file, the document is first uploaded to InlineUploadChatID to get a
// file_id, which is then referenced via EditMessageMedia. On failure it falls
// back to a plain-text error edit, since inline messages have no reply target to
// delete-and-resend against.
func (h *LyricHandler) editLyricDocumentInlineState(ctx context.Context, b *telego.Bot, inlineMessageID string, lyrics *platform.Lyrics, baseName, platformName, trackID string, state lyricRenderState, requesterID int64) {
	inlineMessageID = strings.TrimSpace(inlineMessageID)
	if inlineMessageID == "" || b == nil {
		return
	}
	fileID, caption, keyboard, ok := h.uploadLyricDocumentForInline(ctx, b, lyrics, baseName, platformName, trackID, state, requesterID)
	if !ok {
		// Can't produce a file_id (upload chat unconfigured or upload failed).
		// The lyrics were fetched successfully, so degrade to plain text rather
		// than a misleading "failed" error — this preserves the pre-document
		// behavior when InlineUploadChatID is not set.
		h.editInlineLyricPlainText(ctx, b, inlineMessageID, lyrics)
		return
	}

	media := &telego.InputMediaDocument{
		Type:  telego.MediaTypeDocument,
		Media: telego.InputFile{FileID: fileID},
	}
	if caption != "" {
		media.Caption = caption
		media.ParseMode = telego.ModeHTML
	}
	editParams := &telego.EditMessageMediaParams{
		InlineMessageID: inlineMessageID,
		Media:           media,
		ReplyMarkup:     keyboard,
	}
	var editErr error
	if h.RateLimiter != nil {
		_, editErr = telegram.EditMessageMediaWithRetry(ctx, h.RateLimiter, b, editParams)
	} else {
		_, editErr = b.EditMessageMedia(ctx, editParams)
	}
	if editErr == nil || telegram.IsMessageNotModified(editErr) {
		return
	}
	h.editInlineLyricPlainText(ctx, b, inlineMessageID, lyrics)
}

// editInlineLyricPlainText edits an inline message into the lyrics as plain text,
// truncated to a safe length. It is the fallback when the document path is
// unavailable (no upload chat) or fails, so the user still sees the lyrics.
func (h *LyricHandler) editInlineLyricPlainText(ctx context.Context, b *telego.Bot, inlineMessageID string, lyrics *platform.Lyrics) {
	content := ""
	if lyrics != nil {
		content = strings.TrimSpace(lyrics.Plain)
	}
	if content == "" {
		content = "暂无歌词信息"
	}
	if len([]rune(content)) > 3800 {
		r := []rune(content)
		content = string(r[:3800]) + "\n...(歌词过长已截断)"
	}
	h.editInlineLyricError(ctx, b, inlineMessageID, content)
}

// editInlineLyricError replaces an inline message with a plain-text error. Inline
// messages have no reply target, so this is the only available fallback.
func (h *LyricHandler) editInlineLyricError(ctx context.Context, b *telego.Bot, inlineMessageID, text string) {
	if strings.TrimSpace(inlineMessageID) == "" || b == nil {
		return
	}
	params := &telego.EditMessageTextParams{
		InlineMessageID:    inlineMessageID,
		Text:               text,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		return
	}
	_, _ = b.EditMessageText(ctx, params)
}

// lyricFormatDefaultTranslation reports the default translation-merge state for
// a format: document formats (spl/ttml/amjson/ass/lqe) default on, others off.
func lyricFormatDefaultTranslation(format string) bool {
	switch lyricpkg.NormalizeFormat(format) {
	case "spl", "ttml", "amjson", "ass", "lqe":
		return true
	}
	return false
}

func (h *LyricHandler) sendLyricFallbackError(ctx context.Context, b *telego.Bot, chatID int64, replyToMessageID int) {
	sendFallback := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: chatID},
		Text:            getLrcFailed,
		ReplyParameters: &telego.ReplyParameters{MessageID: replyToMessageID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, sendFallback)
	} else {
		_, _ = b.SendMessage(ctx, sendFallback)
	}
}

// lyricPayloadFrom builds a lyric.Payload from a platform.Lyrics, deriving a
// plain LRC track when only timestamped lines are present.
func lyricPayloadFrom(lyrics *platform.Lyrics, platformName string) lyricpkg.Payload {
	if lyrics == nil {
		return lyricpkg.Payload{}
	}
	plain := strings.TrimSpace(lyrics.Plain)
	if plain == "" && len(lyrics.Timestamped) > 0 {
		plain = buildLRCFromTimestamped(lyrics.Timestamped)
	} else if plain != "" {
		plain = platform.NormalizeLRCTimestamps(lyrics.Plain)
	}
	source := platformName
	if source == "qqmusic" {
		source = "tencent"
	}
	return lyricpkg.Payload{
		Lyric:       plain,
		Translation: lyrics.Translation,
		Roma:        lyrics.Roma,
		RawYRC:      lyrics.RawYRC,
		RawQRC:      lyrics.RawQRC,
		RawLYS:      lyrics.RawLYS,
		RawTTML:     lyrics.RawTTML,
		Source:      source,
	}
}

func buildLRCFromTimestamped(lines []platform.LyricLine) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			continue
		}
		out = append(out, fmt.Sprintf("[%s]%s", formatDuration(line.Time), text))
	}
	return strings.Join(out, "\n")
}

// buildLyricCaption renders an expandable preview of the lyric content plus a
// line naming the current format and active translation/roma toggles. For
// structured/markup formats the raw output is unreadable, so it previews the
// plain-text lyric instead.
func buildLyricCaption(payload lyricpkg.Payload, content string, state lyricRenderState) string {
	format := lyricpkg.NormalizeFormat(state.format)
	preview := strings.TrimSpace(lyricPreviewText(payload, content, format))
	label := lyricFormatDisplayNameForPayload(format, payload)
	header := fmt.Sprintf("当前歌词格式: %s", html.EscapeString(label))
	if extras := lyricCaptionToggleSuffix(payload, format, state); extras != "" {
		header += extras
	}
	if preview == "" {
		return header
	}
	escaped := html.EscapeString(preview)
	candidate := fmt.Sprintf("%s\n<blockquote expandable>%s</blockquote>", header, escaped)
	if len([]rune(candidate)) <= lyricCaptionMaxChars {
		return candidate
	}
	// Trim the preview to fit the caption budget.
	runes := []rune(escaped)
	if len(runes) > 400 {
		escaped = string(runes[:400]) + "…"
	}
	candidate = fmt.Sprintf("%s\n<blockquote expandable>%s</blockquote>", header, escaped)
	if len([]rune(candidate)) <= lyricCaptionMaxChars {
		return candidate
	}
	return header
}

// lyricFormatDisplayNameForPayload is lyricFormatDisplayName, but drops the
// "逐词" wording when the song carries no actual word-by-word timing (e.g. a
// yrc/ttml request that falls back to line-level lyrics). The format itself is
// unchanged — only the human-facing label reflects what was really produced.
func lyricFormatDisplayNameForPayload(format string, payload lyricpkg.Payload) string {
	name := lyricFormatDisplayName(format)
	if strings.Contains(name, "逐词") && !lyricpkg.HasWordTiming(payload) {
		name = strings.TrimSpace(strings.ReplaceAll(name, "逐词", ""))
	}
	return name
}

// lyricCaptionToggleSuffix appends a " · 含翻译/罗马音" note listing the active
// side-tracks, only for formats that support them and when the track actually
// has that content. A toggle being on but the song lacking the data adds
// nothing — the caption never claims content that isn't in the file.
func lyricCaptionToggleSuffix(payload lyricpkg.Payload, format string, state lyricRenderState) string {
	if !lyricFormatSupportsSideTracks(format) {
		return ""
	}
	var parts []string
	if state.includeTranslation && lyricPayloadHasTranslation(payload) {
		parts = append(parts, "翻译")
	}
	if state.includeRoma && lyricPayloadHasRoma(payload) {
		parts = append(parts, "罗马音")
	}
	if len(parts) == 0 {
		return ""
	}
	return " · 含" + strings.Join(parts, "/")
}

// lyricPayloadHasTranslation reports whether the payload yields any actual
// translation text once converted (raw fields can be present but contain only
// timestamps/placeholders that convert to nothing).
func lyricPayloadHasTranslation(payload lyricpkg.Payload) bool {
	return strings.TrimSpace(lyricpkg.Convert(payload, "trans", lyricpkg.Options{})) != ""
}

// lyricPayloadHasRoma reports whether the payload yields any actual
// romanization text once converted.
func lyricPayloadHasRoma(payload lyricpkg.Payload) bool {
	return strings.TrimSpace(lyricpkg.Convert(payload, "roma", lyricpkg.Options{})) != ""
}

// lyricPreviewText returns a readable preview for the caption: the plain-text
// form for word-by-word/markup formats, or the content itself for plain ones.
func lyricPreviewText(payload lyricpkg.Payload, content, format string) string {
	switch format {
	case "txt", "trans", "roma":
		return content
	case "lrc", "elrc", "yrc", "qrc", "lys", "krc", "spl", "srt":
		// Show the plain text so timing tags don't clutter the preview.
		return lyricpkg.Convert(payload, "txt", lyricpkg.Options{})
	default:
		// ttml/amjson/ass/lqe and anything else: preview the plain lyric text.
		return lyricpkg.Convert(payload, "txt", lyricpkg.Options{})
	}
}

func lyricFormatDisplayName(format string) string {
	switch format {
	case "lrc":
		return "LRC"
	case "yrc":
		return "YRC 逐词"
	case "qrc":
		return "QRC 逐词"
	case "lys":
		return "Lyricify Syllable"
	case "krc":
		return "KRC 逐词"
	case "elrc":
		return "Enhanced LRC 逐词"
	case "spl":
		return "SPL 逐词"
	case "ass":
		return "ASS 字幕"
	case "lqe":
		return "Lyricify Quick Export"
	case "ttml":
		return "TTML 逐词"
	case "amjson":
		return "Apple Music JSON"
	case "srt":
		return "SRT 字幕"
	case "txt":
		return "纯文本"
	case "trans":
		return "翻译"
	case "roma":
		return "罗马音"
	default:
		return strings.ToUpper(format)
	}
}

func writeLyricTempFile(text, fileName string) (string, error) {
	ext := lyricFileExt(fileName)
	tmpFile, err := os.CreateTemp("", "musicbot-lyrics-*"+ext)
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

func lyricFileExt(fileName string) string {
	if idx := strings.LastIndex(fileName, "."); idx >= 0 {
		return fileName[idx:]
	}
	return ".lrc"
}

// buildLyricBaseName resolves the "artist - title" stem (without extension) for
// the lyric file, falling back to "歌词".
func (h *LyricHandler) buildLyricBaseName(ctx context.Context, plat platform.Platform, trackID string) string {
	const defaultName = "歌词"
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
	artistJoined := strings.ReplaceAll(strings.Join(artists, "/"), "/", ",")
	title := strings.TrimSpace(track.Title)
	switch {
	case artistJoined == "" && title == "":
		return defaultName
	case artistJoined == "":
		return title
	case title == "":
		return artistJoined
	default:
		return fmt.Sprintf("%s - %s", artistJoined, title)
	}
}

func buildLyricFileNameForFormat(baseName, format string) string {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		baseName = "歌词"
	}
	ext := lyricpkg.FileExtension(format)
	return sanitizeFileName(baseName + "." + ext)
}

func formatDuration(d time.Duration) string {
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	centis := int((d % time.Second) / (10 * time.Millisecond))
	return fmt.Sprintf("%02d:%02d.%02d", minutes, seconds, centis)
}
