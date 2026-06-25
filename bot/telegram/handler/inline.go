package handler

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

// InlineSearchHandler handles inline queries.
type InlineSearchHandler struct {
	Repo             botpkg.SongRepository
	PlatformManager  platform.Manager
	CollectionChosen *ChosenInlineMusicHandler
	Favorites        *FavoritesHandler
	BotName          string
	DefaultPlatform  string
	DefaultQuality   string
	FallbackPlatform string
	PageSize         int
}

var (
	qqCoverResizePattern = regexp.MustCompile(`T002R\d+x\d+M000`)
	qqCoverMidPattern    = regexp.MustCompile(`T002M000([A-Za-z0-9]+)\.jpg`)
	qqSongMidPattern     = regexp.MustCompile(`T062M000([A-Za-z0-9]+)\.jpg`)
)

func (h *InlineSearchHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.InlineQuery == nil {
		return
	}
	query := update.InlineQuery
	if strings.TrimSpace(query.Query) == "" {
		h.inlineHelp(ctx, b, query)
		return
	}

	switch {
	case query.Query == "help":
		h.inlineHelp(ctx, b, query)
	default:
		if h.PlatformManager == nil {
			h.inlineEmpty(ctx, b, query)
			return
		}
		resolvedQuery := resolveShortLinkText(ctx, h.PlatformManager, query.Query)
		normalized := normalizeInlineKeywordQuery(resolvedQuery)
		baseText, platformSuffix, qualityOverride, requestedPage, invalidPageFallbackKeyword := parseInlineSearchOptions(normalized, h.PlatformManager)
		baseText = strings.TrimSpace(baseText)
		if baseText == "" {
			h.inlineEmpty(ctx, b, query)
			return
		}
		if platformName, collectionID, matched := matchPlaylistURL(ctx, h.PlatformManager, baseText); matched {
			h.inlineCollection(ctx, b, query, platformName, collectionID, qualityOverride, requestedPage)
			return
		}
		if platformName, trackID, matched := h.tryResolveDirectTrack(ctx, baseText, platformSuffix); matched {
			h.inlineCachedOrCommand(ctx, b, query, platformName, trackID, qualityOverride, requestedPage, baseText)
			return
		}
		h.inlineSearch(ctx, b, query, baseText, platformSuffix, qualityOverride, requestedPage, invalidPageFallbackKeyword)
	}
}

func (h *InlineSearchHandler) inlineCollection(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, platformName, collectionID, qualityOverride string, requestedPage int) {
	if h == nil || b == nil || query == nil || h.PlatformManager == nil {
		return
	}
	platformName = strings.TrimSpace(platformName)
	collectionID = strings.TrimSpace(collectionID)
	if platformName == "" || collectionID == "" {
		h.inlineEmpty(ctx, b, query)
		return
	}
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	if strings.TrimSpace(qualityOverride) != "" {
		qualityValue = strings.TrimSpace(qualityOverride)
	}
	qualityValue = resolvePlatformQualityValue(ctx, h.Repo, botpkg.PluginScopeUser, query.From.ID, platformName, qualityValue, strings.TrimSpace(qualityOverride) != "")
	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		h.inlineEmpty(ctx, b, query)
		return
	}
	pageSize := h.inlinePageSize()
	if pageSize <= 0 {
		pageSize = 30
	}
	lazy := shouldLazyLoadCollection(platformName)
	chunkOffset := 0
	requestCtx := ctx
	pageStart := (requestedPage - 1) * pageSize
	if pageStart < 0 {
		pageStart = 0
	}
	if lazy {
		chunkOffset = pageStart
		requestCtx = platform.WithPlaylistOffset(requestCtx, chunkOffset)
		requestCtx = platform.WithPlaylistLimit(requestCtx, pageSize)
	}
	playlist, err := plat.GetPlaylist(requestCtx, collectionID)
	if err != nil || playlist == nil {
		inlineMsg := &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  inlineStableID("collection_empty", platformName+"|"+collectionID+"|"+qualityValue),
			Title:               tr(ctx, "no_results"),
			Description:         tr(ctx, "cb_collection_not_found"),
			InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "no_results")},
		}
		_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{InlineQueryID: query.ID, IsPersonal: true, CacheTime: 1, Results: []telego.InlineQueryResult{inlineMsg}})
		return
	}
	collectionType := detectCollectionType(collectionID, playlist.URL)
	collectionLabel := collectionTypeLabel(collectionType)
	inlineMsgs := make([]telego.InlineQueryResult, 0, h.inlinePageSize()+3)
	title := strings.TrimSpace(playlist.Title)
	if title == "" {
		title = collectionLabel
	}
	desc := fmt.Sprintf("%s · %s", platformDisplayName(h.PlatformManager, platformName), collectionLabel)
	if playlist.TrackCount > 0 {
		desc = fmt.Sprintf("%s · %s · %s", platformDisplayName(h.PlatformManager, platformName), collectionLabel, tr(ctx, "cb_track_count", map[string]any{"Count": playlist.TrackCount}))
	} else if len(playlist.Tracks) > 0 {
		desc = fmt.Sprintf("%s · %s · %s", platformDisplayName(h.PlatformManager, platformName), collectionLabel, tr(ctx, "cb_track_count", map[string]any{"Count": len(playlist.Tracks)}))
	}
	thumb := buildInlineThumbnailURL(platformName, strings.TrimSpace(playlist.CoverURL), 150)
	collectionArticle := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  buildInlineCollectionResultID(platformName, collectionID, qualityValue),
		Title:               fmt.Sprintf("%s：%s", collectionLabel, title),
		Description:         desc,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: fmt.Sprintf("%s：%s\n%s", collectionLabel, title, tr(ctx, "cb_collection_tap_expand"))},
		ThumbnailURL:        thumb,
		ThumbnailWidth:      150,
		ThumbnailHeight:     150,
	}
	if h.CollectionChosen != nil {
		state := &inlineCollectionState{
			platformName:    platformName,
			collectionID:    collectionID,
			qualityValue:    qualityValue,
			requesterID:     query.From.ID,
			tracks:          playlist.Tracks,
			totalTracks:     playlist.TrackCount,
			collectionLabel: collectionLabel,
			title:           title,
			url:             strings.TrimSpace(playlist.URL),
			creator:         strings.TrimSpace(playlist.Creator),
			description:     strings.TrimSpace(playlist.Description),
			lazy:            lazy,
			cacheOffset:     chunkOffset,
			updatedAt:       time.Now(),
		}
		if state.totalTracks <= 0 {
			state.totalTracks = len(state.tracks)
		}
		token := h.CollectionChosen.storeInlineCollectionState(state)
		if keyboard := buildInlineCollectionOpenKeyboard(ctx, token, query.From.ID); keyboard != nil {
			collectionArticle.ReplyMarkup = keyboard
		}
	}
	inlineMsgs = append(inlineMsgs, collectionArticle)

	tracks := playlist.Tracks
	if len(tracks) > 0 {
		totalTracks := playlist.TrackCount
		if totalTracks <= 0 {
			totalTracks = len(tracks)
		}
		pageCount := (totalTracks-1)/pageSize + 1
		page := requestedPage
		if page <= 0 || page > pageCount {
			page = 1
		}
		start := (page - 1) * pageSize
		if start < 0 {
			start = 0
		}
		if lazy {
			start = 0
		}
		end := start + pageSize
		if end > len(tracks) {
			end = len(tracks)
		}
		for i := start; i < end; i++ {
			inlineMsgs = append(inlineMsgs, buildInlineTrackArticle(ctx, h, platformName, tracks[i], qualityValue, query.From.ID))
		}
		if pageCount > 1 {
			footerText := tr(ctx, "cb_page_footer", map[string]any{"Page": page, "Total": pageCount})
			hint := tr(ctx, "cb_collection_page_hint")
			inlineMsgs = append(inlineMsgs, &telego.InlineQueryResultArticle{
				Type:                telego.ResultTypeArticle,
				ID:                  inlineStableID("collection_page", fmt.Sprintf("%s|%s|%d|%d", platformName, collectionID, page, pageCount)),
				Title:               footerText,
				Description:         hint,
				InputMessageContent: &telego.InputTextMessageContent{MessageText: hint},
			})
		}
	}
	inlineMsgs = append(inlineMsgs, buildInlineSearchHeader(ctx, h, platformName, qualityValue))

	_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    true,
		CacheTime:     1,
		Results:       inlineMsgs,
	})
}

