package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-flac/go-flac"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

type musicDispatchContextKey string

const forceNonSilentKey musicDispatchContextKey = "force_non_silent"

const downloadProgressMinInterval = 2 * time.Second

func withForceNonSilent(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, forceNonSilentKey, true)
}

func isForceNonSilent(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, ok := ctx.Value(forceNonSilentKey).(bool)
	return ok && value
}

// MusicHandler handles /music and related commands.
type MusicHandler struct {
	Repo               botpkg.SongRepository
	PlatformManager    platform.Manager // NEW: Platform-agnostic music platform manager
	DownloadService    *download.DownloadService
	ID3Service         *id3.ID3Service
	TagProviders       map[string]id3.ID3TagProvider
	Pool               botpkg.WorkerPool
	Logger             botpkg.Logger
	CacheDir           string
	BotName            string
	DefaultQuality     string
	InlineUploadChatID int64
	DefaultPlatform    string
	FallbackPlatform   string
	AdminIDs           map[int64]struct{}
	AdminCommands      []admincmd.Command
	Playlist           *PlaylistHandler
	RecognizeEnabled   bool
	Limiter            chan struct{}
	UploadLimiter      chan struct{}
	UploadQueue        chan uploadTask
	UploadQueueSize    int
	UploadBot          *telego.Bot
	RateLimiter        *telegram.RateLimiter
	queueMu            sync.Mutex
	queuedStatus       []queuedStatus
	statusDirty        bool
	fetchMu            sync.Mutex
	trackFetch         map[string]*trackFetchCall
	downloadInfoFetch  map[string]*downloadInfoFetchCall
	inlineMu           sync.Mutex
	inlineInFlight     map[string]*inlineProcessCall
	downloadQueueMu    sync.Mutex
	downloadQueueSeq   int64
	downloadQueue      []downloadQueueEntry
}

type downloadQueueEntry struct {
	id     int64
	update func(text string)
}

type uploadTask struct {
	ctx       context.Context
	cancel    context.CancelFunc
	b         *telego.Bot
	statusBot *telego.Bot
	statusMsg *telego.Message
	message   *telego.Message
	songInfo  botpkg.SongInfo
	musicPath string
	picPath   string
	cleanup   []string
	resultCh  chan uploadResult
	onDone    func(uploadResult)
}

type queuedStatus struct {
	bot      *telego.Bot
	message  *telego.Message
	songInfo botpkg.SongInfo
}

type uploadResult struct {
	message *telego.Message
	err     error
}

type trackFetchCall struct {
	done  chan struct{}
	track *platform.Track
	err   error
}

type downloadInfoFetchCall struct {
	done chan struct{}
	info *platform.DownloadInfo
	err  error
}

