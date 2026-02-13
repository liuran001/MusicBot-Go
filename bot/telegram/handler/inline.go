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
	BotName          string
	DefaultPlatform  string
	DefaultQuality   string
	FallbackPlatform string
	PageSize         int
}

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
			h.inlineCachedOrCommand(ctx, b, query, platformName, trackID, qualityOverride)
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
	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		h.inlineEmpty(ctx, b, query)
		return
	}
	playlist, err := plat.GetPlaylist(ctx, collectionID)
	if err != nil || playlist == nil {
		inlineMsg := &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()),
			Title:               noResults,
			Description:         "未找到专辑/歌单",
			InputMessageContent: &telego.InputTextMessageContent{MessageText: noResults},
		}
		_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{InlineQueryID: query.ID, IsPersonal: true, CacheTime: 1, Results: []telego.InlineQueryResult{inlineMsg}})
		return
	}
	collectionType := detectCollectionType(collectionID, playlist.URL)
	collectionLabel := collectionTypeLabel(collectionType)
	inlineMsgs := make([]telego.InlineQueryResult, 0, h.inlinePageSize()+3)
	inlineMsgs = append(inlineMsgs, buildInlineSearchHeader(h, platformName, qualityValue))
	title := strings.TrimSpace(playlist.Title)
	if title == "" {
		title = collectionLabel
	}
	desc := fmt.Sprintf("%s · %s", platformDisplayName(h.PlatformManager, platformName), collectionLabel)
	if playlist.TrackCount > 0 {
		desc = fmt.Sprintf("%s · %s · %d 首", platformDisplayName(h.PlatformManager, platformName), collectionLabel, playlist.TrackCount)
	} else if len(playlist.Tracks) > 0 {
		desc = fmt.Sprintf("%s · %s · %d 首", platformDisplayName(h.PlatformManager, platformName), collectionLabel, len(playlist.Tracks))
	}
	thumb := buildInlineThumbnailURL(platformName, strings.TrimSpace(playlist.CoverURL), 150)
	collectionArticle := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  buildInlineCollectionResultID(platformName, collectionID, qualityValue),
		Title:               fmt.Sprintf("%s：%s", collectionLabel, title),
		Description:         desc,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: fmt.Sprintf("%s：%s\n点击后自动展开详情", collectionLabel, title)},
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
			updatedAt:       time.Now(),
		}
		if state.totalTracks <= 0 {
			state.totalTracks = len(state.tracks)
		}
		token := h.CollectionChosen.storeInlineCollectionState(state)
		if keyboard := buildInlineCollectionOpenKeyboard(token, query.From.ID); keyboard != nil {
			collectionArticle.ReplyMarkup = keyboard
		}
	}
	inlineMsgs = append(inlineMsgs, collectionArticle)

	tracks := playlist.Tracks
	if len(tracks) > 0 {
		pageSize := h.inlinePageSize()
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
		end := start + pageSize
		if end > len(tracks) {
			end = len(tracks)
		}
		for i := start; i < end; i++ {
			inlineMsgs = append(inlineMsgs, buildInlineTrackArticle(ctx, h, platformName, tracks[i], qualityValue, query.From.ID))
		}
		footerText := fmt.Sprintf("第 %d 页 / 共 %d 页", page, pageCount)
		hint := "在链接末尾加数字翻页，例如：链接 2"
		inlineMsgs = append(inlineMsgs, &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  fmt.Sprintf("collection_page_%d_%d_%d", page, pageCount, time.Now().UnixMicro()),
			Title:               footerText,
			Description:         hint,
			InputMessageContent: &telego.InputTextMessageContent{MessageText: hint},
		})
	}

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
		Title:               "输入 help 获取帮助",
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
	randomID := time.Now().UnixMicro()
	inlineMsg1 := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  fmt.Sprintf("%d", randomID),
		Title:               "1. 粘贴音乐分享 URL 或输入 MusicID",
		Description:         "MusicBot-Go",
		InputMessageContent: &telego.InputTextMessageContent{MessageText: "MusicBot-Go"},
	}
	inlineMsg2 := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  fmt.Sprintf("%d", randomID+1),
		Title:               "2. 输入关键词搜索歌曲",
		Description:         "MusicBot-Go",
		InputMessageContent: &telego.InputTextMessageContent{MessageText: "MusicBot-Go"},
	}
	inlineMsg3 := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  fmt.Sprintf("%d", randomID+2),
		Title:               "3. 关键词后可加 平台+音质",
		Description:         "示例: 稻香 qq hires",
		InputMessageContent: &telego.InputTextMessageContent{MessageText: "MusicBot-Go"},
	}
	_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       []telego.InlineQueryResult{inlineMsg1, inlineMsg2, inlineMsg3},
		CacheTime:     3600,
	})
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
			ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()),
			Title:               "请输入关键词",
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

	startKeyword := keyWord
	if requestedPlatform != "" {
		startKeyword = startKeyword + " " + requestedPlatform
	}
	if strings.TrimSpace(qualityOverride) != "" {
		startKeyword = startKeyword + " " + strings.TrimSpace(qualityOverride)
	}
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
	searchWithFallback := func(keyword string) ([]platform.Track, string, error) {
		activePlatform := platformName
		activeTracks, searchErr := plat.Search(ctx, keyword, h.inlineSearchLimit(activePlatform))
		if (searchErr != nil || len(activeTracks) == 0) && fallbackPlatform != "" && fallbackPlatform != activePlatform {
			fallbackPlat := h.PlatformManager.Get(fallbackPlatform)
			if fallbackPlat != nil && fallbackPlat.SupportsSearch() {
				fallbackTracks, fallbackErr := fallbackPlat.Search(ctx, keyword, h.inlineSearchLimit(fallbackPlatform))
				if fallbackErr == nil && len(fallbackTracks) > 0 {
					activePlatform = fallbackPlatform
					activeTracks = fallbackTracks
					searchErr = nil
				}
			}
		}
		return activeTracks, activePlatform, searchErr
	}

	tracks, matchedPlatform, err := searchWithFallback(keyWord)
	platformName = matchedPlatform

	if err != nil || len(tracks) == 0 {
		inlineMsg := &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()),
			Title:               noResults,
			Description:         noResults,
			InputMessageContent: &telego.InputTextMessageContent{MessageText: noResults},
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
	inlineMsgs = append(inlineMsgs, buildInlineSearchHeader(h, platformName, qualityValue))
	for i := start; i < end; i++ {
		track := tracks[i]
		inlineMsg := buildInlineTrackArticle(ctx, h, platformName, track, qualityValue, query.From.ID)
		inlineMsgs = append(inlineMsgs, inlineMsg)
	}
	inlineMsgs = append(inlineMsgs, buildInlineSearchPageFooter(keyWord, requestedPlatform, qualityOverride, page, pageCount, len(tracks)))
	params.Results = inlineMsgs
	_ = b.AnswerInlineQuery(ctx, params)
}

