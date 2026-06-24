package handler

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// FavoritesHandler renders the favorites list and handles the /fav and
// /favorites commands. The list is shown as a single text message with an inline
// keyboard, in three contexts that share one renderer:
//
//   - a normal chat (a new message, e.g. /fav with no payload or @bot in a group)
//   - guest mode (an inline message via AnswerGuestQuery, editable in place)
//
// Inline-mode empty queries instead render favorites as selectable cards (see
// inline.go), which is a different UI and not handled here.
type FavoritesHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
	Music           *MusicHandler
	BotName         string
	Logger          botpkg.Logger
	PageSize        int
}

// favoriteListPayload is the per-list-message state referenced by a short token
// in list callback data. The view and page are carried inline in the callback;
// only the immutable identity (which group, who opened it) lives here.
type favoriteListPayload struct {
	groupChatID int64
	requesterID int64
	storedAt    time.Time
}

var favoriteListPayloads = newTTLStore[favoriteListPayload](1 * time.Hour)
var favoriteListTokenCounter uint64

func storeFavoriteListPayload(p favoriteListPayload) string {
	p.storedAt = time.Now()
	token := strconv.FormatUint(uint64(time.Now().UnixNano()), 36) + strconv.FormatUint(atomic.AddUint64(&favoriteListTokenCounter, 1), 36)
	favoriteListPayloads.Store(token, p)
	return token
}

func (h *FavoritesHandler) pageSize() int {
	if h != nil && h.PageSize > 0 {
		return h.PageSize
	}
	return 8
}

