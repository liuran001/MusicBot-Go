package handler

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
}

const episodePageSize = 8

var episodeSearchBackStore = struct {
	mu   sync.Mutex
	data map[string]string
}{data: make(map[string]string)}

var searchPagePattern = regexp.MustCompile(`第\s*(\d+)\s*/\s*(\d+)\s*页`)

func episodeBackKey(chatID int64, messageID int) string {
	return fmt.Sprintf("%d:%d", chatID, messageID)
}

func setEpisodeSearchBackCallback(chatID int64, messageID int, callbackData string) {
	key := episodeBackKey(chatID, messageID)
	episodeSearchBackStore.mu.Lock()
	defer episodeSearchBackStore.mu.Unlock()
	if strings.TrimSpace(callbackData) == "" {
		delete(episodeSearchBackStore.data, key)
		return
	}
	episodeSearchBackStore.data[key] = callbackData
}

func getEpisodeSearchBackCallback(chatID int64, messageID int) string {
	key := episodeBackKey(chatID, messageID)
	episodeSearchBackStore.mu.Lock()
	defer episodeSearchBackStore.mu.Unlock()
	return episodeSearchBackStore.data[key]
}

func extractSearchCurrentPage(text string) (int, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.Contains(trimmed, "搜索结果") {
		return 0, false
	}
	matches := searchPagePattern.FindStringSubmatch(trimmed)
	if len(matches) < 2 {
		return 0, false
	}
	page, err := strconv.Atoi(strings.TrimSpace(matches[1]))
	if err != nil || page <= 0 {
		return 0, false
	}
	return page, true
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
	if len(args) >= 3 && (args[1] == "i" || args[1] == "iep") {
		h.handleInlineCallback(ctx, b, query, args)
		return
	}
	if len(args) >= 4 && args[1] == "ep" {
		h.handleEpisodeCallback(ctx, b, query, args)
		return
	}

	parsed := parseMusicCallbackDataV2(args)
	if !parsed.ok {
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
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "请选择分P"})
			return
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
		if h.Music != nil {
			h.Music.dispatch(withForceNonSilent(ctx), b, msgToUse, platformName, trackID, qualityOverride)
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
			Text:            callbackDenied,
			ShowAlert:       true,
		})
		return
	}

	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
	if h.tryPresentEpisodePicker(ctx, b, query, msg, msgToUse, platformName, trackID, qualityOverride, query.From.ID, requesterID) {
		return
	}
	autoDelete := h.shouldAutoDeleteListMessage(ctx, msg, query.From.ID, nil, nil)
	if h.Music != nil {
		h.Music.dispatch(withForceNonSilent(ctx), b, msgToUse, platformName, trackID, qualityOverride)
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
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "处理中，请稍候"})
		return
	}
	defer releaseInlineGuard()
	if len(args) >= 4 && strings.TrimSpace(args[1]) == "iep" {
		h.handleInlineEpisodeCallback(ctx, b, query, args)
		return
	}
	if len(args) >= 4 && strings.TrimSpace(args[2]) == "random" {
		requesterID, _ := strconv.ParseInt(strings.TrimSpace(args[len(args)-1]), 10, 64)
		if requesterID != 0 && requesterID != query.From.ID {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackDenied, ShowAlert: true})
			return
		}
		platformName, trackID, qualityValue, ok := h.resolveInlineRandomTrack(ctx)
		if !ok {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "暂无可随机歌曲", ShowAlert: true})
			return
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
		h.runInlineDownloadFlowGuarded(detachContext(ctx), b, query.InlineMessageID, query.From.ID, query.From.Username, platformName, trackID, qualityValue)
		return
	}
	if len(args) < 5 {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
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
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
		return
	}
	if requesterID != 0 && requesterID != query.From.ID {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackDenied, ShowAlert: true})
		return
	}
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
	if h.tryPresentInlineEpisodePicker(ctx, b, query, platformName, trackID, qualityOverride, query.From.ID) {
		return
	}

	h.runInlineDownloadFlowGuarded(detachContext(ctx), b, query.InlineMessageID, query.From.ID, query.From.Username, platformName, trackID, qualityOverride)
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
	text, keyboard := buildInlineEpisodePickerPage(platformName, baseTrackID, qualityValue, requesterID, episodes, 1)
	if strings.TrimSpace(text) == "" || keyboard == nil {
		return false
	}
	params := &telego.EditMessageTextParams{InlineMessageID: query.InlineMessageID, Text: text, ReplyMarkup: keyboard}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "请选择分P"})
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
	text, keyboard := buildEpisodePickerPage(platformName, baseTrackID, qualityOverride, reqID, episodes, 1, backCallback)
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

