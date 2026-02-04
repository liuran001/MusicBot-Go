package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

const (
	playlistFetchChunkSize = 30
	playlistCacheTTL       = 10 * time.Minute
)

type playlistState struct {
	playlist     platform.Playlist
	platform     string
	quality      string
	requesterID  int64
	updatedAt    time.Time
	totalTracks  int
	displayLimit int
	lazy         bool
	cacheOffset  int
}

type PlaylistHandler struct {
	PlatformManager platform.Manager
	Repo            botpkg.SongRepository
	RateLimiter     *telegram.RateLimiter
	DefaultQuality  string
	PageSize        int
	playlistMu      sync.Mutex
	playlistCache   map[int]*playlistState
}

func (h *PlaylistHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	_ = h.TryHandle(ctx, b, update)
}

func (h *PlaylistHandler) TryHandle(ctx context.Context, b *telego.Bot, update *telego.Update) bool {
	if update == nil || update.Message == nil {
		return false
	}
	message := update.Message
	if strings.TrimSpace(message.Text) == "" {
		return false
	}

	text := message.Text
	args := commandArguments(text)
	if args != "" {
		text = args
	} else if strings.HasPrefix(strings.TrimSpace(text), "/") {
		return false
	}
	baseText, _, qualityOverride := parseTrailingOptions(text, h.PlatformManager)
	baseText = strings.TrimSpace(baseText)
	if baseText == "" {
		return false
	}
	platformName, playlistID, ok := matchPlaylistURL(ctx, h.PlatformManager, baseText)
	if !ok {
		return false
	}

	threadID := message.MessageThreadID
	replyParams := buildReplyParams(message)
	msgResult, err := b.SendMessage(ctx, &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		MessageThreadID: threadID,
		Text:            fetchingPlaylist,
		ReplyParameters: replyParams,
	})
	if err != nil {
		return true
	}
	if h.PlatformManager == nil {
		h.editPlaylistMessage(ctx, b, msgResult, noResults, nil)
		return true
	}
	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		h.editPlaylistMessage(ctx, b, msgResult, noResults, nil)
		return true
	}

	lazy := h.shouldLazyLoad(platformName)
	playlist, err := h.fetchInitialPlaylist(ctx, plat, playlistID, lazy)
	if err != nil {
		errText := noResults
		if errors.Is(err, platform.ErrUnsupported) {
			errText = "此平台不支持获取歌单"
		} else if errors.Is(err, platform.ErrRateLimited) {
			errText = "请求过于频繁，请稍后再试"
		} else if errors.Is(err, platform.ErrUnavailable) {
			errText = "歌单服务暂时不可用"
		} else if errors.Is(err, platform.ErrNotFound) {
			errText = "未找到歌单"
		}
		h.editPlaylistMessage(ctx, b, msgResult, errText, nil)
		return true
	}
	if playlist == nil || len(playlist.Tracks) == 0 {
		h.editPlaylistMessage(ctx, b, msgResult, playlistEmpty, nil)
		return true
	}

	totalTracks := playlist.TrackCount
	if totalTracks <= 0 {
		totalTracks = len(playlist.Tracks)
	}
	effectiveTotal := totalTracks
	pageTracks, pageOffset := h.slicePlaylistPage(playlist.Tracks, 1)
	requesterID := int64(0)
	if message.From != nil {
		requesterID = message.From.ID
	}
	qualityValue := h.resolveDefaultQuality(ctx, message, requesterID)
	if strings.TrimSpace(qualityOverride) != "" {
		qualityValue = qualityOverride
	}
	platformEmoji := platformEmoji(h.PlatformManager, platformName)
	displayName := platformDisplayName(h.PlatformManager, platformName)
	textHeader := fmt.Sprintf("%s *%s* 歌单\n\n", platformEmoji, mdV2Replacer.Replace(displayName))
	textHeader += formatPlaylistInfo(playlist)
	pageText, keyboard := h.buildPlaylistPage(pageTracks, effectiveTotal, pageOffset, platformName, qualityValue, requesterID, msgResult.MessageID, 1)
	combinedText := textHeader + pageText
	h.editPlaylistMessage(ctx, b, msgResult, combinedText, keyboard)

	h.storePlaylistState(msgResult.MessageID, &playlistState{
		playlist:     *playlist,
		platform:     platformName,
		quality:      qualityValue,
		requesterID:  requesterID,
		updatedAt:    time.Now(),
		totalTracks:  totalTracks,
		displayLimit: 0,
		lazy:         lazy,
		cacheOffset:  0,
	})
	return true
}