func (h *InlineSearchHandler) inlinePageSize() int {
	if h == nil || h.PageSize <= 0 {
		return 8
	}
	if h.PageSize > 48 {
		return 48
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
	inlineMsgs := make([]telego.InlineQueryResult, 0, 2)
	inlineMsgs = append(inlineMsgs, buildInlineSearchHeader(h, platformName, qualityValue))

	title := trackID
	artists := ""
	album := ""
	thumbnailSource := ""
	if h.PlatformManager != nil {
		if plat := h.PlatformManager.Get(platformName); plat != nil {
			if track, err := plat.GetTrack(ctx, trackID); err == nil && track != nil {
				title = strings.TrimSpace(track.Title)
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
		Description:         inlineSubtitle(album, artists),
		InputMessageContent: &telego.InputTextMessageContent{MessageText: waitForDown},
		ReplyMarkup:         buildInlineSendKeyboard(platformName, trackID, qualityValue, query.From.ID),
		ThumbnailURL:        thumbnailURL,
		ThumbnailWidth:      150,
		ThumbnailHeight:     150,
	}
	inlineMsgs = append(inlineMsgs, inlineMsg)
	params := &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       inlineMsgs,
		CacheTime:     60,
	}
	_ = b.AnswerInlineQuery(ctx, params)
}

func buildInlineSearchHeader(h *InlineSearchHandler, platformName, qualityValue string) telego.InlineQueryResult {
	platformText := platformDisplayName(h.PlatformManager, platformName)
	if strings.TrimSpace(platformText) == "" {
		platformText = platformName
	}
	if strings.TrimSpace(qualityValue) == "" {
		qualityValue = "hires"
	}
	qualityText := qualityDisplayName(qualityValue)
	replyMarkup := (*telego.InlineKeyboardMarkup)(nil)
	botName := strings.TrimPrefix(strings.TrimSpace(h.BotName), "@")
	if botName != "" {
		replyMarkup = &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{
			{Text: "⚙️ 修改设置", URL: fmt.Sprintf("https://t.me/%s?start=settings", botName)},
		}}}
	}
	return &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  fmt.Sprintf("meta_%d", time.Now().UnixMicro()),
		Title:               fmt.Sprintf("平台：%s | 音质：%s", platformText, qualityText),
		Description:         "点击修改设置",
		InputMessageContent: &telego.InputTextMessageContent{MessageText: fmt.Sprintf("当前用户设置\n平台：%s\n音质：%s\n\n💡 可在关键词后临时追加参数，例如：稻香 qq high", platformText, qualityText)},
		ReplyMarkup:         replyMarkup,
	}
}

