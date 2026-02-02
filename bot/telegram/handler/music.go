package handler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
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
	UploadLimiter   chan struct{}
	UploadQueue     chan uploadTask
	UploadQueueSize int
	UploadBot       *bot.Bot
	RateLimiter     *telegram.RateLimiter
	initOnce        sync.Once
	queueMu         sync.Mutex
	queuedStatus    []queuedStatus
	statusDirty     bool
}

type uploadTask struct {
	ctx       context.Context
	cancel    context.CancelFunc
	b         *bot.Bot
	statusBot *bot.Bot
	statusMsg *models.Message
	message   *models.Message
	songInfo  botpkg.SongInfo
	musicPath string
	picPath   string
	cleanup   []string
	resultCh  chan uploadResult
	onDone    func(uploadResult)
}

type queuedStatus struct {
	bot      *bot.Bot
	message  *models.Message
	songInfo botpkg.SongInfo
}

type uploadResult struct {
	message *models.Message
	err     error
}

// StartWorker initializes and starts the upload worker.
// Must be called once during app startup with a long-lived context.
func (h *MusicHandler) StartWorker(ctx context.Context) {
	if h.CacheDir == "" {
		h.CacheDir = "./cache"
	}
	ensureDir(h.CacheDir)
	if h.Limiter == nil {
		h.Limiter = make(chan struct{}, 4)
	}
	if h.UploadLimiter == nil {
		h.UploadLimiter = make(chan struct{}, 1)
	}
	if h.UploadQueueSize <= 0 {
		h.UploadQueueSize = 20
	}
	if h.UploadQueue == nil {
		h.UploadQueue = make(chan uploadTask, h.UploadQueueSize)
		go h.runUploadWorker(ctx)
	}
	go h.runStatusRefresher(ctx)
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
	qualityOverride := extractQualityOverride(message)

	h.dispatch(ctx, b, message, platformName, trackID, qualityOverride)
}

func (h *MusicHandler) dispatch(ctx context.Context, b *bot.Bot, message *models.Message, platformName, trackID string, qualityOverride string) {
	if h.Pool == nil {
		go func() {
			_ = h.processMusic(ctx, b, message, platformName, trackID, qualityOverride)
		}()
		return
	}

	go func() {
		if err := h.Pool.Submit(func() {
			defer func() {
				if err := recover(); err != nil {
					if h.Logger != nil {
						h.Logger.Error("music task panic", "platform", platformName, "trackID", trackID, "error", err)
					}
				}
			}()
			_ = h.processMusic(ctx, b, message, platformName, trackID, qualityOverride)
		}); err != nil {
			if h.Logger != nil {
				h.Logger.Error("failed to enqueue music task", "platform", platformName, "trackID", trackID, "error", err)
			}
		}
	}()
}