type PlaylistCallbackHandler struct {
	Playlist    *PlaylistHandler
	RateLimiter *telegram.RateLimiter
}

func (h *PlaylistCallbackHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.CallbackQuery == nil || h.Playlist == nil {
		return
	}
	query := update.CallbackQuery
	parts := strings.Fields(query.Data)
	if len(parts) < 4 || parts[0] != "playlist" {
		return
	}
	messageID, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}
	action := parts[2]
	page := 0
	if action == "page" {
		page, err = strconv.Atoi(parts[3])
		if err != nil {
			return
		}
	}
	requesterIDIndex := 3
	if action == "page" {
		requesterIDIndex = 4
	}
	if len(parts) <= requesterIDIndex {
		return
	}
	requesterID, _ := strconv.ParseInt(parts[requesterIDIndex], 10, 64)
	if query.Message == nil {
		return
	}
	msg := query.Message.Message()
	if msg == nil {
		return
	}
	if msg.Chat.Type != "private" {
		if !isRequesterOrAdmin(ctx, b, msg.Chat.ID, query.From.ID, requesterID) {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
				CallbackQueryID: query.ID,
				Text:            callbackDenied,
				ShowAlert:       true,
			})
			return
		}
	}
	state, ok := h.Playlist.getPlaylistState(messageID)
	if !ok {
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "歌单已过期，请重新发送链接",
		})
		return
	}
	if action == "close" {
		deleteParams := &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID}
		if h.RateLimiter != nil {
			_ = telegram.DeleteMessageWithRetry(ctx, h.RateLimiter, b, deleteParams)
		} else {
			_ = b.DeleteMessage(ctx, deleteParams)
		}
		_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
		return
	}
	if action == "home" {
		page = 1
	}
	if page < 1 {
		page = 1
	}
	totalTracks := state.totalTracks
	if totalTracks <= 0 {
		totalTracks = len(state.playlist.Tracks)
	}
	effectiveTotal := totalTracks
	pageCount := h.Playlist.pageCount(effectiveTotal)
	if page > pageCount {
		page = pageCount
	}

	var pageTracks []platform.Track
	pageOffset := 0
	if state.lazy {
		plat := h.Playlist.PlatformManager.Get(state.platform)
		if plat == nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "歌单加载失败"})
			return
		}
		var err error
		pageTracks, pageOffset, err = h.Playlist.getCachedPage(ctx, plat, state, page)
		if err != nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "歌单加载失败"})
			return
		}
		if len(pageTracks) == 0 {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "未找到结果"})
			return
		}
	} else {
		pageTracks, pageOffset = h.Playlist.slicePlaylistPage(state.playlist.Tracks, page)
	}

	manager := h.Playlist.PlatformManager
	textHeader := fmt.Sprintf("%s *%s* 歌单\n\n", platformEmoji(manager, state.platform), mdV2Replacer.Replace(platformDisplayName(manager, state.platform)))
	textHeader += formatPlaylistInfo(&state.playlist)
	pageText, keyboard := h.Playlist.buildPlaylistPage(pageTracks, effectiveTotal, pageOffset, state.platform, state.quality, state.requesterID, messageID, page)
	text := textHeader + pageText
	params := &telego.EditMessageTextParams{
		ChatID:             telego.ChatID{ID: msg.Chat.ID},
		MessageID:          msg.MessageID,
		Text:               text,
		ParseMode:          telego.ModeMarkdownV2,
		ReplyMarkup:        keyboard,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
	h.Playlist.storePlaylistState(messageID, state)
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
}

