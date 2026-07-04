package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-flac/go-flac"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/i18n"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
	"golang.org/x/sync/singleflight"
)

type musicDispatchContextKey string

const forceNonSilentKey musicDispatchContextKey = "force_non_silent"
const disableFallbackKey musicDispatchContextKey = "disable_fallback"
const downloadWorkAdmissionKey musicDispatchContextKey = "download_work_admission"

const downloadProgressMinInterval = 2 * time.Second
const defaultMusicProcessTimeout = 15 * time.Minute

var (
	probeExtractedAudioCodec = detectExtractedAudioCodec
	extractEmbeddedFLAC      = extractEmbeddedFLACFromContainer
	remuxExtractedAudioM4A   = remuxExtractedAudioToM4A
)

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

func withDisableFallback(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, disableFallbackKey, true)
}

func isFallbackDisabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, ok := ctx.Value(disableFallbackKey).(bool)
	return ok && value
}

func withDownloadWorkAdmission(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, downloadWorkAdmissionKey, true)
}

func hasDownloadWorkAdmission(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, ok := ctx.Value(downloadWorkAdmissionKey).(bool)
	return ok && value
}

var errDownloadQueueOverloaded = errors.New("download queue overloaded")

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
	DefaultLyricFormat string
	ProcessTimeout     time.Duration
	InlineUploadChatID int64
	DefaultPlatform    string
	FallbackPlatform   string
	AdminIDs           *AdminSet
	AdminCommands      []admincmd.Command
	Playlist           *PlaylistHandler
	Artist             *ArtistHandler
	LyricHandler       *LyricHandler
	RecognizeEnabled   bool
	DownloadPool       botpkg.WorkerPool
	Limiter            chan struct{}
	UploadLimiter      chan struct{}
	UploadQueue        chan uploadTask
	UploadWorkerCount  int
	UploadQueueSize    int
	UploadBot          *telego.Bot
	RateLimiter        *telegram.RateLimiter
	ResourceLimiter    *ResourceRateLimiter
	// queueMu protects queuedStatus/statusDirty state.
	queueMu           sync.RWMutex
	queuedStatus      []queuedStatus
	statusDirty       bool
	trackFetchGroup   singleflight.Group
	downloadInfoGroup singleflight.Group
	prepareGroup      singleflight.Group
	// prepareMu protects preparedInFlight map only.
	prepareMu        sync.Mutex
	preparedInFlight map[string]*preparedArtifactState
	// inlineMu protects inlineInFlight map only.
	inlineMu       sync.Mutex
	inlineInFlight map[string]*inlineProcessCall
	// downloadQueueMu protects downloadWaiting/downloadRunning accounting.
	downloadQueueMu sync.Mutex
	// downloadWaiting counts admitted tasks not yet holding a download slot
	// (waiting for a global slot and/or a per-platform serial gate). It is the
	// "total queue" surfaced to users and capped by DownloadQueueWaitLimit.
	downloadWaiting int
	// downloadRunning counts tasks actively holding a global download slot.
	downloadRunning int
	// downloadActive counts admitted download work items that have not yet
	// finished, including tasks waiting in DownloadPool, waiting for a slot,
	// actively downloading, preparing metadata, or uploading the result. The
	// per-user/per-chat/global admission limits below are enforced here so one
	// requester cannot fill the whole internal download queue.
	downloadActive       int
	downloadActiveByUser map[int64]int
	downloadActiveByChat map[int64]int

	// serialGateMu protects the serialGates map.
	serialGateMu sync.Mutex
	// serialGates holds one size-1 semaphore per platform that requires
	// serialized downloads (platform.SerialDownloadGate), e.g. Apple Music's
	// FairPlay wrapper which decrypts one track at a time. Lazily created.
	serialGates map[string]chan struct{}

	// DownloadQueueWaitLimit caps how many tasks may wait in the download queue
	// at once; the next request beyond it is rejected with a "server busy" error.
	// 0 or less means unlimited.
	DownloadQueueWaitLimit int
	// DownloadQueuePerUserLimit caps all not-yet-finished download work admitted
	// from one user. This is stricter than DownloadRateLimitPerUser because it
	// protects the live queue from being monopolized by one requester.
	DownloadQueuePerUserLimit int
	// DownloadQueuePerChatLimit caps live download work admitted from one chat.
	DownloadQueuePerChatLimit int
	// DownloadQueueGlobalLimit caps all live download work before it enters the
	// internal download worker pool. 0 or less means unlimited.
	DownloadQueueGlobalLimit int

	EnableQueueObservability bool
	PluginSettingDefinitions []botpkg.PluginSettingDefinition
}

type DownloadQueueSnapshot struct {
	Waiting          int
	Running          int
	WaitLimit        int
	Active           int
	ActiveLimit      int
	PerUserLimit     int
	PerChatLimit     int
	UploadWaiting    int
	UploadRunning    int
	UploadQueueLimit int
	UploadLimit      int
}

type uploadTask struct {
	ctx        context.Context
	cancel     context.CancelFunc
	enqueuedAt time.Time
	b          *telego.Bot
	statusBot  *telego.Bot
	statusMsg  *telego.Message
	message    *telego.Message
	songInfo   botpkg.SongInfo
	musicPath  string
	picPath    string
	cleanup    []string
	resultCh   chan uploadResult
	onDone     func(uploadResult)
	loc        *i18n.Localizer
	// cacheHit marks a task whose status message already shows the localized
	// "cache hit" text, so the worker must not overwrite it with "uploading".
	// Replaces the previous fragile strings.Contains(hitCache) check, which
	// broke once the text became language-dependent.
	cacheHit bool
}

type queuedStatus struct {
	bot      *telego.Bot
	message  *telego.Message
	songInfo botpkg.SongInfo
	loc      *i18n.Localizer
}

type uploadResult struct {
	message *telego.Message
	err     error
}

type inlineProcessCall struct {
	done chan struct{}
	song *botpkg.SongInfo
	err  error
}

type preparedArtifactState struct {
	waiters  int
	ready    bool
	artifact *preparedArtifact
}

type preparedArtifact struct {
	musicPath string
	picPath   string
	cleanup   []string
	info      preparedSongInfo
}

type preparedSongInfo struct {
	FileExt    string
	MusicSize  int
	BitRate    int
	Quality    string
	PicSize    int
	EmbPicSize int
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
	if h.UploadWorkerCount <= 0 {
		h.UploadWorkerCount = 1
	}
	if h.UploadQueue == nil {
		h.UploadQueue = make(chan uploadTask, h.UploadQueueSize)
		for i := 0; i < h.UploadWorkerCount; i++ {
			go h.runUploadWorker(ctx)
		}
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
				Repo:                     h.Repo,
				PlatformManager:          h.PlatformManager,
				RateLimiter:              h.RateLimiter,
				DefaultPlatform:          h.DefaultPlatform,
				DefaultQuality:           h.DefaultQuality,
				DefaultLyricFormat:       h.DefaultLyricFormat,
				PluginSettingDefinitions: h.PluginSettingDefinitions,
			}
			settingsHandler.Handle(ctx, b, update)
			return
		}
		if platformName, trackID, qualityOverride, ok := parseInlineStartParameter(args); ok {
			h.dispatch(ctx, b, message, platformName, trackID, qualityOverride)
			return
		}
		if platformName, trackID, ok := parseLyricStartParameter(args); ok {
			if h.LyricHandler != nil {
				requesterID := int64(0)
				if message.From != nil {
					requesterID = message.From.ID
				}
				h.LyricHandler.SendTrackLyrics(ctx, b, message.Chat.ID, message.MessageID, platformName, trackID, requesterID)
			}
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
				{Name: "reload", Description: tr(ctx, "help_admin_reload")},
				{Name: "rmcache", Description: tr(ctx, "help_admin_rmcache")},
			}, adminHelp...)
		}
		params := &telego.SendMessageParams{
			ChatID:             telego.ChatID{ID: message.Chat.ID},
			Text:               buildHelpText(ctx, h.PlatformManager, isAdmin, adminHelp, h.RecognizeEnabled, strings.EqualFold(strings.TrimSpace(message.Chat.Type), "private")),
			ParseMode:          telego.ModeMarkdownV2,
			LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
			ReplyParameters:    &telego.ReplyParameters{MessageID: message.MessageID},
		}
		if h.RateLimiter != nil {
			if _, err := telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params); err != nil && h.Logger != nil {
				h.Logger.Warn("failed to send help message", "chatID", message.Chat.ID, "error", err)
			}
		} else {
			if _, err := b.SendMessage(ctx, params); err != nil && h.Logger != nil {
				h.Logger.Warn("failed to send help message", "chatID", message.Chat.ID, "error", err)
			}
		}
		return
	}
	if cmd == "music" {
		args := commandArguments(message.Text)
		if strings.TrimSpace(args) == "" && message.ReplyToMessage != nil {
			// Reply with bare "/music": use the replied message's embedded link
			// (e.g. a bot-sent song message) or its text/caption as the query.
			args = repliedMessageQuery(message.ReplyToMessage)
		}
		if strings.TrimSpace(args) == "" {
			params := &telego.SendMessageParams{
				ChatID:          telego.ChatID{ID: message.Chat.ID},
				Text:            tr(ctx, "input_content"),
				ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
			}
			if h.RateLimiter != nil {
				if _, err := telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params); err != nil && h.Logger != nil {
					h.Logger.Warn("failed to send music usage prompt", "chatID", message.Chat.ID, "error", err)
				}
			} else {
				if _, err := b.SendMessage(ctx, params); err != nil && h.Logger != nil {
					h.Logger.Warn("failed to send music usage prompt", "chatID", message.Chat.ID, "error", err)
				}
			}
			return
		}
		if h.Playlist != nil {
			if h.Playlist.TryHandle(ctx, b, update) {
				return
			}
		}
		if h.Artist != nil {
			if h.Artist.TryHandle(ctx, b, update) {
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
			Text:            tr(ctx, "no_results"),
			ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
		}
		if h.RateLimiter != nil {
			if _, err := telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params); err != nil && h.Logger != nil {
				h.Logger.Warn("failed to send no-results message", "chatID", message.Chat.ID, "error", err)
			}
		} else {
			if _, err := b.SendMessage(ctx, params); err != nil && h.Logger != nil {
				h.Logger.Warn("failed to send no-results message", "chatID", message.Chat.ID, "error", err)
			}
		}
		return
	}
	if cmd != "" && cmd != "start" && cmd != "help" && cmd != "music" && h.PlatformManager != nil {
		if platformName, ok := resolvePlatformAlias(h.PlatformManager, cmd); ok {
			args := commandArguments(message.Text)
			if strings.TrimSpace(args) == "" {
				params := &telego.SendMessageParams{
					ChatID:          telego.ChatID{ID: message.Chat.ID},
					Text:            tr(ctx, "input_content"),
					ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
				}
				if h.RateLimiter != nil {
					if _, err := telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params); err != nil && h.Logger != nil {
						h.Logger.Warn("failed to send platform command usage prompt", "chatID", message.Chat.ID, "error", err)
					}
				} else {
					if _, err := b.SendMessage(ctx, params); err != nil && h.Logger != nil {
						h.Logger.Warn("failed to send platform command usage prompt", "chatID", message.Chat.ID, "error", err)
					}
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
			if resolvedPlatform, resolvedTrackID, ok := h.resolveTrackFromQuery(ctx, message, baseText+" "+platformName); ok {
				h.dispatch(ctx, b, message, resolvedPlatform, resolvedTrackID, qualityOverride)
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
	if !h.isPlatformAutoParseAllowed(ctx, message, platformName, trackID) {
		return
	}
	qualityOverride := extractQualityOverride(message, h.PlatformManager)

	h.dispatch(ctx, b, message, platformName, trackID, qualityOverride)
}

func (h *MusicHandler) isPlatformAutoParseAllowed(ctx context.Context, message *telego.Message, platformName, trackID string) bool {
	if h == nil || h.PlatformManager == nil {
		return true
	}
	platformName = strings.TrimSpace(platformName)
	if platformName == "" {
		return true
	}
	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		return true
	}
	decider, ok := plat.(platform.AutoParseDecider)
	if !ok {
		return true
	}
	settingKey := strings.TrimSpace(decider.AutoParseSettingKey())
	if settingKey == "" {
		return true
	}

	scopeType := botpkg.PluginScopeUser
	scopeID := int64(0)
	if message != nil && message.Chat.Type != "private" {
		scopeType = botpkg.PluginScopeGroup
		scopeID = message.Chat.ID
	} else if message != nil && message.From != nil {
		scopeID = message.From.ID
	}

	mode := ""
	if h.Repo != nil && scopeID != 0 {
		stored, err := h.Repo.GetPluginSetting(ctx, scopeType, scopeID, platformName, settingKey)
		if err == nil {
			mode = strings.TrimSpace(stored)
		}
	}
	if mode == "" {
		if def, ok := h.findPluginSettingDefinition(platformName, settingKey); ok {
			mode = def.DefaultForScope(scopeType)
		}
	}
	allowed, err := decider.ShouldAutoParse(ctx, trackID, mode)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Warn("platform auto-parse decision failed", "platform", platformName, "trackID", trackID, "error", err)
		}
		return false
	}
	return allowed
}

