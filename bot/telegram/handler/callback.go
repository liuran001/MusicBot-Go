package handler

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// CallbackMusicHandler handles callback queries for music buttons.
type CallbackMusicHandler struct {
	Music       *MusicHandler
	BotName     string
	RateLimiter *telegram.RateLimiter
}

type parsedMusicCallback struct {
	platformName    string
	trackID         string
	qualityOverride string
	requesterID     int64
	ok              bool
	tokenExpired    bool
}

const episodePageSize = 8

// episodeSearchBackStore 保存「分集选择器返回搜索结果」的回调数据，按
// chatID:messageID 索引。使用带 TTL 的 store 自动驱逐过期项，避免裸 map
// 随会话累积而无界增长（与 inlineCallbackPayloads 的 30 分钟 TTL 对齐）。
var episodeSearchBackStore = newTTLStore[string](30 * time.Minute)

func episodeBackKey(chatID int64, messageID int) string {
	return fmt.Sprintf("%d:%d", chatID, messageID)
}

func setEpisodeSearchBackCallback(chatID int64, messageID int, callbackData string) {
	key := episodeBackKey(chatID, messageID)
	if strings.TrimSpace(callbackData) == "" {
		episodeSearchBackStore.Delete(key)
		return
	}
	episodeSearchBackStore.Store(key, callbackData)
}

func getEpisodeSearchBackCallback(chatID int64, messageID int) string {
	value, _ := episodeSearchBackStore.Load(episodeBackKey(chatID, messageID))
	return value
}

func (h *CallbackMusicHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	args := strings.Split(query.Data, " ")
	if len(args) < 2 {
		return
	}
	if isInlineMusicCallbackArgs(args) {
		h.handleInlineCallback(ctx, b, query, args)
		return
	}
	if len(args) >= 4 && args[1] == "ep" {
		h.handleEpisodeCallback(ctx, b, query, args)
		return
	}
	if len(args) >= 3 && args[1] == "ept" {
		h.handleEpisodeCallback(ctx, b, query, args)
		return
	}

	parsed := parseMusicCallbackDataV2(args)
	if !parsed.ok {
		return
	}
	if parsed.tokenExpired {
		if query.Message != nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_op_expired"), ShowAlert: true})
		}
		return
	}

	platformName := parsed.platformName
	trackID := parsed.trackID
	requesterID := parsed.requesterID
	qualityOverride := parsed.qualityOverride
	if qualityOverride != "" {
		if _, err := platform.ParseQuality(qualityOverride); err != nil {
			qualityOverride = ""
		}
	}

	if query.Message == nil {
		return
	}
	msg := query.Message.Message()
	if msg == nil {
		return
	}
	chatType := msg.Chat.Type

	msgToUse := msg
	if msg.ReplyToMessage != nil {
		msgToUse = msg.ReplyToMessage
	}

	if chatType == "private" {
		if h.tryPresentEpisodePicker(ctx, b, query, msg, msgToUse, platformName, trackID, qualityOverride, query.From.ID, requesterID) {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_select_episode")})
			return
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
		if h.Music != nil {
			h.Music.dispatch(withDisableFallback(withForceNonSilent(ctx)), b, msgToUse, platformName, trackID, qualityOverride)
		}
		if h.shouldAutoDeleteListMessage(ctx, msg, query.From.ID, nil, nil) {
			deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
			if h.RateLimiter != nil {
				_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
			} else {
				_ = b.DeleteMessage(ctx, deleteParams)
			}
		}
		return
	}

	if !isRequesterOrAdmin(ctx, b, msg.Chat.ID, query.From.ID, requesterID) {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            tr(ctx, "cb_denied"),
			ShowAlert:       true,
		})
		return
	}

	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
	if h.tryPresentEpisodePicker(ctx, b, query, msg, msgToUse, platformName, trackID, qualityOverride, query.From.ID, requesterID) {
		return
	}
	autoDelete := h.shouldAutoDeleteListMessage(ctx, msg, query.From.ID, nil, nil)
	if h.Music != nil {
		h.Music.dispatch(withDisableFallback(withForceNonSilent(ctx)), b, msgToUse, platformName, trackID, qualityOverride)
	}
	if autoDelete {
		deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
		if h.RateLimiter != nil {
			_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
		} else {
			_ = b.DeleteMessage(ctx, deleteParams)
		}
	}
}

func isInlineMusicCallbackArgs(args []string) bool {
	if len(args) < 3 {
		return false
	}
	switch strings.TrimSpace(args[1]) {
	case "i", "it", "iep", "iet":
		return true
	default:
		return false
	}
}