func (h *MusicHandler) processMusic(ctx context.Context, b *bot.Bot, message *models.Message, platformName, trackID string, qualityOverride string) error {
	threadID := 0
	if message != nil {
		threadID = message.MessageThreadID
	}
	replyParams := buildReplyParams(message)

	var songInfo botpkg.SongInfo
	var msgResult *models.Message

	// Request-level cache to avoid duplicate DB queries
	cacheMap := make(map[string]*botpkg.SongInfo)
	getCached := func(platform, trackID, quality string) (*botpkg.SongInfo, error) {
		key := platform + ":" + trackID + ":" + quality
		if cached, ok := cacheMap[key]; ok {
			return cached, nil
		}
		if h.Repo == nil {
			return nil, errors.New("repo not configured")
		}
		cached, err := h.Repo.FindByPlatformTrackID(ctx, platform, trackID, quality)
		if err == nil && cached != nil {
			cacheMap[key] = cached
		}
		return cached, err
	}

	sendFailed := func(err error) {
		var errText string
		if strings.Contains(fmt.Sprintf("%v", err), md5VerFailed) || strings.Contains(fmt.Sprintf("%v", err), downloadTimeout) {
			errText = "%v"
		} else {
			errText = uploadFailed
		}
		text := fmt.Sprintf(musicInfoMsg+errText, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024, strings.ReplaceAll(err.Error(), "BOT_TOKEN", "BOT_TOKEN"))
		if msgResult != nil {
			msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, text)
		}
	}

	var userID int64
	if message != nil && message.From != nil {
		userID = message.From.ID
	}

	quality := platform.QualityHigh
	if h.Repo != nil {
		if message != nil && message.Chat.Type != "private" {
			if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
				if q, err := platform.ParseQuality(settings.DefaultQuality); err == nil {
					quality = q
				}
			}
		} else if userID != 0 {
			if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil {
				if q, err := platform.ParseQuality(settings.DefaultQuality); err == nil {
					quality = q
				}
			}
		}
	}
	if strings.TrimSpace(qualityOverride) != "" {
		if q, err := platform.ParseQuality(qualityOverride); err == nil {
			quality = q
		}
	}

	qualityStr := quality.String()

	if h.Repo != nil {
		cached, err := getCached(platformName, trackID, qualityStr)
		if err == nil && cached != nil {
			if cached.FileID == "" {
				_ = h.Repo.DeleteByPlatformTrackID(ctx, platformName, trackID, qualityStr)
			} else {
				songInfo = *cached

				msgResult, _ = sendStatusMessage(ctx, b, h.RateLimiter, message.Chat.ID, threadID, replyParams, fmt.Sprintf(musicInfoMsg+hitCache, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024))

				if err = h.sendMusic(ctx, b, msgResult, message, &songInfo, "", "", nil, platformName, trackID); err != nil {
					sendFailed(err)
					return err
				}
				return nil
			}
		}
	}

	msgResult, _ = sendStatusMessage(ctx, b, h.RateLimiter, message.Chat.ID, threadID, replyParams, waitForDown)

	h.Limiter <- struct{}{}
	defer func() { <-h.Limiter }()

	if h.Repo != nil {
		cached, err := getCached(platformName, trackID, qualityStr)
		if err == nil && cached != nil {
			if cached.FileID == "" {
				_ = h.Repo.DeleteByPlatformTrackID(ctx, platformName, trackID, qualityStr)
			} else {
				songInfo = *cached
				if msgResult != nil {
					msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fmt.Sprintf(musicInfoMsg+hitCache, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024))
				}
				if err = h.sendMusic(ctx, b, msgResult, message, &songInfo, "", "", nil, platformName, trackID); err != nil {
					sendFailed(err)
					return err
				}
				return nil
			}
		}
	}

	if msgResult != nil {
		msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fetchInfo)
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
			msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fetchInfoFailed)
		}
		return fmt.Errorf("platform not found: %s", platformName)
	}

	track, err := plat.GetTrack(ctx, trackID)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Error("failed to get track", "platform", platformName, "trackID", trackID, "error", err)
		}
		if msgResult != nil {
			msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fetchInfoFailed)
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
			msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fetchInfoFailed)
		}
		return err
	}
	if info == nil || info.URL == "" {
		if msgResult != nil {
			msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fetchInfoFailed)
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
		cached, err := getCached(platformName, trackID, actualQuality)
		if err == nil && cached != nil {
			if cached.FileID == "" {
				_ = h.Repo.DeleteByPlatformTrackID(ctx, platformName, trackID, actualQuality)
			} else {
				songInfo = *cached
				msgResult, _ = sendStatusMessage(ctx, b, h.RateLimiter, message.Chat.ID, threadID, replyParams, fmt.Sprintf(musicInfoMsg+hitCache, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024))
				if err = h.sendMusic(ctx, b, msgResult, message, &songInfo, "", "", nil, platformName, trackID); err != nil {
					sendFailed(err)
					return err
				}
				return nil
			}
		}
	}

	if msgResult != nil {
		msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fmt.Sprintf(musicInfoMsg+downloading, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024))
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
	cleanupList = append(cleanupList, musicPath, picPath)

	if msgResult != nil {
		msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fmt.Sprintf(musicInfoMsg+uploading, songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024))
	}

	if err := h.sendMusic(ctx, b, msgResult, message, &songInfo, musicPath, picPath, cleanupList, platformName, trackID); err != nil {
		cleanupFiles(cleanupList...)
		sendFailed(err)
		return err
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
		editParams := &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			Text:      text,
		}
		if h.RateLimiter != nil {
			_, _ = telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, editParams)
		} else {
			_, _ = b.EditMessageText(ctx, editParams)
		}
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
		if _, err := h.DownloadService.Download(ctx, &platform.DownloadInfo{URL: coverURL, Size: 2 * 1024 * 1024}, picPath, nil); err == nil {
			if stat, statErr := os.Stat(picPath); statErr == nil && stat.Size() > 0 {
				songInfo.PicSize = int(stat.Size())
				cleanupList = append(cleanupList, picPath)
				if resized, err := resizeImg(picPath); err == nil {
					resizePicPath = resized
					cleanupList = append(cleanupList, resizePicPath)
				}
			} else {
				_ = os.Remove(picPath)
				picPath = ""
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
		}
	}
	if resizePicPath != "" {
		thumbPicPath = resizePicPath
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

func (h *MusicHandler) sendMusic(ctx context.Context, b *bot.Bot, statusMsg *models.Message, message *models.Message, songInfo *botpkg.SongInfo, musicPath, picPath string, cleanup []string, platformName, trackID string) error {
	if h == nil {
		return errors.New("music handler not configured")
	}

	h.registerQueuedStatus(b, statusMsg, songInfo)

	resultCh := make(chan uploadResult, 1)
	uploadCtx, cancel := context.WithCancel(context.Background())
	uploadBot := b
	if h.UploadBot != nil {
		uploadBot = h.UploadBot
	}
	statusBot := b
	songCopy := *songInfo
	cleanupCopy := append([]string(nil), cleanup...)
	taskMessage := message
	statusMessage := statusMsg
	task := uploadTask{
		ctx:       uploadCtx,
		cancel:    cancel,
		b:         uploadBot,
		statusBot: statusBot,
		statusMsg: statusMsg,
		message:   message,
		songInfo:  songCopy,
		musicPath: musicPath,
		picPath:   picPath,
		cleanup:   cleanupCopy,
		resultCh:  resultCh,
		onDone: func(result uploadResult) {
			if result.message != nil && result.message.Audio != nil {
				songCopy.FileID = result.message.Audio.FileID
				if result.message.Audio.Thumbnail != nil {
					songCopy.ThumbFileID = result.message.Audio.Thumbnail.FileID
				}
			}
			if h.Repo != nil {
				if result.err == nil && songCopy.FileID != "" {
					if err := h.Repo.Create(context.Background(), &songCopy); err != nil {
						if h.Logger != nil {
							h.Logger.Error("failed to save song info", "platform", platformName, "trackID", trackID, "error", err)
						}
					}
				}
			}
			if statusMessage != nil && taskMessage != nil {
				if result.err == nil {
					_, _ = statusBot.DeleteMessage(context.Background(), &bot.DeleteMessageParams{ChatID: taskMessage.Chat.ID, MessageID: statusMessage.ID})
				} else {
					errText := ""
					if result.err != nil {
						errText = result.err.Error()
					}
					statusMessage = editMessageTextOrSend(context.Background(), statusBot, h.RateLimiter, statusMessage, taskMessage.Chat.ID, fmt.Sprintf(musicInfoMsg+uploadFailed, songCopy.SongName, songCopy.SongAlbum, songCopy.FileExt, float64(songCopy.MusicSize)/1024/1024, errText))
				}
			}
			cleanupFiles(cleanupCopy...)
		},
	}
	select {
	case h.UploadQueue <- task:
		return nil
	default:
		cancel()
		return errors.New("upload queue is full")
	}
}

func (h *MusicHandler) runUploadWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-h.UploadQueue:
			if !ok {
				return
			}
			h.processUploadTask(task)
		}
	}
}