func (h *MusicHandler) findPluginSettingDefinition(plugin string, key string) (botpkg.PluginSettingDefinition, bool) {
	plugin = strings.TrimSpace(plugin)
	key = strings.TrimSpace(key)
	for _, def := range h.PluginSettingDefinitions {
		if strings.TrimSpace(def.Plugin) == plugin && strings.TrimSpace(def.Key) == key {
			return def, true
		}
	}
	return botpkg.PluginSettingDefinition{}, false
}

func (h *MusicHandler) dispatch(ctx context.Context, b *telego.Bot, message *telego.Message, platformName, trackID string, qualityOverride string) bool {
	userID, chatID := downloadRequestIdentity(message)
	releaseAdmission, admitted := h.enterDownloadWork(userID, chatID)
	if !admitted {
		h.notifyDownloadRejected(ctx, b, message, errDownloadQueueOverloaded)
		return false
	}

	processCtx, cancel := h.processContext(detachContext(ctx))
	pool := h.DownloadPool
	if pool == nil {
		pool = h.Pool
	}
	if pool == nil {
		go func() {
			defer cancel()
			defer releaseAdmission()
			_ = h.processMusic(processCtx, b, message, platformName, trackID, qualityOverride)
		}()
		return true
	}

	go func() {
		if err := pool.Submit(func() {
			defer cancel()
			defer releaseAdmission()
			defer func() {
				if err := recover(); err != nil {
					if h.Logger != nil {
						h.Logger.Error("music task panic", "platform", platformName, "trackID", trackID, "error", err)
					}
				}
			}()
			_ = h.processMusic(processCtx, b, message, platformName, trackID, qualityOverride)
		}); err != nil {
			cancel()
			releaseAdmission()
			if h.Logger != nil {
				h.Logger.Error("failed to enqueue music task", "platform", platformName, "trackID", trackID, "error", err)
			}
			h.notifyDownloadRejected(ctx, b, message, errDownloadQueueOverloaded)
		}
	}()
	return true
}

func (h *MusicHandler) submitInlineDownloadWork(ctx context.Context, userID, chatID int64, task func(context.Context), onRejected func(context.Context)) bool {
	if h == nil || task == nil {
		return false
	}
	releaseAdmission, admitted := h.enterDownloadWork(userID, chatID)
	if !admitted {
		if onRejected != nil {
			onRejected(ctx)
		}
		return false
	}

	processCtx, cancel := h.processContext(detachContext(ctx))
	run := func() {
		defer cancel()
		defer releaseAdmission()
		task(withDownloadWorkAdmission(processCtx))
	}

	pool := h.DownloadPool
	if pool == nil {
		pool = h.Pool
	}
	if pool == nil {
		go run()
		return true
	}

	rejectCtx := detachContext(ctx)
	go func() {
		if err := pool.Submit(run); err != nil {
			cancel()
			releaseAdmission()
			if h.Logger != nil {
				h.Logger.Error("failed to enqueue inline download task", "error", err)
			}
			if onRejected != nil {
				onRejected(rejectCtx)
			}
		}
	}()
	return true
}

func downloadRequestIdentity(message *telego.Message) (userID, chatID int64) {
	if message == nil {
		return 0, 0
	}
	chatID = message.Chat.ID
	if message.From != nil {
		userID = message.From.ID
	}
	return userID, chatID
}

func (h *MusicHandler) notifyDownloadRejected(ctx context.Context, b *telego.Bot, message *telego.Message, err error) {
	if h == nil || b == nil || message == nil {
		return
	}
	silent := h.shouldSilentAutoFetch(message)
	if isForceNonSilent(ctx) {
		silent = false
	}
	if silent {
		return
	}
	text := userVisibleDownloadError(ctx, err)
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		MessageThreadID: message.MessageThreadID,
		Text:            text,
		ReplyParameters: buildReplyParams(message),
	}
	if h.RateLimiter != nil {
		if _, sendErr := telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params); sendErr != nil && h.Logger != nil {
			h.Logger.Warn("failed to send download rejection", "chatID", message.Chat.ID, "error", sendErr)
		}
		return
	}
	if _, sendErr := b.SendMessage(ctx, params); sendErr != nil && h.Logger != nil {
		h.Logger.Warn("failed to send download rejection", "chatID", message.Chat.ID, "error", sendErr)
	}
}

