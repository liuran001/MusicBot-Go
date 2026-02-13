package handler

import (
	"context"
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

// ChosenInlineMusicHandler handles chosen inline results (requires inline feedback).
type ChosenInlineMusicHandler struct {
	Music       *MusicHandler
	RateLimiter *telegram.RateLimiter
	mu          sync.Mutex
	cache       map[string]*inlineCollectionState
}

const inlineCollectionCacheTTL = 10 * time.Minute

type inlineCollectionState struct {
	platformName    string
	collectionID    string
	qualityValue    string
	requesterID     int64
	tracks          []platform.Track
	totalTracks     int
	collectionLabel string
	title           string
	url             string
	creator         string
	description     string
	updatedAt       time.Time
}

func (h *ChosenInlineMusicHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if h == nil || h.Music == nil || b == nil || update == nil || update.ChosenInlineResult == nil {
		return
	}
	chosen := update.ChosenInlineResult
	if strings.TrimSpace(chosen.InlineMessageID) == "" {
		return
	}
	platformName, trackID, qualityValue, ok := parseInlinePendingResultID(chosen.ResultID)
	if ok && strings.TrimSpace(platformName) != "" && strings.TrimSpace(trackID) != "" {
		h.handleChosenTrack(ctx, b, chosen, platformName, trackID, qualityValue)
		return
	}
	collectionPlatform, collectionID, collectionQuality, collectionOK := parseInlineCollectionResultID(chosen.ResultID)
	if collectionOK {
		h.handleChosenCollection(ctx, b, chosen, collectionPlatform, collectionID, collectionQuality)
		return
	}
	if strings.HasPrefix(strings.TrimSpace(chosen.ResultID), "z_") {
		platformName, trackID, qualityValue, randomOK := h.resolveChosenInlineRandomTrack(ctx)
		if !randomOK {
			return
		}
		h.handleChosenTrack(ctx, b, chosen, platformName, trackID, qualityValue)
		return
	}
}

func (h *ChosenInlineMusicHandler) resolveChosenInlineRandomTrack(ctx context.Context) (platformName, trackID, qualityValue string, ok bool) {
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

func (h *ChosenInlineMusicHandler) handleChosenTrack(ctx context.Context, b *telego.Bot, chosen *telego.ChosenInlineResult, platformName, trackID, qualityValue string) {
	if h == nil || h.Music == nil || b == nil || chosen == nil {
		return
	}

	setInlineText := func(text string) {
		params := &telego.EditMessageTextParams{InlineMessageID: chosen.InlineMessageID, Text: text}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
	}
	clearInlineReplyMarkup := func() {
		_, _ = b.EditMessageReplyMarkup(ctx, &telego.EditMessageReplyMarkupParams{InlineMessageID: chosen.InlineMessageID})
	}
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
		replyMarkup := buildForwardKeyboard(songInfo.TrackURL, songInfo.Platform, songInfo.TrackID)
		_, err := b.EditMessageMedia(ctx, &telego.EditMessageMediaParams{
			InlineMessageID: chosen.InlineMessageID,
			Media:           media,
			ReplyMarkup:     replyMarkup,
		})
		if err != nil && telegram.IsMessageNotModified(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, nil
	}

	progress := func(text string) {
		setInlineText(text)
	}
	clearInlineReplyMarkup()
	setInlineText(waitForDown)
	songInfo, err := h.Music.prepareInlineSong(ctx, b, chosen.From.ID, platformName, trackID, qualityValue, progress)
	if err != nil {
		if h.Music.Logger != nil {
			h.Music.Logger.Error("failed to prepare inline song", "platform", platformName, "trackID", trackID, "error", err)
		}
		setInlineText(buildMusicInfoText("", "", "", userVisibleDownloadError(err)))
		return
	}
	modified, err := editInlineMedia(songInfo)
	if err != nil {
		if h.Music.Logger != nil {
			h.Music.Logger.Error("failed to edit inline media", "platform", platformName, "trackID", trackID, "error", err)
		}
		setInlineText(buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), userVisibleDownloadError(err)))
		return
	}
	if modified && h.Music.Repo != nil {
		if err := h.Music.Repo.IncrementSendCount(ctx); err != nil && h.Music.Logger != nil {
			h.Music.Logger.Error("failed to update send count", "error", err)
		}
	}
}