func (h *InlineSearchHandler) inlineEmpty(ctx context.Context, b *telego.Bot, query *telego.InlineQuery) {
	inlineMsg := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  query.ID,
		Title:               tr(ctx, "cb_empty_title"),
		Description:         "MusicBot-Go",
		InputMessageContent: &telego.InputTextMessageContent{MessageText: "MusicBot-Go"},
	}
	_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       []telego.InlineQueryResult{inlineMsg},
		CacheTime:     3600,
	})
}

func (h *InlineSearchHandler) inlineHelp(ctx context.Context, b *telego.Bot, query *telego.InlineQuery) {
	platformName := h.resolveDefaultPlatform(ctx, query.From.ID)
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	settingTitle := tr(ctx, "cb_setting_title", map[string]any{"Platform": platformDisplayName(h.PlatformManager, platformName), "Quality": qualityDisplayName(ctx, qualityValue)})
	settingCard := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  inlineStableID("help_settings", fmt.Sprintf("%d|%s|%s", query.From.ID, platformName, qualityValue)),
		Title:               settingTitle,
		Description:         tr(ctx, "cb_tap_edit_settings"),
		InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "cb_settings_message", map[string]any{"Platform": platformDisplayName(h.PlatformManager, platformName), "Quality": qualityDisplayName(ctx, qualityValue)})},
		ReplyMarkup:         buildInlineSettingsKeyboard(ctx, h.BotName),
	}
	randomCard := h.buildInlineRandomCard(ctx, query.From.ID, query.From.ID)
	if randomCard == nil {
		randomCard = &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  inlineStableID("help_random_empty", fmt.Sprintf("%d", query.From.ID)),
			Title:               tr(ctx, "cb_random_one"),
			Description:         tr(ctx, "cb_random_empty_desc"),
			InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "no_results")},
		}
	}
	results := []telego.InlineQueryResult{randomCard, settingCard}
	results = h.appendInlineFavoriteCards(ctx, results, query.From.ID)
	_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    true,
		Results:       results,
		// Short cache so a just-added/removed favorite shows up promptly in the
		// empty-query list instead of being served stale from Telegram's cache.
		CacheTime: 5,
	})
}

func (h *InlineSearchHandler) buildInlineRandomCard(ctx context.Context, id int64, requesterID int64) telego.InlineQueryResult {
	if h == nil {
		return nil
	}
	return &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  fmt.Sprintf("z_%d", id),
		Title:               tr(ctx, "cb_random_one"),
		Description:         tr(ctx, "cb_random_card_desc"),
		InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "wait_for_down")},
		ReplyMarkup:         buildInlineRandomSendKeyboard(ctx, requesterID),
	}
}

