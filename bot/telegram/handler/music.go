package handler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// MusicHandler handles /music and related commands.
type MusicHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager // NEW: Platform-agnostic music platform manager
	DownloadService *download.DownloadService
	ID3Service      *id3.ID3Service
	TagProviders    map[string]id3.ID3TagProvider
	Pool            botpkg.WorkerPool
	Logger          botpkg.Logger
	CacheDir        string
	BotName         string
	Limiter         chan struct{}
}

// Handle processes music download and send flow.
func (h *MusicHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}
	message := update.Message

	platformName, trackID, found := extractPlatformTrack(message, h.PlatformManager)
	if !found {
		return
	}

	_ = h.processMusic(ctx, b, message, platformName, trackID)
}

func (h *MusicHandler) processMusic(ctx context.Context, b *bot.Bot, message *models.Message, platformName, trackID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if h.CacheDir == "" {
		h.CacheDir = "./cache"
	}
	ensureDir(h.CacheDir)
	threadID := 0
	if message != nil {
		threadID = message.MessageThreadID
	}
	replyParams := buildReplyParams(message)

	if h.Limiter == nil {
		h.Limiter = make(chan struct{}, 4)
	}

	var songInfo botpkg.SongInfo
	var msgResult *models.Message

	sendFailed := func(err error) {
		var errText string
		if strings.Contains(fmt.Sprintf("%v", err), md5VerFailed) || strings.Contains(fmt.Sprintf("%v", err), downloadTimeout) {
			errText = "%v"
		} else {
			errText = uploadFailed
		}
		text := fmt.Sprintf(musicInfoMsg+errText, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024, strings.ReplaceAll(err.Error(), "BOT_TOKEN", "BOT_TOKEN"))
		if msgResult != nil {
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    msgResult.Chat.ID,
				MessageID: msgResult.ID,
				Text:      text,
			})
		}
	}

	var userID int64
	if message != nil && message.From != nil {
		userID = message.From.ID
	}

	quality := platform.QualityHigh
	if h.Repo != nil && userID != 0 {
		if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
			if q, err := platform.ParseQuality(settings.DefaultQuality); err == nil {
				quality = q
			}
		}
	}

	qualityStr := quality.String()

	if h.Repo != nil {
		cached, err := h.Repo.FindByPlatformTrackID(ctx, platformName, trackID, qualityStr)
		if err == nil && cached != nil {
			songInfo = *cached

			msgResult, _ = sendStatusMessage(ctx, b, message.Chat.ID, threadID, replyParams, fmt.Sprintf(musicInfoMsg+hitCache, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024))

			if _, err = h.sendMusic(ctx, b, message, &songInfo, "", ""); err != nil {
				sendFailed(err)
				return err
			}
			if msgResult != nil {
				_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID})
			}
			return nil
		}
	}

	msgResult, _ = sendStatusMessage(ctx, b, message.Chat.ID, threadID, replyParams, waitForDown)

	h.Limiter <- struct{}{}
	defer func() { <-h.Limiter }()

	if h.Repo != nil {
		cached, err := h.Repo.FindByPlatformTrackID(ctx, platformName, trackID, qualityStr)
		if err == nil && cached != nil {
			songInfo = *cached
			if msgResult != nil {
				_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
					ChatID:    msgResult.Chat.ID,
					MessageID: msgResult.ID,
					Text:      fmt.Sprintf(musicInfoMsg+hitCache, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024),
				})
			}
			if _, err = h.sendMusic(ctx, b, message, &songInfo, "", ""); err != nil {
				sendFailed(err)
				return err
			}
			if msgResult != nil {
				_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID})
			}
			return nil
		}
	}

	if msgResult != nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      fetchInfo,
		})
	}

	if h.PlatformManager == nil {
		return errors.New("platform manager not configured")
	}

	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		if h.Logger != nil {
			h.Logger.Error("platform not found", "platform", platformName)
		}
		if msgResult != nil {
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    msgResult.Chat.ID,
				MessageID: msgResult.ID,
				Text:      fetchInfoFailed,
			})
		}
		return fmt.Errorf("platform not found: %s", platformName)
	}

	track, err := plat.GetTrack(ctx, trackID)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Error("failed to get track", "platform", platformName, "trackID", trackID, "error", err)
		}
		if msgResult != nil {
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    msgResult.Chat.ID,
				MessageID: msgResult.ID,
				Text:      fetchInfoFailed,
			})
		}
		return err
	}

	fillSongInfoFromTrack(&songInfo, track, platformName, trackID, message)
	info, err := plat.GetDownloadInfo(ctx, trackID, quality)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Error("failed to get download info", "platform", platformName, "trackID", trackID, "error", err)
		}
		if msgResult != nil {
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    msgResult.Chat.ID,
				MessageID: msgResult.ID,
				Text:      fetchInfoFailed,
			})
		}
		return err
	}
	if info == nil || info.URL == "" {
		if msgResult != nil {
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    msgResult.Chat.ID,
				MessageID: msgResult.ID,
				Text:      fetchInfoFailed,
			})
		}
		return errors.New("download info unavailable")
	}
	if info.Format == "" {
		info.Format = "mp3"
	}

	actualQuality := info.Quality.String()
	if actualQuality == "unknown" || actualQuality == "" {
		actualQuality = qualityStr
	}
	if songInfo.Quality == "" {
		songInfo.Quality = actualQuality
	}
	songInfo.FileExt = info.Format
	songInfo.MusicSize = int(info.Size)
	songInfo.BitRate = info.Bitrate * 1000

	if h.Repo != nil && actualQuality != qualityStr {
		cached, err := h.Repo.FindByPlatformTrackID(ctx, platformName, trackID, actualQuality)
		if err == nil && cached != nil {
			songInfo = *cached
			msgResult, _ = sendStatusMessage(ctx, b, message.Chat.ID, threadID, replyParams, fmt.Sprintf(musicInfoMsg+hitCache, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024))
			if _, err = h.sendMusic(ctx, b, message, &songInfo, "", ""); err != nil {
				sendFailed(err)
				return err
			}
			if msgResult != nil {
				_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID})
			}
			return nil
		}
	}

	if msgResult != nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      fmt.Sprintf(musicInfoMsg+downloading, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024),
		})
	}

	musicPath, picPath, cleanupList, err := h.downloadAndPrepareFromPlatform(ctx, plat, track, trackID, info, msgResult, b, message, &songInfo)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Error("failed to download and prepare", "platform", platformName, "trackID", trackID, "error", err)
		}
		cleanupFiles(append(cleanupList, musicPath, picPath)...)
		sendFailed(err)
		return err
	}
	defer cleanupFiles(append(cleanupList, musicPath, picPath)...)

	if msgResult != nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msgResult.Chat.ID,
			MessageID: msgResult.ID,
			Text:      fmt.Sprintf(musicInfoMsg+uploading, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024),
		})
	}

	audio, err := h.sendMusic(ctx, b, message, &songInfo, musicPath, picPath)
	if err != nil {
		sendFailed(err)
		return err
	}

	if audio.Audio != nil {
		songInfo.FileID = audio.Audio.FileID
		if audio.Audio.Thumbnail != nil {
			songInfo.ThumbFileID = audio.Audio.Thumbnail.FileID
		}
	}

	if h.Repo != nil {
		if err := h.Repo.Create(ctx, &songInfo); err != nil {
			if h.Logger != nil {
				h.Logger.Error("failed to save song info", "platform", platformName, "trackID", trackID, "error", err)
			}
		}
	}

	if msgResult != nil {
		_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msgResult.Chat.ID, MessageID: msgResult.ID})
	}

	return nil
}

