package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/db"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
	"github.com/liuran001/MusicBot-Go/bot/recognize"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/liuran001/MusicBot-Go/bot/telegram/handler"
	"github.com/liuran001/MusicBot-Go/bot/worker"
	gormlogger "gorm.io/gorm/logger"
)

// App wires all application dependencies.
type App struct {
	Config           *config.Config
	Logger           *logpkg.Logger
	DB               *db.Repository
	Pool             *worker.Pool
	PlatformManager  platform.Manager
	Telegram         *telegram.Bot
	RecognizeService recognize.Service
	TagProviders     map[string]id3.ID3TagProvider
	Build            BuildInfo
}

// BuildInfo provides build-time metadata.
type BuildInfo struct {
	RuntimeVer string
	BinVersion string
	CommitSHA  string
	BuildTime  string
	BuildArch  string
}

// New builds the application container.
func New(ctx context.Context, configPath string, build BuildInfo) (*App, error) {
	conf, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	log, err := logpkg.New(conf.GetString("LogLevel"))
	if err != nil {
		return nil, err
	}

	gormLogger := logpkg.NewGormLogger(log.Slog(), mapLogLevel(conf.GetString("LogLevel")))
	databasePath := conf.GetString("Database")
	if strings.TrimSpace(databasePath) == "" {
		databasePath = "cache.db"
	}

	repo, err := db.NewSQLiteRepository(databasePath, gormLogger)
	if err != nil {
		return nil, fmt.Errorf("init db: %w", err)
	}
	poolMaxOpen := conf.GetInt("DBMaxOpenConns")
	poolMaxIdle := conf.GetInt("DBMaxIdleConns")
	poolMaxLifetimeSec := conf.GetInt("DBConnMaxLifetimeSec")
	if err := repo.ConfigurePool(poolMaxOpen, poolMaxIdle, time.Duration(poolMaxLifetimeSec)*time.Second); err != nil {
		return nil, fmt.Errorf("configure db pool: %w", err)
	}
	defaultPlatform := strings.TrimSpace(conf.GetString("DefaultPlatform"))
	if defaultPlatform == "" {
		defaultPlatform = "netease"
	}
	repo.SetDefaults(defaultPlatform, conf.GetString("DefaultQuality"))

	poolSize := conf.GetInt("WorkerPoolSize")
	pool := worker.New(poolSize)

	platformManager := platform.NewManager()
	pluginTagProviders := make(map[string]id3.ID3TagProvider)
	var recognizeService recognize.Service
	pluginNames := conf.PluginNames()
	if len(pluginNames) == 0 {
		pluginNames = platformplugins.Names()
	}
	for _, name := range pluginNames {
		enabled := true
		if pluginCfg, ok := conf.GetPluginConfig(name); ok {
			if _, hasKey := pluginCfg["enabled"]; hasKey {
				enabled = conf.GetPluginBool(name, "enabled")
			}
		}
		if !enabled {
			if log != nil {
				log.Info("plugin disabled by config", "plugin", name)
			}
			continue
		}

		factory, ok := platformplugins.Get(name)
		if !ok {
			if log != nil {
				log.Warn("plugin not registered", "plugin", name)
			}
			continue
		}

		contrib, err := factory(conf, log)
		if err != nil {
			if log != nil {
				log.Error("plugin init failed", "plugin", name, "error", err)
			}
			continue
		}
		if contrib == nil {
			continue
		}
		if contrib.Platform != nil {
			platformManager.Register(contrib.Platform)
			if contrib.ID3 != nil {
				pluginTagProviders[contrib.Platform.Name()] = contrib.ID3
			}
		}
		if contrib.Recognizer != nil {
			if recognizeService == nil {
				recognizeService = contrib.Recognizer
			} else if log != nil {
				log.Warn("multiple recognizers configured; ignoring extra", "plugin", name)
			}
		}
	}

	tele, err := telegram.New(conf, log)
	if err != nil {
		return nil, fmt.Errorf("init telegram: %w", err)
	}

	return &App{
		Config:           conf,
		Logger:           log,
		DB:               repo,
		Pool:             pool,
		PlatformManager:  platformManager,
		Telegram:         tele,
		RecognizeService: recognizeService,
		TagProviders:     pluginTagProviders,
		Build:            build,
	}, nil
}