func (h *MusicHandler) runStatusRefresher(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.queueMu.Lock()
			if h.statusDirty {
				h.statusDirty = false
				h.refreshQueuedStatusesLocked()
			}
			h.queueMu.Unlock()
		}
	}
}

func (h *MusicHandler) processUploadTask(task uploadTask) {
	h.dequeueQueuedStatus(task.statusMsg)
	if task.ctx != nil {
		select {
		case <-task.ctx.Done():
			result := uploadResult{err: task.ctx.Err()}
			if task.onDone != nil {
				task.onDone(result)
			}
			h.removeQueuedStatus(task.statusMsg)
			if task.resultCh != nil {
				task.resultCh <- result
			}
			return
		case h.UploadLimiter <- struct{}{}:
		}
	} else {
		h.UploadLimiter <- struct{}{}
	}
	if task.statusMsg != nil && task.statusBot != nil {
		text := fmt.Sprintf(musicInfoMsg+uploading, task.songInfo.SongName, task.songInfo.SongAlbum, task.songInfo.FileExt, float64(task.songInfo.MusicSize)/1024/1024)
		updated := editMessageTextOrSend(context.Background(), task.statusBot, h.RateLimiter, task.statusMsg, task.statusMsg.Chat.ID, text)
		if updated != nil {
			task.statusMsg = updated
		}
	}
	result := uploadResult{}
	result.message, result.err = h.sendMusicDirect(task.ctx, task.b, task.message, &task.songInfo, task.musicPath, task.picPath)
	<-h.UploadLimiter
	if task.onDone != nil {
		task.onDone(result)
	}
	h.removeQueuedStatus(task.statusMsg)
	if task.resultCh != nil {
		task.resultCh <- result
	}
}