type inlineProcessCall struct {
	done chan struct{}
	song *botpkg.SongInfo
	err  error
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
func (h *MusicHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil {
		return
	}
	message := update.Message
	cmd := commandName(message.Text, h.BotName)
	if cmd == "start" {
		args := commandArguments(message.Text)
		if strings.TrimSpace(args) == "settings" {
			settingsHandler := &SettingsHandler{
				Repo:            h.Repo,
				PlatformManager: h.PlatformManager,
				RateLimiter:     h.RateLimiter,
				DefaultPlatform: h.DefaultPlatform,
				DefaultQuality:  h.DefaultQuality,
			}
			settingsHandler.Handle(ctx, b, update)
			return
		}
		if platformName, trackID, qualityOverride, ok := parseInlineStartParameter(args); ok {
			h.dispatch(ctx, b, message, platformName, trackID, qualityOverride)
			return
		}
		if inlineQuery, ok := parseInlineSearchStartParameter(args); ok {
			if platformName, trackID, found := h.resolveTrackFromQuery(ctx, message, inlineQuery); found {
				_, _, qualityOverride := parseTrailingOptions(inlineQuery, h.PlatformManager)
				h.dispatch(ctx, b, message, platformName, trackID, qualityOverride)
				return
			}
		}
	}
	if cmd == "start" || cmd == "help" {
		isAdmin := false
		if message.From != nil {
			isAdmin = isBotAdmin(h.AdminIDs, message.From.ID)
		}
		adminHelp := h.AdminCommands
		if isAdmin {
			adminHelp = append([]admincmd.Command{
				{Name: "reload", Description: "重载动态插件"},
				{Name: "rmcache", Description: "清除缓存（/rmcache <平台>|all）"},
			}, adminHelp...)
		}
		params := &telego.SendMessageParams{
			ChatID:             telego.ChatID{ID: message.Chat.ID},
			Text:               buildHelpText(h.PlatformManager, isAdmin, adminHelp, h.RecognizeEnabled),
			ParseMode:          telego.ModeMarkdownV2,
			LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
			ReplyParameters:    &telego.ReplyParameters{MessageID: message.MessageID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}
	if cmd == "music" {
		args := commandArguments(message.Text)
		if strings.TrimSpace(args) == "" {
			params := &telego.SendMessageParams{
				ChatID:          telego.ChatID{ID: message.Chat.ID},
				Text:            inputContent,
				ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
			}
			if h.RateLimiter != nil {
				_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
			} else {
				_, _ = b.SendMessage(ctx, params)
			}
			return
		}
		if h.Playlist != nil {
			if h.Playlist.TryHandle(ctx, b, update) {
				return
			}
		}
		if platformName, trackID, ok := h.resolveTrackFromQuery(ctx, message, args); ok {
			qualityOverride := extractQualityOverride(message, h.PlatformManager)
			h.dispatch(ctx, b, message, platformName, trackID, qualityOverride)
			return
		}
		params := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: message.Chat.ID},
			Text:            noResults,
			ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}
	if cmd != "" && cmd != "start" && cmd != "help" && cmd != "music" && h.PlatformManager != nil {
		if platformName, ok := resolvePlatformAlias(h.PlatformManager, cmd); ok {
			args := commandArguments(message.Text)
			if strings.TrimSpace(args) == "" {
				params := &telego.SendMessageParams{
					ChatID:          telego.ChatID{ID: message.Chat.ID},
					Text:            inputContent,
					ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
				}
				if h.RateLimiter != nil {
					_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
				} else {
					_, _ = b.SendMessage(ctx, params)
				}
				return
			}
			baseText, _, qualityOverride := parseTrailingOptions(args, h.PlatformManager)
			baseText = strings.TrimSpace(baseText)
			if baseText == "" {
				return
			}
			if trackID, matched := matchPlatformTrack(ctx, h.PlatformManager, platformName, baseText); matched {
				h.dispatch(ctx, b, message, platformName, trackID, qualityOverride)
				return
			}
		}
	}

	platformName, trackID, found := extractPlatformTrack(ctx, message, h.PlatformManager)
	if !found {
		return
	}
	if !isAutoLinkDetectEnabled(ctx, h.Repo, message) {
		return
	}
	qualityOverride := extractQualityOverride(message, h.PlatformManager)

	h.dispatch(ctx, b, message, platformName, trackID, qualityOverride)
}

func (h *MusicHandler) dispatch(ctx context.Context, b *telego.Bot, message *telego.Message, platformName, trackID string, qualityOverride string) {
	baseCtx := detachContext(ctx)
	if h.Pool == nil {
		go func() {
			_ = h.processMusic(baseCtx, b, message, platformName, trackID, qualityOverride)
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
			_ = h.processMusic(baseCtx, b, message, platformName, trackID, qualityOverride)
		}); err != nil {
			if h.Logger != nil {
				h.Logger.Error("failed to enqueue music task", "platform", platformName, "trackID", trackID, "error", err)
			}
		}
	}()
}

func (h *MusicHandler) processMusic(ctx context.Context, b *telego.Bot, message *telego.Message, platformName, trackID string, qualityOverride string) error {
	threadID := 0
	if message != nil {
		threadID = message.MessageThreadID
	}
	replyParams := buildReplyParams(message)
	silent := h.shouldSilentAutoFetch(message)
	if isForceNonSilent(ctx) {
		silent = false
	}

	var songInfo botpkg.SongInfo
	var msgResult *telego.Message

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
		if h.Logger != nil {
			h.Logger.Error("failed to send music", "platform", platformName, "trackID", trackID, "error", err)
		}
		text := buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), userVisibleDownloadError(err))
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

				msgResult, _ = sendStatusMessage(ctx, b, h.RateLimiter, message.Chat.ID, threadID, replyParams, buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), hitCache))

				if err = h.sendMusic(ctx, b, msgResult, message, &songInfo, "", "", nil, platformName, trackID); err != nil {
					sendFailed(err)
					return err
				}
				return nil
			}
		}
	}

	if !silent {
		msgResult, _ = sendStatusMessage(ctx, b, h.RateLimiter, message.Chat.ID, threadID, replyParams, waitForDown)
	}

	var queueStatusUpdater func(string)
	if msgResult != nil {
		queueStatusUpdater = func(text string) {
			msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, text)
		}
	}
	releaseDownloadSlot, err := h.acquireDownloadSlot(ctx, queueStatusUpdater)
	if err != nil {
		return err
	}
	defer releaseDownloadSlot()

	if h.Repo != nil {
		cached, err := getCached(platformName, trackID, qualityStr)
		if err == nil && cached != nil {
			if cached.FileID == "" {
				_ = h.Repo.DeleteByPlatformTrackID(ctx, platformName, trackID, qualityStr)
			} else {
				songInfo = *cached
				if msgResult != nil {
					msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), hitCache))
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

	var (
		track *platform.Track
		plat  platform.Platform
	)
	for {
		plat = h.PlatformManager.Get(platformName)
		if plat == nil {
			if h.Logger != nil {
				h.Logger.Error("platform not found", "platform", platformName)
			}
			if msgResult != nil {
				msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fetchInfoFailed)
			}
			return fmt.Errorf("platform not found: %s", platformName)
		}

		var err error
		track, err = h.getTrackSingleflight(ctx, platformName, trackID)
		if err == nil {
			break
		}
		if errors.Is(err, platform.ErrNotFound) {
			if nextPlatform, nextTrackID, ok := h.resolveFallbackTrack(ctx, message, platformName, trackID); ok {
				platformName = nextPlatform
				trackID = nextTrackID
				if msgResult != nil {
					msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fetchInfo)
				}
				continue
			}
		}
		if h.Logger != nil {
			h.Logger.Error("failed to get track", "platform", platformName, "trackID", trackID, "error", err)
		}
		if msgResult != nil {
			msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, fetchInfoFailed)
		}
		return err
	}

	fillSongInfoFromTrack(&songInfo, track, platformName, trackID, message)
	info, err := h.getDownloadInfoSingleflight(ctx, platformName, trackID, quality)
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
	if h.Logger != nil {
		h.Logger.Debug("download url", "platform", platformName, "trackID", trackID, "quality", info.Quality.String(), "url", info.URL)
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
				if !silent {
					msgResult, _ = sendStatusMessage(ctx, b, h.RateLimiter, message.Chat.ID, threadID, replyParams, buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), hitCache))
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
		msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), downloading))
	}

	musicPath, picPath, cleanupList, err := h.downloadAndPrepareFromPlatform(ctx, plat, track, trackID, info, msgResult, b, message, &songInfo, nil)
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
		msgResult = editMessageTextOrSend(ctx, b, h.RateLimiter, msgResult, message.Chat.ID, buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), uploading))
	}

	if err := h.sendMusic(ctx, b, msgResult, message, &songInfo, musicPath, picPath, cleanupList, platformName, trackID); err != nil {
		cleanupFiles(cleanupList...)
		sendFailed(err)
		return err
	}

	return nil
}

func (h *MusicHandler) getTrackSingleflight(ctx context.Context, platformName, trackID string) (*platform.Track, error) {
	if h == nil || h.PlatformManager == nil {
		return nil, errors.New("platform manager not configured")
	}
	key := fmt.Sprintf("track:%s:%s", platformName, trackID)

	h.fetchMu.Lock()
	if h.trackFetch == nil {
		h.trackFetch = make(map[string]*trackFetchCall)
	}
	if call, ok := h.trackFetch[key]; ok {
		h.fetchMu.Unlock()
		<-call.done
		return call.track, call.err
	}
	call := &trackFetchCall{done: make(chan struct{})}
	h.trackFetch[key] = call
	h.fetchMu.Unlock()

	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		call.err = fmt.Errorf("platform not found: %s", platformName)
	} else {
		call.track, call.err = plat.GetTrack(ctx, trackID)
	}

	h.fetchMu.Lock()
	delete(h.trackFetch, key)
	h.fetchMu.Unlock()
	close(call.done)

	if call.track == nil && call.err == nil {
		return nil, errors.New("invalid track result")
	}
	return call.track, call.err
}