// Start initializes background services. Telegram startup is added in later waves.
func (a *App) Start(ctx context.Context) error {
	// Start recognition service first
	if a.RecognizeService != nil {
		if err := a.RecognizeService.Start(ctx); err != nil {
			if a.Logger != nil {
				a.Logger.Warn("failed to start recognition service", "error", err)
			}
			// Don't fail app startup if recognition service fails
		} else {
			if a.Logger != nil {
				a.Logger.Info("audio recognition service started successfully")
			}
		}
	}

	meCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	me, err := a.Telegram.GetMe(meCtx)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Error("getMe failed", "error", err)
		}
	}
	botName := ""
	if me != nil {
		botName = me.Username
	}

	cacheDir := strings.TrimSpace(a.Config.GetString("CacheDir"))
	if cacheDir == "" {
		cacheDir = "./cache"
	}

	downloadService := download.NewDownloadService(download.DownloadServiceOptions{
		Timeout:              time.Duration(a.Config.GetInt("DownloadTimeout")) * time.Second,
		ReverseProxy:         a.Config.GetString("ReverseProxy"),
		CheckMD5:             a.Config.GetBool("CheckMD5"),
		EnableMultipart:      a.Config.GetBool("EnableMultipartDownload"),
		MultipartConcurrency: a.Config.GetInt("MultipartConcurrency"),
		MultipartMinSize:     int64(a.Config.GetInt("MultipartMinSizeMB")) * 1024 * 1024,
	})
	id3Service := id3.NewID3Service(a.Logger)

	tagProviders := a.TagProviders

	rateLimitPerSecond := a.Config.GetFloat64("RateLimitPerSecond")
	if rateLimitPerSecond <= 0 {
		rateLimitPerSecond = 1.0
	}
	rateLimitBurst := a.Config.GetInt("RateLimitBurst")
	if rateLimitBurst <= 0 {
		rateLimitBurst = 3
	}
	rateLimiter := telegram.NewRateLimiter(rateLimitPerSecond, rateLimitBurst)
	rateLimiter.SetLogger(a.Logger)

	downloadConcurrency := a.Config.GetInt("DownloadConcurrency")
	var downloadLimiter chan struct{}
	if downloadConcurrency > 0 {
		downloadLimiter = make(chan struct{}, downloadConcurrency)
	}
	uploadConcurrency := a.Config.GetInt("UploadConcurrency")
	var uploadLimiter chan struct{}
	if uploadConcurrency > 0 {
		uploadLimiter = make(chan struct{}, uploadConcurrency)
	}
	uploadQueueSize := a.Config.GetInt("UploadQueueSize")

	musicHandler := &handler.MusicHandler{
		Repo:            a.DB,
		Pool:            a.Pool,
		Logger:          a.Logger,
		CacheDir:        cacheDir,
		BotName:         botName,
		PlatformManager: a.PlatformManager,
		DownloadService: downloadService,
		ID3Service:      id3Service,
		TagProviders:    tagProviders,
		Limiter:         downloadLimiter,
		UploadLimiter:   uploadLimiter,
		UploadQueueSize: uploadQueueSize,
		UploadBot:       a.Telegram.UploadClient(),
		RateLimiter:     rateLimiter,
	}
	musicHandler.StartWorker(ctx)

	defaultPlatform := strings.TrimSpace(a.Config.GetString("DefaultPlatform"))
	if defaultPlatform == "" {
		defaultPlatform = "netease"
	}
	defaultQuality := a.Config.GetString("DefaultQuality")
	settingsHandler := &handler.SettingsHandler{
		Repo:            a.DB,
		PlatformManager: a.PlatformManager,
		RateLimiter:     rateLimiter,
		DefaultPlatform: defaultPlatform,
		DefaultQuality:  defaultQuality,
	}
	searchFallback := strings.TrimSpace(a.Config.GetString("SearchFallbackPlatform"))
	if searchFallback == "" {
		searchFallback = "netease"
	}

	router := &handler.Router{
		Music:            musicHandler,
		Search:           &handler.SearchHandler{PlatformManager: a.PlatformManager, Repo: a.DB, RateLimiter: rateLimiter, DefaultPlatform: defaultPlatform, FallbackPlatform: searchFallback},
		Lyric:            &handler.LyricHandler{PlatformManager: a.PlatformManager, RateLimiter: rateLimiter},
		Recognize:        &handler.RecognizeHandler{CacheDir: cacheDir, Music: musicHandler, RateLimiter: rateLimiter, RecognizeService: a.RecognizeService, Logger: a.Logger},
		About:            &handler.AboutHandler{RuntimeVer: a.Build.RuntimeVer, BinVersion: a.Build.BinVersion, CommitSHA: a.Build.CommitSHA, BuildTime: a.Build.BuildTime, BuildArch: a.Build.BuildArch, RateLimiter: rateLimiter},
		Status:           &handler.StatusHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter},
		Settings:         settingsHandler,
		RmCache:          &handler.RmCacheHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter},
		Callback:         &handler.CallbackMusicHandler{Music: musicHandler, BotName: botName, RateLimiter: rateLimiter},
		SettingsCallback: &handler.SettingsCallbackHandler{Repo: a.DB, PlatformManager: a.PlatformManager, SettingsHandler: settingsHandler, RateLimiter: rateLimiter},
		Inline:           &handler.InlineSearchHandler{Repo: a.DB, PlatformManager: a.PlatformManager, BotName: botName},
		PlatformManager:  a.PlatformManager,
	}

	router.Register(a.Telegram.Client(), botName)

	commands := []models.BotCommand{
		{Command: "start", Description: "开始使用 / 下载音乐"},
		{Command: "search", Description: "搜索音乐"},
		{Command: "settings", Description: "设置默认平台和音质"},
		{Command: "lyric", Description: "获取歌词"},
		{Command: "recognize", Description: "识别语音中的歌曲"},
		{Command: "status", Description: "查看统计信息"},
		{Command: "about", Description: "关于本 Bot"},
	}
	_, _ = a.Telegram.Client().SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: commands,
	})

	go a.Telegram.Start(ctx)
	return nil
}

// Shutdown releases resources.
func (a *App) Shutdown(ctx context.Context) error {
	var firstErr error

	if a.RecognizeService != nil {
		if err := a.RecognizeService.Stop(); err != nil {
			if a.Logger != nil {
				a.Logger.Error("failed to stop recognition service", "error", err)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("stop recognition service: %w", err)
			}
		}
	}

	if a.Pool != nil {
		if err := a.Pool.Shutdown(ctx); err != nil {
			a.Pool.StopNow()
			if firstErr == nil {
				firstErr = fmt.Errorf("shutdown worker pool: %w", err)
			}
		}
	}

	if a.DB != nil {
		if err := a.DB.Close(); err != nil {
			if a.Logger != nil {
				a.Logger.Error("failed to close database", "error", err)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("close database: %w", err)
			}
		}
	}

	if a.Logger != nil {
		if err := a.Logger.Close(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("close logger: %w", err)
			}
		}
	}

	return firstErr
}

func mapLogLevel(level string) gormlogger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "trace":
		return gormlogger.Info
	case "warn", "warning":
		return gormlogger.Warn
	case "error", "fatal", "panic":
		return gormlogger.Error
	case "info":
		fallthrough
	default:
		return gormlogger.Info
	}
}
