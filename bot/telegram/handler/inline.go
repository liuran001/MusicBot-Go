package handler

import (
	"context"
	"fmt"
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
	BotName          string
	DefaultPlatform  string
	DefaultQuality   string
	FallbackPlatform string
}

func (h *InlineSearchHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.InlineQuery == nil {
		return
	}
	query := update.InlineQuery

	switch {
	case query.Query == "help":
		h.inlineHelp(ctx, b, query)
	case strings.Contains(query.Query, "search"):
		h.inlineSearch(ctx, b, query)
	default:
		if h.PlatformManager != nil {
			resolvedQuery := resolveShortLinkText(ctx, h.PlatformManager, query.Query)
			if _, _, matched := matchPlaylistURL(ctx, h.PlatformManager, resolvedQuery); matched {
				h.inlineEmpty(ctx, b, query)
				return
			}
			platformName, trackID, matched := h.PlatformManager.MatchText(resolvedQuery)
			if matched {
				if h.inlineCachedOrCommand(ctx, b, query, platformName, trackID) {
					return
				}
				return
			}
			platformName, trackID, matched = h.PlatformManager.MatchURL(resolvedQuery)
			if matched {
				if h.inlineCachedOrCommand(ctx, b, query, platformName, trackID) {
					return
				}
				return
			}
		}
		h.inlineEmpty(ctx, b, query)
	}
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
		Title:               "1.粘贴音乐分享URL或输入MusicID",
		Description:         "MusicBot-Go",
		InputMessageContent: &telego.InputTextMessageContent{MessageText: "MusicBot-Go"},
	}
	inlineMsg2 := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  fmt.Sprintf("%d", randomID+1),
		Title:               "2.输入 search+关键词 搜索歌曲",
		Description:         "MusicBot-Go",
		InputMessageContent: &telego.InputTextMessageContent{MessageText: "MusicBot-Go"},
	}
	_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       []telego.InlineQueryResult{inlineMsg1, inlineMsg2},
		CacheTime:     3600,
	})
}

func (h *InlineSearchHandler) inlineSearch(ctx context.Context, b *telego.Bot, query *telego.InlineQuery) {
	keyWord := strings.Replace(query.Query, "search", "", 1)
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

	platformName := h.DefaultPlatform
	qualityValue := h.DefaultQuality
	if strings.TrimSpace(platformName) == "" {
		platformName = "netease"
	}
	if strings.TrimSpace(qualityValue) == "" {
		qualityValue = "hires"
	}
	fallbackPlatform := h.FallbackPlatform
	if strings.TrimSpace(fallbackPlatform) == "" {
		fallbackPlatform = "netease"
	}
	keyWord, requestedPlatform, hasPlatformSuffix := parseSearchKeywordPlatform(keyWord, h.PlatformManager)
	if hasPlatformSuffix {
		platformName = requestedPlatform
		fallbackPlatform = ""
	}
	if h.Repo != nil {
		if settings, err := h.Repo.GetUserSettings(ctx, query.From.ID); err == nil && settings != nil {
			platformName = settings.DefaultPlatform
			if strings.TrimSpace(settings.DefaultQuality) != "" {
				qualityValue = settings.DefaultQuality
			}
		}
	}
	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		return
	}

	tracks, err := plat.Search(ctx, keyWord, 10)
	if (err != nil || len(tracks) == 0) && fallbackPlatform != "" && fallbackPlatform != platformName {
		fallbackPlat := h.PlatformManager.Get(fallbackPlatform)
		if fallbackPlat != nil && fallbackPlat.SupportsSearch() {
			fallbackTracks, fallbackErr := fallbackPlat.Search(ctx, keyWord, 10)
			if fallbackErr == nil && len(fallbackTracks) > 0 {
				platformName = fallbackPlatform
				tracks = fallbackTracks
				err = nil
			}
		}
	}
	if err != nil || len(tracks) == 0 {
		inlineMsg := &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()),
			Title:               noResults,
			Description:         noResults,
			InputMessageContent: &telego.InputTextMessageContent{MessageText: noResults},
		}
		_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
			InlineQueryID: query.ID,
			IsPersonal:    false,
			Results:       []telego.InlineQueryResult{inlineMsg},
			CacheTime:     3600,
		})
		return
	}

	var inlineMsgs []telego.InlineQueryResult
	for i := 0; i < len(tracks) && i < 10; i++ {
		track := tracks[i]
		var artistNames []string
		for _, artist := range track.Artists {
			artistNames = append(artistNames, artist.Name)
		}
		artistsStr := strings.Join(artistNames, "/")

		inlineMsg := &telego.InlineQueryResultArticle{
			Type:                telego.ResultTypeArticle,
			ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()+int64(i)),
			Title:               track.Title,
			Description:         artistsStr,
			InputMessageContent: &telego.InputTextMessageContent{MessageText: fmt.Sprintf("/%s %s %s", platformName, track.ID, qualityValue)},
		}
		inlineMsgs = append(inlineMsgs, inlineMsg)
	}
	_ = b.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       inlineMsgs,
		CacheTime:     3600,
	})
}

func (h *InlineSearchHandler) inlineCommand(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, platformName, trackID string) {
	if strings.TrimSpace(platformName) == "" || strings.TrimSpace(trackID) == "" {
		h.inlineEmpty(ctx, b, query)
		return
	}
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	messageText := strings.TrimSpace(query.Query)
	if messageText == "" {
		messageText = buildInlineMusicCommand(platformName, trackID, qualityValue)
	}
	inlineMsg := &telego.InlineQueryResultArticle{
		Type:                telego.ResultTypeArticle,
		ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()),
		Title:               fmt.Sprintf("%s %s", platformEmoji(h.PlatformManager, platformName), platformDisplayName(h.PlatformManager, platformName)),
		Description:         tapToDownload,
		InputMessageContent: &telego.InputTextMessageContent{MessageText: messageText},
	}
	params := &telego.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       []telego.InlineQueryResult{inlineMsg},
		CacheTime:     60,
	}
	if startParam := buildInlineStartParameter(platformName, trackID, qualityValue); startParam != "" {
		params.Button = &telego.InlineQueryResultsButton{
			Text:           tapToDownload,
			StartParameter: startParam,
		}
	}
	_ = b.AnswerInlineQuery(ctx, params)
}

func (h *InlineSearchHandler) inlineCachedOrCommand(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, platformName, trackID string) bool {
	if strings.TrimSpace(platformName) == "" || strings.TrimSpace(trackID) == "" {
		return false
	}
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	if info := h.findCachedSong(ctx, platformName, trackID, qualityValue); info != nil {
		h.inlineCached(ctx, b, query, info, platformName)
		return true
	}
	h.inlineCommand(ctx, b, query, platformName, trackID)
	return true
}

func (h *InlineSearchHandler) inlineCached(ctx context.Context, b *telego.Bot, query *telego.InlineQuery, info *botpkg.SongInfo, platformFallback string) {
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
		ID:             query.ID,
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