func buildEpisodePickerPage(platformName, trackID, qualityValue string, requesterID int64, episodes []platform.Episode, page int, backCallback string) (string, *telego.InlineKeyboardMarkup) {
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

	textLines := buildEpisodeHeaderLines(episodes)
	textLines = append(textLines, fmt.Sprintf("第 %d/%d 页", page, totalPages), "")
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
				nav = append(nav, telego.InlineKeyboardButton{Text: "⬅️ 上一页", CallbackData: cb})
			}
		}
		if page < totalPages {
			if cb := buildEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, page+1); cb != "" {
				nav = append(nav, telego.InlineKeyboardButton{Text: "➡️ 下一页", CallbackData: cb})
			}
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
		extraRow := make([]telego.InlineKeyboardButton, 0, 2)
		if page > 1 {
			if cb := buildEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, 1); cb != "" {
				extraRow = append(extraRow, telego.InlineKeyboardButton{Text: "🏠 回到主页", CallbackData: cb})
			}
		}
		if page < totalPages {
			if cb := buildEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, totalPages); cb != "" {
				extraRow = append(extraRow, telego.InlineKeyboardButton{Text: "⏭️ 最后一页", CallbackData: cb})
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
			bottom = append(bottom, telego.InlineKeyboardButton{Text: "↩️ 返回搜索结果", CallbackData: backCallback})
		}
		bottom = append(bottom, telego.InlineKeyboardButton{Text: "❌ 关闭", CallbackData: closeCB})
		rows = append(rows, bottom)
	}
	return strings.Join(textLines, "\n"), &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buildEpisodeHeaderLines(episodes []platform.Episode) []string {
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
		lines = append(lines, "标题: "+titleText)
	}
	if up := strings.TrimSpace(first.CreatorName); up != "" {
		upText := mdV2Replacer.Replace(up)
		if strings.TrimSpace(first.CreatorURL) != "" {
			upText = fmt.Sprintf("[%s](%s)", upText, strings.TrimSpace(first.CreatorURL))
		}
		lines = append(lines, "UP主: "+upText)
	}
	if desc := strings.TrimSpace(first.Description); desc != "" {
		if quote := formatExpandableQuote(mdV2Replacer.Replace(truncateText(desc, 800))); quote != "" {
			lines = append(lines, "", quote)
		}
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	return lines
}

func buildEpisodeShowCallbackData(platformName, trackID, qualityValue string, requesterID int64, page int) string {
	return buildEpisodeCallbackData("s", platformName, trackID, qualityValue, requesterID, page)
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
	data = fmt.Sprintf("music ep %s %s %s %d %d", action, platformName, trackID, requesterID, page)
	if len(data) <= 64 {
		return data
	}
	return ""
}

func parseEpisodeCallbackArgs(args []string) (action, platformName, trackID, qualityValue string, requesterID int64, page int, ok bool) {
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
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "处理中，请稍候"})
		return
	}
	defer releaseEpGuard()
	if msg.Chat.Type != "private" && !isRequesterOrAdmin(ctx, b, msg.Chat.ID, query.From.ID, requesterID) {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackDenied, ShowAlert: true})
		return
	}
	if requesterID != 0 && requesterID != query.From.ID && msg.Chat.Type == "private" {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackDenied, ShowAlert: true})
		return
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "c":
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
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
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "选集加载失败", ShowAlert: true})
			return
		}
		backCallback := getEpisodeSearchBackCallback(msg.Chat.ID, msg.MessageID)
		text, keyboard := buildEpisodePickerPage(platformName, trackID, qualityValue, requesterID, episodes, page, backCallback)
		if text == "" || keyboard == nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "选集加载失败", ShowAlert: true})
			return
		}
		params := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID, Text: text, ParseMode: telego.ModeMarkdownV2, LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true}, ReplyMarkup: keyboard}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
	case "p":
		msgToUse := msg
		if msg.ReplyToMessage != nil {
			msgToUse = msg.ReplyToMessage
		}
		selectedTrackID := buildEpisodeTrackID(h.Music.PlatformManager, platformName, trackID, page, true)
		if strings.TrimSpace(selectedTrackID) == "" {
			selectedTrackID = trackID
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
		autoDelete := h.shouldAutoDeleteListMessage(ctx, msg, query.From.ID, nil, nil)
		h.Music.dispatch(withForceNonSilent(ctx), b, msgToUse, platformName, selectedTrackID, qualityValue)
		if autoDelete {
			deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
			if h.RateLimiter != nil {
				_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
			} else {
				_ = b.DeleteMessage(ctx, deleteParams)
			}
		}
	default:
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
	}
}