func (h *CallbackMusicHandler) handleInlineCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	if query == nil || h == nil || h.Music == nil || b == nil {
		return
	}
	if query.InlineMessageID == "" {
		return
	}
	inlineGuardKey := fmt.Sprintf("music-i:%s", strings.TrimSpace(query.InlineMessageID))
	releaseInlineGuard, acquired := tryAcquireCallbackInFlight(inlineGuardKey, 30*time.Second)
	if !acquired {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_processing")})
		return
	}
	defer releaseInlineGuard()
	if len(args) >= 4 && strings.TrimSpace(args[1]) == "iep" {
		h.handleInlineEpisodeCallback(ctx, b, query, args)
		return
	}
	if len(args) >= 3 && strings.TrimSpace(args[1]) == "iet" {
		h.handleInlineEpisodeCallback(ctx, b, query, args)
		return
	}
	if parsed, ok := parseInlineSendCallbackArgs(args); ok {
		if parsed.requesterID != 0 && parsed.requesterID != query.From.ID {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_denied"), ShowAlert: true})
			return
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
		if h.tryPresentInlineEpisodePicker(ctx, b, query, parsed.platformName, parsed.trackID, parsed.qualityOverride, query.From.ID) {
			return
		}
		h.runInlineDownloadFlowGuarded(detachContext(ctx), b, query.InlineMessageID, query.From.ID, query.From.Username, parsed.platformName, parsed.trackID, parsed.qualityOverride)
		return
	}
	if len(args) >= 4 && strings.TrimSpace(args[2]) == "random" {
		requesterID, _ := strconv.ParseInt(strings.TrimSpace(args[len(args)-1]), 10, 64)
		if requesterID != 0 && requesterID != query.From.ID {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_denied"), ShowAlert: true})
			return
		}
		platformName, trackID, qualityValue, ok := h.resolveInlineRandomTrack(ctx)
		if !ok {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_no_random"), ShowAlert: true})
			return
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
		h.runInlineDownloadFlowGuarded(detachContext(ctx), b, query.InlineMessageID, query.From.ID, query.From.Username, platformName, trackID, qualityValue)
		return
	}
	if len(args) < 5 {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_bad_params"), ShowAlert: true})
		return
	}
	platformName := strings.TrimSpace(args[2])
	trackID := strings.TrimSpace(args[3])
	requesterID, _ := strconv.ParseInt(args[len(args)-1], 10, 64)
	qualityOverride := ""
	if len(args) >= 6 {
		qualityOverride = strings.TrimSpace(args[4])
	}
	if platformName == "" || trackID == "" {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_bad_params"), ShowAlert: true})
		return
	}
	if requesterID != 0 && requesterID != query.From.ID {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_denied"), ShowAlert: true})
		return
	}
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
	if h.tryPresentInlineEpisodePicker(ctx, b, query, platformName, trackID, qualityOverride, query.From.ID) {
		return
	}

	h.runInlineDownloadFlowGuarded(detachContext(ctx), b, query.InlineMessageID, query.From.ID, query.From.Username, platformName, trackID, qualityOverride)
}

func parseInlineSendCallbackArgs(args []string) (parsedMusicCallback, bool) {
	if len(args) >= 3 && strings.TrimSpace(args[1]) == "it" {
		payload, ok := getInlineCallbackPayload(strings.TrimSpace(args[2]))
		if !ok {
			return parsedMusicCallback{}, false
		}
		return parsedMusicCallback{
			platformName:    payload.platformName,
			trackID:         payload.trackID,
			qualityOverride: payload.qualityValue,
			requesterID:     payload.requesterID,
			ok:              true,
		}, true
	}
	if len(args) < 5 || strings.TrimSpace(args[1]) != "i" {
		return parsedMusicCallback{}, false
	}
	parsed := parsedMusicCallback{platformName: strings.TrimSpace(args[2]), trackID: strings.TrimSpace(args[3]), ok: true}
	parsed.requesterID, _ = strconv.ParseInt(strings.TrimSpace(args[len(args)-1]), 10, 64)
	if len(args) >= 6 {
		parsed.qualityOverride = strings.TrimSpace(args[4])
	}
	if parsed.platformName == "" || parsed.trackID == "" {
		return parsedMusicCallback{}, false
	}
	return parsed, true
}

func (h *CallbackMusicHandler) runInlineDownloadFlowGuarded(ctx context.Context, b *telego.Bot, inlineMessageID string, userID int64, userName, platformName, trackID, qualityOverride string) bool {
	guardKey := fmt.Sprintf("music-flow:%s", strings.TrimSpace(inlineMessageID))
	release, ok := tryAcquireCallbackInFlight(guardKey, 45*time.Second)
	if !ok {
		return false
	}
	go func() {
		defer release()
		h.runInlineDownloadFlow(ctx, b, inlineMessageID, userID, userName, platformName, trackID, qualityOverride)
	}()
	return true
}

func (h *CallbackMusicHandler) tryPresentInlineEpisodePicker(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, platformName, trackID, qualityValue string, requesterID int64) bool {
	if h == nil || h.Music == nil || b == nil || query == nil || strings.TrimSpace(query.InlineMessageID) == "" {
		return false
	}
	baseTrackID, _, hasExplicitPage, ok := parseEpisodeTrackID(h.Music.PlatformManager, platformName, trackID)
	if !ok || hasExplicitPage || strings.TrimSpace(baseTrackID) == "" {
		return false
	}
	episodes, err := h.fetchEpisodes(ctx, platformName, baseTrackID)
	if err != nil || len(episodes) <= 1 {
		return false
	}
	text, keyboard := buildInlineEpisodePickerPage(ctx, platformName, baseTrackID, qualityValue, requesterID, episodes, 1)
	if strings.TrimSpace(text) == "" || keyboard == nil {
		return false
	}
	params := &telego.EditMessageTextParams{InlineMessageID: query.InlineMessageID, Text: text, ReplyMarkup: keyboard}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_select_episode")})
	return true
}

func (h *CallbackMusicHandler) tryPresentEpisodePicker(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, listMsg, msgToUse *telego.Message, platformName, trackID, qualityOverride string, operatorID, requesterID int64) bool {
	if h == nil || h.Music == nil || h.Music.PlatformManager == nil || b == nil || query == nil || listMsg == nil || msgToUse == nil {
		return false
	}
	baseTrackID, _, hasExplicitPage, ok := parseEpisodeTrackID(h.Music.PlatformManager, platformName, trackID)
	if !ok || hasExplicitPage || strings.TrimSpace(baseTrackID) == "" {
		return false
	}
	episodes, err := h.fetchEpisodes(ctx, platformName, baseTrackID)
	if err != nil || len(episodes) <= 1 {
		return false
	}
	reqID := requesterID
	if reqID == 0 {
		reqID = operatorID
	}
	backCallback := fmt.Sprintf("search %d home %d", listMsg.MessageID, reqID)
	setEpisodeSearchBackCallback(listMsg.Chat.ID, listMsg.MessageID, backCallback)
	text, keyboard := buildEpisodePickerPage(ctx, platformName, baseTrackID, qualityOverride, reqID, episodes, 1, backCallback)
	if strings.TrimSpace(text) == "" || keyboard == nil {
		return false
	}
	params := &telego.EditMessageTextParams{
		ChatID:             telego.ChatID{ID: listMsg.Chat.ID},
		MessageID:          listMsg.MessageID,
		Text:               text,
		ParseMode:          telego.ModeMarkdownV2,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
		ReplyMarkup:        keyboard,
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
	return true
}

func (h *CallbackMusicHandler) fetchEpisodes(ctx context.Context, platformName, trackID string) ([]platform.Episode, error) {
	if h == nil || h.Music == nil || h.Music.PlatformManager == nil {
		return nil, platform.ErrUnavailable
	}
	plat := h.Music.PlatformManager.Get(strings.TrimSpace(platformName))
	if plat == nil {
		return nil, platform.ErrUnavailable
	}
	provider, ok := plat.(platform.EpisodeProvider)
	if !ok {
		return nil, platform.ErrUnsupported
	}
	return provider.ListEpisodes(ctx, strings.TrimSpace(trackID))
}

func buildEpisodePickerPage(ctx context.Context, platformName, trackID, qualityValue string, requesterID int64, episodes []platform.Episode, page int, backCallback string) (string, *telego.InlineKeyboardMarkup) {
	if len(episodes) == 0 {
		return "", nil
	}
	if page <= 0 {
		page = 1
	}
	totalPages := int(math.Ceil(float64(len(episodes)) / float64(episodePageSize)))
	if totalPages <= 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * episodePageSize
	end := start + episodePageSize
	if end > len(episodes) {
		end = len(episodes)
	}
	visible := episodes[start:end]

	textLines := buildEpisodeHeaderLines(ctx, episodes)
	textLines = append(textLines, tr(ctx, "pl_page_of", map[string]any{"Page": page, "Total": totalPages}), "")
	for _, ep := range visible {
		title := strings.TrimSpace(ep.Title)
		if title == "" {
			title = fmt.Sprintf("P%d", ep.Index)
		}
		episodeLink := mdV2Replacer.Replace(title)
		if strings.TrimSpace(ep.URL) != "" {
			episodeLink = fmt.Sprintf("[%s](%s)", mdV2Replacer.Replace(title), strings.TrimSpace(ep.URL))
		}
		textLines = append(textLines, fmt.Sprintf("%d\\. %s", ep.Index, episodeLink))
	}

	rows := make([][]telego.InlineKeyboardButton, 0, 8)
	currentRow := make([]telego.InlineKeyboardButton, 0, episodePageSize)
	for _, ep := range visible {
		cb := buildEpisodeSelectCallbackData(platformName, trackID, qualityValue, requesterID, ep.Index)
		if cb == "" {
			continue
		}
		currentRow = append(currentRow, telego.InlineKeyboardButton{Text: fmt.Sprintf("%d", ep.Index), CallbackData: cb})
		if len(currentRow) == episodePageSize {
			rows = append(rows, currentRow)
			currentRow = make([]telego.InlineKeyboardButton, 0, episodePageSize)
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	if totalPages > 1 {
		nav := make([]telego.InlineKeyboardButton, 0, 2)
		if page > 1 {
			if cb := buildEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, page-1); cb != "" {
				nav = append(nav, telego.InlineKeyboardButton{Text: tr(ctx, "pl_prev_page_arrow"), CallbackData: cb})
			}
		}
		if page < totalPages {
			if cb := buildEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, page+1); cb != "" {
				nav = append(nav, telego.InlineKeyboardButton{Text: tr(ctx, "pl_next_page_arrow"), CallbackData: cb})
			}
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
		extraRow := make([]telego.InlineKeyboardButton, 0, 2)
		if page > 1 {
			if cb := buildEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, 1); cb != "" {
				extraRow = append(extraRow, telego.InlineKeyboardButton{Text: tr(ctx, "pl_home_arrow"), CallbackData: cb})
			}
		}
		if page < totalPages {
			if cb := buildEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, totalPages); cb != "" {
				extraRow = append(extraRow, telego.InlineKeyboardButton{Text: tr(ctx, "pl_last_page_arrow"), CallbackData: cb})
			}
		}
		if len(extraRow) > 0 {
			rows = append(rows, extraRow)
		}
	}

	if len(rows) == 0 {
		return "", nil
	}
	if closeCB := buildEpisodeCloseCallbackData(platformName, trackID, qualityValue, requesterID); closeCB != "" {
		bottom := make([]telego.InlineKeyboardButton, 0, 2)
		if strings.TrimSpace(backCallback) != "" {
			bottom = append(bottom, telego.InlineKeyboardButton{Text: tr(ctx, "pl_back_to_search"), CallbackData: backCallback})
		}
		bottom = append(bottom, telego.InlineKeyboardButton{Text: tr(ctx, "pl_episode_close"), CallbackData: closeCB})
		rows = append(rows, bottom)
	}
	return strings.Join(textLines, "\n"), &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buildEpisodeHeaderLines(ctx context.Context, episodes []platform.Episode) []string {
	lines := make([]string, 0, 8)
	if len(episodes) == 0 {
		return lines
	}
	first := episodes[0]
	if title := strings.TrimSpace(first.VideoTitle); title != "" {
		titleText := mdV2Replacer.Replace(title)
		if strings.TrimSpace(first.VideoURL) != "" {
			titleText = fmt.Sprintf("[%s](%s)", titleText, strings.TrimSpace(first.VideoURL))
		}
		lines = append(lines, tr(ctx, "pl_title")+": "+titleText)
	}
	if up := strings.TrimSpace(first.CreatorName); up != "" {
		upText := mdV2Replacer.Replace(up)
		if strings.TrimSpace(first.CreatorURL) != "" {
			upText = fmt.Sprintf("[%s](%s)", upText, strings.TrimSpace(first.CreatorURL))
		}
		lines = append(lines, tr(ctx, "pl_uploader")+": "+upText)
	}
	if desc := strings.TrimSpace(first.Description); desc != "" {
		if quote := formatExpandableQuote(ctx, mdV2Replacer.Replace(truncateText(desc, 800))); quote != "" {
			lines = append(lines, "", quote)
		}
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	return lines
}

func buildEpisodeSelectCallbackData(platformName, trackID, qualityValue string, requesterID int64, page int) string {
	return buildEpisodeCallbackData("p", platformName, trackID, qualityValue, requesterID, page)
}

func buildEpisodeNavCallbackData(platformName, trackID, qualityValue string, requesterID int64, page int) string {
	return buildEpisodeCallbackData("n", platformName, trackID, qualityValue, requesterID, page)
}

func buildEpisodeCloseCallbackData(platformName, trackID, qualityValue string, requesterID int64) string {
	return buildEpisodeCallbackData("c", platformName, trackID, qualityValue, requesterID, 1)
}

func buildEpisodeCallbackData(action, platformName, trackID, qualityValue string, requesterID int64, page int) string {
	action = strings.TrimSpace(strings.ToLower(action))
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	qualityValue = strings.TrimSpace(qualityValue)
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if page <= 0 {
		page = 1
	}
	if requesterID == 0 || !isInlineStartToken(action) || !isInlineStartToken(platformName) || !isInlineStartToken(trackID) || !isInlineStartToken(qualityValue) {
		return ""
	}
	data := fmt.Sprintf("music ep %s %s %s %s %d %d", action, platformName, trackID, qualityValue, requesterID, page)
	if len(data) <= 64 {
		return data
	}
	// 长 trackID 时直接拼接会超过 Telegram 64 字节回调上限。复用带 TTL 的 payload
	// store，避免像旧的精简 fallback 那样丢掉 quality 字段导致音质回落（与 inline
	// 路径的 buildInlineEpisodeCallbackData 对称）。
	if token := storeInlineCallbackPayload(inlineCallbackPayload{action: action, platformName: platformName, trackID: trackID, qualityValue: qualityValue, requesterID: requesterID, page: page}); token != "" {
		data = fmt.Sprintf("music ept %s", token)
		if len(data) <= 64 {
			return data
		}
	}
	return ""
}

func parseEpisodeCallbackArgs(args []string) (action, platformName, trackID, qualityValue string, requesterID int64, page int, ok bool) {
	if len(args) >= 3 && strings.TrimSpace(args[1]) == "ept" {
		payload, found := getInlineCallbackPayload(strings.TrimSpace(args[2]))
		if !found {
			return "", "", "", "", 0, 0, false
		}
		return payload.action, payload.platformName, payload.trackID, payload.qualityValue, payload.requesterID, payload.page, true
	}
	if len(args) < 7 || strings.TrimSpace(args[1]) != "ep" {
		return "", "", "", "", 0, 0, false
	}
	action = strings.TrimSpace(args[2])
	platformName = strings.TrimSpace(args[3])
	trackID = strings.TrimSpace(args[4])
	if action == "" || platformName == "" || trackID == "" {
		return "", "", "", "", 0, 0, false
	}
	if len(args) >= 8 {
		qualityValue = strings.TrimSpace(args[5])
		requesterID, _ = strconv.ParseInt(strings.TrimSpace(args[6]), 10, 64)
		page, _ = strconv.Atoi(strings.TrimSpace(args[7]))
	} else {
		qualityValue = ""
		requesterID, _ = strconv.ParseInt(strings.TrimSpace(args[5]), 10, 64)
		page, _ = strconv.Atoi(strings.TrimSpace(args[6]))
	}
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if page <= 0 {
		page = 1
	}
	return action, platformName, trackID, qualityValue, requesterID, page, true
}

func (h *CallbackMusicHandler) handleEpisodeCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	if h == nil || h.Music == nil || b == nil || query == nil || query.Message == nil {
		return
	}
	action, platformName, trackID, qualityValue, requesterID, page, ok := parseEpisodeCallbackArgs(args)
	if !ok {
		return
	}
	msg := query.Message.Message()
	if msg == nil {
		return
	}
	epGuardKey := fmt.Sprintf("music-ep:%d:%d", msg.Chat.ID, msg.MessageID)
	releaseEpGuard, acquired := tryAcquireCallbackInFlight(epGuardKey, 8*time.Second)
	if !acquired {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_processing")})
		return
	}
	defer releaseEpGuard()
	if msg.Chat.Type != "private" && !isRequesterOrAdmin(ctx, b, msg.Chat.ID, query.From.ID, requesterID) {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_denied"), ShowAlert: true})
		return
	}
	if requesterID != 0 && requesterID != query.From.ID && msg.Chat.Type == "private" {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_denied"), ShowAlert: true})
		return
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "c":
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
		deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
		if h.RateLimiter != nil {
			_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
		} else {
			_ = b.DeleteMessage(ctx, deleteParams)
		}
		return
	case "s", "n":
		episodes, err := h.fetchEpisodes(ctx, platformName, trackID)
		if err != nil || len(episodes) == 0 {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_episode_load_failed"), ShowAlert: true})
			return
		}
		backCallback := getEpisodeSearchBackCallback(msg.Chat.ID, msg.MessageID)
		text, keyboard := buildEpisodePickerPage(ctx, platformName, trackID, qualityValue, requesterID, episodes, page, backCallback)
		if text == "" || keyboard == nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_episode_load_failed"), ShowAlert: true})
			return
		}
		params := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID, Text: text, ParseMode: telego.ModeMarkdownV2, LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true}, ReplyMarkup: keyboard}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
	case "p":
		msgToUse := msg
		if msg.ReplyToMessage != nil {
			msgToUse = msg.ReplyToMessage
		}
		selectedTrackID := buildEpisodeTrackID(h.Music.PlatformManager, platformName, trackID, page, true)
		if strings.TrimSpace(selectedTrackID) == "" {
			selectedTrackID = trackID
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
		autoDelete := h.shouldAutoDeleteListMessage(ctx, msg, query.From.ID, nil, nil)
		h.Music.dispatch(withDisableFallback(withForceNonSilent(ctx)), b, msgToUse, platformName, selectedTrackID, qualityValue)
		if autoDelete {
			deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
			if h.RateLimiter != nil {
				_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
			} else {
				_ = b.DeleteMessage(ctx, deleteParams)
			}
		}
	default:
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_bad_params"), ShowAlert: true})
	}
}