func buildInlineSettingsKeyboard(ctx context.Context, botName string) *telego.InlineKeyboardMarkup {
	name := strings.TrimPrefix(strings.TrimSpace(botName), "@")
	if name == "" {
		return nil
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{
		{Text: tr(ctx, "cb_edit_settings_btn"), URL: fmt.Sprintf("https://t.me/%s?start=settings", name)},
	}}}
}

const (
	inlineDefaultSearchLimit = 48
	inlineNeteaseSearchLimit = 48
)

func (h *InlineSearchHandler) inlineSearch(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, keyWord, requestedPlatform, qualityOverride string, requestedPage int, invalidPageFallbackKeyword string) {
	keyWord = strings.TrimSpace(keyWord)
	if keyWord == "" {
		inlineMsg := &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  "inline_empty_keyword",
			Title:               tr(ctx, "cb_input_keyword"),
			Description:         "MusicBot-Go",
			InputMessageContent: &telego.InputTextMessageContent{MessageText: "MusicBot-Go"},
		}
		_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
			InlineQueryID: query.ID,
			IsPersonal:    false,
			Results:       []telego.InlineQueryResult{inlineMsg},
			CacheTime:     3600,
		})
		return
	}

	if h.PlatformManager == nil {
		return
	}

	platformName := h.resolveDefaultPlatform(ctx, query.From.ID)
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	fallbackPlatform := h.FallbackPlatform
	if strings.TrimSpace(fallbackPlatform) == "" {
		fallbackPlatform = "netease"
	}
	if requestedPlatform != "" {
		platformName = requestedPlatform
		fallbackPlatform = ""
	}
	if strings.TrimSpace(qualityOverride) != "" {
		qualityValue = qualityOverride
	}

	var inlineMsgs []telego.InlineQueryResult

	params := &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    true,
		CacheTime:     1,
	}

	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		h.inlineEmpty(ctx, b, query)
		return
	}

	pageSize := h.inlinePageSize()

	biliFilter := true
	if enabled, supported, _ := resolveSearchFilterEnabled(ctx, h.PlatformManager, h.Repo, platformName, botpkg.PluginScopeUser, query.From.ID); supported {
		biliFilter = enabled
	}
	searchCtx := withSearchFilterContext(ctx, h.PlatformManager, platformName, biliFilter)

	searchWithFallback := func(keyword string) ([]platform.Track, string, error) {
		tracks, matchedPlatform, _, searchErr := searchTracksWithFallback(searchCtx, h.PlatformManager, platformName, fallbackPlatform, keyword, h.inlineSearchLimit, true)
		return tracks, matchedPlatform, searchErr
	}

	tracks, matchedPlatform, err := searchWithFallback(keyWord)
	platformName = matchedPlatform
	qualityValue = resolvePlatformQualityValue(ctx, h.Repo, botpkg.PluginScopeUser, query.From.ID, platformName, qualityValue, strings.TrimSpace(qualityOverride) != "")

	if err != nil || len(tracks) == 0 {
		inlineMsg := &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  inlineStableID("inline_no_results", keyWord+"|"+platformName+"|"+qualityValue),
			Title:               tr(ctx, "no_results"),
			Description:         tr(ctx, "no_results"),
			InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "no_results")},
		}
		params.Results = []telego.InlineQueryResult{inlineMsg}
		_ = b.AnswerInlineQuery(ctx, params)
		return
	}

	pageCount := (len(tracks)-1)/pageSize + 1
	page := requestedPage
	if page > pageCount && strings.TrimSpace(invalidPageFallbackKeyword) != "" {
		fallbackKeyword := strings.TrimSpace(invalidPageFallbackKeyword)
		fallbackTracks, fallbackMatchedPlatform, fallbackErr := searchWithFallback(fallbackKeyword)
		if fallbackErr == nil && len(fallbackTracks) > 0 {
			keyWord = fallbackKeyword
			tracks = fallbackTracks
			platformName = fallbackMatchedPlatform
			qualityValue = resolvePlatformQualityValue(ctx, h.Repo, botpkg.PluginScopeUser, query.From.ID, platformName, qualityValue, strings.TrimSpace(qualityOverride) != "")
			pageCount = (len(tracks)-1)/pageSize + 1
		}
	}
	if page <= 0 || page > pageCount {
		page = 1
	}
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	if end > len(tracks) {
		end = len(tracks)
	}

	inlineMsgs = make([]telego.InlineQueryResult, 0, pageSize+2)
	for i := start; i < end; i++ {
		track := tracks[i]
		inlineMsg := buildInlineTrackArticle(ctx, h, platformName, track, qualityValue, query.From.ID)
		inlineMsgs = append(inlineMsgs, inlineMsg)
	}
	inlineMsgs = append(inlineMsgs, buildInlineSearchPageFooter(ctx, keyWord, requestedPlatform, qualityOverride, page, pageCount, len(tracks)))
	inlineMsgs = append(inlineMsgs, buildInlineSearchHeader(ctx, h, platformName, qualityValue))
	params.Results = inlineMsgs
	_ = b.AnswerInlineQuery(ctx, params)
}

func (h *InlineSearchHandler) inlinePageSize() int {
	if h == nil || h.PageSize <= 0 {
		return 30
	}
	if h.PageSize > 30 {
		return 30
	}
	return h.PageSize
}

func (h *InlineSearchHandler) inlineSearchLimit(platformName string) int {
	if strings.TrimSpace(strings.ToLower(platformName)) == "netease" {
		return inlineNeteaseSearchLimit
	}
	return inlineDefaultSearchLimit
}