func (h *ChosenInlineMusicHandler) handleChosenCollection(ctx context.Context, b *telego.Bot, chosen *telego.ChosenInlineResult, platformName, collectionID, qualityValue string) {
	if h == nil || h.Music == nil || h.Music.PlatformManager == nil || b == nil || chosen == nil {
		return
	}
	plat := h.Music.PlatformManager.Get(platformName)
	if plat == nil {
		return
	}
	loadingText := fetchingPlaylist
	if collectionTypeLabelFromID(collectionID) == "专辑" {
		loadingText = "正在获取专辑..."
	}
	setInlineText := func(text string) {
		params := &telego.EditMessageTextParams{InlineMessageID: chosen.InlineMessageID, Text: text}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.EditMessageText(ctx, params)
		}
	}
	setInlineText(loadingText)
	playlist, err := plat.GetPlaylist(ctx, collectionID)
	if err != nil || playlist == nil || len(playlist.Tracks) == 0 {
		setInlineText(noResults)
		return
	}
	state := &inlineCollectionState{
		platformName:    platformName,
		collectionID:    collectionID,
		qualityValue:    qualityValue,
		requesterID:     chosen.From.ID,
		tracks:          playlist.Tracks,
		totalTracks:     playlist.TrackCount,
		collectionLabel: collectionTypeLabel(detectCollectionType(collectionID, playlist.URL)),
		title:           strings.TrimSpace(playlist.Title),
		url:             strings.TrimSpace(playlist.URL),
		creator:         strings.TrimSpace(playlist.Creator),
		description:     strings.TrimSpace(playlist.Description),
		updatedAt:       time.Now(),
	}
	if state.totalTracks <= 0 {
		state.totalTracks = len(state.tracks)
	}
	if strings.TrimSpace(state.qualityValue) == "" {
		state.qualityValue = "hires"
	}
	token := h.storeInlineCollectionState(state)
	text, markup := h.renderInlineCollectionPage(state, token, 1)
	params := &telego.EditMessageTextParams{
		InlineMessageID:    chosen.InlineMessageID,
		Text:               text,
		ParseMode:          telego.ModeMarkdownV2,
		ReplyMarkup:        markup,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.EditMessageText(ctx, params)
	}
}

func (h *ChosenInlineMusicHandler) inlineCollectionPageSize() int {
	if h == nil || h.Music == nil || h.Music.Playlist == nil {
		return 8
	}
	size := h.Music.Playlist.PageSize
	if size <= 0 {
		return 8
	}
	if size > 12 {
		return 12
	}
	return size
}