func (h *MusicHandler) downloadAndPrepareFromPlatform(ctx context.Context, plat platform.Platform, track *platform.Track, trackID string, info *platform.DownloadInfo, msg *models.Message, b *bot.Bot, message *models.Message, songInfo *botpkg.SongInfo) (string, string, []string, error) {
	cleanupList := make([]string, 0, 4)
	if h.DownloadService == nil {
		return "", "", cleanupList, errors.New("download service not configured")
	}
	if info == nil || info.URL == "" {
		return "", "", cleanupList, errors.New("download info unavailable")
	}

	if info.Format == "" {
		info.Format = "mp3"
	}

	songInfo.FileExt = info.Format
	songInfo.MusicSize = int(info.Size)
	songInfo.BitRate = info.Bitrate * 1000
	if songInfo.Quality == "" {
		songInfo.Quality = info.Quality.String()
	}

	stamp := time.Now().UnixMicro()
	musicFileName := fmt.Sprintf("%d-%s.%s", stamp, sanitizeFileName(track.Title), info.Format)
	filePath := filepath.Join(h.CacheDir, musicFileName)

	progress := func(written, total int64) {
		if msg == nil {
			return
		}
		totalMB := float64(total) / 1024 / 1024
		writtenMB := float64(written) / 1024 / 1024
		progressPct := 0.0
		if total > 0 {
			progressPct = float64(written) * 100 / float64(total)
		}
		text := fmt.Sprintf("正在下载：%s\n进度：%.2f%% (%.2f MB / %.2f MB)", track.Title, progressPct, writtenMB, totalMB)
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			Text:      text,
		})
	}

	if _, err := h.DownloadService.Download(ctx, info, filePath, progress); err != nil {
		_ = os.Remove(filePath)
		return "", "", cleanupList, err
	}

	picPath, resizePicPath := "", ""
	coverURL := ""
	if track.CoverURL != "" {
		coverURL = track.CoverURL
	} else if track.Album != nil && track.Album.CoverURL != "" {
		coverURL = track.Album.CoverURL
	}
	if coverURL != "" {
		picPath = filepath.Join(h.CacheDir, fmt.Sprintf("%d-%s", stamp, path.Base(coverURL)))
		if _, err := h.DownloadService.Download(ctx, &platform.DownloadInfo{URL: coverURL}, picPath, nil); err == nil {
			if stat, statErr := os.Stat(picPath); statErr == nil {
				songInfo.PicSize = int(stat.Size())
				cleanupList = append(cleanupList, picPath)
				if resized, err := resizeImg(picPath); err == nil {
					resizePicPath = resized
					cleanupList = append(cleanupList, resizePicPath)
				}
			}
		} else {
			picPath = ""
		}
	}

	embedPicPath := picPath
	thumbPicPath := picPath
	if picPath != "" {
		if stat, err := os.Stat(picPath); err == nil {
			if stat.Size() > 2*1024*1024 && resizePicPath != "" {
				embedPicPath = resizePicPath
				if embStat, err := os.Stat(resizePicPath); err == nil {
					songInfo.EmbPicSize = int(embStat.Size())
				}
			} else {
				songInfo.EmbPicSize = int(stat.Size())
			}
			if stat.Size() > 200*1024 && resizePicPath != "" {
				thumbPicPath = resizePicPath
			}
		}
	}

	finalDir := filepath.Join(h.CacheDir, fmt.Sprintf("%d", stamp))
	_ = os.Mkdir(finalDir, os.ModePerm)
	fileName := sanitizeFileName(fmt.Sprintf("%v - %v.%v", strings.ReplaceAll(songInfo.SongArtists, "/", ","), songInfo.SongName, songInfo.FileExt))
	finalPath := filepath.Join(finalDir, fileName)
	if err := os.Rename(filePath, finalPath); err == nil {
		filePath = finalPath
	}
	cleanupList = append(cleanupList, filePath, finalDir)

	if h.ID3Service != nil && h.TagProviders != nil {
		if provider, ok := h.TagProviders[plat.Name()]; ok && provider != nil {
			tagData, tagErr := provider.GetTagData(ctx, track, info)
			if tagErr != nil {
				if h.Logger != nil {
					h.Logger.Error("failed to get tag data", "platform", plat.Name(), "trackID", trackID, "error", tagErr)
				}
			} else {
				if err := h.ID3Service.EmbedTags(filePath, tagData, embedPicPath); err != nil {
					if h.Logger != nil {
						h.Logger.Error("failed to embed tags", "platform", plat.Name(), "trackID", trackID, "error", err)
					}
				}
			}
		}
	}

	return filePath, thumbPicPath, cleanupList, nil
}