func (h *InlineSearchHandler) inlineCommand(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, platformName, trackID, qualityOverride string) {
	if strings.TrimSpace(platformName) == "" || strings.TrimSpace(trackID) == "" {
		h.inlineEmpty(ctx, b, query)
		return
	}
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	if strings.TrimSpace(qualityOverride) != "" {
		qualityValue = strings.TrimSpace(qualityOverride)
	}
	qualityValue = resolvePlatformQualityValue(ctx, h.Repo, botpkg.PluginScopeUser, query.From.ID, platformName, qualityValue, strings.TrimSpace(qualityOverride) != "")
	inlineMsgs := make([]telego.InlineQueryResult, 0, 2)

	title := trackID
	artists := ""
	album := ""
	thumbnailSource := ""
	if h.PlatformManager != nil {
		if plat := h.PlatformManager.Get(platformName); plat != nil {
			if track, err := plat.GetTrack(ctx, trackID); err == nil && track != nil {
				title = strings.TrimSpace(track.Title)
				if strings.TrimSpace(track.ID) != "" {
					trackID = track.ID
				}
				artists = inlineArtistsLabel(track.Artists)
				thumbnailSource = strings.TrimSpace(track.CoverURL)
				if track.Album != nil {
					album = strings.TrimSpace(track.Album.Title)
					if thumbnailSource == "" {
						thumbnailSource = strings.TrimSpace(track.Album.CoverURL)
					}
				}
			}
		}
	}
	thumbnailURL := buildInlineThumbnailURL(platformName, thumbnailSource, 150)
	inlineMsg := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  buildInlinePendingResultID(platformName, trackID, qualityValue),
		Title:               fallbackString(title, trackID),
		Description:         inlineSubtitle(ctx, album, artists),
		InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "wait_for_down")},
		ReplyMarkup:         buildInlineSendKeyboard(ctx, platformName, trackID, qualityValue, query.From.ID),
		ThumbnailURL:        thumbnailURL,
		ThumbnailWidth:      150,
		ThumbnailHeight:     150,
	}
	inlineMsgs = append(inlineMsgs, inlineMsg)
	inlineMsgs = append(inlineMsgs, buildInlineSearchHeader(ctx, h, platformName, qualityValue))
	params := &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       inlineMsgs,
		CacheTime:     60,
	}
	_ = b.AnswerInlineQuery(ctx, params)
}

func buildInlineSearchHeader(ctx context.Context, h *InlineSearchHandler, platformName, qualityValue string) telego.InlineQueryResult {
	platformText := platformDisplayName(h.PlatformManager, platformName)
	if strings.TrimSpace(platformText) == "" {
		platformText = platformName
	}
	if strings.TrimSpace(qualityValue) == "" {
		qualityValue = "hires"
	}
	qualityText := qualityDisplayName(ctx, qualityValue)
	replyMarkup := (*telego.InlineKeyboardMarkup)(nil)
	botName := strings.TrimPrefix(strings.TrimSpace(h.BotName), "@")
	if botName != "" {
		replyMarkup = &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{
			{Text: tr(ctx, "cb_edit_settings_btn"), URL: fmt.Sprintf("https://t.me/%s?start=settings", botName)},
		}}}
	}
	return &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  inlineStableID("meta", platformText+"|"+qualityText),
		Title:               tr(ctx, "cb_setting_title", map[string]any{"Platform": platformText, "Quality": qualityText}),
		Description:         tr(ctx, "cb_search_header_desc"),
		InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "cb_settings_message", map[string]any{"Platform": platformText, "Quality": qualityText})},
		ReplyMarkup:         replyMarkup,
	}
}

func buildInlineSearchPageFooter(ctx context.Context, keyword, platformName, qualityValue string, page, pageCount, total int) telego.InlineQueryResult {
	keyword = strings.TrimSpace(keyword)
	platformName = inlinePageHintPlatformToken(strings.TrimSpace(platformName))
	qualityValue = strings.TrimSpace(qualityValue)
	if page < 1 {
		page = 1
	}
	if pageCount < 1 {
		pageCount = 1
	}
	if total < 0 {
		total = 0
	}
	queryParts := make([]string, 0, 4)
	if keyword != "" {
		queryParts = append(queryParts, keyword)
	}
	if platformName != "" {
		queryParts = append(queryParts, platformName)
	}
	if qualityValue != "" {
		queryParts = append(queryParts, qualityValue)
	}
	nextPage := page + 1
	if nextPage < 2 || nextPage > pageCount {
		nextPage = 2
	}
	queryParts = append(queryParts, strconv.Itoa(nextPage))
	hintQuery := strings.TrimSpace(strings.Join(queryParts, " "))
	if hintQuery == "2" {
		hintQuery = tr(ctx, "cb_page_hint_example")
	}
	title := tr(ctx, "cb_page_footer", map[string]any{"Page": page, "Total": pageCount})
	desc := tr(ctx, "cb_search_page_desc", map[string]any{"Total": total, "Hint": hintQuery})
	return &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  inlineStableID("page", fmt.Sprintf("%s|%s|%s|%d|%d|%d", keyword, platformName, qualityValue, page, pageCount, total)),
		Title:               title,
		Description:         desc,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: desc},
	}
}

func inlineStableID(prefix, payload string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "id"
	}
	payload = strings.TrimSpace(payload)
	sum := md5.Sum([]byte(payload))
	return fmt.Sprintf("%s_%x", prefix, sum[:6])
}