func (h *ChosenInlineMusicHandler) renderInlineCollectionPage(state *inlineCollectionState, token string, page int) (string, *telego.InlineKeyboardMarkup) {
	if state == nil || len(state.tracks) == 0 {
		return noResults, nil
	}
	pageSize := h.inlineCollectionPageSize()
	pageCount := 1
	if state.totalTracks > 0 {
		pageCount = (state.totalTracks-1)/pageSize + 1
	}
	if page < 1 {
		page = 1
	}
	if page > pageCount {
		page = pageCount
	}
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	if end > len(state.tracks) {
		end = len(state.tracks)
	}
	var bld strings.Builder
	bld.WriteString(fmt.Sprintf("%s *%s* %s\n\n", platformEmoji(h.Music.PlatformManager, state.platformName), mdV2Replacer.Replace(platformDisplayName(h.Music.PlatformManager, state.platformName)), state.collectionLabel))
	bld.WriteString(renderInlineCollectionInfo(state))
	if pageCount > 1 {
		bld.WriteString(fmt.Sprintf("第 %d/%d 页\n\n", page, pageCount))
	} else {
		bld.WriteString("\n")
	}
	rows := make([][]telego.InlineKeyboardButton, 0, 3)
	numberRow := make([]telego.InlineKeyboardButton, 0, end-start)
	for i := start; i < end; i++ {
		track := state.tracks[i]
		displayIndex := i - start + 1
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
		bld.WriteString(fmt.Sprintf("%d\\. 「%s」 \\- %s\n", displayIndex, trackLink, strings.Join(artistParts, " / ")))
		callbackData := buildInlineSendCallbackData(state.platformName, track.ID, state.qualityValue, state.requesterID)
		if callbackData != "" {
			numberRow = append(numberRow, telego.InlineKeyboardButton{Text: fmt.Sprintf("%d", displayIndex), CallbackData: callbackData})
		}
	}
	if len(numberRow) > 0 {
		rows = append(rows, numberRow)
	}
	if pageCount > 1 {
		nav := make([]telego.InlineKeyboardButton, 0, 2)
		if page > 1 {
			nav = append(nav, telego.InlineKeyboardButton{Text: "上一页", CallbackData: fmt.Sprintf("ipl %s page %d %d", token, page-1, state.requesterID)})
		}
		if page < pageCount {
			nav = append(nav, telego.InlineKeyboardButton{Text: "下一页", CallbackData: fmt.Sprintf("ipl %s page %d %d", token, page+1, state.requesterID)})
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
		if page > 1 {
			rows = append(rows, []telego.InlineKeyboardButton{{Text: "回到首页", CallbackData: fmt.Sprintf("ipl %s home %d", token, state.requesterID)}})
		}
	}
	return bld.String(), &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func renderInlineCollectionInfo(state *inlineCollectionState) string {
	if state == nil {
		return ""
	}
	var bld strings.Builder
	title := strings.TrimSpace(state.title)
	if title != "" {
		escaped := mdV2Replacer.Replace(title)
		if strings.TrimSpace(state.url) != "" {
			bld.WriteString(fmt.Sprintf("%s: [%s](%s)\n", state.collectionLabel, escaped, state.url))
		} else {
			bld.WriteString(fmt.Sprintf("%s: %s\n", state.collectionLabel, escaped))
		}
	}
	if strings.TrimSpace(state.creator) != "" {
		bld.WriteString(fmt.Sprintf("创建者: %s\n", mdV2Replacer.Replace(state.creator)))
	}
	if state.totalTracks > 0 {
		bld.WriteString(fmt.Sprintf("曲目数: %d\n", state.totalTracks))
	}
	if desc := strings.TrimSpace(state.description); desc != "" {
		if quote := formatExpandableQuote(mdV2Replacer.Replace(truncateText(desc, 800))); quote != "" {
			bld.WriteString(quote)
			bld.WriteString("\n")
		}
	}
	bld.WriteString("\n")
	return bld.String()
}

func (h *ChosenInlineMusicHandler) storeInlineCollectionState(state *inlineCollectionState) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cache == nil {
		h.cache = make(map[string]*inlineCollectionState)
	}
	h.cleanupInlineCollectionCacheLocked()
	token := fmt.Sprintf("%x", time.Now().UnixNano())
	if len(token) > 8 {
		token = token[len(token)-8:]
	}
	h.cache[token] = state
	return token
}

func (h *ChosenInlineMusicHandler) getInlineCollectionState(token string) (*inlineCollectionState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cache == nil {
		return nil, false
	}
	h.cleanupInlineCollectionCacheLocked()
	state, ok := h.cache[token]
	if ok && state != nil {
		state.updatedAt = time.Now()
	}
	return state, ok
}

func (h *ChosenInlineMusicHandler) cleanupInlineCollectionCacheLocked() {
	if h.cache == nil {
		return
	}
	cutoff := time.Now().Add(-inlineCollectionCacheTTL)
	for token, state := range h.cache {
		if state == nil || state.updatedAt.Before(cutoff) {
			delete(h.cache, token)
		}
	}
}