func (h *MusicHandler) getDownloadInfoSingleflight(ctx context.Context, platformName, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if h == nil || h.PlatformManager == nil {
		return nil, errors.New("platform manager not configured")
	}
	key := fmt.Sprintf("download_info:%s:%s:%s", platformName, trackID, quality.String())

	h.fetchMu.Lock()
	if h.downloadInfoFetch == nil {
		h.downloadInfoFetch = make(map[string]*downloadInfoFetchCall)
	}
	if call, ok := h.downloadInfoFetch[key]; ok {
		h.fetchMu.Unlock()
		<-call.done
		if call.err != nil {
			return nil, call.err
		}
		if call.info == nil {
			return nil, errors.New("invalid download info result")
		}
		return cloneDownloadInfo(call.info), nil
	}
	call := &downloadInfoFetchCall{done: make(chan struct{})}
	h.downloadInfoFetch[key] = call
	h.fetchMu.Unlock()

	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		call.err = fmt.Errorf("platform not found: %s", platformName)
	} else {
		call.info, call.err = plat.GetDownloadInfo(ctx, trackID, quality)
	}

	h.fetchMu.Lock()
	delete(h.downloadInfoFetch, key)
	h.fetchMu.Unlock()
	close(call.done)

	if call.err != nil {
		return nil, call.err
	}
	if call.info == nil {
		return nil, errors.New("invalid download info result")
	}
	return cloneDownloadInfo(call.info), nil
}

func cloneDownloadInfo(info *platform.DownloadInfo) *platform.DownloadInfo {
	if info == nil {
		return nil
	}
	copy := *info
	if len(info.Headers) > 0 {
		copy.Headers = make(map[string]string, len(info.Headers))
		for k, v := range info.Headers {
			copy.Headers[k] = v
		}
	}
	return &copy
}

func (h *MusicHandler) resolveTrackFromQuery(ctx context.Context, message *telego.Message, args string) (string, string, bool) {
	args = strings.TrimSpace(args)
	if args == "" || h == nil || h.PlatformManager == nil {
		return "", "", false
	}

	baseText, platformSuffix, _ := parseTrailingOptions(args, h.PlatformManager)
	baseText = strings.TrimSpace(baseText)
	if baseText == "" {
		return "", "", false
	}

	fields := strings.Fields(baseText)
	if len(fields) >= 2 {
		if platformName, ok := resolvePlatformAlias(h.PlatformManager, fields[0]); ok {
			if plat := h.PlatformManager.Get(platformName); plat != nil {
				return platformName, fields[1], true
			}
		}
	}
	if platformSuffix != "" && len(fields) == 1 {
		if h.PlatformManager.Get(platformSuffix) != nil && isLikelyIDToken(fields[0]) {
			return platformSuffix, fields[0], true
		}
	}

	resolvedText := resolveShortLinkText(ctx, h.PlatformManager, baseText)
	if _, _, matched := matchPlaylistURL(ctx, h.PlatformManager, resolvedText); matched {
		return "", "", false
	}
	if urlStr := extractFirstURL(resolvedText); urlStr != "" {
		if plat, id, matched := h.PlatformManager.MatchURL(urlStr); matched {
			return plat, id, true
		}
	}

	if plat, id, matched := h.PlatformManager.MatchText(resolvedText); matched {
		return plat, id, true
	}

	keyword := baseText
	if keyword == "" {
		return "", "", false
	}

	primaryPlatform := h.resolveDefaultPlatform(ctx, message)
	if platformSuffix != "" {
		primaryPlatform = platformSuffix
	}
	fallbackPlatform := strings.TrimSpace(h.FallbackPlatform)
	if platformSuffix != "" {
		fallbackPlatform = ""
	}

	order := h.buildSearchOrder(primaryPlatform, fallbackPlatform)
	for _, platformName := range order {
		plat := h.PlatformManager.Get(platformName)
		if plat == nil || !plat.SupportsSearch() {
			continue
		}
		limit := searchLimitForPlatform(platformName)
		tracks, err := plat.Search(ctx, keyword, limit)
		if err != nil || len(tracks) == 0 {
			continue
		}
		for _, track := range tracks {
			if strings.TrimSpace(track.ID) != "" {
				return platformName, track.ID, true
			}
		}
	}

	return "", "", false
}

func (h *MusicHandler) resolveFallbackTrack(ctx context.Context, message *telego.Message, platformName, trackID string) (string, string, bool) {
	keyword, ok := h.fallbackKeyword(message)
	if !ok {
		return "", "", false
	}
	resolvedPlatform, resolvedTrackID, ok := h.resolveTrackFromQuery(ctx, message, keyword)
	if !ok {
		return "", "", false
	}
	if resolvedPlatform == platformName && resolvedTrackID == trackID {
		return "", "", false
	}
	return resolvedPlatform, resolvedTrackID, true
}

func (h *MusicHandler) fallbackKeyword(message *telego.Message) (string, bool) {
	if message == nil {
		return "", false
	}
	cmd := commandName(message.Text, h.BotName)
	if cmd != "" && cmd != "music" {
		return "", false
	}
	text := strings.TrimSpace(message.Text)
	if cmd == "music" {
		text = strings.TrimSpace(commandArguments(message.Text))
	}
	if text == "" {
		return "", false
	}
	if extractFirstURL(text) != "" {
		return "", false
	}
	fields := strings.Fields(text)
	if len(fields) >= 2 && h.PlatformManager != nil {
		if h.PlatformManager.Get(fields[0]) != nil {
			return "", false
		}
	}
	return text, true
}

func (h *MusicHandler) resolveDefaultPlatform(ctx context.Context, message *telego.Message) string {
	platformName := strings.TrimSpace(h.DefaultPlatform)
	if platformName == "" {
		platformName = "netease"
	}
	if h.Repo == nil || message == nil {
		return platformName
	}
	if message.Chat.Type != "private" {
		if settings, err := h.Repo.GetGroupSettings(ctx, message.Chat.ID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultPlatform) != "" {
				platformName = settings.DefaultPlatform
			}
		}
		return platformName
	}
	if message.From != nil {
		if settings, err := h.Repo.GetUserSettings(ctx, message.From.ID); err == nil && settings != nil {
			if strings.TrimSpace(settings.DefaultPlatform) != "" {
				platformName = settings.DefaultPlatform
			}
		}
	}
	return platformName
}

func (h *MusicHandler) buildSearchOrder(primary, fallback string) []string {
	seen := make(map[string]struct{})
	add := func(name string, order []string) []string {
		name = strings.TrimSpace(name)
		if name == "" {
			return order
		}
		if _, ok := seen[name]; ok {
			return order
		}
		seen[name] = struct{}{}
		return append(order, name)
	}

	order := make([]string, 0, 4)
	order = add(primary, order)
	order = add(fallback, order)

	for _, name := range h.searchPlatforms() {
		order = add(name, order)
	}

	return order
}

func (h *MusicHandler) searchPlatforms() []string {
	if h == nil || h.PlatformManager == nil {
		return nil
	}
	names := h.PlatformManager.List()
	results := make([]string, 0, len(names))
	for _, name := range names {
		plat := h.PlatformManager.Get(name)
		if plat == nil || !plat.SupportsSearch() {
			continue
		}
		results = append(results, name)
	}
	return results
}