func parseInlineSearchOptions(text string, manager platform.Manager) (baseText, platformName, quality string, page int, invalidPageFallbackKeyword string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", "", "", 1, ""
	}
	baseText, platformName, quality = parseTrailingOptions(trimmed, manager)
	page = 1
	invalidPageFallbackKeyword = ""

	fields := strings.Fields(trimmed)
	if len(fields) < 2 {
		return baseText, platformName, quality, page, invalidPageFallbackKeyword
	}
	last := strings.TrimSpace(fields[len(fields)-1])
	candidate, err := strconv.Atoi(last)
	if err != nil {
		return baseText, platformName, quality, page, invalidPageFallbackKeyword
	}
	if candidate <= 0 {
		return baseText, platformName, quality, page, invalidPageFallbackKeyword
	}

	withoutPage := strings.TrimSpace(strings.Join(fields[:len(fields)-1], " "))
	parsedBase, parsedPlatform, parsedQuality := parseTrailingOptions(withoutPage, manager)
	if strings.TrimSpace(parsedBase) == "" {
		return baseText, platformName, quality, page, invalidPageFallbackKeyword
	}
	if strings.TrimSpace(parsedPlatform) != "" || strings.TrimSpace(parsedQuality) != "" {
		return parsedBase, parsedPlatform, parsedQuality, candidate, invalidPageFallbackKeyword
	}
	invalidPageFallbackKeyword = baseText
	return parsedBase, parsedPlatform, parsedQuality, candidate, invalidPageFallbackKeyword
}

func inlinePageHintPlatformToken(platformName string) string {
	switch strings.ToLower(strings.TrimSpace(platformName)) {
	case "qqmusic":
		return "qq"
	default:
		return strings.TrimSpace(platformName)
	}
}

func buildInlineTrackArticle(ctx context.Context, h *InlineSearchHandler, platformName string, track platform.Track, qualityValue string, requesterID int64) telego.InlineQueryResult {
	thumbnailSource := strings.TrimSpace(track.CoverURL)
	if thumbnailSource == "" && track.Album != nil {
		thumbnailSource = strings.TrimSpace(track.Album.CoverURL)
	}
	if thumbnailSource == "" && h != nil && h.PlatformManager != nil {
		plat := strings.ToLower(strings.TrimSpace(platformName))
		if strings.Contains(plat, "qq") || strings.Contains(plat, "tencent") {
			if p := h.PlatformManager.Get(platformName); p != nil && strings.TrimSpace(track.ID) != "" {
				if detail, err := p.GetTrack(ctx, track.ID); err == nil && detail != nil {
					if strings.TrimSpace(detail.CoverURL) != "" {
						thumbnailSource = strings.TrimSpace(detail.CoverURL)
					} else if detail.Album != nil {
						thumbnailSource = strings.TrimSpace(detail.Album.CoverURL)
					}
				}
			}
		}
	}
	thumbnailURL := buildInlineThumbnailURL(platformName, thumbnailSource, 150)
	return &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  buildInlinePendingResultID(platformName, track.ID, qualityValue),
		Title:               fallbackString(strings.TrimSpace(track.Title), track.ID),
		Description:         inlineSubtitle(ctx, trackAlbumLabel(track.Album), inlineArtistsLabel(track.Artists)),
		InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "wait_for_down")},
		ReplyMarkup:         buildInlineSendKeyboard(ctx, platformName, track.ID, qualityValue, requesterID),
		ThumbnailURL:        thumbnailURL,
		ThumbnailWidth:      150,
		ThumbnailHeight:     150,
	}
}

// buildInlineFavoriteCard builds a lightweight inline result card for a favorite.
// Unlike buildInlineTrackArticle it never makes a network call (no thumbnail
// lookup), keeping the empty-query favorites list snappy. Selecting it routes
// through the normal pending-result download flow (ChosenInlineResult).
func buildInlineFavoriteCard(ctx context.Context, fav *botpkg.Favorite, qualityValue string, requesterID int64) telego.InlineQueryResult {
	if fav == nil {
		return nil
	}
	title := strings.TrimSpace(fav.SongName)
	if title == "" {
		title = fav.TrackID
	}
	desc := strings.TrimSpace(fav.SongArtists)
	if album := strings.TrimSpace(fav.SongAlbum); album != "" {
		if desc != "" {
			desc += " · "
		}
		desc += album
	}
	return &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  buildInlinePendingResultID(fav.Platform, fav.TrackID, qualityValue),
		Title:               "⭐ " + title,
		Description:         desc,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "wait_for_down")},
		ReplyMarkup:         buildInlineSendKeyboard(ctx, fav.Platform, fav.TrackID, qualityValue, requesterID),
	}
}

// buildInlineFavoriteMenuCard builds a card that, when selected, posts the full
// favorites menu (the same list message with send / manage / random buttons).
// Inline mode has no chat ID, so it's a personal list.
func (h *InlineSearchHandler) buildInlineFavoriteMenuCard(ctx context.Context, userID int64) telego.InlineQueryResult {
	if h == nil || h.Favorites == nil || userID == 0 {
		return nil
	}
	token := storeFavoriteListPayload(favoriteListPayload{requesterID: userID})
	text, markup := h.Favorites.buildListView(ctx, favoriteListContext{token: token, requesterID: userID, view: "u", page: 1})
	return &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  inlineStableID("fav_menu", fmt.Sprintf("%d", userID)),
		Title:               tr(ctx, "cb_my_favorites"),
		Description:         tr(ctx, "cb_favorites_menu_desc"),
		InputMessageContent: &telego.InputTextMessageContent{MessageText: text, ParseMode: telego.ModeHTML, LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true}},
		ReplyMarkup:         markup,
	}
}