func (h *PlaylistHandler) buildPlaylistPage(tracks []platform.Track, totalTracks, _ int, platformName, qualityValue string, requesterID int64, messageID int, page int) (string, *telego.InlineKeyboardMarkup) {
	if len(tracks) == 0 {
		return noResults, nil
	}
	if totalTracks <= 0 {
		totalTracks = len(tracks)
	}
	if page < 1 {
		page = 1
	}
	pageSize := h.pageSize()
	pageCount := (totalTracks-1)/pageSize + 1
	if page > pageCount {
		page = pageCount
	}
	var textMessage strings.Builder
	if pageCount > 1 {
		textMessage.WriteString(fmt.Sprintf("第 %d/%d 页\n\n", page, pageCount))
	} else {
		textMessage.WriteString("\n")
	}
	buttons := make([]telego.InlineKeyboardButton, 0, len(tracks))
	for idx, track := range tracks {
		escapedTitle := mdV2Replacer.Replace(track.Title)
		trackLink := escapedTitle
		if strings.TrimSpace(track.URL) != "" {
			trackLink = fmt.Sprintf("[%s](%s)", escapedTitle, track.URL)
		}
		var artistParts []string
		for _, artist := range track.Artists {
			escapedArtist := mdV2Replacer.Replace(artist.Name)
			if strings.TrimSpace(artist.URL) != "" {
				artistParts = append(artistParts, fmt.Sprintf("[%s](%s)", escapedArtist, artist.URL))
			} else {
				artistParts = append(artistParts, escapedArtist)
			}
		}
		songArtists := strings.Join(artistParts, " / ")
		displayIndex := idx + 1
		textMessage.WriteString(fmt.Sprintf("%d\\. 「%s」 \\- %s\n", displayIndex, trackLink, songArtists))
		callbackData := fmt.Sprintf("music %s %s %s %d", platformName, track.ID, qualityValue, requesterID)
		buttons = append(buttons, telego.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d", displayIndex),
			CallbackData: callbackData,
		})
	}

	var rows [][]telego.InlineKeyboardButton
	if len(buttons) > 0 {
		rows = append(rows, buttons)
	}
	if pageCount > 1 {
		navRow := make([]telego.InlineKeyboardButton, 0, 2)
		if page == 1 {
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "关闭", CallbackData: fmt.Sprintf("playlist %d close %d", messageID, requesterID)})
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "下一页", CallbackData: fmt.Sprintf("playlist %d page %d %d", messageID, page+1, requesterID)})
			rows = append(rows, navRow)
		} else if page == pageCount {
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "上一页", CallbackData: fmt.Sprintf("playlist %d page %d %d", messageID, page-1, requesterID)})
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "回到首页", CallbackData: fmt.Sprintf("playlist %d home %d", messageID, requesterID)})
			rows = append(rows, navRow)
		} else {
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "上一页", CallbackData: fmt.Sprintf("playlist %d page %d %d", messageID, page-1, requesterID)})
			navRow = append(navRow, telego.InlineKeyboardButton{Text: "下一页", CallbackData: fmt.Sprintf("playlist %d page %d %d", messageID, page+1, requesterID)})
			rows = append(rows, navRow)
			homeRow := []telego.InlineKeyboardButton{{Text: "回到首页", CallbackData: fmt.Sprintf("playlist %d home %d", messageID, requesterID)}}
			rows = append(rows, homeRow)
		}
	} else if page == 1 {
		rows = append(rows, []telego.InlineKeyboardButton{{Text: "关闭", CallbackData: fmt.Sprintf("playlist %d close %d", messageID, requesterID)}})
	}
	keyboard := &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
	return textMessage.String(), keyboard
}

func (h *PlaylistHandler) resolveDefaultQuality(ctx context.Context, message *telego.Message, userID int64) string {
	qualityValue := strings.TrimSpace(h.DefaultQuality)
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if h.Repo == nil {
		return qualityValue
	}
	if message != nil && message.Chat.Type != "private" {
		if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultQuality) != "" {
				qualityValue = settings.DefaultQuality
			}
		}
		return qualityValue
	}
	if userID != 0 {
		if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultQuality) != "" {
				qualityValue = settings.DefaultQuality
			}
		}
	}
	return qualityValue
}

func (h *PlaylistHandler) pageSize() int {
	if h == nil {
		return 8
	}
	if h.PageSize > 0 {
		return h.PageSize
	}
	return 8
}

func (h *PlaylistHandler) shouldLazyLoad(platformName string) bool {
	name := strings.TrimSpace(platformName)
	return name == "qqmusic" || name == "netease"
}

func (h *PlaylistHandler) pageCount(total int) int {
	if total <= 0 {
		return 1
	}
	pageSize := h.pageSize()
	return (total-1)/pageSize + 1
}