func parseInlineEpisodeCallbackArgs(args []string) (action, platformName, trackID, qualityValue string, requesterID int64, page int, ok bool) {
	if len(args) >= 3 && strings.TrimSpace(args[1]) == "iet" {
		payload, found := getInlineCallbackPayload(strings.TrimSpace(args[2]))
		if !found {
			return "", "", "", "", 0, 0, false
		}
		return payload.action, payload.platformName, payload.trackID, payload.qualityValue, payload.requesterID, payload.page, true
	}
	if len(args) < 7 || strings.TrimSpace(args[1]) != "iep" {
		return "", "", "", "", 0, 0, false
	}
	action = strings.TrimSpace(args[2])
	platformName = strings.TrimSpace(args[3])
	trackID = strings.TrimSpace(args[4])
	if action == "" || platformName == "" || trackID == "" {
		return "", "", "", "", 0, 0, false
	}
	if len(args) >= 8 {
		qualityValue = strings.TrimSpace(args[5])
		requesterID, _ = strconv.ParseInt(strings.TrimSpace(args[6]), 10, 64)
		page, _ = strconv.Atoi(strings.TrimSpace(args[7]))
	} else {
		qualityValue = ""
		requesterID, _ = strconv.ParseInt(strings.TrimSpace(args[5]), 10, 64)
		page, _ = strconv.Atoi(strings.TrimSpace(args[6]))
	}
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if page <= 0 {
		page = 1
	}
	return action, platformName, trackID, qualityValue, requesterID, page, true
}

