package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/db"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/dynplugin"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
	"github.com/liuran001/MusicBot-Go/bot/recognize"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/liuran001/MusicBot-Go/bot/telegram/handler"
	"github.com/liuran001/MusicBot-Go/bot/worker"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	gormlogger "gorm.io/gorm/logger"
)

// App wires all application dependencies.
type App struct {
	Config           *config.Config
	ConfigPath       string
	Logger           *logpkg.Logger
	DB               *db.Repository
	Pool             *worker.Pool
	PlatformManager  platform.Manager
	DynPlugins       *dynplugin.Manager
	AdminIDs         map[int64]struct{}
	AdminCommands    []admincmd.Command
	Telegram         *telegram.Bot
	RecognizeService recognize.Service
	TagProviders     map[string]id3.ID3TagProvider
	Build            BuildInfo
	botHandler       *th.BotHandler
}

func registerContribution(
	platformManager platform.Manager,
	pluginTagProviders map[string]id3.ID3TagProvider,
	recognizeService *recognize.Service,
	adminCommands *[]admincmd.Command,
	contrib *platformplugins.Contribution,
	log *logpkg.Logger,
) {
	if contrib == nil {
		return
	}
	platformsToRegister := contrib.Platforms
	if len(platformsToRegister) == 0 && contrib.Platform != nil {
		platformsToRegister = []platform.Platform{contrib.Platform}
	}

	for _, plat := range platformsToRegister {
		if plat != nil {
			platformManager.Register(plat)
			if contrib.ID3 != nil {
				pluginTagProviders[plat.Name()] = contrib.ID3
			}
		}
	}

	if contrib.Recognizer != nil {
		if *recognizeService == nil {
			*recognizeService = contrib.Recognizer
		} else if log != nil {
			log.Warn("multiple recognizers configured; ignoring extra", "plugin", "dynamic")
		}
	}

	if adminCommands != nil && len(contrib.Commands) > 0 {
		*adminCommands = append(*adminCommands, contrib.Commands...)
	}
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

	log, err := logpkg.New(conf.GetString("LogLevel"), conf.GetString("LogFormat"), conf.GetBool("LogSource"))
	if err != nil {
		return nil, err
	}

	gormLogger := logpkg.NewGormLogger(log.Slog(), mapGormLogLevel(conf.GetString("GormLogLevel"), conf.GetString("LogLevel")))
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
	dynManager := dynplugin.NewManager(log)
	adminIDs := parseAdminIDs(conf.GetString("BotAdmin"))
	pluginTagProviders := make(map[string]id3.ID3TagProvider)
	adminCommands := make([]admincmd.Command, 0)
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
			continue
		}

		contrib, err := factory(conf, log)
		if err != nil {
			if log != nil {
				log.Error("plugin init failed", "plugin", name, "error", err)
			}
			continue
		}
		registerContribution(platformManager, pluginTagProviders, &recognizeService, &adminCommands, contrib, log)
	}

	if err := dynManager.Load(ctx, conf, platformManager); err != nil {
		if log != nil {
			log.Warn("dynamic plugin load failed", "error", err)
		}
	}

	tele, err := telegram.New(conf, log)
	if err != nil {
		return nil, fmt.Errorf("init telegram: %w", err)
	}

	return &App{
		Config:           conf,
		ConfigPath:       configPath,
		Logger:           log,
		DB:               repo,
		Pool:             pool,
		PlatformManager:  platformManager,
		DynPlugins:       dynManager,
		AdminIDs:         adminIDs,
		AdminCommands:    adminCommands,
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
	botName := strings.TrimSpace(a.Telegram.Client().Username())
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
	defaultPlatform := strings.TrimSpace(a.Config.GetString("DefaultPlatform"))
	if defaultPlatform == "" {
		defaultPlatform = "netease"
	}
	searchFallback := strings.TrimSpace(a.Config.GetString("SearchFallbackPlatform"))
	if searchFallback == "" {
		searchFallback = "netease"
	}
	whitelistIDs := parseWhitelistIDs(a.Config.GetString("WhitelistChatIDs"))
	whitelist := handler.NewWhitelist(a.Config.GetBool("EnableWhitelist"), whitelistIDs, a.AdminIDs, a.ConfigPath)

	adminCommands := make([]admincmd.Command, 0, len(a.AdminCommands)+2)
	adminCommands = append(adminCommands, handler.BuildCheckCookieCommand(a.PlatformManager))
	if whitelist.Enabled() {
		adminCommands = append(adminCommands, BuildWhitelistCommand(whitelist))
	}
	adminCommands = append(adminCommands, a.AdminCommands...)
	adminCommandNames := make([]string, 0, len(adminCommands))
	for _, cmd := range adminCommands {
		if strings.TrimSpace(cmd.Name) == "" {
			continue
		}
		adminCommandNames = append(adminCommandNames, cmd.Name)
	}

	defaultQuality := a.Config.GetString("DefaultQuality")
	pageSize := a.Config.GetInt("ListPageSize")
	playlistHandler := &handler.PlaylistHandler{
		PlatformManager: a.PlatformManager,
		Repo:            a.DB,
		RateLimiter:     rateLimiter,
		DefaultQuality:  defaultQuality,
		PageSize:        pageSize,
	}
	playlistCallback := &handler.PlaylistCallbackHandler{Playlist: playlistHandler, RateLimiter: rateLimiter}

	musicHandler := &handler.MusicHandler{
		Repo:             a.DB,
		Pool:             a.Pool,
		Logger:           a.Logger,
		CacheDir:         cacheDir,
		BotName:          botName,
		DefaultPlatform:  defaultPlatform,
		FallbackPlatform: searchFallback,
		AdminIDs:         a.AdminIDs,
		AdminCommands:    adminCommands,
		PlatformManager:  a.PlatformManager,
		DownloadService:  downloadService,
		ID3Service:       id3Service,
		TagProviders:     tagProviders,
		Limiter:          downloadLimiter,
		UploadLimiter:    uploadLimiter,
		UploadQueueSize:  uploadQueueSize,
		UploadBot:        a.Telegram.UploadClient(),
		RateLimiter:      rateLimiter,
		Playlist:         playlistHandler,
		RecognizeEnabled: a.Config.GetBool("EnableRecognize"),
	}
	musicHandler.StartWorker(ctx)

	settingsHandler := &handler.SettingsHandler{
		Repo:            a.DB,
		PlatformManager: a.PlatformManager,
		RateLimiter:     rateLimiter,
		DefaultPlatform: defaultPlatform,
		DefaultQuality:  defaultQuality,
	}
	searchHandler := &handler.SearchHandler{PlatformManager: a.PlatformManager, Repo: a.DB, RateLimiter: rateLimiter, DefaultPlatform: defaultPlatform, FallbackPlatform: searchFallback, PageSize: pageSize}
	adminHandler := &handler.AdminCommandHandler{
		BotName:     botName,
		AdminIDs:    a.AdminIDs,
		RateLimiter: rateLimiter,
		Commands:    adminCommands,
	}
	searchCallback := &handler.SearchCallbackHandler{Search: searchHandler, RateLimiter: rateLimiter}
	reloadHandler := &handler.ReloadHandler{Reload: a.ReloadDynamicPlugins, RateLimiter: rateLimiter, Logger: a.Logger, AdminIDs: a.AdminIDs}

	enableRecognize := a.Config.GetBool("EnableRecognize")

	var recognizeHandler handler.MessageHandler
	if enableRecognize {
		recognizeHandler = &handler.RecognizeHandler{CacheDir: cacheDir, Music: musicHandler, RateLimiter: rateLimiter, RecognizeService: a.RecognizeService, Logger: a.Logger, DownloadBot: a.Telegram.DownloadClient()}
	}

	router := &handler.Router{
		Music:            musicHandler,
		Playlist:         playlistHandler,
		Search:           searchHandler,
		Lyric:            &handler.LyricHandler{PlatformManager: a.PlatformManager, RateLimiter: rateLimiter},
		Recognize:        recognizeHandler,
		About:            &handler.AboutHandler{RuntimeVer: a.Build.RuntimeVer, BinVersion: a.Build.BinVersion, CommitSHA: a.Build.CommitSHA, BuildTime: a.Build.BuildTime, BuildArch: a.Build.BuildArch, DynPlugins: a.DynPlugins, RateLimiter: rateLimiter},
		Status:           &handler.StatusHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter},
		Settings:         settingsHandler,
		RmCache:          &handler.RmCacheHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter, AdminIDs: a.AdminIDs},
		Callback:         &handler.CallbackMusicHandler{Music: musicHandler, BotName: botName, RateLimiter: rateLimiter},
		SettingsCallback: &handler.SettingsCallbackHandler{Repo: a.DB, PlatformManager: a.PlatformManager, SettingsHandler: settingsHandler, RateLimiter: rateLimiter},
		SearchCallback:   searchCallback,
		PlaylistCallback: playlistCallback,
		Reload:           reloadHandler,
		Admin:            adminHandler,
		Inline:           &handler.InlineSearchHandler{Repo: a.DB, PlatformManager: a.PlatformManager, BotName: botName, DefaultPlatform: defaultPlatform, DefaultQuality: defaultQuality, FallbackPlatform: searchFallback},
		PlatformManager:  a.PlatformManager,
		AdminCommands:    adminCommandNames,
		Whitelist:        whitelist,
		Logger:           a.Logger,
	}

	updates, err := a.Telegram.Client().UpdatesViaLongPolling(ctx, nil)
	if err != nil {
		return fmt.Errorf("init telegram: %w", err)
	}
	botHandler, err := th.NewBotHandler(a.Telegram.Client(), updates)
	if err != nil {
		return fmt.Errorf("init telegram: %w", err)
	}
	a.botHandler = botHandler

	router.Register(botHandler, botName)

	commands := []telego.BotCommand{
		{Command: "help", Description: "查看使用说明"},
		{Command: "music", Description: "下载音乐"},
		{Command: "search", Description: "搜索音乐"},
		{Command: "settings", Description: "设置默认平台和音质"},
		{Command: "lyric", Description: "获取歌词"},
	}
	if enableRecognize {
		commands = append(commands, telego.BotCommand{Command: "recognize", Description: "识别语音中的歌曲"})
	}
	commands = append(commands,
		telego.BotCommand{Command: "status", Description: "查看统计信息"},
		telego.BotCommand{Command: "about", Description: "关于本 Bot"},
	)
	_ = a.Telegram.Client().SetMyCommands(ctx, &telego.SetMyCommandsParams{
		Commands: commands,
	})

	go func() {
		_ = botHandler.Start()
	}()
	return nil
}