func (h *PlaylistHandler) fetchInitialPlaylist(ctx context.Context, plat platform.Platform, playlistID string, lazy bool) (*platform.Playlist, error) {
	if plat == nil {
		return nil, platform.NewUnavailableError("unknown", "playlist", playlistID)
	}
	requestCtx := ctx
	if lazy {
		chunkOffset, chunkLimit := h.chunkForPage(1)
		requestCtx = platform.WithPlaylistOffset(requestCtx, chunkOffset)
		requestCtx = platform.WithPlaylistLimit(requestCtx, chunkLimit)
	}
	return plat.GetPlaylist(requestCtx, playlistID)
}

func (h *PlaylistHandler) chunkForPage(page int) (int, int) {
	if page < 1 {
		page = 1
	}
	chunkSize := playlistFetchChunkSize
	pageSize := h.pageSize()
	if chunkSize < pageSize {
		chunkSize = pageSize
	}
	offset := (page - 1) * h.pageSize()
	if offset < 0 {
		offset = 0
	}
	chunkOffset := (offset / chunkSize) * chunkSize
	chunkLimit := chunkSize
	if chunkLimit < 0 {
		chunkLimit = 0
	}
	return chunkOffset, chunkLimit
}

func (h *PlaylistHandler) chunkForOffset(offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	page := offset/h.pageSize() + 1
	return h.chunkForPage(page)
}