// appendInlineFavoriteCards appends the user's personal favorites as inline
// result cards (group favorites are unavailable here — inline mode has no chat
// ID). A "favorites menu" card is placed above the songs so the full list (with
// management) can be opened. Errors and empty lists are silently skipped.
func (h *InlineSearchHandler) appendInlineFavoriteCards(ctx context.Context, results []telego.InlineQueryResult, userID int64) []telego.InlineQueryResult {
	if h == nil || h.Repo == nil || userID == 0 {
		return results
	}
	limit := h.inlinePageSize()
	if limit <= 0 || limit > 24 {
		limit = 24
	}
	favs, err := h.Repo.ListFavorites(ctx, botpkg.FavoriteScopeUser, userID, limit, 0)
	if err != nil || len(favs) == 0 {
		return results
	}
	if menu := h.buildInlineFavoriteMenuCard(ctx, userID); menu != nil {
		results = append(results, menu)
	}
	quality := h.resolveDefaultQuality(ctx, userID)
	for _, fav := range favs {
		if card := buildInlineFavoriteCard(ctx, fav, quality, userID); card != nil {
			results = append(results, card)
		}
	}
	return results
}

func buildInlineThumbnailURL(platformName, rawURL string, size int) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if size <= 0 {
		size = 150
	}
	plat := strings.ToLower(strings.TrimSpace(platformName))

	// 网易云: 增加/覆盖 ?param=150y150
	if plat == "netease" {
		if coverID := extractNeteaseCoverID(rawURL); coverID != "" {
			encrypted := neteaseEncryptID(coverID)
			if encrypted != "" {
				return fmt.Sprintf("https://p3.music.126.net/%s/%s.jpg?param=%dy%d", encrypted, coverID, size, size)
			}
		}
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return rawURL
		}
		query := parsed.Query()
		query.Set("param", fmt.Sprintf("%dy%d", size, size))
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}

	// QQ音乐: T002R{size}x{size}M000
	if strings.Contains(plat, "qq") || strings.Contains(plat, "tencent") {
		if qqCoverResizePattern.MatchString(rawURL) {
			return qqCoverResizePattern.ReplaceAllString(rawURL, fmt.Sprintf("T002R%dx%dM000", size, size))
		}
		// QQ 搜索结果常见格式: T002M000{mid}.jpg
		if matches := qqCoverMidPattern.FindStringSubmatch(rawURL); len(matches) == 2 {
			return strings.Replace(rawURL, matches[0], fmt.Sprintf("T002R%dx%dM000%s.jpg", size, size, matches[1]), 1)
		}
		// QQ 单曲封面格式: T062M000{mid}.jpg -> T062R{size}x{size}M000{mid}.jpg
		if matches := qqSongMidPattern.FindStringSubmatch(rawURL); len(matches) == 2 {
			return strings.Replace(rawURL, matches[0], fmt.Sprintf("T062R%dx%dM000%s.jpg", size, size, matches[1]), 1)
		}
	}

	return rawURL
}