func (h *MusicHandler) sendMusic(ctx context.Context, b *bot.Bot, message *models.Message, songInfo *botpkg.SongInfo, musicPath, picPath string) (*models.Message, error) {
	if songInfo == nil {
		return nil, errors.New("song info required")
	}
	threadID := 0
	if message != nil {
		threadID = message.MessageThreadID
	}

	var audioFile models.InputFile
	if songInfo.FileID != "" {
		audioFile = &models.InputFileString{Data: songInfo.FileID}
	} else {
		file, err := os.Open(musicPath)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		audioFile = &models.InputFileUpload{Filename: filepath.Base(musicPath), Data: file}
		_, _ = b.SendChatAction(ctx, &bot.SendChatActionParams{ChatID: message.Chat.ID, MessageThreadID: threadID, Action: models.ChatActionUploadDocument})
	}

	caption := buildMusicCaption(songInfo, h.BotName)
	params := &bot.SendAudioParams{
		ChatID:          message.Chat.ID,
		MessageThreadID: threadID,
		Audio:           audioFile,
		Caption:         caption,
		ParseMode:       models.ParseModeHTML,
		Title:           songInfo.SongName,
		Performer:       songInfo.SongArtists,
		Duration:        songInfo.Duration,
		ReplyParameters: buildReplyParams(message),
	}

	if songInfo.ThumbFileID != "" {
		params.Thumbnail = &models.InputFileString{Data: songInfo.ThumbFileID}
	} else if picPath != "" {
		if _, err := os.Stat(picPath); err == nil {
			pic, err := os.Open(picPath)
			if err == nil {
				defer pic.Close()
				params.Thumbnail = &models.InputFileUpload{Filename: filepath.Base(picPath), Data: pic}
			}
		}
	}

	audio, err := b.SendAudio(ctx, params)
	if err != nil && strings.Contains(fmt.Sprintf("%v", err), "replied message not found") {
		params.ReplyParameters = nil
		audio, err = b.SendAudio(ctx, params)
	}
	return audio, err
}

func buildReplyParams(message *models.Message) *models.ReplyParameters {
	if message == nil {
		return nil
	}
	if message.Chat.Type != "private" {
		return nil
	}
	return &models.ReplyParameters{MessageID: message.ID}
}

func sendStatusMessage(ctx context.Context, b *bot.Bot, chatID int64, threadID int, replyParams *models.ReplyParameters, text string) (*models.Message, error) {
	params := &bot.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Text:            text,
		ReplyParameters: replyParams,
	}
	msg, err := b.SendMessage(ctx, params)
	if err != nil && replyParams != nil && strings.Contains(fmt.Sprintf("%v", err), "replied message not found") {
		params.ReplyParameters = nil
		msg, err = b.SendMessage(ctx, params)
	}
	return msg, err
}