func (h *CallbackMusicHandler) handleInlineEpisodeCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	if h == nil || h.Music == nil || b == nil || query == nil || strings.TrimSpace(query.InlineMessageID) == "" {
		return
	}
	action, platformName, trackID, qualityValue, requesterID, page, ok := parseInlineEpisodeCallbackArgs(args)
	if !ok {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_bad_params"), ShowAlert: true})
		return
	}
	if requesterID != 0 && requesterID != query.From.ID {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_denied"), ShowAlert: true})
		return
	}
	iepGuardKey := fmt.Sprintf("music-iep:%s", strings.TrimSpace(query.InlineMessageID))
	releaseIepGuard, acquired := tryAcquireCallbackInFlight(iepGuardKey, 8*time.Second)
	if !acquired {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_processing")})
		return
	}
	defer releaseIepGuard()
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "c":
		params := &telego.EditMessageTextParams{InlineMessageID: query.InlineMessageID, Text: tr(ctx, "cb_episode_closed")}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
		return
	case "s", "n":
		episodes, err := h.fetchEpisodes(ctx, platformName, trackID)
		if err != nil || len(episodes) == 0 {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_episode_load_failed"), ShowAlert: true})
			return
		}
		text, keyboard := buildInlineEpisodePickerPage(ctx, platformName, trackID, qualityValue, requesterID, episodes, page)
		if text == "" || keyboard == nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_episode_load_failed"), ShowAlert: true})
			return
		}
		params := &telego.EditMessageTextParams{InlineMessageID: query.InlineMessageID, Text: text, ParseMode: telego.ModeMarkdownV2, LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true}, ReplyMarkup: keyboard}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
	case "p":
		selectedTrackID := buildEpisodeTrackID(h.Music.PlatformManager, platformName, trackID, page, true)
		if strings.TrimSpace(selectedTrackID) == "" {
			selectedTrackID = trackID
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_success")})
		h.runInlineDownloadFlowGuarded(detachContext(ctx), b, query.InlineMessageID, query.From.ID, query.From.Username, platformName, selectedTrackID, qualityValue)
	default:
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "cb_bad_params"), ShowAlert: true})
	}
}

