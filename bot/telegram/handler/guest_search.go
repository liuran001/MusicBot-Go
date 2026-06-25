package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

const (
	guestSearchCacheTTL        = 10 * time.Minute
	guestSearchCacheMaxEntries = 512
)

type guestSearchStore struct {
	mu   sync.Mutex
	data map[string]*searchState
}

// guestSearchTokenCounter 与 UnixNano 拼接，保证 guest 搜索 token 在同一纳秒的
// 并发调用下仍唯一。
var guestSearchTokenCounter uint64

func newGuestSearchStore() *guestSearchStore {
	return &guestSearchStore{data: make(map[string]*searchState)}
}

func (s *guestSearchStore) store(state *searchState) string {
	if state == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = make(map[string]*searchState)
	}
	s.cleanupLocked()
	// 纯 UnixNano 在同一纳秒两次调用会碰撞、后者覆盖前者。追加 atomic 计数器
	// 保证唯一（对齐 guest_mode.go 的 nextGuestResultID 与 helpers.go 的
	// inlineCallbackTokenCounter）。
	token := strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.FormatUint(atomic.AddUint64(&guestSearchTokenCounter, 1), 36)
	state.updatedAt = time.Now()
	s.data[token] = state
	return token
}

func (s *guestSearchStore) get(token string) (*searchState, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return nil, false
	}
	s.cleanupLocked()
	state, ok := s.data[token]
	if ok && state != nil {
		state.updatedAt = time.Now()
	}
	return state, ok
}

func (s *guestSearchStore) delete(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, token)
}

func (s *guestSearchStore) cleanupLocked() {
	if s.data == nil {
		return
	}
	cutoff := time.Now().Add(-guestSearchCacheTTL)
	for key, state := range s.data {
		if state == nil || state.updatedAt.Before(cutoff) {
			delete(s.data, key)
		}
	}
	for len(s.data) > guestSearchCacheMaxEntries {
		oldestKey := ""
		oldestTime := time.Now()
		first := true
		for key, state := range s.data {
			updatedAt := time.Time{}
			if state != nil {
				updatedAt = state.updatedAt
			}
			if first || updatedAt.Before(oldestTime) {
				first = false
				oldestKey = key
				oldestTime = updatedAt
			}
		}
		if oldestKey == "" {
			break
		}
		delete(s.data, oldestKey)
	}
}

func (h *GuestModeHandler) guestSearchStore() *guestSearchStore {
	h.searchOnce.Do(func() {
		if h.search == nil {
			h.search = newGuestSearchStore()
		}
	})
	return h.search
}

func (h *GuestModeHandler) guestPageSize() int {
	if h == nil || h.SearchHandler == nil {
		return 8
	}
	return h.SearchHandler.pageSize()
}

func (h *GuestModeHandler) guestSearchLimit(platformName string) int {
	if h == nil || h.SearchHandler == nil {
		if strings.TrimSpace(platformName) == "netease" {
			return neteaseSearchLimit
		}
		return defaultSearchLimit
	}
	return h.SearchHandler.searchLimit(platformName)
}