func searchLimitForPlatform(platformName string) int {
	if strings.TrimSpace(platformName) == "netease" {
		return neteaseSearchLimit
	}
	return defaultSearchLimit
}

func (h *MusicHandler) shouldSilentAutoFetch(message *telego.Message) bool {
	if message == nil {
		return false
	}
	if message.Chat.Type == "private" {
		return false
	}
	if isCommandMessage(message) {
		return false
	}
	return !strings.HasPrefix(strings.TrimSpace(message.Text), "/")
}

func (h *MusicHandler) downloadAndPrepareFromPlatform(ctx context.Context, plat platform.Platform, track *platform.Track, trackID string, info *platform.DownloadInfo, msg *telego.Message, b *telego.Bot, message *telego.Message, songInfo *botpkg.SongInfo, externalProgress func(written, total int64)) (string, string, []string, error) {
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

	lastProgressText := ""
	lastProgressAt := time.Time{}
	minInterval := downloadProgressMinInterval
	progress := func(written, total int64) {
		if externalProgress != nil {
			externalProgress(written, total)
		}
		if msg == nil {
			return
		}
		now := time.Now()
		if !lastProgressAt.IsZero() && now.Sub(lastProgressAt) < minInterval {
			return
		}
		writtenMB := float64(written) / 1024 / 1024
		text := ""
		if total <= 0 {
			text = fmt.Sprintf("正在下载：%s\n已下载：%.2f MB", track.Title, writtenMB)
		} else {
			totalMB := float64(total) / 1024 / 1024
			progressPct := float64(written) * 100 / float64(total)
			text = fmt.Sprintf("正在下载：%s\n进度：%.2f%% (%.2f MB / %.2f MB)", track.Title, progressPct, writtenMB, totalMB)
		}
		if total > 0 && written >= total && lastProgressText != "" {
			return
		}
		if msg.Text == text || lastProgressText == text {
			lastProgressText = text
			return
		}
		lastProgressText = text
		lastProgressAt = now
		editParams := &telego.EditMessageTextParams{
			ChatID:    telego.ChatID{ID: msg.Chat.ID},
			MessageID: msg.MessageID,
			Text:      text,
		}
		if h.RateLimiter != nil {
			if editedMsg, err := telegram.EditMessageTextWithRetry(ctx, h.RateLimiter, b, editParams); err == nil {
				if editedMsg != nil {
					msg = editedMsg
				} else {
					msg.Text = text
				}
			}
		} else {
			if editedMsg, err := b.EditMessageText(ctx, editParams); err == nil {
				if editedMsg != nil {
					msg = editedMsg
				} else {
					msg.Text = text
				}
			}
		}
	}

	if _, err := h.DownloadService.Download(ctx, info, filePath, progress); err != nil {
		_ = os.Remove(filePath)
		return "", "", cleanupList, err
	}

	// Derive bitrate from actual file size + duration (from track or FLAC streaminfo)
	deriveBitrateFromFile(filePath, songInfo)

	picPath, resizePicPath := "", ""
	coverURL := ""
	if track.CoverURL != "" {
		coverURL = track.CoverURL
	} else if track.Album != nil && track.Album.CoverURL != "" {
		coverURL = track.Album.CoverURL
	}
	if coverURL != "" {
		picPath = filepath.Join(h.CacheDir, fmt.Sprintf("%d-%s", stamp, path.Base(coverURL)))
		if _, err := h.DownloadService.Download(ctx, &platform.DownloadInfo{URL: coverURL, Size: 0}, picPath, nil); err == nil {
			if stat, statErr := os.Stat(picPath); statErr == nil && stat.Size() > 0 {
				songInfo.PicSize = int(stat.Size())
				cleanupList = append(cleanupList, picPath)
				if resized, err := resizeImg(picPath); err == nil {
					resizePicPath = resized
					cleanupList = append(cleanupList, resizePicPath)
				} else {
					if h.Logger != nil {
						h.Logger.Warn("failed to resize cover image", "track", trackID, "error", err)
					}
				}
			} else {
				if h.Logger != nil {
					if statErr != nil {
						h.Logger.Warn("failed to stat cover file", "track", trackID, "error", statErr)
					} else {
						h.Logger.Warn("cover file is empty", "track", trackID)
					}
				}
				_ = os.Remove(picPath)
				picPath = ""
			}
		} else {
			if h.Logger != nil {
				h.Logger.Warn("failed to download cover", "track", trackID, "url", coverURL, "error", err)
			}
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

	if h.ID3Service != nil {
		var tagData *id3.TagData

		if h.TagProviders != nil {
			if provider, ok := h.TagProviders[plat.Name()]; ok && provider != nil {
				var tagErr error
				tagData, tagErr = provider.GetTagData(ctx, track, info)
				if tagErr != nil {
					if h.Logger != nil {
						h.Logger.Error("failed to get tag data", "platform", plat.Name(), "trackID", trackID, "error", tagErr)
					}
					tagData = nil
				}
			}
		}

		if tagData == nil {
			tagData = h.buildFallbackTagData(ctx, plat, track, embedPicPath)
		}

		if tagData != nil {
			if err := h.ID3Service.EmbedTags(filePath, tagData, embedPicPath); err != nil {
				if h.Logger != nil {
					h.Logger.Error("failed to embed tags", "platform", plat.Name(), "trackID", trackID, "error", err)
				}
			}
		}
	}

	return filePath, thumbPicPath, cleanupList, nil
}

func (h *MusicHandler) sendMusic(ctx context.Context, b *telego.Bot, statusMsg *telego.Message, message *telego.Message, songInfo *botpkg.SongInfo, musicPath, picPath string, cleanup []string, platformName, trackID string) error {
	if h == nil {
		return errors.New("music handler not configured")
	}

	h.registerQueuedStatus(b, statusMsg, songInfo)

	baseCtx := detachContext(ctx)
	resultCh := make(chan uploadResult, 1)
	uploadCtx, cancel := context.WithCancel(baseCtx)
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
			if h.Repo != nil && result.err == nil && songCopy.FileID != "" {
				if err := h.Repo.Create(baseCtx, &songCopy); err != nil {
					if h.Logger != nil {
						h.Logger.Error("failed to save song info", "platform", platformName, "trackID", trackID, "error", err)
					}
				}
				if err := h.Repo.IncrementSendCount(baseCtx); err != nil {
					if h.Logger != nil {
						h.Logger.Error("failed to update send count", "error", err)
					}
				}
			}
			if statusMessage != nil && taskMessage != nil {
				if result.err == nil {
					_ = statusBot.DeleteMessage(baseCtx, &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: taskMessage.Chat.ID}, MessageID: statusMessage.MessageID})
				} else {
					if h.Logger != nil {
						h.Logger.Error("upload worker failed", "platform", platformName, "trackID", trackID, "error", result.err)
					}
					statusMessage = editMessageTextOrSend(baseCtx, statusBot, h.RateLimiter, statusMessage, taskMessage.Chat.ID, buildMusicInfoText(songCopy.SongName, songCopy.SongAlbum, formatFileInfo(songCopy.FileExt, songCopy.MusicSize), userVisibleDownloadError(result.err)))
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
			shouldRefresh := false
			h.queueMu.Lock()
			if h.statusDirty {
				h.statusDirty = false
				shouldRefresh = true
			}
			h.queueMu.Unlock()
			if shouldRefresh {
				h.refreshQueuedStatuses(ctx)
			}
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
		text := buildMusicInfoText(task.songInfo.SongName, task.songInfo.SongAlbum, formatFileInfo(task.songInfo.FileExt, task.songInfo.MusicSize), uploading)
		statusCtx := task.ctx
		if statusCtx == nil {
			statusCtx = context.Background()
		}
		updated := editMessageTextOrSend(statusCtx, task.statusBot, h.RateLimiter, task.statusMsg, task.statusMsg.Chat.ID, text)
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

func (h *MusicHandler) registerQueuedStatus(b *telego.Bot, statusMsg *telego.Message, songInfo *botpkg.SongInfo) {
	if h == nil || statusMsg == nil || songInfo == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	entry := queuedStatus{bot: b, message: statusMsg, songInfo: *songInfo}
	h.queuedStatus = append(h.queuedStatus, entry)
	h.statusDirty = true
}

func (h *MusicHandler) removeQueuedStatus(statusMsg *telego.Message) {
	if h == nil || statusMsg == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	filtered := h.queuedStatus[:0]
	for _, entry := range h.queuedStatus {
		if entry.message == nil || entry.message.MessageID == statusMsg.MessageID {
			continue
		}
		filtered = append(filtered, entry)
	}
	h.queuedStatus = filtered
	h.statusDirty = true
}

func (h *MusicHandler) dequeueQueuedStatus(statusMsg *telego.Message) {
	if h == nil || statusMsg == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	filtered := h.queuedStatus[:0]
	removed := false
	for _, entry := range h.queuedStatus {
		if !removed && entry.message != nil && entry.message.MessageID == statusMsg.MessageID {
			removed = true
			continue
		}
		filtered = append(filtered, entry)
	}
	h.queuedStatus = filtered
	h.statusDirty = true
}

func (h *MusicHandler) refreshQueuedStatuses(ctx context.Context) {
	if h == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var snapshot []queuedStatus
	h.queueMu.Lock()
	if len(h.queuedStatus) > 0 {
		snapshot = make([]queuedStatus, len(h.queuedStatus))
		copy(snapshot, h.queuedStatus)
	}
	h.queueMu.Unlock()
	if len(snapshot) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for idx, entry := range snapshot {
		if entry.bot == nil || entry.message == nil {
			continue
		}
		text := buildMusicInfoText(entry.songInfo.SongName, entry.songInfo.SongAlbum, formatFileInfo(entry.songInfo.FileExt, entry.songInfo.MusicSize), uploading)
		if idx > 0 {
			queueText := fmt.Sprintf("当前正在发送队列中，前面还有 %d 个任务", idx)
			text = text + "\n" + queueText
		}
		if entry.message.Text == text {
			continue
		}
		params := &telego.EditMessageTextParams{
			ChatID:    telego.ChatID{ID: entry.message.Chat.ID},
			MessageID: entry.message.MessageID,
			Text:      text,
		}
		editedMsg, err := entry.bot.EditMessageText(ctx, params)
		if err == nil {
			if editedMsg != nil {
				h.updateQueuedStatusMessage(entry.message.MessageID, editedMsg)
			} else {
				h.updateQueuedStatusText(entry.message.MessageID, text)
			}
			continue
		}
		if err != nil && strings.Contains(fmt.Sprintf("%v", err), "message to edit not found") {
			newMsg, sendErr := entry.bot.SendMessage(ctx, &telego.SendMessageParams{ChatID: telego.ChatID{ID: entry.message.Chat.ID}, Text: text})
			if sendErr == nil && newMsg != nil {
				h.updateQueuedStatusMessage(entry.message.MessageID, newMsg)
			}
		}
	}
}

func (h *MusicHandler) updateQueuedStatusMessage(oldMessageID int, newMsg *telego.Message) {
	if h == nil || newMsg == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	for idx, entry := range h.queuedStatus {
		if entry.message != nil && entry.message.MessageID == oldMessageID {
			entry.message = newMsg
			h.queuedStatus[idx] = entry
			return
		}
	}
}

func (h *MusicHandler) updateQueuedStatusText(messageID int, text string) {
	if h == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	for idx, entry := range h.queuedStatus {
		if entry.message != nil && entry.message.MessageID == messageID {
			entry.message.Text = text
			h.queuedStatus[idx] = entry
			return
		}
	}
}

func (h *MusicHandler) sendMusicDirect(ctx context.Context, b *telego.Bot, message *telego.Message, songInfo *botpkg.SongInfo, musicPath, picPath string) (*telego.Message, error) {
	if songInfo == nil {
		return nil, errors.New("song info required")
	}
	if message == nil {
		return nil, errors.New("message required")
	}
	if message.Chat.ID == 0 {
		return nil, errors.New("message chat required")
	}
	uploadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	threadID := 0
	if message != nil {
		threadID = message.MessageThreadID
	}

	var audioFile telego.InputFile
	openAudioUpload := func() (telego.InputFile, *os.File, error) {
		if strings.TrimSpace(musicPath) == "" {
			return telego.InputFile{}, nil, errors.New("music file path is empty")
		}
		stat, err := os.Stat(musicPath)
		if err != nil {
			return telego.InputFile{}, nil, fmt.Errorf("music file not found: %w", err)
		}
		if stat.Size() == 0 {
			return telego.InputFile{}, nil, errors.New("music file is empty")
		}
		file, err := os.Open(musicPath)
		if err != nil {
			return telego.InputFile{}, nil, err
		}
		return telego.InputFile{File: file}, file, nil
	}
	openThumbUpload := func() (*telego.InputFile, *os.File) {
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
		return &telego.InputFile{File: file}, file
	}
	if songInfo.FileID != "" {
		audioFile = telego.InputFile{FileID: songInfo.FileID}
	} else {
		audioUpload, audioHandle, err := openAudioUpload()
		if err != nil {
			return nil, err
		}
		defer audioHandle.Close()
		audioFile = audioUpload
		_ = b.SendChatAction(uploadCtx, &telego.SendChatActionParams{ChatID: telego.ChatID{ID: message.Chat.ID}, MessageThreadID: threadID, Action: telego.ChatActionUploadDocument})
	}

	caption := buildMusicCaption(h.PlatformManager, songInfo, h.BotName)
	params := &telego.SendAudioParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		MessageThreadID: threadID,
		Audio:           audioFile,
		Caption:         caption,
		ParseMode:       telego.ModeHTML,
		Title:           songInfo.SongName,
		Performer:       songInfo.SongArtists,
		Duration:        songInfo.Duration,
		ReplyParameters: buildReplyParams(message),
	}
	params.ReplyMarkup = buildForwardKeyboard(songInfo.TrackURL, songInfo.Platform, songInfo.TrackID)

	if songInfo.ThumbFileID != "" {
		params.Thumbnail = &telego.InputFile{FileID: songInfo.ThumbFileID}
	} else if picPath != "" {
		if thumbUpload, thumbHandle := openThumbUpload(); thumbUpload != nil {
			defer thumbHandle.Close()
			params.Thumbnail = thumbUpload
		}
	}

	var audio *telego.Message
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
		params.Audio = telego.InputFile{File: file}
		if h.RateLimiter != nil {
			audio, err = telegram.SendAudioWithRetry(uploadCtx, h.RateLimiter, b, params)
		} else {
			audio, err = b.SendAudio(uploadCtx, params)
		}
	}
	return audio, err
}

func buildReplyParams(message *telego.Message) *telego.ReplyParameters {
	if message == nil {
		return nil
	}
	return &telego.ReplyParameters{MessageID: message.MessageID}
}

func sendStatusMessage(ctx context.Context, b *telego.Bot, rateLimiter *telegram.RateLimiter, chatID int64, threadID int, replyParams *telego.ReplyParameters, text string) (*telego.Message, error) {
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: chatID},
		MessageThreadID: threadID,
		Text:            text,
		ReplyParameters: replyParams,
	}
	var msg *telego.Message
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