func buildInlineEpisodePickerPage(ctx context.Context, platformName, trackID, qualityValue string, requesterID int64, episodes []platform.Episode, page int) (string, *telego.InlineKeyboardMarkup) {
	if len(episodes) == 0 {
		return "", nil
	}
	if page <= 0 {
		page = 1
	}
	// inline 多P页面与 inline 专辑页保持同样的单页容量（当前为 8）
	pageSize := episodePageSize
	totalPages := int(math.Ceil(float64(len(episodes)) / float64(pageSize)))
	if totalPages <= 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(episodes) {
		end = len(episodes)
	}
	visible := episodes[start:end]
	textLines := make([]string, 0, len(visible)+8)
	textLines = append(textLines, fmt.Sprintf("%s *%s* %s", platformEmoji(nil, platformName), mdV2Replacer.Replace(platformDisplayName(nil, platformName)), tr(ctx, "cb_episodes_label")), "")
	textLines = append(textLines, buildEpisodeHeaderLines(ctx, episodes)...)
	textLines = append(textLines, tr(ctx, "cb_page_of", map[string]any{"Page": page, "Total": totalPages}), "")
	for _, ep := range visible {
		displayIndex := ep.Index
		title := strings.TrimSpace(ep.Title)
		if title == "" {
			title = fmt.Sprintf("P%d", ep.Index)
		}
		episodeLink := mdV2Replacer.Replace(title)
		if strings.TrimSpace(ep.URL) != "" {
			episodeLink = fmt.Sprintf("[%s](%s)", mdV2Replacer.Replace(title), strings.TrimSpace(ep.URL))
		}
		textLines = append(textLines, fmt.Sprintf("%d\\. %s", displayIndex, episodeLink))
	}

	rows := make([][]telego.InlineKeyboardButton, 0, 8)
	currentRow := make([]telego.InlineKeyboardButton, 0, pageSize)
	for _, ep := range visible {
		displayIndex := ep.Index
		cb := buildInlineEpisodeSelectCallbackData(platformName, trackID, qualityValue, requesterID, ep.Index)
		if cb == "" {
			continue
		}
		currentRow = append(currentRow, telego.InlineKeyboardButton{Text: fmt.Sprintf("%d", displayIndex), CallbackData: cb})
		if len(currentRow) == pageSize {
			rows = append(rows, currentRow)
			currentRow = make([]telego.InlineKeyboardButton, 0, pageSize)
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}
	if totalPages > 1 {
		nav := make([]telego.InlineKeyboardButton, 0, 2)
		if page > 1 {
			if cb := buildInlineEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, page-1); cb != "" {
				nav = append(nav, telego.InlineKeyboardButton{Text: tr(ctx, "cb_prev_page_arrow"), CallbackData: cb})
			}
		}
		if page < totalPages {
			if cb := buildInlineEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, page+1); cb != "" {
				nav = append(nav, telego.InlineKeyboardButton{Text: tr(ctx, "cb_next_page_arrow"), CallbackData: cb})
			}
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
		extraRow := make([]telego.InlineKeyboardButton, 0, 2)
		if page > 1 {
			if cb := buildInlineEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, 1); cb != "" {
				extraRow = append(extraRow, telego.InlineKeyboardButton{Text: tr(ctx, "cb_home_page"), CallbackData: cb})
			}
		}
		if page < totalPages {
			if cb := buildInlineEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, totalPages); cb != "" {
				extraRow = append(extraRow, telego.InlineKeyboardButton{Text: tr(ctx, "cb_last_page"), CallbackData: cb})
			}
		}
		if len(extraRow) > 0 {
			rows = append(rows, extraRow)
		}
	}
	if len(rows) == 0 {
		return "", nil
	}
	return strings.Join(textLines, "\n"), &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (h *CallbackMusicHandler) resolveInlineRandomTrack(ctx context.Context) (platformName, trackID, qualityValue string, ok bool) {
	if h == nil || h.Music == nil || h.Music.Repo == nil {
		return "", "", "", false
	}
	info, err := h.Music.Repo.FindRandomCachedSong(ctx)
	if err != nil || info == nil {
		return "", "", "", false
	}
	platformName = strings.TrimSpace(info.Platform)
	if platformName == "" {
		platformName = "netease"
	}
	trackID = strings.TrimSpace(info.TrackID)
	if trackID == "" && info.MusicID > 0 {
		trackID = strconv.Itoa(info.MusicID)
	}
	if trackID == "" {
		return "", "", "", false
	}
	qualityValue = strings.TrimSpace(info.Quality)
	if qualityValue == "" {
		qualityValue = "hires"
	}
	return platformName, trackID, qualityValue, true
}