func (h *MusicHandler) registerQueuedStatus(b *bot.Bot, statusMsg *models.Message, songInfo *botpkg.SongInfo) {
	if h == nil || statusMsg == nil || songInfo == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	entry := queuedStatus{bot: b, message: statusMsg, songInfo: *songInfo}
	h.queuedStatus = append(h.queuedStatus, entry)
	h.statusDirty = true
}

func (h *MusicHandler) removeQueuedStatus(statusMsg *models.Message) {
	if h == nil || statusMsg == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	filtered := h.queuedStatus[:0]
	for _, entry := range h.queuedStatus {
		if entry.message == nil || entry.message.ID == statusMsg.ID {
			continue
		}
		filtered = append(filtered, entry)
	}
	h.queuedStatus = filtered
	h.statusDirty = true
}

func (h *MusicHandler) dequeueQueuedStatus(statusMsg *models.Message) {
	if h == nil || statusMsg == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	filtered := h.queuedStatus[:0]
	removed := false
	for _, entry := range h.queuedStatus {
		if !removed && entry.message != nil && entry.message.ID == statusMsg.ID {
			removed = true
			continue
		}
		filtered = append(filtered, entry)
	}
	h.queuedStatus = filtered
	h.statusDirty = true
}

func (h *MusicHandler) refreshQueuedStatuses() {
	if h == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	h.refreshQueuedStatusesLocked()
}

func (h *MusicHandler) refreshQueuedStatusesLocked() {
	if len(h.queuedStatus) == 0 {
		return
	}
	for idx, entry := range h.queuedStatus {
		if entry.bot == nil || entry.message == nil {
			continue
		}
		text := fmt.Sprintf(musicInfoMsg+uploading, entry.songInfo.SongName, entry.songInfo.SongAlbum, entry.songInfo.FileExt, float64(entry.songInfo.MusicSize)/1024/1024)
		if idx > 0 {
			queueText := fmt.Sprintf("当前正在发送队列中，前面还有 %d 个任务", idx)
			text = text + "\n" + queueText
		}
		params := &bot.EditMessageTextParams{
			ChatID:    entry.message.Chat.ID,
			MessageID: entry.message.ID,
			Text:      text,
		}
		_, err := entry.bot.EditMessageText(context.Background(), params)
		if err != nil && strings.Contains(fmt.Sprintf("%v", err), "message to edit not found") {
			newMsg, sendErr := entry.bot.SendMessage(context.Background(), &bot.SendMessageParams{ChatID: entry.message.Chat.ID, Text: text})
			if sendErr == nil && newMsg != nil {
				entry.message = newMsg
				h.queuedStatus[idx] = entry
			}
		}
	}
}