func (h *MusicHandler) processContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := defaultMusicProcessTimeout
	if h != nil && h.ProcessTimeout > 0 {
		timeout = h.ProcessTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (h *MusicHandler) processMusic(ctx context.Context, b *telego.Bot, message *telego.Message, platformName, trackID string, qualityOverride string) error {
	if message == nil {
		return errors.New("message required")
	}
	if replacementPlatform, replacementTrackID, hijacked, replacementLabel := maybeApplyAprilFoolsTrackHijack(platformName, trackID); hijacked {
		if h != nil && h.Logger != nil {
			h.Logger.Info("april fools hijacked download request", "from_platform", platformName, "from_track_id", trackID, "to_platform", replacementPlatform, "to_track_id", replacementTrackID, "replacement", replacementLabel)
		}
		platformName = replacementPlatform
		trackID = replacementTrackID
	}

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
	status := newStatusSession(ctx, b, h.RateLimiter, message.Chat.ID, threadID, replyParams)

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
		text := buildMusicInfoText(ctx, songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), userVisibleDownloadError(ctx, err))
		status.Edit(text)
	}
	handleInvalidCachedFileID := func(err error, cacheQuality string) bool {
		if !isTelegramFileIDInvalid(err) {
			return false
		}
		if h.Logger != nil {
			h.Logger.Warn("cached telegram file id invalid, fallback to redownload", "platform", platformName, "trackID", trackID, "quality", cacheQuality, "error", err)
		}
		if h.Repo != nil {
			_ = h.Repo.DeleteByPlatformTrackID(ctx, platformName, trackID, cacheQuality)
		}
		songInfo.FileID = ""
		songInfo.ThumbFileID = ""
		return true
	}

	var userID int64
	if message.From != nil {
		userID = message.From.ID
	}

	quality := h.resolveRequestedQuality(ctx, message, userID, platformName, qualityOverride)

	qualityStr := quality.String()
	if handled, err := h.tryPresentDirectEpisodes(ctx, b, message, platformName, trackID, qualityStr); handled {
		return err
	}

	if cachedInfo, sent, err := h.trySendCachedTrack(ctx, b, status, message, platformName, trackID, qualityStr, false, getCached, handleInvalidCachedFileID); err != nil {
		songInfo = cachedInfo
		sendFailed(err)
		return err
	} else if sent {
		return nil
	}

	// Throttle genuine downloads (cache misses only — both cache checks above
	// return early). A download fans out into GetTrack + GetDownloadInfo (stream
	// URL resolve / decrypt, expensive for Apple/Spotify) plus the transfer, so
	// it is the heaviest per-user platform op and is rate-limited per user /
	// platform / global. Cache hits never reach here and stay free.
	if !h.ResourceLimiter.AllowFor(ActionDownload, userID, message.Chat.ID, platformName) {
		err := platform.ErrRateLimited
		sendFailed(err)
		return err
	}

	if !silent {
		status.Upsert(tr(ctx, "fetch_info"))
	}

	// Show a single static "queued" notice (with a button to check live counts)
	// the moment the task has to wait — not a per-change refresh, which would
	// rewrite every waiter's message and risk Telegram's edit rate limit.
	onQueued := func() {
		if silent || status.Message() == nil {
			return
		}
		status.EditWithMarkup(tr(ctx, "download_queued"), downloadQueueButton(ctx))
	}
	releaseDownloadSlot, err := h.acquireDownloadSlot(ctx, platformName, trackID, quality, onQueued)
	if err != nil {
		sendFailed(err)
		return err
	}
	defer releaseDownloadSlot()

	if cachedInfo, sent, err := h.trySendCachedTrack(ctx, b, status, message, platformName, trackID, qualityStr, silent, getCached, handleInvalidCachedFileID); err != nil {
		songInfo = cachedInfo
		sendFailed(err)
		return err
	} else if sent {
		return nil
	}

	if h.PlatformManager == nil {
		return errors.New("platform manager not configured")
	}

	track, plat, resolvedPlatform, resolvedTrackID, err := h.loadTrackWithFallback(ctx, message, status, platformName, trackID)
	if err != nil {
		return err
	}
	platformName = resolvedPlatform
	trackID = resolvedTrackID

	fillSongInfoFromTrack(&songInfo, track, platformName, trackID, message)
	info, err := h.loadDownloadInfo(ctx, status, platformName, trackID, quality)
	if err != nil {
		return err
	}

	actualQuality := info.Quality.String()
	if actualQuality == "unknown" || actualQuality == "" {
		actualQuality = qualityStr
	}
	if songInfo.Quality == "" {
		songInfo.Quality = actualQuality
		songInfo.QualityVerified = true
	}
	songInfo.FileExt = info.Format
	songInfo.MusicSize = 0
	songInfo.BitRate = info.Bitrate * 1000

	if actualQuality != qualityStr {
		if cachedInfo, sent, err := h.trySendCachedTrack(ctx, b, status, message, platformName, trackID, actualQuality, silent, getCached, handleInvalidCachedFileID); err != nil {
			songInfo = cachedInfo
			sendFailed(err)
			return err
		} else if sent {
			return nil
		}
	}

	status.Edit(buildMusicInfoText(ctx, songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), tr(ctx, "downloading")))

	musicPath, picPath, releasePrepared, err := h.acquirePreparedMedia(ctx, platformName, trackID, actualQuality, plat, track, info, status.Message(), b, message, &songInfo, nil)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Error("failed to download and prepare", "platform", platformName, "trackID", trackID, "error", err)
		}
		sendFailed(err)
		return err
	}

	status.Edit(buildMusicInfoText(ctx, songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), tr(ctx, "uploading")))

	if err := h.sendMusic(ctx, b, status.Message(), message, &songInfo, musicPath, picPath, nil, releasePrepared, platformName, trackID); err != nil {
		if releasePrepared != nil {
			releasePrepared()
		}
		sendFailed(err)
		return err
	}

	return nil
}

func (h *MusicHandler) tryPresentDirectEpisodes(ctx context.Context, b *telego.Bot, message *telego.Message, platformName, trackID, qualityValue string) (bool, error) {
	if h == nil || h.PlatformManager == nil || b == nil || message == nil {
		return false, nil
	}
	baseTrackID, _, hasExplicitPage, ok := parseEpisodeTrackID(h.PlatformManager, platformName, trackID)
	if !ok || hasExplicitPage || strings.TrimSpace(baseTrackID) == "" {
		return false, nil
	}
	plat := h.PlatformManager.Get(strings.TrimSpace(platformName))
	if plat == nil {
		return false, nil
	}
	if _, ok := plat.(platform.EpisodeTrackIDResolver); !ok {
		return false, nil
	}
	provider, ok := plat.(platform.EpisodeProvider)
	if !ok {
		return false, nil
	}
	var episodeUserID int64
	if message.From != nil {
		episodeUserID = message.From.ID
	}
	if !h.ResourceLimiter.AllowFor(ActionEpisode, episodeUserID, message.Chat.ID, platformName) {
		return false, nil
	}
	episodes, err := provider.ListEpisodes(ctx, baseTrackID)
	if err != nil || len(episodes) <= 1 {
		return false, nil
	}
	requesterID := int64(0)
	if message.From != nil {
		requesterID = message.From.ID
	}
	text, keyboard := buildEpisodePickerPage(ctx, platformName, baseTrackID, qualityValue, requesterID, episodes, 1, "")
	if strings.TrimSpace(text) == "" || keyboard == nil {
		return false, nil
	}
	params := &telego.SendMessageParams{
		ChatID: telego.ChatID{ID: message.Chat.ID},
		Text:   text,
		ReplyParameters: &telego.ReplyParameters{
			MessageID: message.MessageID,
		},
		ParseMode:          telego.ModeMarkdownV2,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
		ReplyMarkup:        keyboard,
	}
	if message.MessageThreadID != 0 {
		params.MessageThreadID = message.MessageThreadID
	}
	if h.RateLimiter != nil {
		_, err = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, err = b.SendMessage(ctx, params)
	}
	if err != nil {
		return true, err
	}
	return true, nil
}

func (h *MusicHandler) resolveRequestedQuality(ctx context.Context, message *telego.Message, userID int64, platformName, qualityOverride string) platform.Quality {
	quality := platform.QualityHigh
	scopeType := botpkg.PluginScopeUser
	scopeID := userID
	if h != nil && h.Repo != nil {
		if message != nil && message.Chat.Type != "private" {
			scopeType = botpkg.PluginScopeGroup
			scopeID = message.Chat.ID
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
		return quality
	}
	if q, err := platform.ParseQuality(resolvePlatformQualityValue(ctx, h.Repo, scopeType, scopeID, platformName, quality.String(), false)); err == nil {
		quality = q
	}
	return quality
}

func (h *MusicHandler) trySendCachedTrack(
	ctx context.Context,
	b *telego.Bot,
	status *statusSession,
	message *telego.Message,
	platformName, trackID, cacheQuality string,
	silent bool,
	getCached func(platformName, trackID, quality string) (*botpkg.SongInfo, error),
	onInvalidCachedFileID func(err error, cacheQuality string) bool,
) (botpkg.SongInfo, bool, error) {
	// Return values:
	//   songInfo: resolved cached song info (non-zero when cache record exists)
	//   sent:     true when message has been successfully sent and caller should return
	//   err:      terminal error for caller-side reporting
	if h == nil || h.Repo == nil || getCached == nil {
		return botpkg.SongInfo{}, false, nil
	}

	cached, err := getCached(platformName, trackID, cacheQuality)
	if err != nil || cached == nil {
		return botpkg.SongInfo{}, false, nil
	}
	if cached.FileID == "" {
		_ = h.Repo.DeleteByPlatformTrackID(ctx, platformName, trackID, cacheQuality)
		return botpkg.SongInfo{}, false, nil
	}

	songInfo := *cached
	if h != nil {
		h.refreshCachedSongLinks(ctx, &songInfo)
		verifyCachedNeteaseQuality(ctx, h.PlatformManager, h.Repo, h.Logger, &songInfo, platformName, trackID, cacheQuality)
	}
	if !silent {
		status.Upsert(buildMusicInfoText(ctx, songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), tr(ctx, "hit_cache")))
	}
	if err := h.sendMusic(ctx, b, status.Message(), message, &songInfo, "", "", nil, nil, platformName, trackID); err != nil {
		if onInvalidCachedFileID != nil && onInvalidCachedFileID(err, cacheQuality) {
			return songInfo, false, nil
		}
		return songInfo, false, err
	}

	return songInfo, true, nil
}

func (h *MusicHandler) refreshCachedSongLinks(ctx context.Context, songInfo *botpkg.SongInfo) {
	if h == nil || h.PlatformManager == nil || h.Repo == nil || songInfo == nil {
		return
	}
	if strings.TrimSpace(songInfo.Platform) != "kugou" || strings.TrimSpace(songInfo.TrackID) == "" {
		return
	}
	if !needsKugouLinkRefresh(songInfo) {
		return
	}
	track, err := h.getTrackSingleflight(ctx, songInfo.Platform, songInfo.TrackID)
	if err != nil || track == nil {
		return
	}
	fillSongInfoFromTrack(songInfo, track, songInfo.Platform, songInfo.TrackID, nil)
	_ = h.Repo.Create(ctx, songInfo)
}

func needsKugouLinkRefresh(songInfo *botpkg.SongInfo) bool {
	if songInfo == nil {
		return false
	}
	if strings.TrimSpace(songInfo.Platform) != "kugou" {
		return false
	}
	trackURL := strings.TrimSpace(songInfo.TrackURL)
	artistURLs := strings.TrimSpace(songInfo.SongArtistsURLs)
	albumURL := strings.TrimSpace(songInfo.AlbumURL)

	if trackURL == "" || strings.Contains(trackURL, "www.kugou.com/song/#hash=") {
		return true
	}
	if albumURL == "" && strings.TrimSpace(songInfo.SongAlbum) != "" {
		return true
	}
	if artistURLs == "" {
		return true
	}
	for _, artistURL := range strings.Split(artistURLs, ",") {
		artistURL = strings.TrimSpace(artistURL)
		if artistURL == "" || strings.Contains(artistURL, "m.kugou.com/singer/info/") {
			return true
		}
	}
	return false
}

