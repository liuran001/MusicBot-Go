package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// InlineSearchHandler handles inline queries.
type InlineSearchHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	BotName         string
	DefaultPlatform string
	DefaultQuality  string
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
		musicID := parseMusicID(query.Query)
		if musicID != 0 {
			h.inlineMusic(ctx, b, query, musicID)
		} else {
			h.inlineEmpty(ctx, b, query)
		}
	}
}

func (h *InlineSearchHandler) inlineMusic(ctx context.Context, b *bot.Bot, query *models.InlineQuery, musicID int) {
	if h.Repo == nil {
		h.inlineEmpty(ctx, b, query)
		return
	}
	info, err := h.Repo.FindByMusicID(ctx, musicID)
	if err == nil && info != nil && info.FileID != "" && info.SongName != "" {
		platformName := info.Platform
		if strings.TrimSpace(platformName) == "" {
			platformName = h.DefaultPlatform
		}
		keyboard := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: fmt.Sprintf("%s- %s", info.SongName, info.SongArtists), URL: fmt.Sprintf("https://music.163.com/song?id=%d", info.MusicID)}},
			{{Text: sendMeTo, SwitchInlineQuery: fmt.Sprintf("https://music.163.com/song?id=%d", info.MusicID)}},
		}}

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
