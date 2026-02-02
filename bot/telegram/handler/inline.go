package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
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

func (h *InlineSearchHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
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
			platformName, trackID, matched := h.PlatformManager.MatchText(query.Query)
			if matched {
				if h.inlineCachedOrCommand(ctx, b, query, platformName, trackID) {
					return
				}
				return
			}
			platformName, trackID, matched = h.PlatformManager.MatchURL(query.Query)
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

func (h *InlineSearchHandler) inlineMusic(ctx context.Context, b *bot.Bot, query *models.InlineQuery, musicID int) {
	if h.Repo == nil {
		h.inlineEmpty(ctx, b, query)
		return
	}
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	info := h.findCachedSong(ctx, "netease", fmt.Sprintf("%d", musicID), qualityValue)
	if info != nil {
		h.inlineCached(ctx, b, query, info, "netease")
		return
	}

	inlineMsg := &models.InlineQueryResultArticle{
		ID:                  query.ID,
		Title:               noCache,
		Description:         tapToDownload,
		InputMessageContent: &models.InputTextMessageContent{MessageText: query.Query},
	}
	_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       []models.InlineQueryResult{inlineMsg},
		CacheTime:     60,
		Button:        &models.InlineQueryResultsButton{Text: tapMeToDown, StartParameter: fmt.Sprintf("%d", musicID)},
	})
}

func (h *InlineSearchHandler) inlineEmpty(ctx context.Context, b *bot.Bot, query *models.InlineQuery) {
	inlineMsg := &models.InlineQueryResultArticle{
		ID:                  query.ID,
		Title:               "输入 help 获取帮助",
		Description:         "MusicBot-Go",
		InputMessageContent: &models.InputTextMessageContent{MessageText: "MusicBot-Go"},
	}
	_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       []models.InlineQueryResult{inlineMsg},
		CacheTime:     3600,
	})
}

func (h *InlineSearchHandler) inlineHelp(ctx context.Context, b *bot.Bot, query *models.InlineQuery) {
	randomID := time.Now().UnixMicro()
	inlineMsg1 := &models.InlineQueryResultArticle{
		ID:                  fmt.Sprintf("%d", randomID),
		Title:               "1.粘贴音乐分享URL或输入MusicID",
		Description:         "MusicBot-Go",
		InputMessageContent: &models.InputTextMessageContent{MessageText: "MusicBot-Go"},
	}
	inlineMsg2 := &models.InlineQueryResultArticle{
		ID:                  fmt.Sprintf("%d", randomID+1),
		Title:               "2.输入 search+关键词 搜索歌曲",
		Description:         "MusicBot-Go",
		InputMessageContent: &models.InputTextMessageContent{MessageText: "MusicBot-Go"},
	}
	_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       []models.InlineQueryResult{inlineMsg1, inlineMsg2},
		CacheTime:     3600,
	})
}

func (h *InlineSearchHandler) inlineSearch(ctx context.Context, b *bot.Bot, query *models.InlineQuery) {
	keyWord := strings.Replace(query.Query, "search", "", 1)
	keyWord = strings.TrimSpace(keyWord)
	if keyWord == "" {
		inlineMsg := &models.InlineQueryResultArticle{
			ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()),
			Title:               "请输入关键词",
			Description:         "MusicBot-Go",
			InputMessageContent: &models.InputTextMessageContent{MessageText: "MusicBot-Go"},
		}
		_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{
			InlineQueryID: query.ID,
			IsPersonal:    false,
			Results:       []models.InlineQueryResult{inlineMsg},
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
				plat = fallbackPlat
				tracks = fallbackTracks
				err = nil
			}
		}
	}
	if err != nil || len(tracks) == 0 {
		inlineMsg := &models.InlineQueryResultArticle{
			ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()),
			Title:               noResults,
			Description:         noResults,
			InputMessageContent: &models.InputTextMessageContent{MessageText: noResults},
		}
		_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{
			InlineQueryID: query.ID,
			IsPersonal:    false,
			Results:       []models.InlineQueryResult{inlineMsg},
			CacheTime:     3600,
		})
		return
	}

	var inlineMsgs []models.InlineQueryResult
	for i := 0; i < len(tracks) && i < 10; i++ {
		track := tracks[i]
		var artistNames []string
		for _, artist := range track.Artists {
			artistNames = append(artistNames, artist.Name)
		}
		artistsStr := strings.Join(artistNames, "/")

		inlineMsg := &models.InlineQueryResultArticle{
			ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()+int64(i)),
			Title:               track.Title,
			Description:         artistsStr,
			InputMessageContent: &models.InputTextMessageContent{MessageText: fmt.Sprintf("/%s %s %s", platformName, track.ID, qualityValue)},
		}
		inlineMsgs = append(inlineMsgs, inlineMsg)
	}
	_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       inlineMsgs,
		CacheTime:     3600,
	})
}