func (h *PlaylistHandler) slicePlaylistPage(tracks []platform.Track, page int) ([]platform.Track, int) {
	if len(tracks) == 0 {
		return nil, 0
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * h.pageSize()
	if offset < 0 {
		offset = 0
	}
	if offset >= len(tracks) {
		return nil, offset
	}
	end := offset + h.pageSize()
	if end > len(tracks) {
		end = len(tracks)
	}
	return tracks[offset:end], offset
}

func (h *PlaylistHandler) sliceCachedPage(state *playlistState, page int) ([]platform.Track, int) {
	if state == nil || len(state.playlist.Tracks) == 0 {
		return nil, 0
	}
	if page < 1 {
		page = 1
	}
	pageOffset := (page - 1) * h.pageSize()
	localStart := pageOffset - state.cacheOffset
	if localStart < 0 || localStart >= len(state.playlist.Tracks) {
		return nil, pageOffset
	}
	localEnd := localStart + h.pageSize()
	if localEnd > len(state.playlist.Tracks) {
		localEnd = len(state.playlist.Tracks)
	}
	return state.playlist.Tracks[localStart:localEnd], pageOffset
}

func (h *PlaylistHandler) getCachedPage(ctx context.Context, plat platform.Platform, state *playlistState, page int) ([]platform.Track, int, error) {
	if state == nil || plat == nil {
		return nil, 0, platform.NewUnavailableError("unknown", "playlist", "")
	}
	if page < 1 {
		page = 1
	}
	effectiveTotal := state.totalTracks
	if effectiveTotal <= 0 {
		effectiveTotal = len(state.playlist.Tracks)
	}
	pageStart := (page - 1) * h.pageSize()
	if pageStart >= effectiveTotal {
		return nil, pageStart, nil
	}
	pageEnd := pageStart + h.pageSize()
	if effectiveTotal > 0 && pageEnd > effectiveTotal {
		pageEnd = effectiveTotal
	}
	need := pageEnd - pageStart

	if !h.chunkContainsOffset(state, pageStart) {
		if err := h.refreshChunkAtOffset(ctx, plat, state, pageStart); err != nil {
			return nil, pageStart, err
		}
	}
	tracks, _ := h.sliceCachedPage(state, page)
	if len(tracks) >= need {
		return tracks[:need], pageStart, nil
	}
	if pageStart+len(tracks) >= effectiveTotal {
		return tracks, pageStart, nil
	}

	nextOffset := state.cacheOffset + len(state.playlist.Tracks)
	if nextOffset < pageEnd {
		if err := h.refreshChunkAtOffset(ctx, plat, state, nextOffset); err != nil {
			return nil, pageStart, err
		}
		if len(state.playlist.Tracks) > 0 {
			remaining := need - len(tracks)
			if remaining > len(state.playlist.Tracks) {
				remaining = len(state.playlist.Tracks)
			}
			tracks = append(tracks, state.playlist.Tracks[:remaining]...)
		}
	}
	return tracks, pageStart, nil
}

func (h *PlaylistHandler) chunkContainsOffset(state *playlistState, offset int) bool {
	if state == nil || len(state.playlist.Tracks) == 0 {
		return false
	}
	if offset < state.cacheOffset {
		return false
	}
	return offset < state.cacheOffset+len(state.playlist.Tracks)
}

func (h *PlaylistHandler) refreshChunkAtOffset(ctx context.Context, plat platform.Platform, state *playlistState, offset int) error {
	chunkOffset, chunkLimit := h.chunkForOffset(offset)
	if chunkLimit <= 0 {
		state.playlist.Tracks = nil
		state.cacheOffset = chunkOffset
		return nil
	}
	requestCtx := platform.WithPlaylistLimit(ctx, chunkLimit)
	requestCtx = platform.WithPlaylistOffset(requestCtx, chunkOffset)
	playlist, err := plat.GetPlaylist(requestCtx, state.playlist.ID)
	if err != nil {
		return err
	}
	if playlist == nil || len(playlist.Tracks) == 0 {
		state.playlist.Tracks = nil
		state.cacheOffset = chunkOffset
		return nil
	}
	tracks := playlist.Tracks
	if chunkLimit > 0 && len(tracks) > chunkLimit {
		tracks = tracks[:chunkLimit]
	}
	state.playlist.Tracks = tracks
	state.cacheOffset = chunkOffset
	return nil
}

func formatPlaylistInfo(playlist *platform.Playlist) string {
	if playlist == nil {
		return ""
	}
	var builder strings.Builder
	if title := strings.TrimSpace(playlist.Title); title != "" {
		escapedTitle := mdV2Replacer.Replace(title)
		if strings.TrimSpace(playlist.URL) != "" {
			builder.WriteString(fmt.Sprintf("歌单: [%s](%s)\n", escapedTitle, playlist.URL))
		} else {
			builder.WriteString(fmt.Sprintf("歌单: %s\n", escapedTitle))
		}
	}
	if creator := strings.TrimSpace(playlist.Creator); creator != "" {
		builder.WriteString(fmt.Sprintf("创建者: %s\n", mdV2Replacer.Replace(creator)))
	}
	trackCount := playlist.TrackCount
	if trackCount <= 0 {
		trackCount = len(playlist.Tracks)
	}
	if trackCount > 0 {
		builder.WriteString(fmt.Sprintf("曲目数: %d\n", trackCount))
	}
	if desc := strings.TrimSpace(playlist.Description); desc != "" {
		builder.WriteString(fmt.Sprintf("简介: %s\n", mdV2Replacer.Replace(truncateText(desc, 120))))
	}
	builder.WriteString("\n")
	return builder.String()
}

func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" || limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func (h *PlaylistHandler) editPlaylistMessage(ctx context.Context, b *telego.Bot, msgResult *telego.Message, text string, keyboard *telego.InlineKeyboardMarkup) {
	if msgResult == nil {
		return
	}
	params := &telego.EditMessageTextParams{
		ChatID:             telego.ChatID{ID: msgResult.Chat.ID},
		MessageID:          msgResult.MessageID,
		Text:               text,
		ParseMode:          telego.ModeMarkdownV2,
		ReplyMarkup:        keyboard,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
}

func (h *PlaylistHandler) storePlaylistState(messageID int, state *playlistState) {
	if messageID == 0 || state == nil {
		return
	}
	h.playlistMu.Lock()
	defer h.playlistMu.Unlock()
	if h.playlistCache == nil {
		h.playlistCache = make(map[int]*playlistState)
	}
	h.cleanupPlaylistStateLocked()
	h.playlistCache[messageID] = state
}

func (h *PlaylistHandler) getPlaylistState(messageID int) (*playlistState, bool) {
	h.playlistMu.Lock()
	defer h.playlistMu.Unlock()
	if h.playlistCache == nil {
		return nil, false
	}
	h.cleanupPlaylistStateLocked()
	state, ok := h.playlistCache[messageID]
	if ok && state != nil {
		state.updatedAt = time.Now()
	}
	return state, ok
}

func (h *PlaylistHandler) cleanupPlaylistStateLocked() {
	if h.playlistCache == nil {
		return
	}
	cutoff := time.Now().Add(-playlistCacheTTL)
	for key, state := range h.playlistCache {
		if state == nil || state.updatedAt.Before(cutoff) {
			delete(h.playlistCache, key)
		}
	}
}