func extractNeteaseCoverID(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	if !strings.Contains(strings.ToLower(parsed.Host), "music.126.net") {
		return ""
	}
	path := strings.Trim(parsed.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	filename := parts[len(parts)-1]
	if dot := strings.Index(filename, "."); dot > 0 {
		filename = filename[:dot]
	}
	if filename == "" {
		return ""
	}
	for _, ch := range filename {
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	return filename
}

func neteaseEncryptID(id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	magic := []byte("3go8&$8*3*3h0k(2)2")
	songID := []byte(id)
	for i := range songID {
		songID[i] = songID[i] ^ magic[i%len(magic)]
	}
	digest := md5.Sum(songID)
	encoded := base64.StdEncoding.EncodeToString(digest[:])
	encoded = strings.ReplaceAll(encoded, "/", "_")
	encoded = strings.ReplaceAll(encoded, "+", "-")
	return encoded
}

func inlineSubtitle(ctx context.Context, album, artists string) string {
	album = strings.TrimSpace(album)
	artists = strings.TrimSpace(artists)
	if artists == "" {
		artists = tr(ctx, "cb_unknown_artist")
	}
	if album == "" {
		return artists
	}
	return album + " · " + artists
}

func inlineArtistsLabel(artists []platform.Artist) string {
	if len(artists) == 0 {
		return ""
	}
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		if name := strings.TrimSpace(artist.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, "/")
}

func trackAlbumLabel(album *platform.Album) string {
	if album == nil {
		return ""
	}
	return strings.TrimSpace(album.Title)
}

func fallbackString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func qualityDisplayName(ctx context.Context, quality string) string {
	switch strings.TrimSpace(strings.ToLower(quality)) {
	case "standard":
		return tr(ctx, "cb_quality_standard")
	case "high":
		return tr(ctx, "cb_quality_high")
	case "lossless":
		return tr(ctx, "cb_quality_lossless")
	case "hires":
		return tr(ctx, "cb_quality_hires")
	default:
		return quality
	}
}

func (h *InlineSearchHandler) inlineCachedOrCommand(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, platformName, trackID, qualityOverride string, requestedPage int, originalQuery string) bool {
	if strings.TrimSpace(platformName) == "" || strings.TrimSpace(trackID) == "" {
		return false
	}
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	if strings.TrimSpace(qualityOverride) != "" {
		qualityValue = strings.TrimSpace(qualityOverride)
	}
	if h.tryInlineDirectEpisodes(ctx, b, query, platformName, trackID, qualityValue, requestedPage, originalQuery) {
		return true
	}
	if info := h.findCachedSong(ctx, platformName, trackID, qualityValue); info != nil {
		h.inlineCached(ctx, b, query, info, platformName, qualityValue)
		return true
	}
	h.inlineCommand(ctx, b, query, platformName, trackID, qualityOverride)
	return true
}

func (h *InlineSearchHandler) tryInlineDirectEpisodes(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, platformName, trackID, qualityValue string, requestedPage int, originalQuery string) bool {
	if h == nil || b == nil || query == nil || h.PlatformManager == nil {
		return false
	}
	baseTrackID, _, hasExplicitPage, ok := parseEpisodeTrackID(h.PlatformManager, platformName, trackID)
	if !ok || hasExplicitPage || strings.TrimSpace(baseTrackID) == "" {
		return false
	}
	plat := h.PlatformManager.Get(strings.TrimSpace(platformName))
	if plat == nil {
		return false
	}
	if _, ok := plat.(platform.EpisodeTrackIDResolver); !ok {
		return false
	}
	provider, ok := plat.(platform.EpisodeProvider)
	if !ok {
		return false
	}
	episodes, err := provider.ListEpisodes(ctx, baseTrackID)
	if err != nil || len(episodes) <= 1 {
		return false
	}

	pageSize := h.inlinePageSize()
	if pageSize <= 0 {
		pageSize = 30
	}
	pageCount := (len(episodes)-1)/pageSize + 1
	page := requestedPage
	if page <= 0 || page > pageCount {
		page = 1
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(episodes) {
		end = len(episodes)
	}

	episodeCollectionID := buildEpisodeCollectionID(h.PlatformManager, platformName, baseTrackID)
	results := make([]telego.InlineQueryResult, 0, (end-start)+2)
	first := episodes[0]
	headerTitle := strings.TrimSpace(first.VideoTitle)
	if headerTitle == "" {
		headerTitle = strings.TrimSpace(first.Title)
	}
	headerDesc := strings.TrimSpace(first.CreatorName)
	if headerDesc == "" {
		headerDesc = tr(ctx, "cb_tap_episode_below")
	}
	results = append(results, &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  buildInlineCollectionResultID(platformName, episodeCollectionID, qualityValue),
		Title:               fallbackString(headerTitle, baseTrackID),
		Description:         headerDesc,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "cb_fetching_episodes")},
		ReplyMarkup: func() *telego.InlineKeyboardMarkup {
			if cb := buildInlineEpisodeShowCallbackData(platformName, baseTrackID, qualityValue, query.From.ID, 1); cb != "" {
				return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{
					{Text: tr(ctx, "inline_tap_to_send"), CallbackData: cb},
				}}}
			}
			return nil
		}(),
	})

	for i := start; i < end; i++ {
		ep := episodes[i]
		displayIndex := ep.Index
		title := strings.TrimSpace(ep.Title)
		if title == "" {
			title = fmt.Sprintf("P%d", ep.Index)
		}
		explicitTrackID := buildEpisodeTrackID(h.PlatformManager, platformName, baseTrackID, ep.Index, true)
		if strings.TrimSpace(explicitTrackID) == "" {
			explicitTrackID = strings.TrimSpace(ep.TrackID)
		}
		results = append(results, &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  buildInlinePendingResultID(platformName, explicitTrackID, qualityValue),
			Title:               fmt.Sprintf("%d. %s", displayIndex, title),
			Description:         strings.TrimSpace(first.CreatorName),
			InputMessageContent: &telego.InputTextMessageContent{MessageText: tr(ctx, "wait_for_down")},
			ReplyMarkup:         buildInlineSendKeyboard(ctx, platformName, explicitTrackID, qualityValue, query.From.ID),
		})
	}

	if pageCount > 1 {
		hintQuery := strings.TrimSpace(originalQuery)
		if hintQuery == "" {
			hintQuery = strings.TrimSpace(baseTrackID)
		}
		hintQuery = fmt.Sprintf("%s %d", hintQuery, page+1)
		title := tr(ctx, "cb_page_footer", map[string]any{"Page": page, "Total": pageCount})
		desc := tr(ctx, "cb_episode_page_hint", map[string]any{"Hint": hintQuery})
		results = append(results, &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  inlineStableID("ep_page", fmt.Sprintf("%s|%s|%d|%d", platformName, baseTrackID, page, pageCount)),
			Title:               title,
			Description:         desc,
			InputMessageContent: &telego.InputTextMessageContent{MessageText: desc},
		})
	}

	results = append(results, buildInlineSearchHeader(ctx, h, platformName, qualityValue))
	_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{InlineQueryID: query.ID, IsPersonal: true, CacheTime: 1, Results: results})
	return true
}