func editMessageTextOrSend(ctx context.Context, b *telego.Bot, rateLimiter *telegram.RateLimiter, msg *telego.Message, chatID int64, text string) *telego.Message {
	if msg == nil {
		return nil
	}
	if msg.Text == text {
		return msg
	}
	editParams := &telego.EditMessageTextParams{
		ChatID:    telego.ChatID{ID: msg.Chat.ID},
		MessageID: msg.MessageID,
		Text:      text,
	}
	var editedMsg *telego.Message
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
	sendParams := &telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text:   text,
	}
	var newMsg *telego.Message
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

func detachContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func parseInlineStartParameter(value string) (platformName, trackID, qualityOverride string, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", "", false
	}
	parts := strings.Split(value, "_")
	if len(parts) < 3 {
		return "", "", "", false
	}
	if parts[0] != "cache" {
		return "", "", "", false
	}
	platformName = parts[1]
	trackID = parts[2]
	if !isInlineStartToken(platformName) || !isInlineStartToken(trackID) {
		return "", "", "", false
	}
	if len(parts) >= 4 {
		qualityOverride = parts[3]
		if !isInlineStartToken(qualityOverride) {
			qualityOverride = ""
		}
		if qualityOverride != "" {
			if _, err := platform.ParseQuality(qualityOverride); err != nil {
				qualityOverride = ""
			}
		}
	}
	return platformName, trackID, qualityOverride, true
}