func (h *CallbackMusicHandler) runInlineDownloadFlow(ctx context.Context, b *telego.Bot, inlineMessageID string, userID int64, userName, platformName, trackID, qualityOverride string) {
	if h == nil || h.Music == nil || b == nil || inlineMessageID == "" {
		return
	}
	runInlineMediaFlow(ctx, b, inlineMediaFlowDeps{Music: h.Music, RateLimiter: h.RateLimiter}, inlineMessageID, userID, userName, platformName, trackID, qualityOverride, 0, false)
}

func (h *CallbackMusicHandler) shouldAutoDeleteListMessage(ctx context.Context, msg *telego.Message, userID int64, userSettings *botpkg.UserSettings, groupSettings *botpkg.GroupSettings) bool {
	if msg == nil {
		return false
	}
	if msg.Chat.Type == "private" {
		if userSettings != nil {
			return userSettings.AutoDeleteList
		}
		if h != nil && h.Music != nil && h.Music.Repo != nil && userID != 0 {
			if settings, err := h.Music.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
				return settings.AutoDeleteList
			}
		}
		return false
	}
	if groupSettings != nil {
		return groupSettings.AutoDeleteList
	}
	if h != nil && h.Music != nil && h.Music.Repo != nil {
		if settings, err := h.Music.Repo.GetGroupSettings(ctx, msg.Chat.ID); err == nil && settings != nil {
			return settings.AutoDeleteList
		}
	}
	return true
}