func (h *MusicHandler) loadTrackWithFallback(ctx context.Context, message *telego.Message, status *statusSession, platformName, trackID string) (*platform.Track, platform.Platform, string, string, error) {
	if h == nil || h.PlatformManager == nil {
		status.Edit(tr(ctx, "fetch_info_failed"))
		return nil, nil, platformName, trackID, errors.New("platform manager not configured")
	}

	for {
		plat := h.PlatformManager.Get(platformName)
		if plat == nil {
			if h.Logger != nil {
				h.Logger.Error("platform not found", "platform", platformName)
			}
			status.Edit(tr(ctx, "fetch_info_failed"))
			return nil, nil, platformName, trackID, fmt.Errorf("platform not found: %s", platformName)
		}

		track, err := h.getTrackSingleflight(ctx, platformName, trackID)
		if err == nil {
			return track, plat, platformName, trackID, nil
		}
		if errors.Is(err, platform.ErrNotFound) && !isFallbackDisabled(ctx) {
			if nextPlatform, nextTrackID, ok := h.resolveFallbackTrack(ctx, message, platformName, trackID); ok {
				platformName = nextPlatform
				trackID = nextTrackID
				status.Edit(tr(ctx, "fetch_info"))
				continue
			}
		}
		if h.Logger != nil {
			h.Logger.Error("failed to get track", "platform", platformName, "trackID", trackID, "error", err)
		}
		status.Edit(tr(ctx, "fetch_info_failed"))
		return nil, nil, platformName, trackID, err
	}
}

func (h *MusicHandler) loadDownloadInfo(ctx context.Context, status *statusSession, platformName, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	info, err := h.getDownloadInfoSingleflight(ctx, platformName, trackID, quality)
	if err != nil {
		if h != nil && h.Logger != nil {
			h.Logger.Error("failed to get download info", "platform", platformName, "trackID", trackID, "error", err)
		}
		status.Edit(tr(ctx, "fetch_info_failed"))
		return nil, err
	}
	if info == nil || info.URL == "" {
		status.Edit(tr(ctx, "fetch_info_failed"))
		return nil, errors.New("download info unavailable")
	}
	if h != nil && h.Logger != nil {
		h.Logger.Debug("download url", "platform", platformName, "trackID", trackID, "quality", info.Quality.String(), "url", info.URL)
	}
	if info.Format == "" {
		info.Format = "mp3"
	}
	return info, nil
}

func (h *MusicHandler) getTrackSingleflight(ctx context.Context, platformName, trackID string) (*platform.Track, error) {
	if h == nil || h.PlatformManager == nil {
		return nil, errors.New("platform manager not configured")
	}
	key := fmt.Sprintf("track:%s:%s", platformName, trackID)

	value, err, _ := h.trackFetchGroup.Do(key, func() (interface{}, error) {
		plat := h.PlatformManager.Get(platformName)
		if plat == nil {
			return nil, fmt.Errorf("platform not found: %s", platformName)
		}
		track, fetchErr := plat.GetTrack(ctx, trackID)
		if track == nil && fetchErr == nil {
			return nil, errors.New("invalid track result")
		}
		return track, fetchErr
	})
	if err != nil {
		return nil, err
	}
	track, ok := value.(*platform.Track)
	if !ok || track == nil {
		return nil, errors.New("invalid track result")
	}
	return track, nil
}

func (h *MusicHandler) getDownloadInfoSingleflight(ctx context.Context, platformName, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if h == nil || h.PlatformManager == nil {
		return nil, errors.New("platform manager not configured")
	}
	key := fmt.Sprintf("download_info:%s:%s:%s", platformName, trackID, quality.String())

	value, err, _ := h.downloadInfoGroup.Do(key, func() (interface{}, error) {
		plat := h.PlatformManager.Get(platformName)
		if plat == nil {
			return nil, fmt.Errorf("platform not found: %s", platformName)
		}
		info, fetchErr := plat.GetDownloadInfo(ctx, trackID, quality)
		if fetchErr != nil {
			return nil, fetchErr
		}
		if info == nil {
			return nil, errors.New("invalid download info result")
		}
		return cloneDownloadInfo(info), nil
	})
	if err != nil {
		return nil, err
	}
	info, ok := value.(*platform.DownloadInfo)
	if !ok || info == nil {
		return nil, errors.New("invalid download info result")
	}
	return cloneDownloadInfo(info), nil
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

func capturePreparedSongInfo(songInfo *botpkg.SongInfo) preparedSongInfo {
	if songInfo == nil {
		return preparedSongInfo{}
	}
	return preparedSongInfo{
		FileExt:    songInfo.FileExt,
		MusicSize:  songInfo.MusicSize,
		BitRate:    songInfo.BitRate,
		Quality:    songInfo.Quality,
		PicSize:    songInfo.PicSize,
		EmbPicSize: songInfo.EmbPicSize,
	}
}

func applyPreparedSongInfo(songInfo *botpkg.SongInfo, prepared preparedSongInfo) {
	if songInfo == nil {
		return
	}
	songInfo.FileExt = prepared.FileExt
	songInfo.MusicSize = prepared.MusicSize
	songInfo.BitRate = prepared.BitRate
	if strings.TrimSpace(songInfo.Quality) == "" {
		songInfo.Quality = prepared.Quality
		songInfo.QualityVerified = true
	}
	songInfo.PicSize = prepared.PicSize
	songInfo.EmbPicSize = prepared.EmbPicSize
}

func normalizeCleanupPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		result = append(result, p)
	}
	return result
}

func (h *MusicHandler) releasePreparedWaiter(key string) {
	if h == nil {
		return
	}
	var cleanup []string
	h.prepareMu.Lock()
	state, ok := h.preparedInFlight[key]
	if !ok {
		h.prepareMu.Unlock()
		return
	}
	if state.waiters > 0 {
		state.waiters--
	}
	if state.waiters == 0 && state.ready {
		if state.artifact != nil {
			cleanup = append(cleanup, state.artifact.cleanup...)
		}
		delete(h.preparedInFlight, key)
	}
	h.prepareMu.Unlock()
	if len(cleanup) > 0 {
		cleanupFiles(cleanup...)
	}
}