func BuildWhitelistCommand(wl *handler.Whitelist) admincmd.Command {
	return admincmd.Command{
		Name:        "wl",
		Description: "白名单管理 (add/del/list)",
		Handler: func(ctx context.Context, args string) (string, error) {
			_ = ctx
			fields := strings.Fields(strings.TrimSpace(args))
			if len(fields) == 0 {
				return "用法:\n/wl add <chatID>\n/wl del <chatID>\n/wl list", nil
			}
			sub := strings.ToLower(strings.TrimSpace(fields[0]))
			switch sub {
			case "add":
				if len(fields) < 2 {
					return "用法: /wl add <chatID>", nil
				}
				chatID, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
				if err != nil {
					return "chatID 格式错误", nil
				}
				added := wl.Add(chatID)
				if err := wl.Persist(); err != nil {
					return "", err
				}
				if added {
					return fmt.Sprintf("已添加白名单: %d", chatID), nil
				}
				return fmt.Sprintf("白名单已存在: %d", chatID), nil
			case "del":
				if len(fields) < 2 {
					return "用法: /wl del <chatID>", nil
				}
				chatID, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
				if err != nil {
					return "chatID 格式错误", nil
				}
				removed := wl.Remove(chatID)
				if err := wl.Persist(); err != nil {
					return "", err
				}
				if removed {
					return fmt.Sprintf("已移除白名单: %d", chatID), nil
				}
				return fmt.Sprintf("白名单不存在: %d", chatID), nil
			case "list":
				ids := wl.List()
				if len(ids) == 0 {
					return "白名单为空", nil
				}
				rows := make([]string, 0, len(ids)+1)
				rows = append(rows, "白名单列表:")
				for _, id := range ids {
					rows = append(rows, strconv.FormatInt(id, 10))
				}
				return strings.Join(rows, "\n"), nil
			default:
				return "用法:\n/wl add <chatID>\n/wl del <chatID>\n/wl list", nil
			}
		},
	}
}