func (h *GuestModeHandler) renderGuestSearchPage(ctx context.Context, state *searchState, token string, page int) (string, *telego.InlineKeyboardMarkup) {
	if h == nil || state == nil {
		return tr(ctx, "no_results"), nil
	}
	tracks, _ := state.getTracks(state.platform)
	if page < 1 {
		page = 1
	}
	pageSize := h.guestPageSize()
	pageCount := 1
	if len(tracks) > 0 {
		pageCount = (len(tracks)-1)/pageSize + 1
	}
	displayPageCount := pageCount
	hasMore := state.hasMore(state.platform)
	if hasMore {
		limit := state.limit
		if limit <= 0 {
			limit = len(tracks)
		}
		if limit > 0 {
			limitPages := (limit + pageSize - 1) / pageSize
			if limitPages > displayPageCount {
				displayPageCount = limitPages
			}
		}
	}
	if page > pageCount {
		page = pageCount
	}
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	if end > len(tracks) {
		end = len(tracks)
	}

	var bld strings.Builder
	platformEmojiText := platformEmoji(h.PlatformManager, state.platform)
	displayName := platformDisplayName(h.PlatformManager, state.platform)
	isPlaylist := state != nil && state.playlist != nil
	if isPlaylist {
		// Playlist/album mode: use the same header style as PlaylistHandler
		// (platform · label, then title/creator/track-count via
		// formatPlaylistInfo). No keyword line, no "点击数字" hint duplication.
		collectionLabel := strings.TrimSpace(state.collectionLabel)
		if collectionLabel == "" {
			collectionLabel = collectionTypeLabel(ctx, collectionTypePlaylist)
		}
		bld.WriteString(fmt.Sprintf("%s *%s* %s\n\n", platformEmojiText, mdV2Replacer.Replace(displayName), collectionLabel))
		bld.WriteString(formatPlaylistInfo(ctx, state.playlist, collectionLabel))
	} else {
		bld.WriteString(fmt.Sprintf("%s *%s* %s\n\\* %s\n\n", platformEmojiText, mdV2Replacer.Replace(displayName), trMd(ctx, "guest_search_results"), trMd(ctx, "guest_pick_number_hint")))
		if strings.TrimSpace(state.keyword) != "" {
			bld.WriteString(fmt.Sprintf("%s%s\n", trMd(ctx, "guest_keyword_label"), mdV2Replacer.Replace(state.keyword)))
		}
	}
	if pageCount > 1 || hasMore {
		bld.WriteString(trMd(ctx, "guest_page_indicator", map[string]any{"Page": page, "Total": displayPageCount}) + "\n\n")
	} else if !isPlaylist {
		bld.WriteString("\n")
	}

	rows := make([][]telego.InlineKeyboardButton, 0, 8)
	resultButtons := make([]telego.InlineKeyboardButton, 0, pageSize)
	for i := start; i < end; i++ {
		track := tracks[i]
		escapedTitle := mdV2Replacer.Replace(track.Title)
		trackLink := escapedTitle
		if strings.TrimSpace(track.URL) != "" {
			trackLink = fmt.Sprintf("[%s](%s)", escapedTitle, track.URL)
		}
		artistParts := make([]string, 0, len(track.Artists))
		for _, artist := range track.Artists {
			escapedArtist := mdV2Replacer.Replace(artist.Name)
			if strings.TrimSpace(artist.URL) != "" {
				artistParts = append(artistParts, fmt.Sprintf("[%s](%s)", escapedArtist, artist.URL))
			} else {
				artistParts = append(artistParts, escapedArtist)
			}
		}
		bld.WriteString(fmt.Sprintf("%d\\. 「%s」 \\- %s\n", i-start+1, trackLink, strings.Join(artistParts, " / ")))
		if cb := buildInlineSendCallbackData(state.platform, track.ID, state.quality, state.requesterID); cb != "" {
			resultButtons = append(resultButtons, telego.InlineKeyboardButton{Text: fmt.Sprintf("%d", i-start+1), CallbackData: cb})
		}
	}
	if len(resultButtons) > 0 {
		rows = append(rows, resultButtons)
	}

	if pageCount > 1 || hasMore {
		nav := make([]telego.InlineKeyboardButton, 0, 2)
		if page > 1 {
			nav = append(nav, telego.InlineKeyboardButton{Text: tr(ctx, "guest_nav_prev"), CallbackData: fmt.Sprintf("guest %s page %d %d", token, page-1, state.requesterID)})
		}
		if page < pageCount || hasMore {
			nav = append(nav, telego.InlineKeyboardButton{Text: tr(ctx, "guest_nav_next"), CallbackData: fmt.Sprintf("guest %s page %d %d", token, page+1, state.requesterID)})
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
		if page > 1 {
			rows = append(rows, []telego.InlineKeyboardButton{{Text: tr(ctx, "guest_nav_home"), CallbackData: fmt.Sprintf("guest %s home %d", token, state.requesterID)}})
		}
	}

	if !isPlaylist {
		if platformRows := h.buildGuestPlatformSwitchRows(state, token); len(platformRows) > 0 {
			rows = append(rows, platformRows...)
		}
		if strings.TrimSpace(state.searchFilterText) != "" {
			filterText := tr(ctx, "guest_filter_on")
			toggleAction := "off"
			if !state.biliFilter {
				filterText = tr(ctx, "guest_filter_off")
				toggleAction = "on"
			}
			rows = append(rows, []telego.InlineKeyboardButton{{
				Text:         fmt.Sprintf("%s: %s", state.searchFilterText, filterText),
				CallbackData: fmt.Sprintf("guest %s bilifilter %s %d", token, toggleAction, state.requesterID),
			}})
		}
	}
	rows = append(rows, []telego.InlineKeyboardButton{{Text: tr(ctx, "guest_close"), CallbackData: fmt.Sprintf("guest %s close %d", token, state.requesterID)}})

	return bld.String(), &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (h *GuestModeHandler) buildGuestPlatformSwitchRows(state *searchState, token string) [][]telego.InlineKeyboardButton {
	if h == nil || state == nil || h.SearchHandler == nil {
		return nil
	}
	platforms := h.SearchHandler.searchPlatforms()
	if len(platforms) <= 1 {
		return nil
	}
	buttons := make([]telego.InlineKeyboardButton, 0, len(platforms))
	for _, name := range platforms {
		if state.unavailable != nil && state.unavailable[name] {
			continue
		}
		displayName := platformDisplayName(h.PlatformManager, name)
		text := fmt.Sprintf("%s %s", platformEmoji(h.PlatformManager, name), displayName)
		if name == state.platform {
			text = "✅ " + displayName
		}
		buttons = append(buttons, telego.InlineKeyboardButton{Text: text, CallbackData: fmt.Sprintf("guest %s platform %s %d", token, name, state.requesterID)})
	}
	if len(buttons) <= 1 {
		return nil
	}
	const maxButtonsPerRow = 3
	rows := make([][]telego.InlineKeyboardButton, 0, (len(buttons)+maxButtonsPerRow-1)/maxButtonsPerRow)
	for start := 0; start < len(buttons); start += maxButtonsPerRow {
		end := start + maxButtonsPerRow
		if end > len(buttons) {
			end = len(buttons)
		}
		rows = append(rows, buttons[start:end])
	}
	return rows
}

func (h *GuestModeHandler) editGuestInlineText(ctx context.Context, b *telego.Bot, inlineMessageID, text string, markup *telego.InlineKeyboardMarkup, parseMode string) error {
	if strings.TrimSpace(inlineMessageID) == "" || b == nil {
		return nil
	}
	params := &telego.EditMessageTextParams{
		InlineMessageID:    inlineMessageID,
		Text:               text,
		ParseMode:          parseMode,
		ReplyMarkup:        markup,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if h != nil && h.RateLimiter != nil {
		_, err := telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		return err
	}
	_, err := b.EditMessageText(ctx, params)
	return err
}

type GuestSearchCallbackHandler struct {
	Guest       *GuestModeHandler
	RateLimiter *telegram.RateLimiter
}

func (h *GuestSearchCallbackHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if h == nil || h.Guest == nil || b == nil || update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	if strings.TrimSpace(query.InlineMessageID) == "" {
		return
	}
	parts := strings.Fields(query.Data)
	if len(parts) < 4 || parts[0] != "guest" {
		return
	}
	token := strings.TrimSpace(parts[1])
	action := strings.TrimSpace(parts[2])
	if token == "" {
		return
	}
	requesterID, err := strconv.ParseInt(strings.TrimSpace(parts[len(parts)-1]), 10, 64)
	if err != nil || requesterID == 0 {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_bad_params"), ShowAlert: true})
		return
	}
	if query.From.ID != requesterID {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "callback_denied"), ShowAlert: true})
		return
	}

	withInlineMessageLock(query.InlineMessageID, func() {
		state, ok := h.Guest.guestSearchStore().get(token)
		if !ok || state == nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_search_expired"), ShowAlert: true})
			return
		}

		page := state.currentPage
		if page < 1 {
			page = 1
		}
		switch action {
		case "close":
			h.Guest.guestSearchStore().delete(token)
			_ = h.Guest.editGuestInlineText(ctx, b, query.InlineMessageID, tr(ctx, "guest_closed"), nil, "")
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
			return
		case "home":
			page = 1
		case "page":
			if len(parts) < 5 {
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_bad_params"), ShowAlert: true})
				return
			}
			page, err = strconv.Atoi(strings.TrimSpace(parts[3]))
			if err != nil || page < 1 {
				page = 1
			}
		case "platform":
			if len(parts) < 5 {
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_bad_params"), ShowAlert: true})
				return
			}
			platformName := strings.TrimSpace(parts[3])
			if platformName == "" || h.Guest.PlatformManager == nil || h.Guest.PlatformManager.Get(platformName) == nil {
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_platform_unavailable"), ShowAlert: true})
				return
			}
			state.platform = platformName
			state.searchFilterText = ""
			state.limit = h.Guest.guestSearchLimit(platformName)
			state.setUnavailable(platformName, false)
			if enabled, supported, label := resolveSearchFilterEnabled(ctx, h.Guest.PlatformManager, h.Guest.repo(), platformName, botpkg.PluginScopeUser, requesterID); supported {
				state.biliFilter = enabled
				state.searchFilterText = label
			}
			page = 1
		case "bilifilter":
			if len(parts) < 5 {
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_bad_params"), ShowAlert: true})
				return
			}
			state.biliFilter = strings.TrimSpace(parts[3]) == "on"
			if state.tracksByPlatform != nil {
				delete(state.tracksByPlatform, state.platform)
			}
			page = 1
		default:
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_bad_params"), ShowAlert: true})
			return
		}

		tracks, hasTracks := state.getTracks(state.platform)
		pageSize := h.Guest.guestPageSize()
		requiredLimit := page * pageSize
		if requiredLimit < pageSize {
			requiredLimit = pageSize
		}
		limit := state.limit
		if limit <= 0 {
			limit = h.Guest.guestSearchLimit(state.platform)
		}
		if requiredLimit > limit {
			requiredLimit = limit
		}
		needFetch := !hasTracks || len(tracks) < requiredLimit
		if needFetch {
			plat := h.Guest.PlatformManager.Get(state.platform)
			if plat == nil || !plat.SupportsSearch() {
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_platform_unavailable"), ShowAlert: true})
				return
			}
			searchCtx := withSearchFilterContext(ctx, h.Guest.PlatformManager, state.platform, state.biliFilter)
			requestLimit := requiredLimit
			if requestLimit < pageSize {
				requestLimit = pageSize
			}
			if requestLimit > limit {
				requestLimit = limit
			}
			tracks, err = plat.Search(searchCtx, state.keyword, requestLimit)
			if err != nil {
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_search_failed_retry"), ShowAlert: true})
				return
			}
			if len(tracks) == 0 {
				state.setUnavailable(state.platform, true)
				text := tr(ctx, "guest_no_results_platform", map[string]any{"Platform": platformDisplayName(h.Guest.PlatformManager, state.platform)})
				_, keyboard := h.Guest.renderGuestSearchPage(ctx, state, token, 1)
				_ = h.Guest.editGuestInlineText(ctx, b, query.InlineMessageID, text, keyboard, "")
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "no_results")})
				return
			}
			state.setTracks(state.platform, tracks)
			state.setHasMore(state.platform, len(tracks) >= requestLimit && requestLimit < limit)
		}

		state.currentPage = page
		state.updatedAt = time.Now()
		text, markup := h.Guest.renderGuestSearchPage(ctx, state, token, page)
		if err := h.Guest.editGuestInlineText(ctx, b, query.InlineMessageID, text, markup, telego.ModeMarkdownV2); err != nil {
			return
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: tr(ctx, "guest_page_toast", map[string]any{"Page": page})})
	})
}

func (h *GuestModeHandler) repo() botpkg.SongRepository {
	if h == nil {
		return nil
	}
	if h.SearchHandler != nil && h.SearchHandler.Repo != nil {
		return h.SearchHandler.Repo
	}
	if h.Music != nil {
		return h.Music.Repo
	}
	if h.LyricHandler != nil {
		return h.LyricHandler.Repo
	}
	return nil
}