func (h *InlineSearchHandler) inlineCommand(ctx context.Context, b *bot.Bot, query *models.InlineQuery, platformName, trackID string) {
	if strings.TrimSpace(platformName) == "" || strings.TrimSpace(trackID) == "" {
		h.inlineEmpty(ctx, b, query)
		return
	}
	qualityValue := h.resolveDefaultQuality(ctx, query.From.ID)
	inlineMsg := &models.InlineQueryResultArticle{
		ID:                  fmt.Sprintf("%d", time.Now().UnixMicro()),
		Title:               fmt.Sprintf("%s %s", platformEmoji(platformName), platformDisplayName(platformName)),
		Description:         tapToDownload,
		InputMessageContent: &models.InputTextMessageContent{MessageText: fmt.Sprintf("/%s %s %s", platformName, trackID, qualityValue)},
	}
	_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		IsPersonal:    false,
		Results:       []models.InlineQueryResult{inlineMsg},
		CacheTime:     60,
	})
}

func (h *InlineSearchHandler) inlineCachedOrCommand(ctx context.Context, b *bot.Bot, query *models.InlineQuery, platformName, trackID string) bool {
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

func (h *InlineSearchHandler) inlineCached(ctx context.Context, b *bot.Bot, query *models.InlineQuery, info *botpkg.SongInfo, platformFallback string) {
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
	commandQuery := ""
	if trackID != "" {
		commandQuery = fmt.Sprintf("/%s %s %s", platformName, trackID, qualityValue)
	}

	var rows [][]models.InlineKeyboardButton
	linkURL := strings.TrimSpace(info.TrackURL)
	if linkURL == "" && platformName == "netease" && info.MusicID != 0 {
		linkURL = fmt.Sprintf("https://music.163.com/song?id=%d", info.MusicID)
	}
	if linkURL != "" {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: fmt.Sprintf("%s- %s", info.SongName, info.SongArtists), URL: linkURL},
		})
	}
	if commandQuery != "" {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: sendMeTo, SwitchInlineQuery: commandQuery},
		})
	}
	var keyboard *models.InlineKeyboardMarkup
	if len(rows) > 0 {
		keyboard = &models.InlineKeyboardMarkup{InlineKeyboard: rows}
	}

	newAudio := &models.InlineQueryResultCachedDocument{
		ID:             query.ID,
		DocumentFileID: info.FileID,
		Title:          fmt.Sprintf("%s - %s", info.SongArtists, info.SongName),
		Caption:        fmt.Sprintf(musicInfo, info.SongName, info.SongArtists, info.SongAlbum, platformTag(platformName), info.FileExt, float64(info.MusicSize+info.EmbPicSize)/1024/1024, float64(info.BitRate)/1000, h.BotName),
		ReplyMarkup:    keyboard,
		Description:    info.SongAlbum,
	}

	_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{
		InlineQueryID: query.ID,
		Results:       []models.InlineQueryResult{newAudio},
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