// ReloadDynamicPlugins reloads script-based plugins from disk.
func (a *App) ReloadDynamicPlugins(ctx context.Context) error {
	if a.DynPlugins == nil {
		return fmt.Errorf("dynamic plugins not configured")
	}
	if strings.TrimSpace(a.ConfigPath) == "" {
		return fmt.Errorf("config path missing")
	}
	conf, err := config.Load(a.ConfigPath)
	if err != nil {
		return err
	}
	a.Config = conf
	refreshAdminIDs(a.AdminIDs, conf.GetString("BotAdmin"))
	return a.DynPlugins.Reload(ctx, conf, a.PlatformManager)
}

func parseAdminIDs(raw string) map[int64]struct{} {
	ids := make(map[int64]struct{})
	for _, value := range splitAdminIDs(raw) {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func parseWhitelistIDs(raw string) map[int64]struct{} {
	ids := make(map[int64]struct{})
	for _, value := range splitAdminIDs(raw) {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func refreshAdminIDs(dst map[int64]struct{}, raw string) {
	if dst == nil {
		return
	}
	for key := range dst {
		delete(dst, key)
	}
	for _, value := range splitAdminIDs(raw) {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			dst[id] = struct{}{}
		}
	}
}

func splitAdminIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

// Shutdown releases resources.
func (a *App) Shutdown(ctx context.Context) error {
	var firstErr error

	if a.botHandler != nil {
		if err := a.botHandler.StopWithContext(ctx); err != nil {
			if a.Logger != nil {
				a.Logger.Error("failed to stop telegram handler", "error", err)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("stop telegram handler: %w", err)
			}
		}
		a.botHandler = nil
	}

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

func mapGormLogLevel(level, fallback string) gormlogger.LogLevel {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		return mapLogLevel(fallback)
	}
	switch level {
	case "silent", "off":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "warn", "warning":
		return gormlogger.Warn
	case "info", "debug", "trace":
		return gormlogger.Info
	default:
		return mapLogLevel(fallback)
	}
}