func parseInlineEpisodeCallbackArgs(args []string) (action, platformName, trackID, qualityValue string, requesterID int64, page int, ok bool) {
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
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
		return
	}
	if requesterID != 0 && requesterID != query.From.ID {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackDenied, ShowAlert: true})
		return
	}
	iepGuardKey := fmt.Sprintf("music-iep:%s", strings.TrimSpace(query.InlineMessageID))
	releaseIepGuard, acquired := tryAcquireCallbackInFlight(iepGuardKey, 8*time.Second)
	if !acquired {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "处理中，请稍候"})
		return
	}
	defer releaseIepGuard()
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "c":
		params := &telego.EditMessageTextParams{InlineMessageID: query.InlineMessageID, Text: "已关闭选集"}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
		return
	case "s", "n":
		episodes, err := h.fetchEpisodes(ctx, platformName, trackID)
		if err != nil || len(episodes) == 0 {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "选集加载失败", ShowAlert: true})
			return
		}
		text, keyboard := buildInlineEpisodePickerPage(platformName, trackID, qualityValue, requesterID, episodes, page)
		if text == "" || keyboard == nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "选集加载失败", ShowAlert: true})
			return
		}
		params := &telego.EditMessageTextParams{InlineMessageID: query.InlineMessageID, Text: text, ParseMode: telego.ModeMarkdownV2, LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true}, ReplyMarkup: keyboard}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
	case "p":
		selectedTrackID := buildEpisodeTrackID(h.Music.PlatformManager, platformName, trackID, page, true)
		if strings.TrimSpace(selectedTrackID) == "" {
			selectedTrackID = trackID
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: callbackText})
		h.runInlineDownloadFlowGuarded(detachContext(ctx), b, query.InlineMessageID, query.From.ID, query.From.Username, platformName, selectedTrackID, qualityValue)
	default:
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "参数错误", ShowAlert: true})
	}
}