// truncateButtonLabel rune-safely caps a button label so a long title doesn't
// produce an oversized button.
func truncateButtonLabel(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

// favoriteTrackLink returns a clickable URL for a favorite's track, falling back
// to a constructed netease song URL when none was stored.
func favoriteTrackLink(fav *botpkg.Favorite) string {
	if u := strings.TrimSpace(fav.TrackURL); u != "" {
		return u
	}
	if fav.Platform == "netease" && strings.TrimSpace(fav.TrackID) != "" {
		return "https://music.163.com/song?id=" + strings.TrimSpace(fav.TrackID)
	}
	return ""
}

// favoriteArtistsHTML renders the artists as HTML, hyperlinking each to its
// stored URL when available. Matches buildMusicCaption's "/"-joined convention.
func favoriteArtistsHTML(fav *botpkg.Favorite) string {
	raw := strings.TrimSpace(fav.SongArtists)
	if raw == "" {
		return ""
	}
	artists := strings.Split(raw, "/")
	urls := strings.Split(fav.SongArtistsURLs, ",")
	parts := make([]string, 0, len(artists))
	for i, a := range artists {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		esc := html.EscapeString(a)
		if i < len(urls) && strings.TrimSpace(urls[i]) != "" {
			esc = fmt.Sprintf("<a href=\"%s\">%s</a>", html.EscapeString(strings.TrimSpace(urls[i])), esc)
		}
		parts = append(parts, esc)
	}
	return strings.Join(parts, " / ")
}

// favoriteSongHTML renders "<song> - <artists>" with the song name linked to the
// track URL and each artist to its URL (HTML parse mode).
func favoriteSongHTML(fav *botpkg.Favorite) string {
	name := strings.TrimSpace(fav.SongName)
	if name == "" {
		name = fav.Platform + ":" + fav.TrackID
	}
	nameHTML := html.EscapeString(name)
	if link := favoriteTrackLink(fav); link != "" {
		nameHTML = fmt.Sprintf("<a href=\"%s\">%s</a>", html.EscapeString(link), nameHTML)
	}
	if artistsHTML := favoriteArtistsHTML(fav); artistsHTML != "" {
		return nameHTML + " - " + artistsHTML
	}
	return nameHTML
}

func favoriteScopeForView(view string, payload favoriteListPayload) (string, int64) {
	if view == "g" {
		return botpkg.FavoriteScopeGroup, payload.groupChatID
	}
	return botpkg.FavoriteScopeUser, payload.requesterID
}

// defaultListView picks the initial view: the group list when group favorites
// are enabled and non-empty (per the spec — "default shows group favorites"),
// otherwise the personal list.
func (h *FavoritesHandler) defaultListView(ctx context.Context, groupChatID int64, isGroupChat bool) string {
	if isGroupChat && groupChatID != 0 && groupFavoritesAvailable(resolveGroupFavoritesMode(ctx, h.Repo, groupChatID)) {
		if n, _ := h.Repo.CountFavorites(ctx, botpkg.FavoriteScopeGroup, groupChatID); n > 0 {
			return "g"
		}
	}
	return "u"
}

type favoriteListContext struct {
	token       string
	groupChatID int64
	requesterID int64
	isGroupChat bool
	view        string
	page        int
}

// buildListView renders the list text and inline keyboard for the given context.
func (h *FavoritesHandler) buildListView(ctx context.Context, lc favoriteListContext) (string, *telego.InlineKeyboardMarkup) {
	groupAvailable := lc.isGroupChat && lc.groupChatID != 0 && groupFavoritesAvailable(resolveGroupFavoritesMode(ctx, h.Repo, lc.groupChatID))
	view := lc.view
	if view == "g" && !groupAvailable {
		view = "u"
	}
	scopeType, scopeID := favoriteScopeForView(view, favoriteListPayload{groupChatID: lc.groupChatID, requesterID: lc.requesterID})

	pageSize := h.pageSize()
	total, _ := h.Repo.CountFavorites(ctx, scopeType, scopeID)
	pageCount := 1
	if total > 0 {
		pageCount = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	page := lc.page
	if page < 1 {
		page = 1
	}
	if page > pageCount {
		page = pageCount
	}
	offset := (page - 1) * pageSize
	favs, _ := h.Repo.ListFavorites(ctx, scopeType, scopeID, pageSize, offset)

	var sb strings.Builder
	header := "⭐ 我的收藏"
	if view == "g" {
		header = "👥 群聊收藏"
	}
	sb.WriteString(fmt.Sprintf("%s（%d 首）", header, total))
	if pageCount > 1 {
		sb.WriteString(fmt.Sprintf("  ·  第 %d/%d 页", page, pageCount))
	}
	sb.WriteString("\n")
	if total == 0 {
		sb.WriteString("\n还没有收藏。发送歌曲后点底部「⭐ 收藏」即可收藏。")
	} else {
		sb.WriteString("\n")
		for i, fav := range favs {
			idx := offset + i + 1
			line := fmt.Sprintf("%d. %s", idx, favoriteSongHTML(fav))
			if view == "g" {
				who := strings.TrimSpace(fav.AddedByName)
				if who == "" {
					who = "匿名"
				}
				line += "  · 👤 " + html.EscapeString(who)
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	var rows [][]telego.InlineKeyboardButton
	// One row per song: a wide "send" button (downloads/sends the track) plus a
	// compact "remove" button.
	for i, fav := range favs {
		idx := offset + i + 1
		name := strings.TrimSpace(fav.SongName)
		if name == "" {
			name = fav.Platform + ":" + fav.TrackID
		}
		sendData := fmt.Sprintf("favm s %s %s %d %d", lc.token, view, page, i)
		rmData := fmt.Sprintf("favm x %s %s %d %d", lc.token, view, page, i)
		rows = append(rows, []telego.InlineKeyboardButton{
			{Text: truncateButtonLabel(fmt.Sprintf("▶️ %d. %s", idx, name), 42), CallbackData: sendData},
			{Text: "🗑", CallbackData: rmData},
		})
	}

	var ctrl []telego.InlineKeyboardButton
	if groupAvailable {
		if view == "g" {
			ctrl = append(ctrl, telego.InlineKeyboardButton{Text: "⭐ 我的收藏", CallbackData: fmt.Sprintf("favm n %s u 1", lc.token)})
		} else {
			ctrl = append(ctrl, telego.InlineKeyboardButton{Text: "👥 群聊收藏", CallbackData: fmt.Sprintf("favm n %s g 1", lc.token)})
		}
	}
	if total > 0 {
		ctrl = append(ctrl, telego.InlineKeyboardButton{Text: "🎲 随机一首", CallbackData: fmt.Sprintf("favm r %s %s", lc.token, view)})
	}
	if len(ctrl) > 0 {
		rows = append(rows, ctrl)
	}

	if pageCount > 1 {
		var pg []telego.InlineKeyboardButton
		if page > 1 {
			pg = append(pg, telego.InlineKeyboardButton{Text: "◀ 上一页", CallbackData: fmt.Sprintf("favm n %s %s %d", lc.token, view, page-1)})
		}
		if page < pageCount {
			pg = append(pg, telego.InlineKeyboardButton{Text: "下一页 ▶", CallbackData: fmt.Sprintf("favm n %s %s %d", lc.token, view, page+1)})
		}
		if len(pg) > 0 {
			rows = append(rows, pg)
		}
	}

	var markup *telego.InlineKeyboardMarkup
	if len(rows) > 0 {
		markup = &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
	}
	return sb.String(), markup
}

// sendListMessage posts the favorites list as a new message to a normal chat.
func (h *FavoritesHandler) sendListMessage(ctx context.Context, b *telego.Bot, chatID int64, replyToID int, requesterID, groupChatID int64, isGroupChat bool, view string) {
	token := storeFavoriteListPayload(favoriteListPayload{groupChatID: groupChatID, requesterID: requesterID})
	text, markup := h.buildListView(ctx, favoriteListContext{token: token, groupChatID: groupChatID, requesterID: requesterID, isGroupChat: isGroupChat, view: view, page: 1})
	params := &telego.SendMessageParams{
		ChatID:             telego.ChatID{ID: chatID},
		Text:               text,
		ParseMode:          telego.ModeHTML,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if replyToID != 0 {
		params.ReplyParameters = &telego.ReplyParameters{MessageID: replyToID}
	}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}

// answerGuestList answers a guest query with the favorites list as an inline
// message. Guest mode has the group chat ID (Message.Chat.ID), so it can show
// group favorites — unlike inline mode.
func (h *FavoritesHandler) answerGuestList(ctx context.Context, b *telego.Bot, message *telego.Message, guestQueryID string) {
	requesterID, _ := guestRequester(message)
	groupChatID, isGroupChat := guestChatContext(message)
	view := "u"
	if isGroupChat {
		view = h.defaultListView(ctx, groupChatID, isGroupChat)
	}
	token := storeFavoriteListPayload(favoriteListPayload{groupChatID: groupChatID, requesterID: requesterID})
	text, markup := h.buildListView(ctx, favoriteListContext{token: token, groupChatID: groupChatID, requesterID: requesterID, isGroupChat: isGroupChat, view: view, page: 1})
	article := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  nextGuestResultID("fav"),
		Title:               "收藏列表",
		InputMessageContent: &telego.InputTextMessageContent{MessageText: text, ParseMode: telego.ModeHTML, LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true}},
		ReplyMarkup:         markup,
	}
	_, _ = b.AnswerGuestQuery(ctx, &telego.AnswerGuestQueryParams{GuestQueryID: guestQueryID, Result: article})
}

func (h *FavoritesHandler) answerCb(ctx context.Context, b *telego.Bot, callbackQueryID, text string) {
	params := &telego.AnswerCallbackQueryParams{CallbackQueryID: callbackQueryID}
	if text != "" {
		params.Text = text
	}
	_ = b.AnswerCallbackQuery(ctx, params)
}

func (h *FavoritesHandler) reply(ctx context.Context, b *telego.Bot, message *telego.Message, text string) {
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		Text:            text,
		ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}

// editListMessage re-renders the list in place, branching on whether the list is
// a normal chat message or an inline (guest) message.
func (h *FavoritesHandler) editListMessage(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, text string, markup *telego.InlineKeyboardMarkup) {
	if query.Message != nil {
		if msg := query.Message.Message(); msg != nil {
			params := &telego.EditMessageTextParams{
				ChatID:             telego.ChatID{ID: msg.Chat.ID},
				MessageID:          msg.MessageID,
				Text:               text,
				ParseMode:          telego.ModeHTML,
				LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
			}
			if markup != nil {
				params.ReplyMarkup = markup
			}
			if h.RateLimiter != nil {
				_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.EditMessageText(ctx, params)
			}
			return
		}
	}
	if strings.TrimSpace(query.InlineMessageID) != "" {
		params := &telego.EditMessageTextParams{
			InlineMessageID:    query.InlineMessageID,
			Text:               text,
			ParseMode:          telego.ModeHTML,
			LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
		}
		if markup != nil {
			params.ReplyMarkup = markup
		}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
	}
}

func (h *FavoritesHandler) rerender(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, payload favoriteListPayload, token, view string, page int) {
	lc := favoriteListContext{
		token:       token,
		groupChatID: payload.groupChatID,
		requesterID: payload.requesterID,
		isGroupChat: payload.groupChatID != 0,
		view:        view,
		page:        page,
	}
	text, markup := h.buildListView(ctx, lc)
	h.editListMessage(ctx, b, query, text, markup)
}

// handleListCallback dispatches favorites-list interactions ("favm ...").
func (h *FavoritesHandler) handleListCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	if len(args) < 3 {
		h.answerCb(ctx, b, query.ID, "")
		return
	}
	action := args[1]
	token := strings.TrimSpace(args[2])
	payload, ok := favoriteListPayloads.Load(token)
	if !ok {
		h.answerCb(ctx, b, query.ID, "列表已过期，请重新打开")
		return
	}
	clicker := query.From.ID
	switch action {
	case "n": // navigate: favm n <token> <view> <page>
		if len(args) < 5 {
			h.answerCb(ctx, b, query.ID, "")
			return
		}
		if clicker != payload.requesterID {
			h.answerCb(ctx, b, query.ID, "这不是你打开的收藏列表")
			return
		}
		view := args[3]
		page, _ := strconv.Atoi(args[4])
		h.answerCb(ctx, b, query.ID, "")
		h.rerender(ctx, b, query, payload, token, view, page)
	case "r": // random send: favm r <token> <view>
		if len(args) < 4 {
			h.answerCb(ctx, b, query.ID, "")
			return
		}
		if clicker != payload.requesterID {
			h.answerCb(ctx, b, query.ID, "这不是你打开的收藏列表")
			return
		}
		h.handleRandom(ctx, b, query, payload, args[3])
	case "s": // send a specific favorite: favm s <token> <view> <page> <idx>
		if len(args) < 6 {
			h.answerCb(ctx, b, query.ID, "")
			return
		}
		if clicker != payload.requesterID {
			h.answerCb(ctx, b, query.ID, "这不是你打开的收藏列表")
			return
		}
		view := args[3]
		page, _ := strconv.Atoi(args[4])
		idx, _ := strconv.Atoi(args[5])
		h.handleSend(ctx, b, query, payload, view, page, idx)
	case "x": // remove: favm x <token> <view> <page> <idx>
		if len(args) < 6 {
			h.answerCb(ctx, b, query.ID, "")
			return
		}
		view := args[3]
		page, _ := strconv.Atoi(args[4])
		idx, _ := strconv.Atoi(args[5])
		h.handleRemove(ctx, b, query, payload, token, view, page, idx, clicker)
	default:
		h.answerCb(ctx, b, query.ID, "")
	}
}

func (h *FavoritesHandler) handleRandom(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, payload favoriteListPayload, view string) {
	scopeType, scopeID := favoriteScopeForView(view, payload)
	fav, err := h.Repo.RandomFavorite(ctx, scopeType, scopeID)
	if err != nil || fav == nil {
		h.answerCb(ctx, b, query.ID, "收藏列表是空的")
		return
	}
	h.sendFavoriteTrack(ctx, b, query, payload, fav)
}

func (h *FavoritesHandler) handleSend(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, payload favoriteListPayload, view string, page, idx int) {
	scopeType, scopeID := favoriteScopeForView(view, payload)
	pageSize := h.pageSize()
	offset := (page - 1) * pageSize
	favs, _ := h.Repo.ListFavorites(ctx, scopeType, scopeID, pageSize, offset)
	if idx < 0 || idx >= len(favs) {
		h.answerCb(ctx, b, query.ID, "该收藏已不存在")
		return
	}
	h.sendFavoriteTrack(ctx, b, query, payload, favs[idx])
}

// sendFavoriteTrack delivers a favorited track. In a normal chat it sends a new
// audio message; in guest/inline mode it edits the list message in place into
// the audio — the same approach as guest search (the only way to deliver media
// when the bot can't post a new message to the chat).
func (h *FavoritesHandler) sendFavoriteTrack(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, payload favoriteListPayload, fav *botpkg.Favorite) {
	if h.Music == nil || fav == nil {
		h.answerCb(ctx, b, query.ID, "")
		return
	}
	// Normal chat: send a new audio message attributed to the clicker.
	if query.Message != nil {
		if msg := query.Message.Message(); msg != nil {
			h.answerCb(ctx, b, query.ID, "正在发送…")
			clicker := query.From
			sendMsg := *msg
			sendMsg.From = &clicker
			h.Music.dispatch(withDisableFallback(withForceNonSilent(ctx)), b, &sendMsg, fav.Platform, fav.TrackID, "")
			return
		}
	}
	// Guest/inline: edit the message in place into the audio.
	if strings.TrimSpace(query.InlineMessageID) != "" {
		h.answerCb(ctx, b, query.ID, "正在发送…")
		userName := callbackUserDisplayName(&query.From)
		isGroup := payload.groupChatID != 0
		go runInlineMediaFlow(detachContext(ctx), b, inlineMediaFlowDeps{Music: h.Music, RateLimiter: h.RateLimiter}, query.InlineMessageID, query.From.ID, userName, fav.Platform, fav.TrackID, "", payload.groupChatID, isGroup)
		return
	}
	h.answerCb(ctx, b, query.ID, "")
}

func (h *FavoritesHandler) handleRemove(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, payload favoriteListPayload, token, view string, page, idx int, clicker int64) {
	scopeType, scopeID := favoriteScopeForView(view, payload)
	// Personal list: only its owner (the requester) may remove.
	if view != "g" && clicker != payload.requesterID {
		h.answerCb(ctx, b, query.ID, "这不是你打开的收藏列表")
		return
	}
	pageSize := h.pageSize()
	offset := (page - 1) * pageSize
	favs, _ := h.Repo.ListFavorites(ctx, scopeType, scopeID, pageSize, offset)
	if idx < 0 || idx >= len(favs) {
		h.answerCb(ctx, b, query.ID, "该收藏已不存在")
		h.rerender(ctx, b, query, payload, token, view, page)
		return
	}
	fav := favs[idx]
	// Group list: the collector may remove their own; admins may remove any.
	// In guest mode the admin check fails (bot is not a member), so it degrades
	// to collector-only — the safe default.
	if view == "g" {
		if fav.AddedByUserID != clicker && !isRequesterOrAdmin(ctx, b, scopeID, clicker, 0) {
			h.answerCb(ctx, b, query.ID, "只能取消自己收藏的歌曲（管理员可取消任意）")
			return
		}
	}
	if err := h.Repo.RemoveFavorite(ctx, scopeType, scopeID, fav.Platform, fav.TrackID); err != nil {
		h.answerCb(ctx, b, query.ID, "操作失败，请稍后再试")
		return
	}
	h.answerCb(ctx, b, query.ID, "已取消收藏")
	h.rerender(ctx, b, query, payload, token, view, page)
}

// Handle handles /fav and /favorites. With no payload (and no replied song) it
// shows the list; with a track (a payload that resolves, or a replied song
// message) it toggles the favorite. A leading/trailing "group"/"g"/"群" token
// targets the group scope.
func (h *FavoritesHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil {
		return
	}
	message := update.Message
	args := commandArguments(message.Text)
	args, wantGroup := extractGroupScopeToken(args)

	target := strings.TrimSpace(args)
	if target == "" && message.ReplyToMessage != nil {
		target = strings.TrimSpace(repliedMessageQuery(message.ReplyToMessage))
	}
	if target != "" {
		h.handleCommandToggle(ctx, b, message, target, wantGroup)
		return
	}

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
	view := "u"
	if wantGroup && isGroupChat {
		view = "g"
	} else {
		view = h.defaultListView(ctx, groupChatID, isGroupChat)
	}
	h.sendListMessage(ctx, b, message.Chat.ID, message.MessageID, requesterID, groupChatID, isGroupChat, view)
}

func (h *FavoritesHandler) handleCommandToggle(ctx context.Context, b *telego.Bot, message *telego.Message, target string, wantGroup bool) {
	if h.Music == nil || message.From == nil {
		return
	}
	platformName, trackID, ok := h.Music.resolveTrackFromQuery(ctx, message, target)
	if !ok {
		h.reply(ctx, b, message, "未找到歌曲。请回复一条歌曲消息，或在命令后附上链接 / ID。")
		return
	}
	scopeType := botpkg.FavoriteScopeUser
	scopeID := message.From.ID
	if wantGroup {
		if message.Chat.Type == "private" {
			h.reply(ctx, b, message, "私聊无法使用群聊收藏。")
			return
		}
		scopeType = botpkg.FavoriteScopeGroup
		scopeID = message.Chat.ID
	}
	out, err := toggleFavorite(ctx, b, h.Repo, h.PlatformManager, scopeType, scopeID, message.From.ID, callbackUserDisplayName(message.From), platformName, trackID)
	if err != nil {
		h.reply(ctx, b, message, "操作失败，请稍后再试。")
		return
	}
	h.reply(ctx, b, message, favoriteToggleMessage(out, scopeType))
}

// extractGroupScopeToken strips a leading or trailing group-scope token
// ("group"/"g"/"群"/"群聊"/"群组") from the args, reporting whether one was found.
func extractGroupScopeToken(args string) (string, bool) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "", false
	}
	isTok := func(s string) bool {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "group", "g", "群", "群聊", "群组":
			return true
		}
		return false
	}
	if isTok(fields[0]) {
		return strings.TrimSpace(strings.Join(fields[1:], " ")), true
	}
	if isTok(fields[len(fields)-1]) {
		return strings.TrimSpace(strings.Join(fields[:len(fields)-1], " ")), true
	}
	return args, false
}