func (h *InlineSearchHandler) inlineCached(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, info *botpkg.SongInfo, platformFallback, qualityFallback string) {
	if info == nil {
		return
	}
	platformName := strings.TrimSpace(info.Platform)
	if platformName == "" {
		platformName = platformFallback
	}
	if platformName == "" {
		platformName = h.DefaultPlatform
	}
	if strings.TrimSpace(platformName) == "" {
		platformName = "netease"
	}
	qualityValue := strings.TrimSpace(info.Quality)
	if qualityValue == "" {
		qualityValue = h.resolveDefaultQuality(ctx, query.From.ID)
	}
	if strings.TrimSpace(qualityValue) == "" {
		qualityValue = "hires"
	}
	trackID := strings.TrimSpace(info.TrackID)
	if trackID == "" && platformName == "netease" && info.MusicID != 0 {
		trackID = fmt.Sprintf("%d", info.MusicID)
	}
	songInfo := *info
	if strings.TrimSpace(songInfo.TrackURL) == "" && platformName == "netease" && trackID != "" {
		songInfo.TrackURL = fmt.Sprintf("https://music.163.com/song?id=%s", trackID)
	}
	var keyboard *telego.InlineKeyboardMarkup
	if resolveForwardButtonEnabledForUser(ctx, h.Repo, query.From.ID) {
		keyboard = buildSongBottomKeyboard(ctx, h.Repo, songButtonOptions{
			platformName:  platformName,
			trackID:       trackID,
			trackURL:      songInfo.TrackURL,
			quality:       qualityValue,
			requesterID:   query.From.ID,
			botName:       h.BotName,
			inlineContext: true,
		})
	}

	newAudio := &telego.InlineQueryResultCachedDocument{
		Type:           telego.ResultTypeDocument,
		ID:             buildInlineCachedResultID(platformName, trackID, qualityValue),
		DocumentFileID: info.FileID,
		Title:          fmt.Sprintf("%s - %s", songInfo.SongArtists, songInfo.SongName),
		Caption:        buildMusicCaption(h.PlatformManager, &songInfo, h.BotName),
		ParseMode:      telego.ModeHTML,
		ReplyMarkup:    keyboard,
		Description:    songInfo.SongAlbum,
	}

	_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		Results:       []telego.InlineQueryResult{newAudio},
		IsPersonal:    false,
		CacheTime:     3600,
	})
}

func (h *InlineSearchHandler) resolveDefaultQuality(ctx context.Context, userID int64) string {
	qualityValue := strings.TrimSpace(h.DefaultQuality)
	if h.Repo != nil && userID != 0 {
		if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultQuality) != "" {
				qualityValue = settings.DefaultQuality
			}
		}
	}
	if strings.TrimSpace(qualityValue) == "" {
		qualityValue = "hires"
	}
	return qualityValue
}

func (h *InlineSearchHandler) resolveDefaultPlatform(ctx context.Context, userID int64) string {
	platformName := strings.TrimSpace(h.DefaultPlatform)
	if platformName == "" {
		platformName = "netease"
	}
	if h.Repo != nil && userID != 0 {
		if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultPlatform) != "" {
				platformName = settings.DefaultPlatform
			}
		}
	}
	return platformName
}

func normalizeInlineKeywordQuery(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) >= len("search") && strings.EqualFold(trimmed[:len("search")], "search") {
		trimmed = strings.TrimSpace(trimmed[len("search"):])
	}
	return trimmed
}

func (h *InlineSearchHandler) tryResolveDirectTrack(ctx context.Context, text, platformSuffix string) (platformName, trackID string, matched bool) {
	if h == nil || h.PlatformManager == nil {
		return "", "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", false
	}
	fields := strings.Fields(text)
	if len(fields) >= 2 {
		if platformName, ok := resolvePlatformAlias(h.PlatformManager, fields[0]); ok {
			candidate := strings.TrimSpace(strings.Join(fields[1:], " "))
			if trackID, ok := matchPlatformTrack(ctx, h.PlatformManager, platformName, candidate); ok {
				return platformName, trackID, true
			}
		}
	}
	if platformSuffix != "" && len(fields) == 1 && isLikelyIDToken(fields[0]) {
		return platformSuffix, fields[0], true
	}
	if platformName, trackID, ok := h.PlatformManager.MatchText(text); ok {
		return platformName, trackID, true
	}
	if platformName, trackID, ok := h.PlatformManager.MatchURL(text); ok {
		return platformName, trackID, true
	}
	if urlStr := extractFirstURL(text); urlStr != "" && urlStr != text {
		if platformName, trackID, ok := h.PlatformManager.MatchURL(urlStr); ok {
			return platformName, trackID, true
		}
		if platformName, trackID, ok := h.PlatformManager.MatchText(urlStr); ok {
			return platformName, trackID, true
		}
	}
	return "", "", false
}

func (h *InlineSearchHandler) findCachedSong(ctx context.Context, platformName, trackID, quality string) *botpkg.SongInfo {
	if h.Repo == nil {
		return nil
	}
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	if platformName == "" || trackID == "" {
		return nil
	}
	for _, q := range qualityFallbacks(quality) {
		info, err := h.Repo.FindByPlatformTrackID(ctx, platformName, trackID, q)
		if err == nil && info != nil && info.FileID != "" && info.SongName != "" {
			verifyCachedNeteaseQuality(ctx, h.PlatformManager, h.Repo, nil, info, platformName, trackID, info.Quality)
			return info
		}
	}
	if platformName == "netease" {
		if id, err := strconv.Atoi(trackID); err == nil {
			info, err := h.Repo.FindByMusicID(ctx, id)
			if err == nil && info != nil && info.FileID != "" && info.SongName != "" {
				verifyCachedNeteaseQuality(ctx, h.PlatformManager, h.Repo, nil, info, platformName, trackID, info.Quality)
				return info
			}
		}
	}
	return nil
}

func qualityFallbacks(primary string) []string {
	order := []string{"hires", "lossless", "high", "standard"}
	result := make([]string, 0, len(order)+1)
	primary = strings.TrimSpace(primary)
	if primary != "" {
		result = append(result, primary)
	}
	for _, q := range order {
		if q == primary {
			continue
		}
		result = append(result, q)
	}
	return result
}