func (h *MusicHandler) sendMusicDirect(ctx context.Context, b *bot.Bot, message *models.Message, songInfo *botpkg.SongInfo, musicPath, picPath string) (*models.Message, error) {
	if songInfo == nil {
		return nil, errors.New("song info required")
	}
	uploadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	threadID := 0
	if message != nil {
		threadID = message.MessageThreadID
	}

	var audioFile models.InputFile
	openAudioUpload := func() (*models.InputFileUpload, *os.File, error) {
		if strings.TrimSpace(musicPath) == "" {
			return nil, nil, errors.New("music file path is empty")
		}
		stat, err := os.Stat(musicPath)
		if err != nil {
			return nil, nil, fmt.Errorf("music file not found: %w", err)
		}
		if stat.Size() == 0 {
			return nil, nil, errors.New("music file is empty")
		}
		file, err := os.Open(musicPath)
		if err != nil {
			return nil, nil, err
		}
		return &models.InputFileUpload{Filename: filepath.Base(musicPath), Data: file}, file, nil
	}
	openThumbUpload := func() (*models.InputFileUpload, *os.File) {
		if strings.TrimSpace(picPath) == "" {
			return nil, nil
		}
		stat, err := os.Stat(picPath)
		if err != nil || stat.Size() == 0 {
			return nil, nil
		}
		file, err := os.Open(picPath)
		if err != nil {
			return nil, nil
		}
		return &models.InputFileUpload{Filename: filepath.Base(picPath), Data: file}, file
	}
	if songInfo.FileID != "" {
		audioFile = &models.InputFileString{Data: songInfo.FileID}
	} else {
		audioUpload, audioHandle, err := openAudioUpload()
		if err != nil {
			return nil, err
		}
		defer audioHandle.Close()
		audioFile = audioUpload
		_, _ = b.SendChatAction(uploadCtx, &bot.SendChatActionParams{ChatID: message.Chat.ID, MessageThreadID: threadID, Action: models.ChatActionUploadDocument})
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
		if thumbUpload, thumbHandle := openThumbUpload(); thumbUpload != nil {
			defer thumbHandle.Close()
			params.Thumbnail = thumbUpload
		}
	}

	var audio *models.Message
	var err error
	if h.RateLimiter != nil {
		audio, err = telegram.SendAudioWithRetry(uploadCtx, h.RateLimiter, b, params)
	} else {
		audio, err = b.SendAudio(uploadCtx, params)
	}
	if err != nil && (strings.Contains(fmt.Sprintf("%v", err), "replied message not found") || strings.Contains(fmt.Sprintf("%v", err), "message to be replied not found")) {
		params.ReplyParameters = nil
		if songInfo.FileID == "" {
			if audioUpload, audioHandle, fileErr := openAudioUpload(); fileErr == nil {
				defer audioHandle.Close()
				params.Audio = audioUpload
			}
			params.Thumbnail = nil
			if thumbUpload, thumbHandle := openThumbUpload(); thumbUpload != nil {
				defer thumbHandle.Close()
				params.Thumbnail = thumbUpload
			}
		}
		if h.RateLimiter != nil {
			audio, err = telegram.SendAudioWithRetry(uploadCtx, h.RateLimiter, b, params)
		} else {
			audio, err = b.SendAudio(uploadCtx, params)
		}
	}
	if err != nil && strings.Contains(fmt.Sprintf("%v", err), "file must be non-empty") && songInfo.FileID == "" {
		params.Thumbnail = nil
		if strings.TrimSpace(musicPath) == "" {
			return audio, err
		}
		file, fileErr := os.Open(musicPath)
		if fileErr != nil {
			return audio, err
		}
		defer file.Close()
		params.Audio = &models.InputFileUpload{Filename: filepath.Base(musicPath), Data: file}
		if h.RateLimiter != nil {
			audio, err = telegram.SendAudioWithRetry(uploadCtx, h.RateLimiter, b, params)
		} else {
			audio, err = b.SendAudio(uploadCtx, params)
		}
	}
	return audio, err
}

func buildReplyParams(message *models.Message) *models.ReplyParameters {
	if message == nil {
		return nil
	}
	return &models.ReplyParameters{MessageID: message.ID}
}

func sendStatusMessage(ctx context.Context, b *bot.Bot, rateLimiter *telegram.RateLimiter, chatID int64, threadID int, replyParams *models.ReplyParameters, text string) (*models.Message, error) {
	params := &bot.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Text:            text,
		ReplyParameters: replyParams,
	}
	var msg *models.Message
	var err error
	if rateLimiter != nil {
		msg, err = telegram.SendMessageWithRetry(ctx, rateLimiter, b, params)
	} else {
		msg, err = b.SendMessage(ctx, params)
	}
	if err != nil && replyParams != nil && (strings.Contains(fmt.Sprintf("%v", err), "replied message not found") || strings.Contains(fmt.Sprintf("%v", err), "message to be replied not found")) {
		params.ReplyParameters = nil
		if rateLimiter != nil {
			msg, err = telegram.SendMessageWithRetry(ctx, rateLimiter, b, params)
		} else {
			msg, err = b.SendMessage(ctx, params)
		}
	}
	return msg, err
}

func editMessageTextOrSend(ctx context.Context, b *bot.Bot, rateLimiter *telegram.RateLimiter, msg *models.Message, chatID int64, text string) *models.Message {
	if msg == nil {
		return nil
	}
	editParams := &bot.EditMessageTextParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		Text:      text,
	}
	var editedMsg *models.Message
	var err error
	if rateLimiter != nil {
		editedMsg, err = telegram.EditMessageTextWithRetry(ctx, rateLimiter, b, editParams)
	} else {
		editedMsg, err = b.EditMessageText(ctx, editParams)
	}
	if err == nil {
		return editedMsg
	}
	if !strings.Contains(fmt.Sprintf("%v", err), "message to edit not found") {
		return msg
	}
	sendParams := &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	}
	var newMsg *models.Message
	if rateLimiter != nil {
		newMsg, err = telegram.SendMessageWithRetry(ctx, rateLimiter, b, sendParams)
	} else {
		newMsg, err = b.SendMessage(ctx, sendParams)
	}
	if err != nil {
		return msg
	}
	return newMsg
}