func parseInlineSearchStartParameter(value string) (query string, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "search_") {
		return "", false
	}
	encoded := strings.TrimPrefix(value, "search_")
	if encoded == "" {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}
	query = strings.TrimSpace(string(decoded))
	if query == "" {
		return "", false
	}
	return query, true
}

func (h *MusicHandler) resolveInlineQualityValue(ctx context.Context, userID int64, qualityOverride string) string {
	qualityValue := strings.TrimSpace(qualityOverride)
	if qualityValue == "" {
		qualityValue = strings.TrimSpace(h.DefaultQuality)
	}
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if h.Repo != nil && userID != 0 && strings.TrimSpace(qualityOverride) == "" {
		if settings, err := h.Repo.GetUserSettings(ctx, userID); err == nil && settings != nil && strings.TrimSpace(settings.DefaultQuality) != "" {
			qualityValue = strings.TrimSpace(settings.DefaultQuality)
		}
	}
	return qualityValue
}

func (h *MusicHandler) findInlineCachedSong(ctx context.Context, userID int64, platformName, trackID, qualityOverride string) (*botpkg.SongInfo, string, error) {
	if h == nil || h.Repo == nil {
		return nil, "", nil
	}
	qualityValue := h.resolveInlineQualityValue(ctx, userID, qualityOverride)
	cached, err := h.Repo.FindByPlatformTrackID(ctx, platformName, trackID, qualityValue)
	if err != nil {
		return nil, qualityValue, err
	}
	if cached == nil || strings.TrimSpace(cached.FileID) == "" {
		return nil, qualityValue, nil
	}
	copy := *cached
	return &copy, qualityValue, nil
}