func (h *MusicHandler) acquirePreparedMedia(
	ctx context.Context,
	platformName, trackID, quality string,
	plat platform.Platform,
	track *platform.Track,
	info *platform.DownloadInfo,
	msg *telego.Message,
	b *telego.Bot,
	message *telego.Message,
	songInfo *botpkg.SongInfo,
	externalProgress func(written, total int64),
) (string, string, func(), error) {
	if h == nil {
		return "", "", nil, errors.New("music handler not configured")
	}
	key := fmt.Sprintf("prepared:%s:%s:%s", strings.TrimSpace(platformName), strings.TrimSpace(trackID), strings.TrimSpace(quality))

	h.prepareMu.Lock()
	if h.preparedInFlight == nil {
		h.preparedInFlight = make(map[string]*preparedArtifactState)
	}
	state := h.preparedInFlight[key]
	if state == nil {
		state = &preparedArtifactState{}
		h.preparedInFlight[key] = state
	}
	state.waiters++
	h.prepareMu.Unlock()

	resultCh := h.prepareGroup.DoChan(key, func() (interface{}, error) {
		sharedCtx := detachContext(ctx)
		localSongInfo := botpkg.SongInfo{}
		if songInfo != nil {
			localSongInfo = *songInfo
		}
		musicPath, picPath, cleanupList, downloadErr := h.downloadAndPrepareFromPlatform(sharedCtx, plat, track, trackID, cloneDownloadInfo(info), msg, b, message, &localSongInfo, externalProgress)

		artifact := &preparedArtifact{}
		if downloadErr == nil {
			artifact.musicPath = musicPath
			artifact.picPath = picPath
			artifact.info = capturePreparedSongInfo(&localSongInfo)
			artifact.cleanup = normalizeCleanupPaths(append(cleanupList, musicPath, picPath))
		} else {
			cleanupFiles(normalizeCleanupPaths(append(cleanupList, musicPath, picPath))...)
			artifact = nil
		}

		var cleanupNow []string
		h.prepareMu.Lock()
		state := h.preparedInFlight[key]
		if state == nil {
			state = &preparedArtifactState{}
			h.preparedInFlight[key] = state
		}
		state.ready = true
		state.artifact = artifact
		if state.waiters == 0 {
			if artifact != nil {
				cleanupNow = append(cleanupNow, artifact.cleanup...)
			}
			delete(h.preparedInFlight, key)
		}
		h.prepareMu.Unlock()
		if len(cleanupNow) > 0 {
			cleanupFiles(cleanupNow...)
		}

		if downloadErr != nil {
			return nil, downloadErr
		}
		return artifact, nil
	})

	select {
	case <-ctx.Done():
		h.releasePreparedWaiter(key)
		return "", "", nil, ctx.Err()
	case result := <-resultCh:
		if result.Err != nil {
			h.releasePreparedWaiter(key)
			return "", "", nil, result.Err
		}
		artifact, ok := result.Val.(*preparedArtifact)
		if !ok || artifact == nil {
			h.releasePreparedWaiter(key)
			return "", "", nil, errors.New("invalid prepared artifact result")
		}
		if err := ctx.Err(); err != nil {
			h.releasePreparedWaiter(key)
			return "", "", nil, err
		}
		applyPreparedSongInfo(songInfo, artifact.info)
		var releaseOnce sync.Once
		releaseFn := func() {
			releaseOnce.Do(func() {
				h.releasePreparedWaiter(key)
			})
		}
		return artifact.musicPath, artifact.picPath, releaseFn, nil
	}
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
			candidate := strings.TrimSpace(strings.Join(fields[1:], " "))
			if trackID, matched := matchPlatformTrack(ctx, h.PlatformManager, platformName, candidate); matched {
				return platformName, trackID, true
			}
			if candidate == "" {
				return "", "", false
			}
			baseText = candidate
			platformSuffix = platformName
			fields = strings.Fields(baseText)
		}
	}
	if platformSuffix != "" && len(fields) == 1 {
		if h.PlatformManager.Get(platformSuffix) != nil && !isBareNumericText(fields[0]) {
			if trackID, matched := matchPlatformTrack(ctx, h.PlatformManager, platformSuffix, fields[0]); matched {
				return platformSuffix, trackID, true
			}
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

	if plat, id, matched := matchTextTrack(h.PlatformManager, resolvedText); matched {
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
	if h.PlatformManager != nil {
		if h.PlatformManager.Get(text) != nil {
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
	songInfo.MusicSize = 0
	songInfo.BitRate = info.Bitrate * 1000
	if songInfo.Quality == "" {
		songInfo.Quality = info.Quality.String()
		songInfo.QualityVerified = true
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
			text = tr(ctx, "downloading_named", map[string]any{"Title": track.Title, "WrittenMB": fmt.Sprintf("%.2f", writtenMB)})
		} else {
			if songInfo != nil && total > 0 {
				songInfo.MusicSize = int(total)
			}
			totalMB := float64(total) / 1024 / 1024
			progressPct := float64(written) * 100 / float64(total)
			text = tr(ctx, "downloading_progress", map[string]any{"Title": track.Title, "Percent": fmt.Sprintf("%.2f", progressPct), "WrittenMB": fmt.Sprintf("%.2f", writtenMB), "TotalMB": fmt.Sprintf("%.2f", totalMB)})
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
			if editedMsg, err := telegram.EditMessageTextBestEffort(ctx, h.RateLimiter, b, editParams); err == nil {
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
	if h != nil && h.Logger != nil {
		h.Logger.Debug("prepared media downloaded", "initial_path", filePath, "initial_ext", songInfo.FileExt, "info_format", info.Format)
	}
	filePath, songInfo.FileExt = normalizeExtractedAudioPath(filePath, songInfo.FileExt)
	if songInfo.FileExt != "" {
		info.Format = songInfo.FileExt
	}
	if h != nil && h.Logger != nil {
		h.Logger.Debug("prepared media normalized", "normalized_path", filePath, "normalized_ext", songInfo.FileExt, "info_format", info.Format)
	}

	// Derive bitrate from actual file size + duration (from track or FLAC streaminfo)
	deriveBitrateFromFile(filePath, songInfo)

	picPath, resizePicPath := h.prepareCoverFiles(ctx, track, trackID, stamp, songInfo, &cleanupList)

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
	if h != nil && h.Logger != nil {
		h.Logger.Debug("prepared media final path", "final_path", filePath, "final_ext", songInfo.FileExt)
	}
	cleanupList = append(cleanupList, filePath, finalDir)

	h.embedTrackTags(ctx, plat, track, trackID, info, filePath, embedPicPath)

	return filePath, thumbPicPath, cleanupList, nil
}

func (h *MusicHandler) prepareCoverFiles(ctx context.Context, track *platform.Track, trackID string, stamp int64, songInfo *botpkg.SongInfo, cleanupList *[]string) (string, string) {
	if h == nil || track == nil || h.DownloadService == nil {
		return "", ""
	}
	coverURL := ""
	if track.CoverURL != "" {
		coverURL = track.CoverURL
	} else if track.Album != nil && track.Album.CoverURL != "" {
		coverURL = track.Album.CoverURL
	}
	if coverURL == "" {
		return "", ""
	}

	picPath := filepath.Join(h.CacheDir, fmt.Sprintf("%d-%s", stamp, path.Base(coverURL)))
	if _, err := h.DownloadService.Download(ctx, &platform.DownloadInfo{URL: coverURL, Size: 0}, picPath, nil); err != nil {
		if h.Logger != nil {
			h.Logger.Warn("failed to download cover", "track", trackID, "url", coverURL, "error", err)
		}
		return "", ""
	}

	stat, statErr := os.Stat(picPath)
	if statErr != nil || stat.Size() <= 0 {
		if h.Logger != nil {
			if statErr != nil {
				h.Logger.Warn("failed to stat cover file", "track", trackID, "error", statErr)
			} else {
				h.Logger.Warn("cover file is empty", "track", trackID)
			}
		}
		_ = os.Remove(picPath)
		return "", ""
	}

	songInfo.PicSize = int(stat.Size())
	if cleanupList != nil {
		*cleanupList = append(*cleanupList, picPath)
	}

	resizePicPath := ""
	if resized, err := resizeImg(picPath); err == nil {
		resizePicPath = resized
		if cleanupList != nil {
			*cleanupList = append(*cleanupList, resizePicPath)
		}
	} else if h.Logger != nil {
		h.Logger.Warn("failed to resize cover image", "track", trackID, "error", err)
	}

	return picPath, resizePicPath
}

func (h *MusicHandler) embedTrackTags(ctx context.Context, plat platform.Platform, track *platform.Track, trackID string, info *platform.DownloadInfo, filePath, embedPicPath string) {
	if h == nil || h.ID3Service == nil {
		return
	}

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
	if tagData == nil {
		return
	}
	if err := h.ID3Service.EmbedTags(filePath, tagData, embedPicPath); err != nil && h.Logger != nil {
		errText := strings.ToLower(strings.TrimSpace(err.Error()))
		if strings.Contains(errText, "unsupported ftyp") || strings.Contains(errText, "unsupported audio format for tags") {
			h.Logger.Warn("skip unsupported tag embedding", "platform", plat.Name(), "trackID", trackID, "error", err)
			return
		}
		h.Logger.Error("failed to embed tags", "platform", plat.Name(), "trackID", trackID, "error", err)
	}
}

func (h *MusicHandler) sendMusic(ctx context.Context, b *telego.Bot, statusMsg *telego.Message, message *telego.Message, songInfo *botpkg.SongInfo, musicPath, picPath string, cleanup []string, cleanupDone func(), platformName, trackID string) error {
	if h == nil {
		return errors.New("music handler not configured")
	}

	reqLoc := i18n.From(ctx)
	h.registerQueuedStatus(b, statusMsg, songInfo, reqLoc)

	baseCtx := detachContext(ctx)
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	resultCh := make(chan uploadResult, 1)
	uploadCtx, cancel := context.WithCancel(baseCtx)
	cleanupCtx := detachContext(baseCtx)
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
		ctx:        uploadCtx,
		cancel:     cancel,
		enqueuedAt: time.Now(),
		b:          uploadBot,
		statusBot:  statusBot,
		statusMsg:  statusMsg,
		message:    message,
		songInfo:   songCopy,
		musicPath:  musicPath,
		picPath:    picPath,
		cleanup:    cleanupCopy,
		resultCh:   resultCh,
		loc:        reqLoc,
		cacheHit:   musicPath == "",
		onDone: func(result uploadResult) {
			if result.message != nil && result.message.Audio != nil {
				songCopy.FileID = result.message.Audio.FileID
				if result.message.Audio.Thumbnail != nil {
					songCopy.ThumbFileID = result.message.Audio.Thumbnail.FileID
				}
			}
			if h.Repo != nil && result.err == nil && songCopy.FileID != "" {
				if err := h.Repo.Create(cleanupCtx, &songCopy); err != nil {
					if h.Logger != nil {
						h.Logger.Error("failed to save song info", "platform", platformName, "trackID", trackID, "error", err)
					}
				}
				if err := h.Repo.IncrementSendCount(cleanupCtx); err != nil {
					if h.Logger != nil {
						h.Logger.Error("failed to update send count", "error", err)
					}
				}
			}
			if statusMessage != nil && taskMessage != nil {
				if result.err == nil {
					if err := statusBot.DeleteMessage(cleanupCtx, &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: taskMessage.Chat.ID}, MessageID: statusMessage.MessageID}); err != nil && h.Logger != nil {
						h.Logger.Warn("failed to delete status message", "chatID", taskMessage.Chat.ID, "messageID", statusMessage.MessageID, "error", err)
					}
				} else {
					if h.Logger != nil {
						h.Logger.Error("upload worker failed", "platform", platformName, "trackID", trackID, "error", result.err)
					}
					statusMessage = editMessageTextOrSend(cleanupCtx, statusBot, h.RateLimiter, statusMessage, taskMessage.Chat.ID, buildMusicInfoText(cleanupCtx, songCopy.SongName, songCopy.SongAlbum, formatFileInfo(songCopy.FileExt, songCopy.MusicSize), userVisibleDownloadError(cleanupCtx, result.err)))
				}
			}
			cleanupFiles(cleanupCopy...)
			if cleanupDone != nil {
				cleanupDone()
			}
		},
	}
	select {
	case h.UploadQueue <- task:
		if h.Logger != nil && h.EnableQueueObservability {
			h.Logger.Debug("upload task enqueued", "platform", platformName, "trackID", trackID, "queue_len", len(h.UploadQueue), "queue_cap", cap(h.UploadQueue))
		}
		return nil
	default:
		cancel()
		if h.Logger != nil && h.EnableQueueObservability {
			h.Logger.Warn("upload queue full", "platform", platformName, "trackID", trackID, "queue_len", len(h.UploadQueue), "queue_cap", cap(h.UploadQueue))
		}
		if cleanupDone != nil {
			cleanupDone()
		}
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
			// 兜底 recover：processUploadTask 内部已有 panic 防护并完成收尾，
			// 这里再加一层确保任何意外都不会让唯一的上传 worker goroutine 退出。
			func() {
				defer func() {
					if r := recover(); r != nil && h.Logger != nil {
						h.Logger.Error("upload worker recovered from panic", "error", r, "stack", string(debug.Stack()))
					}
				}()
				h.processUploadTask(task)
			}()
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
			h.queueMu.RLock()
			dirty := h.statusDirty
			h.queueMu.RUnlock()
			if dirty {
				h.queueMu.Lock()
				if h.statusDirty {
					h.statusDirty = false
					shouldRefresh = true
				}
				h.queueMu.Unlock()
			}
			if shouldRefresh {
				h.refreshQueuedStatuses(ctx)
			}
		}
	}
}

func (h *MusicHandler) processUploadTask(task uploadTask) {
	if h != nil && h.Logger != nil && h.EnableQueueObservability && !task.enqueuedAt.IsZero() {
		wait := time.Since(task.enqueuedAt)
		if wait > 2*time.Second {
			h.Logger.Warn("upload task waited in queue", "wait_ms", wait.Milliseconds(), "queue_len", len(h.UploadQueue), "queue_cap", cap(h.UploadQueue))
		}
	}

	h.dequeueQueuedStatus(task.statusMsg)

	// finalize 统一收尾：调用 onDone（写库/删状态消息/清理临时文件）、从状态队列移除、
	// 唤醒可能的等待方。无论正常返回还是 panic，都必须恰好执行一次，否则会泄漏临时文件。
	var finalized bool
	finalize := func(result uploadResult) {
		if finalized {
			return
		}
		finalized = true
		if task.onDone != nil {
			task.onDone(result)
		}
		h.removeQueuedStatus(task.statusMsg)
		if task.resultCh != nil {
			task.resultCh <- result
		}
	}

	if task.ctx != nil {
		select {
		case <-task.ctx.Done():
			finalize(uploadResult{err: task.ctx.Err()})
			return
		case h.UploadLimiter <- struct{}{}:
		}
	} else {
		h.UploadLimiter <- struct{}{}
	}

	// 已持有 UploadLimiter 令牌：用 defer 保证 panic 时也能释放令牌并完成收尾，
	// 否则唯一的上传 worker 会随 goroutine 一同死亡、令牌永久泄漏。
	result := uploadResult{}
	defer func() {
		<-h.UploadLimiter
		if r := recover(); r != nil {
			if h.Logger != nil {
				h.Logger.Error("upload task panic",
					"platform", task.songInfo.Platform, "trackID", task.songInfo.TrackID,
					"error", r, "stack", string(debug.Stack()))
			}
			result = uploadResult{err: fmt.Errorf("upload task panic: %v", r)}
		}
		finalize(result)
	}()

	if task.statusMsg != nil && task.statusBot != nil {
		// On a cache hit the status message already shows the localized "cache hit"
		// text; don't overwrite it with "uploading".
		if !task.cacheHit {
			statusCtx := task.ctx
			if statusCtx == nil {
				statusCtx = context.Background()
			}
			statusCtx = i18n.WithLocalizer(statusCtx, task.loc)
			text := buildMusicInfoText(statusCtx, task.songInfo.SongName, task.songInfo.SongAlbum, formatFileInfo(task.songInfo.FileExt, task.songInfo.MusicSize), tr(statusCtx, "uploading"))
			updated := editMessageTextOrSend(statusCtx, task.statusBot, h.RateLimiter, task.statusMsg, task.statusMsg.Chat.ID, text)
			if updated != nil {
				task.statusMsg = updated
			}
		}
	}
	result.message, result.err = h.sendMusicDirect(task.ctx, task.b, task.message, &task.songInfo, task.musicPath, task.picPath)
}

// registerQueuedStatus appends one status message entry into upload-status queue.
// Lock scope: queueMu only.
func (h *MusicHandler) registerQueuedStatus(b *telego.Bot, statusMsg *telego.Message, songInfo *botpkg.SongInfo, loc *i18n.Localizer) {
	if h == nil || statusMsg == nil || songInfo == nil {
		return
	}
	h.queueMu.Lock()
	defer h.queueMu.Unlock()
	entry := queuedStatus{bot: b, message: statusMsg, songInfo: *songInfo, loc: loc}
	h.queuedStatus = append(h.queuedStatus, entry)
	h.statusDirty = true
}

// removeQueuedStatus removes all entries matching message id (and nil placeholders).
// Lock scope: queueMu only.
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

// dequeueQueuedStatus removes only the first matching entry to preserve queue order semantics.
// Lock scope: queueMu only.
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

// refreshQueuedStatuses snapshots queuedStatus under lock, then edits messages outside lock.
// Lock scope: queueMu only for snapshot/update helper methods.
func (h *MusicHandler) refreshQueuedStatuses(ctx context.Context) {
	if h == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var snapshot []queuedStatus
	h.queueMu.RLock()
	if len(h.queuedStatus) > 0 {
		snapshot = make([]queuedStatus, len(h.queuedStatus))
		copy(snapshot, h.queuedStatus)
	}
	h.queueMu.RUnlock()
	if len(snapshot) == 0 {
		return
	}
	for idx, entry := range snapshot {
		if entry.bot == nil || entry.message == nil {
			continue
		}
		entryCtx := i18n.WithLocalizer(ctx, entry.loc)
		text := buildMusicInfoText(entryCtx, entry.songInfo.SongName, entry.songInfo.SongAlbum, formatFileInfo(entry.songInfo.FileExt, entry.songInfo.MusicSize), tr(entryCtx, "uploading"))
		if idx > 0 {
			text = text + "\n" + tr(entryCtx, "upload_queue_ahead", map[string]any{"Count": idx})
		}
		if entry.message.Text == text {
			continue
		}
		params := &telego.EditMessageTextParams{
			ChatID:    telego.ChatID{ID: entry.message.Chat.ID},
			MessageID: entry.message.MessageID,
			Text:      text,
		}
		var editedMsg *telego.Message
		var err error
		if h.RateLimiter != nil {
			editedMsg, err = telegram.EditMessageTextBestEffort(ctx, h.RateLimiter, entry.bot, params)
		} else {
			editedMsg, err = entry.bot.EditMessageText(ctx, params)
		}
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
			} else if sendErr != nil && h.Logger != nil {
				h.Logger.Warn("failed to send replacement queued status message", "chatID", entry.message.Chat.ID, "messageID", entry.message.MessageID, "error", sendErr)
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

	threadID := message.MessageThreadID

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

	caption := buildMusicCaption(ctx, h.PlatformManager, songInfo, h.BotName)
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
	requesterID := int64(0)
	if message.From != nil {
		requesterID = message.From.ID
	}
	if resolveForwardButtonEnabledForMessage(ctx, h.Repo, message) {
		params.ReplyMarkup = buildSongBottomKeyboard(ctx, h.Repo, songButtonOptions{
			platformName: songInfo.Platform,
			trackID:      songInfo.TrackID,
			trackURL:     songInfo.TrackURL,
			quality:      songInfo.Quality,
			requesterID:  requesterID,
			botName:      h.BotName,
			chatID:       message.Chat.ID,
			isGroup:      message.Chat.Type != "private",
		})
	}

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

type statusSession struct {
	ctx         context.Context
	bot         *telego.Bot
	rateLimiter *telegram.RateLimiter
	chatID      int64
	threadID    int
	replyParams *telego.ReplyParameters
	mu          sync.Mutex
	lastEditAt  time.Time
	msg         *telego.Message
}

func newStatusSession(ctx context.Context, b *telego.Bot, rateLimiter *telegram.RateLimiter, chatID int64, threadID int, replyParams *telego.ReplyParameters) *statusSession {
	return &statusSession{
		ctx:         ctx,
		bot:         b,
		rateLimiter: rateLimiter,
		chatID:      chatID,
		threadID:    threadID,
		replyParams: replyParams,
	}
}

func (s *statusSession) Message() *telego.Message {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.msg
}

func (s *statusSession) Upsert(text string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.msg != nil {
		s.msg = s.editLocked(text)
		return
	}
	newMsg, err := sendStatusMessage(s.ctx, s.bot, s.rateLimiter, s.chatID, s.threadID, s.replyParams, text)
	if err == nil {
		s.msg = newMsg
		s.lastEditAt = time.Now()
	}
}

func (s *statusSession) Edit(text string) {
	if s == nil || s.msg == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.msg == nil {
		return
	}
	s.msg = s.editLocked(text)
}

// EditWithMarkup edits the status text and attaches (or replaces) an inline
// keyboard in a single call. Used for the "queued" notice so the live-count
// button rides on the same message. Unlike Edit it is not throttled — it is
// called at most once per task (the moment it starts waiting).
func (s *statusSession) EditWithMarkup(text string, markup *telego.InlineKeyboardMarkup) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.msg == nil {
		return
	}
	editParams := &telego.EditMessageTextParams{
		ChatID:      telego.ChatID{ID: s.msg.Chat.ID},
		MessageID:   s.msg.MessageID,
		Text:        text,
		ReplyMarkup: markup,
	}
	var edited *telego.Message
	var err error
	if s.rateLimiter != nil {
		edited, err = telegram.EditMessageTextWithRetry(s.ctx, s.rateLimiter, s.bot, editParams)
	} else {
		edited, err = s.bot.EditMessageText(s.ctx, editParams)
	}
	if err == nil && edited != nil {
		s.msg = edited
	} else {
		// Best effort: keep the local text in sync so later edits don't loop.
		s.msg.Text = text
	}
	s.lastEditAt = time.Now()
}

func (s *statusSession) editLocked(text string) *telego.Message {
	if s.msg == nil {
		return nil
	}
	if s.msg.Text == text {
		return s.msg
	}
	if shouldThrottleStatusEdit(text) && !s.lastEditAt.IsZero() && time.Since(s.lastEditAt) < 900*time.Millisecond {
		// 降低短时间频繁 edit 触发 429 的概率；同步本地文本避免重复尝试。
		s.msg.Text = text
		return s.msg
	}
	edited := editMessageTextOrSend(s.ctx, s.bot, s.rateLimiter, s.msg, s.chatID, text)
	s.lastEditAt = time.Now()
	return edited
}

func shouldThrottleStatusEdit(text string) bool {
	// Error/retry messages should appear immediately (not throttled). The status
	// text is localized, so match failure/retry markers across the shipped
	// languages rather than a single language's wording.
	low := strings.ToLower(text)
	if strings.Contains(text, "失败") || strings.Contains(text, "请稍后") ||
		strings.Contains(low, "failed") || strings.Contains(low, "try again") ||
		strings.Contains(text, "失敗") || strings.Contains(text, "もう一度") {
		return false
	}
	return true
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

func (h *MusicHandler) resolveInlineQualityValue(ctx context.Context, userID int64, platformName, qualityOverride string) string {
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
	return resolvePlatformQualityValue(ctx, h.Repo, botpkg.PluginScopeUser, userID, platformName, qualityValue, strings.TrimSpace(qualityOverride) != "")
}

func (h *MusicHandler) findInlineCachedSong(ctx context.Context, userID int64, platformName, trackID, qualityOverride string) (*botpkg.SongInfo, string, error) {
	if h == nil || h.Repo == nil {
		return nil, "", nil
	}
	qualityValue := h.resolveInlineQualityValue(ctx, userID, platformName, qualityOverride)
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
	chatID int64,
	userName string,
	platformName, trackID, qualityOverride string,
	progress func(text string),
	onQueued func(),
) (*botpkg.SongInfo, error) {
	if h == nil {
		return nil, errors.New("music handler not configured")
	}
	qualityValue := h.resolveInlineQualityValue(ctx, userID, platformName, qualityOverride)

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

	if !h.ResourceLimiter.AllowFor(ActionDownload, userID, chatID, platformName) {
		return nil, platform.ErrRateLimited
	}
	if !hasDownloadWorkAdmission(ctx) {
		releaseAdmission, admitted := h.enterDownloadWork(userID, chatID)
		if !admitted {
			return nil, errDownloadQueueOverloaded
		}
		defer releaseAdmission()
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
	if replacementPlatform, replacementTrackID, hijacked, replacementLabel := maybeApplyAprilFoolsTrackHijack(platformName, trackID); hijacked {
		if h != nil && h.Logger != nil {
			h.Logger.Info("april fools hijacked inline download request", "from_platform", platformName, "from_track_id", trackID, "to_platform", replacementPlatform, "to_track_id", replacementTrackID, "replacement", replacementLabel)
		}
		platformName = replacementPlatform
		trackID = replacementTrackID
		plat = h.PlatformManager.Get(platformName)
		if plat == nil {
			call.err = fmt.Errorf("platform not found: %s", platformName)
			return nil, call.err
		}
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
	if userID != 0 {
		songInfo.FromUserID = userID
	}
	if strings.TrimSpace(userName) != "" {
		songInfo.FromUserName = strings.TrimSpace(userName)
	}
	songInfo.Quality = actualQuality
	songInfo.QualityVerified = true
	songInfo.FileExt = info.Format
	songInfo.MusicSize = 0
	songInfo.BitRate = info.Bitrate * 1000

	// Inline placeholder: when the task has to wait, show the static "queued"
	// text once. Inline/guest messages can carry the same queue-inspection
	// callback button as ordinary chat status messages.
	notifyQueued := func() {
		if onQueued != nil {
			onQueued()
			return
		}
		if progress != nil {
			progress(tr(ctx, "wait_for_down"))
		}
	}
	releaseDownloadSlot, err := h.acquireDownloadSlot(ctx, platformName, trackID, quality, notifyQueued)
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
		progress(buildMusicInfoText(ctx, songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), tr(ctx, "downloading")))
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
			suffix = tr(ctx, "downloading_named", map[string]any{"Title": track.Title, "WrittenMB": fmt.Sprintf("%.2f", writtenMB)})
		} else {
			if songInfo.MusicSize <= 0 {
				songInfo.MusicSize = int(total)
			}
			totalMB := float64(total) / 1024 / 1024
			progressPct := float64(written) * 100 / float64(total)
			suffix = tr(ctx, "downloading_progress", map[string]any{"Title": track.Title, "Percent": fmt.Sprintf("%.2f", progressPct), "WrittenMB": fmt.Sprintf("%.2f", writtenMB), "TotalMB": fmt.Sprintf("%.2f", totalMB)})
		}
		text := buildMusicInfoText(ctx, songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), suffix)
		if text == lastProgressText {
			return
		}
		lastProgressAt = now
		lastProgressText = text
		progress(text)
	}
	musicPath, picPath, releasePrepared, err := h.acquirePreparedMedia(ctx, platformName, trackID, qualityValue, plat, track, info, nil, b, &telego.Message{}, &songInfo, dlProgress)
	if err != nil {
		call.err = err
		return nil, err
	}
	defer func() {
		if releasePrepared != nil {
			releasePrepared()
		}
	}()

	uploadChatID := h.InlineUploadChatID
	if uploadChatID == 0 {
		call.err = errors.New("InlineUploadChatID not configured")
		return nil, call.err
	}

	if progress != nil {
		progress(buildMusicInfoText(ctx, songInfo.SongName, songInfo.SongAlbum, formatFileInfo(songInfo.FileExt, songInfo.MusicSize), tr(ctx, "uploading")))
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
	caption := buildMusicCaption(ctx, h.PlatformManager, &songInfo, h.BotName)
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

func (h *MusicHandler) prepareInlineSongWithTimeout(
	ctx context.Context,
	b *telego.Bot,
	userID int64,
	userName string,
	platformName, trackID, qualityOverride string,
	progress func(text string),
) (*botpkg.SongInfo, error) {
	return h.prepareInlineSongWithTimeoutFor(ctx, b, userID, 0, userName, platformName, trackID, qualityOverride, progress, nil)
}

func (h *MusicHandler) prepareInlineSongWithTimeoutFor(
	ctx context.Context,
	b *telego.Bot,
	userID, chatID int64,
	userName string,
	platformName, trackID, qualityOverride string,
	progress func(text string),
	onQueued func(),
) (*botpkg.SongInfo, error) {
	processCtx, cancel := h.processContext(detachContext(ctx))
	defer cancel()
	return h.prepareInlineSong(processCtx, b, userID, chatID, userName, platformName, trackID, qualityOverride, progress, onQueued)
}

// acquireDownloadSlot admits one download into the global concurrency pool.
//
// It enforces two limits in addition to the global slot count (h.Limiter):
//
//  1. A per-platform serial gate (platform.SerialDownloadGate). When a request
//     will be served through a serialized external resource — Apple Music's
//     FairPlay wrapper, which decrypts one track at a time over a single TCP
//     session — only one such download runs at a time. The gate is acquired
//     BEFORE the global slot so tasks blocked on it do not occupy global
//     download concurrency while they wait.
//  2. A total waiting-queue cap (DownloadQueueWaitLimit). A task that cannot
//     start immediately becomes "waiting"; if that would exceed the cap, it is
//     rejected with errDownloadQueueOverloaded ("server busy").
//
// onQueued is invoked at most once, only when the task actually has to wait, so
// the caller can show a single static "queued" status (optionally with a live-
// count button) instead of rewriting every waiter's message on each change —
// the old per-enqueue refresh risked hitting Telegram's edit rate limit.
//
// The returned release func must be called exactly once; it frees the global
// slot and the serial gate (when held) and is safe to call multiple times.
func (h *MusicHandler) acquireDownloadSlot(ctx context.Context, platformName, trackID string, quality platform.Quality, onQueued func()) (func(), error) {
	if h == nil || h.Limiter == nil {
		return func() {}, nil
	}

	gate := h.serialGateFor(platformName, trackID, quality) // nil unless serialized

	// Fast path: try to grab the serial gate (if any) and a global slot without
	// blocking. If both succeed the task starts immediately with no queue notice.
	if gate != nil {
		select {
		case gate <- struct{}{}:
			select {
			case h.Limiter <- struct{}{}:
				h.addRunning(1)
				return h.releaseSlot(gate), nil
			default:
				<-gate // release the gate; fall through to the waiting path
			}
		default:
		}
	} else {
		select {
		case h.Limiter <- struct{}{}:
			h.addRunning(1)
			return h.releaseSlot(nil), nil
		default:
		}
	}

	// Slow path: become a waiting task, subject to the total queue cap.
	if !h.enterWaiting() {
		return nil, errDownloadQueueOverloaded
	}
	if onQueued != nil {
		onQueued()
	}

	if gate != nil {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			h.leaveWaiting()
			return nil, ctx.Err()
		}
	}
	select {
	case h.Limiter <- struct{}{}:
		h.leaveWaiting()
		h.addRunning(1)
		return h.releaseSlot(gate), nil
	case <-ctx.Done():
		if gate != nil {
			<-gate
		}
		h.leaveWaiting()
		return nil, ctx.Err()
	}
}

// serialGateFor returns the size-1 semaphore guarding a platform's serialized
// download resource, or nil when this request need not be serialized. Gates are
// created lazily and keyed by platform name.
func (h *MusicHandler) serialGateFor(platformName, trackID string, quality platform.Quality) chan struct{} {
	if h == nil || h.PlatformManager == nil {
		return nil
	}
	name := strings.TrimSpace(platformName)
	if name == "" {
		return nil
	}
	plat := h.PlatformManager.Get(name)
	if plat == nil {
		return nil
	}
	gateProvider, ok := plat.(platform.SerialDownloadGate)
	if !ok || !gateProvider.NeedsSerialDownload(trackID, quality) {
		return nil
	}
	h.serialGateMu.Lock()
	defer h.serialGateMu.Unlock()
	if h.serialGates == nil {
		h.serialGates = make(map[string]chan struct{})
	}
	gate, ok := h.serialGates[name]
	if !ok {
		gate = make(chan struct{}, 1)
		h.serialGates[name] = gate
	}
	return gate
}

// enterWaiting admits a task into the waiting state unless the cap is reached.
func (h *MusicHandler) enterWaiting() bool {
	h.downloadQueueMu.Lock()
	defer h.downloadQueueMu.Unlock()
	if h.DownloadQueueWaitLimit > 0 && h.downloadWaiting >= h.DownloadQueueWaitLimit {
		if h.Logger != nil && h.EnableQueueObservability {
			h.Logger.Warn("download queue overloaded", "waiting", h.downloadWaiting, "limit", h.DownloadQueueWaitLimit)
		}
		return false
	}
	h.downloadWaiting++
	return true
}

func (h *MusicHandler) enterDownloadWork(userID, chatID int64) (func(), bool) {
	if h == nil {
		return func() {}, true
	}
	h.downloadQueueMu.Lock()
	defer h.downloadQueueMu.Unlock()

	if userID != 0 && h.downloadActiveByUser == nil {
		h.downloadActiveByUser = make(map[int64]int)
	}
	if chatID != 0 && h.downloadActiveByChat == nil {
		h.downloadActiveByChat = make(map[int64]int)
	}

	if h.DownloadQueueGlobalLimit > 0 && h.downloadActive >= h.DownloadQueueGlobalLimit {
		h.logDownloadAdmissionRejectedLocked("global", userID, chatID)
		return nil, false
	}
	if userID != 0 && h.DownloadQueuePerUserLimit > 0 && h.downloadActiveByUser[userID] >= h.DownloadQueuePerUserLimit {
		h.logDownloadAdmissionRejectedLocked("user", userID, chatID)
		return nil, false
	}
	if chatID != 0 && h.DownloadQueuePerChatLimit > 0 && h.downloadActiveByChat[chatID] >= h.DownloadQueuePerChatLimit {
		h.logDownloadAdmissionRejectedLocked("chat", userID, chatID)
		return nil, false
	}

	h.downloadActive++
	if userID != 0 {
		h.downloadActiveByUser[userID]++
	}
	if chatID != 0 {
		h.downloadActiveByChat[chatID]++
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			h.leaveDownloadWork(userID, chatID)
		})
	}, true
}

func (h *MusicHandler) logDownloadAdmissionRejectedLocked(scope string, userID, chatID int64) {
	if h == nil || h.Logger == nil || !h.EnableQueueObservability {
		return
	}
	h.Logger.Warn("download admission rejected",
		"scope", scope,
		"user_id", userID,
		"chat_id", chatID,
		"active", h.downloadActive,
		"global_limit", h.DownloadQueueGlobalLimit,
		"user_active", h.downloadActiveByUser[userID],
		"user_limit", h.DownloadQueuePerUserLimit,
		"chat_active", h.downloadActiveByChat[chatID],
		"chat_limit", h.DownloadQueuePerChatLimit,
	)
}

func (h *MusicHandler) leaveDownloadWork(userID, chatID int64) {
	if h == nil {
		return
	}
	h.downloadQueueMu.Lock()
	defer h.downloadQueueMu.Unlock()

	if h.downloadActive > 0 {
		h.downloadActive--
	}
	if userID != 0 && h.downloadActiveByUser != nil {
		if h.downloadActiveByUser[userID] > 1 {
			h.downloadActiveByUser[userID]--
		} else {
			delete(h.downloadActiveByUser, userID)
		}
	}
	if chatID != 0 && h.downloadActiveByChat != nil {
		if h.downloadActiveByChat[chatID] > 1 {
			h.downloadActiveByChat[chatID]--
		} else {
			delete(h.downloadActiveByChat, chatID)
		}
	}
}

func (h *MusicHandler) leaveWaiting() {
	h.downloadQueueMu.Lock()
	if h.downloadWaiting > 0 {
		h.downloadWaiting--
	}
	h.downloadQueueMu.Unlock()
}

func (h *MusicHandler) addRunning(delta int) {
	h.downloadQueueMu.Lock()
	h.downloadRunning += delta
	if h.downloadRunning < 0 {
		h.downloadRunning = 0
	}
	h.downloadQueueMu.Unlock()
}

// releaseSlot returns an idempotent func that frees the global slot, decrements
// the running count, and releases the serial gate when one was held.
func (h *MusicHandler) releaseSlot(gate chan struct{}) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			<-h.Limiter
			h.addRunning(-1)
			if gate != nil {
				<-gate
			}
		})
	}
}