func buildInlineEpisodePickerPage(platformName, trackID, qualityValue string, requesterID int64, episodes []platform.Episode, page int) (string, *telego.InlineKeyboardMarkup) {
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
	textLines = append(textLines, fmt.Sprintf("%s *%s* 选集", platformEmoji(nil, platformName), mdV2Replacer.Replace(platformDisplayName(nil, platformName))), "")
	textLines = append(textLines, buildEpisodeHeaderLines(episodes)...)
	textLines = append(textLines, fmt.Sprintf("第 %d/%d 页", page, totalPages), "")
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
				nav = append(nav, telego.InlineKeyboardButton{Text: "⬅️ 上一页", CallbackData: cb})
			}
		}
		if page < totalPages {
			if cb := buildInlineEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, page+1); cb != "" {
				nav = append(nav, telego.InlineKeyboardButton{Text: "➡️ 下一页", CallbackData: cb})
			}
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
		extraRow := make([]telego.InlineKeyboardButton, 0, 2)
		if page > 1 {
			if cb := buildInlineEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, 1); cb != "" {
				extraRow = append(extraRow, telego.InlineKeyboardButton{Text: "🏠 回到主页", CallbackData: cb})
			}
		}
		if page < totalPages {
			if cb := buildInlineEpisodeNavCallbackData(platformName, trackID, qualityValue, requesterID, totalPages); cb != "" {
				extraRow = append(extraRow, telego.InlineKeyboardButton{Text: "⏭️ 最后一页", CallbackData: cb})
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
	withInlineMessageLock(inlineMessageID, func() {
		lastInlineText := ""
		setInlineText := func(text string, markup *telego.InlineKeyboardMarkup) {
			text = strings.TrimSpace(text)
			if text == "" || text == lastInlineText {
				return
			}
			if markup != nil {
				params := &telego.EditMessageTextParams{InlineMessageID: inlineMessageID, Text: text, ReplyMarkup: markup}
				if h.RateLimiter != nil {
					_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
				} else {
					_, _ = b.EditMessageText(ctx, params)
				}
				lastInlineText = text
				return
			}
			params := &telego.EditMessageTextParams{InlineMessageID: inlineMessageID, Text: text, ReplyMarkup: markup}
			if h.RateLimiter != nil {
				_, _ = telegram.EditMessageTextBestEffort(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.EditMessageText(ctx, params)
			}
			lastInlineText = text
		}
		clearInlineReplyMarkup := func() {
			params := &telego.EditMessageReplyMarkupParams{InlineMessageID: inlineMessageID}
			if h.RateLimiter != nil {
				_, _ = telegram.EditMessageReplyMarkupWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.EditMessageReplyMarkup(ctx, params)
			}
		}
		retryMarkup := buildInlineSendKeyboard(platformName, trackID, qualityOverride, userID)
		editInlineMedia := func(songInfo *botpkg.SongInfo) (bool, error) {
			if songInfo == nil || strings.TrimSpace(songInfo.FileID) == "" {
				return false, fmt.Errorf("inline media requires file_id")
			}
			media := &telego.InputMediaAudio{
				Type:      telego.MediaTypeAudio,
				Media:     telego.InputFile{FileID: songInfo.FileID},
				Caption:   buildMusicCaption(h.Music.PlatformManager, songInfo, h.Music.BotName),
				ParseMode: telego.ModeHTML,
				Title:     songInfo.SongName,
				Performer: songInfo.SongArtists,
				Duration:  songInfo.Duration,
			}
			if strings.TrimSpace(songInfo.ThumbFileID) != "" {
				media.Thumbnail = &telego.InputFile{FileID: songInfo.ThumbFileID}
			}
			var replyMarkup *telego.InlineKeyboardMarkup
			if resolveForwardButtonEnabledForUser(ctx, h.Music.Repo, userID) {
				replyMarkup = buildForwardKeyboard(songInfo.TrackURL, songInfo.Platform, songInfo.TrackID)
			}
			params := &telego.EditMessageMediaParams{
				InlineMessageID: inlineMessageID,
				Media:           media,
				ReplyMarkup:     replyMarkup,
			}
			var err error
			if h.RateLimiter != nil {
				_, err = telegram.EditMessageMediaWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, err = b.EditMessageMedia(ctx, params)
			}
			if err != nil && telegram.IsMessageNotModified(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return true, nil
		}

		progress := func(text string) {
			setInlineText(text, nil)
		}
		if cachedSong, _, cacheErr := h.Music.findInlineCachedSong(ctx, userID, platformName, trackID, qualityOverride); cacheErr == nil && cachedSong != nil {
			modified, err := editInlineMedia(cachedSong)
			if err == nil {
				if modified && h.Music.Repo != nil {
					if err := h.Music.Repo.IncrementSendCount(ctx); err != nil && h.Music.Logger != nil {
						h.Music.Logger.Error("failed to update send count", "error", err)
					}
				}
				return
			}
			if h.Music.Logger != nil {
				h.Music.Logger.Warn("failed to edit cached inline media, fallback to prepare", "platform", platformName, "trackID", trackID, "error", err)
			}
		}
		clearInlineReplyMarkup()
		setInlineText(waitForDown, nil)
		songInfo, err := h.Music.prepareInlineSong(ctx, b, userID, userName, platformName, trackID, qualityOverride, progress)
		if err != nil {
			if h.Music.Logger != nil {
				h.Music.Logger.Error("failed to prepare inline song", "platform", platformName, "trackID", trackID, "error", err)
			}
			setInlineText(buildMusicInfoText("", "", "", userVisibleDownloadError(err)), retryMarkup)
			return
		}
		modified, err := editInlineMedia(songInfo)
		if err != nil {
			if h.Music.Logger != nil {
				h.Music.Logger.Error("failed to edit inline media", "platform", platformName, "trackID", trackID, "error", err)
			}
			setInlineText(buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), userVisibleDownloadError(err)), retryMarkup)
			return
		}
		if modified && h.Music.Repo != nil {
			if err := h.Music.Repo.IncrementSendCount(ctx); err != nil && h.Music.Logger != nil {
				h.Music.Logger.Error("failed to update send count", "error", err)
			}
		}
	})
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