func (h *MusicHandler) prepareInlineSong(
	ctx context.Context,
	b *telego.Bot,
	userID int64,
	platformName, trackID, qualityOverride string,
	progress func(text string),
) (*botpkg.SongInfo, error) {
	if h == nil {
		return nil, errors.New("music handler not configured")
	}
	qualityValue := h.resolveInlineQualityValue(ctx, userID, qualityOverride)

	findCached := func() (*botpkg.SongInfo, error) {
		if h.Repo == nil {
			return nil, nil
		}
		cached, err := h.Repo.FindByPlatformTrackID(ctx, platformName, trackID, qualityValue)
		if err != nil || cached == nil || strings.TrimSpace(cached.FileID) == "" {
			return nil, err
		}
		return cached, nil
	}

	if cached, _ := findCached(); cached != nil {
		copy := *cached
		return &copy, nil
	}

	key := fmt.Sprintf("inline:%s:%s:%s", strings.TrimSpace(platformName), strings.TrimSpace(trackID), strings.TrimSpace(qualityValue))
	h.inlineMu.Lock()
	if h.inlineInFlight == nil {
		h.inlineInFlight = make(map[string]*inlineProcessCall)
	}
	if call, ok := h.inlineInFlight[key]; ok {
		h.inlineMu.Unlock()
		<-call.done
		if call.song == nil {
			return nil, call.err
		}
		copy := *call.song
		return &copy, call.err
	}
	call := &inlineProcessCall{done: make(chan struct{})}
	h.inlineInFlight[key] = call
	h.inlineMu.Unlock()

	defer func() {
		h.inlineMu.Lock()
		delete(h.inlineInFlight, key)
		h.inlineMu.Unlock()
		close(call.done)
	}()

	if cached, _ := findCached(); cached != nil {
		copy := *cached
		call.song = &copy
		return &copy, nil
	}

	if h.PlatformManager == nil {
		call.err = errors.New("platform manager not configured")
		return nil, call.err
	}
	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		call.err = fmt.Errorf("platform not found: %s", platformName)
		return nil, call.err
	}

	quality := platform.QualityHigh
	if parsed, err := platform.ParseQuality(qualityValue); err == nil {
		quality = parsed
	}
	track, err := h.getTrackSingleflight(ctx, platformName, trackID)
	if err != nil {
		call.err = err
		return nil, err
	}
	info, err := h.getDownloadInfoSingleflight(ctx, platformName, trackID, quality)
	if err != nil {
		call.err = err
		return nil, err
	}
	if info == nil || strings.TrimSpace(info.URL) == "" {
		call.err = errors.New("download info unavailable")
		return nil, call.err
	}
	if info.Format == "" {
		info.Format = "mp3"
	}
	actualQuality := info.Quality.String()
	if actualQuality == "" || actualQuality == "unknown" {
		actualQuality = quality.String()
	}
	if strings.TrimSpace(actualQuality) == "" {
		actualQuality = qualityValue
	}
	if strings.TrimSpace(actualQuality) == "" {
		actualQuality = "hires"
	}
	qualityValue = actualQuality

	if cached, _ := findCached(); cached != nil {
		copy := *cached
		call.song = &copy
		return &copy, nil
	}

	var songInfo botpkg.SongInfo
	fillSongInfoFromTrack(&songInfo, track, platformName, trackID, &telego.Message{})
	songInfo.Quality = actualQuality
	songInfo.FileExt = info.Format
	songInfo.MusicSize = int(info.Size)
	songInfo.BitRate = info.Bitrate * 1000

	releaseDownloadSlot, err := h.acquireDownloadSlot(ctx, progress)
	if err != nil {
		call.err = err
		return nil, err
	}
	defer releaseDownloadSlot()

	if cached, _ := findCached(); cached != nil {
		copy := *cached
		call.song = &copy
		return &copy, nil
	}

	if progress != nil {
		progress(buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), downloading))
	}

	lastProgressAt := time.Time{}
	lastProgressText := ""
	dlProgress := func(written, total int64) {
		if progress == nil {
			return
		}
		now := time.Now()
		if !lastProgressAt.IsZero() && now.Sub(lastProgressAt) < downloadProgressMinInterval {
			return
		}
		writtenMB := float64(written) / 1024 / 1024
		suffix := ""
		if total <= 0 {
			suffix = fmt.Sprintf("正在下载：%s\n已下载：%.2f MB", track.Title, writtenMB)
		} else {
			totalMB := float64(total) / 1024 / 1024
			progressPct := float64(written) * 100 / float64(total)
			suffix = fmt.Sprintf("正在下载：%s\n进度：%.2f%% (%.2f MB / %.2f MB)", track.Title, progressPct, writtenMB, totalMB)
		}
		text := buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), suffix)
		if text == lastProgressText {
			return
		}
		lastProgressAt = now
		lastProgressText = text
		progress(text)
	}
	musicPath, picPath, cleanupList, err := h.downloadAndPrepareFromPlatform(ctx, plat, track, trackID, info, nil, b, &telego.Message{}, &songInfo, dlProgress)
	if err != nil {
		call.err = err
		return nil, err
	}
	defer cleanupFiles(cleanupList...)

	uploadChatID := h.InlineUploadChatID
	if uploadChatID == 0 {
		call.err = errors.New("InlineUploadChatID not configured")
		return nil, call.err
	}

	if progress != nil {
		progress(buildMusicInfoText(songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), uploading))
	}

	uploadBot := b
	if h.UploadBot != nil {
		uploadBot = h.UploadBot
	}
	file, err := os.Open(musicPath)
	if err != nil {
		call.err = err
		return nil, err
	}
	defer file.Close()
	caption := buildMusicCaption(h.PlatformManager, &songInfo, h.BotName)
	params := &telego.SendAudioParams{
		ChatID:    telego.ChatID{ID: uploadChatID},
		Audio:     telego.InputFile{File: file},
		Caption:   caption,
		ParseMode: telego.ModeHTML,
		Title:     songInfo.SongName,
		Performer: songInfo.SongArtists,
		Duration:  songInfo.Duration,
	}
	if strings.TrimSpace(picPath) != "" {
		if thumbStat, thumbErr := os.Stat(picPath); thumbErr == nil && thumbStat.Size() > 0 {
			if thumbFile, thumbOpenErr := os.Open(picPath); thumbOpenErr == nil {
				defer thumbFile.Close()
				params.Thumbnail = &telego.InputFile{File: thumbFile}
			}
		}
	}
	var uploaded *telego.Message
	if h.RateLimiter != nil {
		uploaded, err = telegram.SendAudioWithRetry(ctx, h.RateLimiter, uploadBot, params)
	} else {
		uploaded, err = uploadBot.SendAudio(ctx, params)
	}
	if err != nil || uploaded == nil || uploaded.Audio == nil || strings.TrimSpace(uploaded.Audio.FileID) == "" {
		if err == nil {
			err = errors.New("upload failed")
		}
		call.err = err
		return nil, err
	}
	songInfo.FileID = uploaded.Audio.FileID
	if uploaded.Audio.Thumbnail != nil {
		songInfo.ThumbFileID = uploaded.Audio.Thumbnail.FileID
	}

	if h.Repo != nil {
		_ = h.Repo.Create(ctx, &songInfo)
	}
	copy := songInfo
	call.song = &copy
	return &copy, nil
}

func (h *MusicHandler) acquireDownloadSlot(ctx context.Context, update func(text string)) (func(), error) {
	if h == nil || h.Limiter == nil {
		return func() {}, nil
	}
	select {
	case h.Limiter <- struct{}{}:
		return func() { <-h.Limiter }, nil
	default:
	}

	entryID := h.enqueueDownloadQueue(update)
	select {
	case h.Limiter <- struct{}{}:
		h.dequeueDownloadQueue(entryID)
		return func() { <-h.Limiter }, nil
	case <-ctx.Done():
		h.dequeueDownloadQueue(entryID)
		return nil, ctx.Err()
	}
}