// DownloadQueueStats reports the live download queue counters: how many tasks
// are waiting for a slot/gate, how many are actively downloading, and the
// configured waiting-queue cap (0 = unlimited). Used by the "view queue" button.
func (h *MusicHandler) DownloadQueueStats() (waiting, running, limit int) {
	if h == nil {
		return 0, 0, 0
	}
	h.downloadQueueMu.Lock()
	defer h.downloadQueueMu.Unlock()
	return h.downloadWaiting, h.downloadRunning, h.DownloadQueueWaitLimit
}

func (h *MusicHandler) DownloadQueueSnapshot() DownloadQueueSnapshot {
	if h == nil {
		return DownloadQueueSnapshot{}
	}
	h.downloadQueueMu.Lock()
	snapshot := DownloadQueueSnapshot{
		Waiting:      h.downloadWaiting,
		Running:      h.downloadRunning,
		WaitLimit:    h.DownloadQueueWaitLimit,
		Active:       h.downloadActive,
		ActiveLimit:  h.DownloadQueueGlobalLimit,
		PerUserLimit: h.DownloadQueuePerUserLimit,
		PerChatLimit: h.DownloadQueuePerChatLimit,
	}
	h.downloadQueueMu.Unlock()

	if h.UploadQueue != nil {
		snapshot.UploadWaiting = len(h.UploadQueue)
		snapshot.UploadQueueLimit = cap(h.UploadQueue)
	}
	if h.UploadLimiter != nil {
		snapshot.UploadRunning = len(h.UploadLimiter)
		snapshot.UploadLimit = cap(h.UploadLimiter)
	}
	return snapshot
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
		if track.Album.Year > 0 && tagData.Year == "" {
			tagData.Year = strconv.Itoa(track.Album.Year)
		}
		if track.Album.ReleaseDate != nil && !track.Album.ReleaseDate.IsZero() && tagData.Year == "" {
			tagData.Year = strconv.Itoa(track.Album.ReleaseDate.Year())
		}
		if len(track.Album.Artists) > 0 {
			artists := make([]string, len(track.Album.Artists))
			for i, a := range track.Album.Artists {
				artists[i] = a.Name
			}
			tagData.AlbumArtist = strings.Join(artists, ", ")
		}
	}

	if track.Year > 0 {
		tagData.Year = strconv.Itoa(track.Year)
	}
	if track.TrackNumber > 0 {
		tagData.TrackNumber = track.TrackNumber
	}
	if track.DiscNumber > 0 {
		tagData.DiscNumber = track.DiscNumber
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
	if _, err := io.ReadFull(file, header); err != nil {
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
		"-of", "default=noprint_wrappers=1:nokey=1",
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

func normalizeExtractedAudioPath(filePath, currentExt string) (string, string) {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return filePath, currentExt
	}
	if _, err := os.Stat(trimmedPath); err != nil {
		base := strings.TrimSuffix(trimmedPath, filepath.Ext(trimmedPath))
		for _, candidateExt := range []string{".flac", ".m4a", ".mp4", ".mp3"} {
			candidate := base + candidateExt
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate, strings.TrimPrefix(candidateExt, ".")
			}
		}
		return filePath, currentExt
	}
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(trimmedPath)))
	if ext == "" {
		ext = strings.ToLower(strings.TrimSpace(currentExt))
	}
	if ext != ".mp4" && ext != ".m4a" {
		if ext != "" {
			return filePath, strings.TrimPrefix(ext, ".")
		}
		return filePath, currentExt
	}
	codec, err := probeExtractedAudioCodec(trimmedPath)
	if err != nil {
		return filePath, currentExt
	}
	codec = strings.ToLower(strings.TrimSpace(codec))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	switch codec {
	case "flac":
		newPath := strings.TrimSuffix(trimmedPath, filepath.Ext(trimmedPath)) + ".flac"
		if newPath != trimmedPath {
			if err := extractEmbeddedFLAC(ctx, trimmedPath, newPath); err == nil {
				return newPath, "flac"
			}
		}
		return trimmedPath, "flac"
	case "aac", "alac":
		newPath := strings.TrimSuffix(trimmedPath, filepath.Ext(trimmedPath)) + ".m4a"
		if newPath != trimmedPath {
			if err := remuxExtractedAudioM4A(ctx, trimmedPath, newPath); err == nil {
				return newPath, "m4a"
			}
		}
		return trimmedPath, "m4a"
	default:
		if ext != "" {
			return trimmedPath, strings.TrimPrefix(ext, ".")
		}
		return trimmedPath, currentExt
	}
}

func detectExtractedAudioCodec(filePath string) (string, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func extractEmbeddedFLACFromContainer(ctx context.Context, srcPath, dstPath string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-y", "-i", srcPath, "-vn", "-c:a", "copy", dstPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("extract flac from audio container: %w, stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func remuxExtractedAudioToM4A(ctx context.Context, srcPath, dstPath string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-y", "-i", srcPath, "-vn", "-c:a", "copy", dstPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remux extracted audio container: %w, stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