func buildInlineSearchPageFooter(keyword, platformName, qualityValue string, page, pageCount, total int) telego.InlineQueryResult {
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
		hintQuery = "关键词 qq hires 2"
	}
	title := fmt.Sprintf("第 %d 页 / 共 %d 页", page, pageCount)
	desc := fmt.Sprintf("共 %d 条结果；在关键词末尾加数字翻页，如：%s", total, hintQuery)
	return &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  fmt.Sprintf("page_%d_%d_%d", page, pageCount, time.Now().UnixMicro()),
		Title:               title,
		Description:         desc,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: desc},
	}
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
	if candidate > 9 {
		return baseText, platformName, quality, page, invalidPageFallbackKeyword
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
		Description:         inlineSubtitle(trackAlbumLabel(track.Album), inlineArtistsLabel(track.Artists)),
		InputMessageContent: &telego.InputTextMessageContent{MessageText: waitForDown},
		ReplyMarkup:         buildInlineSendKeyboard(platformName, track.ID, qualityValue, requesterID),
		ThumbnailURL:        thumbnailURL,
		ThumbnailWidth:      150,
		ThumbnailHeight:     150,
	}
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
		re := regexp.MustCompile(`T002R\d+x\d+M000`)
		if re.MatchString(rawURL) {
			return re.ReplaceAllString(rawURL, fmt.Sprintf("T002R%dx%dM000", size, size))
		}
		// QQ 搜索结果常见格式: T002M000{mid}.jpg
		reMid := regexp.MustCompile(`T002M000([A-Za-z0-9]+)\.jpg`)
		if matches := reMid.FindStringSubmatch(rawURL); len(matches) == 2 {
			return strings.Replace(rawURL, matches[0], fmt.Sprintf("T002R%dx%dM000%s.jpg", size, size, matches[1]), 1)
		}
		// QQ 单曲封面格式: T062M000{mid}.jpg -> T062R{size}x{size}M000{mid}.jpg
		reSong := regexp.MustCompile(`T062M000([A-Za-z0-9]+)\.jpg`)
		if matches := reSong.FindStringSubmatch(rawURL); len(matches) == 2 {
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

func inlineSubtitle(album, artists string) string {
	album = strings.TrimSpace(album)
	artists = strings.TrimSpace(artists)
	if artists == "" {
		artists = "未知歌手"
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

func qualityDisplayName(quality string) string {
	switch strings.TrimSpace(strings.ToLower(quality)) {
	case "standard":
		return "标准"
	case "high":
		return "高品质"
	case "lossless":
		return "无损"
	case "hires":
		return "Hi-Res"
	default:
		return quality
	}
}

func (h *InlineSearchHandler) inlineCachedOrCommand(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, platformName, trackID, qualityOverride string) bool {
	if strings.TrimSpace(platformName) == "" || strings.TrimSpace(trackID) == "" {
		return false
	}
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	if strings.TrimSpace(qualityOverride) != "" {
		qualityValue = strings.TrimSpace(qualityOverride)
	}
	if info := h.findCachedSong(ctx, platformName, trackID, qualityValue); info != nil {
		h.inlineCached(ctx, b, query, info, platformName, qualityValue)
		return true
	}
	h.inlineCommand(ctx, b, query, platformName, trackID, qualityOverride)
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
	keyboard := buildForwardKeyboard(songInfo.TrackURL, platformName, trackID)

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
			return info
		}
	}
	if platformName == "netease" {
		if id, err := strconv.Atoi(trackID); err == nil {
			info, err := h.Repo.FindByMusicID(ctx, id)
			if err == nil && info != nil && info.FileID != "" && info.SongName != "" {
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