func (h *MusicHandler) enqueueDownloadQueue(update func(text string)) int64 {
	if h == nil {
		return 0
	}
	h.downloadQueueMu.Lock()
	h.downloadQueueSeq++
	entryID := h.downloadQueueSeq
	h.downloadQueue = append(h.downloadQueue, downloadQueueEntry{id: entryID, update: update})
	snapshot := append([]downloadQueueEntry(nil), h.downloadQueue...)
	h.downloadQueueMu.Unlock()
	h.refreshDownloadQueue(snapshot)
	return entryID
}

func (h *MusicHandler) dequeueDownloadQueue(entryID int64) {
	if h == nil || entryID == 0 {
		return
	}
	h.downloadQueueMu.Lock()
	filtered := h.downloadQueue[:0]
	for _, entry := range h.downloadQueue {
		if entry.id == entryID {
			continue
		}
		filtered = append(filtered, entry)
	}
	h.downloadQueue = filtered
	snapshot := append([]downloadQueueEntry(nil), h.downloadQueue...)
	h.downloadQueueMu.Unlock()
	h.refreshDownloadQueue(snapshot)
}

func (h *MusicHandler) refreshDownloadQueue(snapshot []downloadQueueEntry) {
	for idx, entry := range snapshot {
		if entry.update == nil {
			continue
		}
		ahead := idx
		text := waitForDown
		if ahead > 0 {
			text = fmt.Sprintf("%s\n当前正在下载队列中，前面还有 %d 个任务", waitForDown, ahead)
		}
		entry.update(text)
	}
}

func isInlineStartToken(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '_' || ch == '-':
		default:
			return false
		}
	}
	return true
}

// deriveBitrateFromFile derives bitrate and updates songInfo from actual file metrics.
// Uses file size and duration (from track or FLAC streaminfo if available).
// If duration is missing, attempts ffprobe as fallback.
// If duration still unknown, clears placeholder bitrate (>=900 kbps).
// Errors are silently ignored.
func deriveBitrateFromFile(filePath string, songInfo *botpkg.SongInfo) {
	if songInfo == nil || strings.TrimSpace(filePath) == "" {
		return
	}

	// Get file size
	stat, err := os.Stat(filePath)
	if err != nil || stat.Size() <= 0 {
		return
	}
	fileSizeBytes := stat.Size()

	// Correct file extension if FLAC header detected
	if isValidFLACFile(filePath) && !strings.EqualFold(songInfo.FileExt, "flac") {
		songInfo.FileExt = "flac"
	}

	// Determine duration: try existing, then FLAC, then ffprobe
	durationSeconds := songInfo.Duration
	if durationSeconds <= 0 || strings.EqualFold(songInfo.FileExt, "flac") {
		// Try FLAC streaminfo
		flacDuration := parseFLACDuration(filePath)
		if flacDuration > 0 {
			durationSeconds = flacDuration
			songInfo.Duration = flacDuration
		}
	}

	// Fallback: try ffprobe if duration still unknown
	if durationSeconds <= 0 {
		ffprobeDuration := getFFprobeDuration(filePath)
		if ffprobeDuration > 0 {
			durationSeconds = ffprobeDuration
			songInfo.Duration = ffprobeDuration
		}
	}

	// Prefer ffprobe-reported bitrate if available
	ffprobeBitrate := getFFprobeBitrate(filePath)
	if ffprobeBitrate > 0 {
		songInfo.BitRate = ffprobeBitrate
	} else if durationSeconds > 0 {
		bits := fileSizeBytes * 8
		bitRateBps := int(bits / int64(durationSeconds))
		if bitRateBps > 0 {
			songInfo.BitRate = bitRateBps
		}
	} else if songInfo.BitRate >= 900000 {
		// Duration still unknown: clear placeholder bitrate (>= 900 kbps = 900000 bps)
		songInfo.BitRate = 0
	}

	// Always update file size from actual file
	songInfo.MusicSize = int(fileSizeBytes)
}

func (h *MusicHandler) buildFallbackTagData(ctx context.Context, plat platform.Platform, track *platform.Track, picPath string) *id3.TagData {
	if track == nil {
		return nil
	}

	tagData := &id3.TagData{
		Title:    track.Title,
		CoverURL: track.CoverURL,
	}

	if len(track.Artists) > 0 {
		artists := make([]string, len(track.Artists))
		for i, a := range track.Artists {
			artists[i] = a.Name
		}
		tagData.Artist = strings.Join(artists, ", ")
	}

	if track.Album != nil {
		tagData.Album = track.Album.Title
		if len(track.Album.Artists) > 0 {
			artists := make([]string, len(track.Album.Artists))
			for i, a := range track.Album.Artists {
				artists[i] = a.Name
			}
			tagData.AlbumArtist = strings.Join(artists, ", ")
		}
	}

	if plat.SupportsLyrics() {
		if lyrics, err := plat.GetLyrics(ctx, track.ID); err == nil && lyrics != nil {
			if strings.TrimSpace(lyrics.Plain) != "" {
				tagData.Lyrics = lyrics.Plain
			}
		}
	}

	return tagData
}

// parseFLACDuration extracts duration in seconds from FLAC file's streaminfo block.
// Returns 0 if unable to parse or format is invalid.
func parseFLACDuration(filePath string) int {
	file, err := os.Open(filePath)
	if err != nil {
		return 0
	}
	defer file.Close()

	parsed, err := flac.ParseMetadata(file)
	if err != nil {
		return 0
	}

	streamInfo, err := parsed.GetStreamInfo()
	if err != nil || streamInfo == nil {
		return 0
	}

	if streamInfo.SampleRate > 0 && streamInfo.SampleCount > 0 {
		durationSeconds := int(streamInfo.SampleCount / int64(streamInfo.SampleRate))
		return durationSeconds
	}

	return 0
}

func isValidFLACFile(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	header := make([]byte, 4)
	if _, err := file.Read(header); err != nil {
		return false
	}

	return header[0] == 0x66 && header[1] == 0x4C && header[2] == 0x61 && header[3] == 0x43
}

func getFFprobeDuration(filePath string) int {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1:nokey=1",
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	durStr := strings.TrimSpace(string(output))
	if durStr == "" {
		return 0
	}

	durationFloat, err := strconv.ParseFloat(durStr, 64)
	if err != nil {
		return 0
	}

	if durationFloat <= 0 {
		return 0
	}

	return int(durationFloat)
}

func getFFprobeBitrate(filePath string) int {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=bit_rate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	bitrateStr := strings.TrimSpace(string(output))
	if bitrateStr == "" || strings.EqualFold(bitrateStr, "N/A") {
		return 0
	}

	bitrateFloat, err := strconv.ParseFloat(bitrateStr, 64)
	if err != nil || bitrateFloat <= 0 {
		return 0
	}

	return int(bitrateFloat)
}