func isRequesterOrAdmin(ctx context.Context, b *telego.Bot, chatID int64, userID int64, requesterID int64) bool {
	if requesterID != 0 && requesterID == userID {
		return true
	}
	if b == nil {
		return false
	}
	member, err := b.GetChatMember(ctx, &telego.GetChatMemberParams{ChatID: telego.ChatID{ID: chatID}, UserID: userID})
	if err == nil && member != nil {
		status := member.MemberStatus()
		if status == telego.MemberStatusCreator || status == telego.MemberStatusAdministrator {
			return true
		}
	}
	admins, err := b.GetChatAdministrators(ctx, &telego.GetChatAdministratorsParams{ChatID: telego.ChatID{ID: chatID}})
	if err != nil {
		return false
	}
	for _, admin := range admins {
		if admin.MemberUser().ID != userID {
			continue
		}
		status := admin.MemberStatus()
		return status == telego.MemberStatusCreator || status == telego.MemberStatusAdministrator
	}
	return false
}

func parseMusicCallbackDataV2(args []string) parsedMusicCallback {
	if len(args) < 2 {
		return parsedMusicCallback{}
	}
	// "music mt <token>"：长/含空格 trackID 走 TTL payload store（与 inline "it" 对称）。
	if len(args) >= 3 && strings.TrimSpace(args[1]) == "mt" {
		payload, found := getInlineCallbackPayload(strings.TrimSpace(args[2]))
		if !found {
			return parsedMusicCallback{ok: true, tokenExpired: true}
		}
		return parsedMusicCallback{
			platformName:    payload.platformName,
			trackID:         payload.trackID,
			qualityOverride: payload.qualityValue,
			requesterID:     payload.requesterID,
			ok:              true,
		}
	}
	parsed := parsedMusicCallback{ok: true}
	switch len(args) {
	case 2:
		parsed.platformName = "netease"
		parsed.trackID = args[1]
	case 3:
		if isNumeric(args[1]) && isNumeric(args[2]) {
			parsed.platformName = "netease"
			parsed.trackID = args[1]
			parsed.requesterID, _ = strconv.ParseInt(args[2], 10, 64)
		} else {
			parsed.platformName = args[1]
			parsed.trackID = args[2]
		}
	case 4:
		parsed.platformName = args[1]
		parsed.trackID = args[2]
		parsed.requesterID, _ = strconv.ParseInt(args[3], 10, 64)
	default:
		parsed.platformName = args[1]
		parsed.trackID = args[2]
		parsed.qualityOverride = args[3]
		parsed.requesterID, _ = strconv.ParseInt(args[4], 10, 64)
	}
	return parsed
}
