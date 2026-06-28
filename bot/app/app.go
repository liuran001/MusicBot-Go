package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/db"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/dynplugin"
	"github.com/liuran001/MusicBot-Go/bot/i18n"
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
	Config                   *config.Config
	ConfigPath               string
	Logger                   *logpkg.Logger
	DB                       *db.Repository
	Pool                     *worker.Pool
	PlatformManager          platform.Manager
	DynPlugins               *dynplugin.Manager
	AdminIDs                 map[int64]struct{}
	adminSet                 *handler.AdminSet
	AdminCommands            []admincmd.Command
	Telegram                 *telegram.Bot
	RecognizeService         recognize.Service
	TagProviders             map[string]id3.ID3TagProvider
	PluginSettingDefinitions []botpkg.PluginSettingDefinition
	Build                    BuildInfo
	botHandler               *th.BotHandler
}

func registerContribution(
	platformManager platform.Manager,
	pluginTagProviders map[string]id3.ID3TagProvider,
	recognizeService *recognize.Service,
	adminCommands *[]admincmd.Command,
	pluginSettingDefinitions *[]botpkg.PluginSettingDefinition,
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

	if pluginSettingDefinitions != nil && len(contrib.SettingDefinitions) > 0 {
		*pluginSettingDefinitions = append(*pluginSettingDefinitions, contrib.SettingDefinitions...)
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
	conf, log, err := loadConfigAndLogger(configPath)
	if err != nil {
		return nil, err
	}

	repo, err := initRepository(conf, log)
	if err != nil {
		return nil, err
	}

	pool := initWorkerPool(conf, log)
	platformManager, dynManager, adminIDs, pluginTagProviders, adminCommands, pluginSettingDefinitions, recognizeService := initPluginRuntime(ctx, conf, log)

	tele, err := initTelegramBot(conf, log)
	if err != nil {
		return nil, err
	}

	return &App{
		Config:                   conf,
		ConfigPath:               configPath,
		Logger:                   log,
		DB:                       repo,
		Pool:                     pool,
		PlatformManager:          platformManager,
		DynPlugins:               dynManager,
		AdminIDs:                 adminIDs,
		adminSet:                 handler.NewAdminSet(adminIDs),
		AdminCommands:            adminCommands,
		Telegram:                 tele,
		RecognizeService:         recognizeService,
		TagProviders:             pluginTagProviders,
		PluginSettingDefinitions: pluginSettingDefinitions,
		Build:                    build,
	}, nil
}

func loadConfigAndLogger(configPath string) (*config.Config, *logpkg.Logger, error) {
	conf, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}
	log, err := logpkg.New(conf.GetString("LogLevel"), conf.GetString("LogFormat"), conf.GetBool("LogSource"))
	if err != nil {
		return nil, nil, err
	}
	return conf, log, nil
}

func initRepository(conf *config.Config, log *logpkg.Logger) (*db.Repository, error) {
	gormLogger := logpkg.NewGormLogger(log.Slog(), mapGormLogLevel(conf.GetString("GormLogLevel"), conf.GetString("LogLevel")))
	databasePath := conf.GetString("Database")
	if strings.TrimSpace(databasePath) == "" {
		databasePath = "cache.db"
	}
	dataDatabasePath := conf.GetString("DataDatabase")
	if strings.TrimSpace(dataDatabasePath) == "" {
		dataDatabasePath = "data.db"
	}

	repo, err := db.NewSQLiteRepository(databasePath, dataDatabasePath, gormLogger)
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
	repo.SetDefaults(defaultPlatform, conf.GetString("DefaultQuality"), conf.GetString("DefaultLyricFormat"))
	return repo, nil
}

func initWorkerPool(conf *config.Config, log *logpkg.Logger) *worker.Pool {
	poolSize := conf.GetInt("WorkerPoolSize")
	pool := worker.New(poolSize)
	pool.SetPanicHandler(func(recovered any, stack []byte) {
		if log != nil {
			log.Error("worker task panic recovered", "panic", recovered, "stack", string(stack))
		}
	})
	return pool
}

func initPluginRuntime(ctx context.Context, conf *config.Config, log *logpkg.Logger) (
	platformManager platform.Manager,
	dynManager *dynplugin.Manager,
	adminIDs map[int64]struct{},
	pluginTagProviders map[string]id3.ID3TagProvider,
	adminCommands []admincmd.Command,
	pluginSettingDefinitions []botpkg.PluginSettingDefinition,
	recognizeService recognize.Service,
) {
	platformManager = platform.NewManager()
	dynManager = dynplugin.NewManager(log)
	adminIDs = parseIDSet(conf.GetString("BotAdmin"))
	pluginTagProviders = make(map[string]id3.ID3TagProvider)
	adminCommands = make([]admincmd.Command, 0)
	pluginSettingDefinitions = make([]botpkg.PluginSettingDefinition, 0)

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
		registerContribution(platformManager, pluginTagProviders, &recognizeService, &adminCommands, &pluginSettingDefinitions, contrib, log)
	}

	pluginSettingDefinitions = append(pluginSettingDefinitions, handler.ForwardButtonSettingDefinition())
	pluginSettingDefinitions = append(pluginSettingDefinitions, handler.GroupFavoritesSettingDefinition())

	if err := dynManager.Load(ctx, conf, platformManager); err != nil {
		if log != nil {
			log.Warn("dynamic plugin load failed", "error", err)
		}
	}

	return
}

func initTelegramBot(conf *config.Config, log *logpkg.Logger) (*telegram.Bot, error) {
	tele, err := telegram.New(conf, log)
	if err != nil {
		return nil, fmt.Errorf("init telegram: %w", err)
	}
	return tele, nil
}

// Start initializes background services. Telegram startup is added in later waves.
func (a *App) Start(ctx context.Context) error {
	// Load localization catalogs before any handler can render user-facing text.
	// A failure here is non-fatal: the i18n layer degrades to echoing message IDs,
	// so the bot still starts rather than crashing on a missing locale asset.
	if _, err := i18n.Init(); err != nil && a.Logger != nil {
		a.Logger.Warn("failed to init i18n catalogs", "error", err)
	}

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

	proxyAddr := strings.TrimSpace(a.Config.GetString("DownloadProxy"))

	downloadService := download.NewDownloadService(download.DownloadServiceOptions{
		Timeout:              time.Duration(a.Config.GetInt("DownloadTimeout")) * time.Second,
		Proxy:                proxyAddr,
		CheckMD5:             a.Config.GetBool("CheckMD5"),
		MaxRetries:           a.Config.GetInt("DownloadMaxRetries"),
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
	globalRateLimitPerSecond := a.Config.GetFloat64("GlobalRateLimitPerSecond")
	if globalRateLimitPerSecond < 0 {
		globalRateLimitPerSecond = 0
	}
	globalRateLimitBurst := a.Config.GetInt("GlobalRateLimitBurst")
	if globalRateLimitBurst < 0 {
		globalRateLimitBurst = 0
	}
	rateLimiter := telegram.NewRateLimiterWithGlobal(rateLimitPerSecond, rateLimitBurst, globalRateLimitPerSecond, globalRateLimitBurst)
	rateLimiter.SetLogger(a.Logger)
	resourceLimiter := handler.NewResourceRateLimiter(buildResourceRateLimits(a.Config))
	enableAprilFools := a.Config.GetBool("EnableAprilFools")
	aprilFoolsTextPrankProbability := a.Config.GetFloat64("AprilFoolsTextPrankProbability")
	aprilFoolsTrackHijackProbability := a.Config.GetFloat64("AprilFoolsTrackHijackProbability")
	telegram.SetAprilFoolsEnabled(enableAprilFools)
	telegram.SetAprilFoolsTextPrankProbability(aprilFoolsTextPrankProbability)
	handler.SetAprilFoolsEnabled(enableAprilFools)
	handler.SetAprilFoolsTrackHijackProbability(aprilFoolsTrackHijackProbability)

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
	uploadWorkerCount := a.Config.GetInt("UploadWorkerCount")
	downloadQueueWaitLimit := a.Config.GetInt("DownloadQueueWaitLimit")
	defaultPlatform := strings.TrimSpace(a.Config.GetString("DefaultPlatform"))
	if defaultPlatform == "" {
		defaultPlatform = "netease"
	}
	searchFallback := strings.TrimSpace(a.Config.GetString("SearchFallbackPlatform"))
	if searchFallback == "" {
		searchFallback = "netease"
	}
	whitelistIDs := parseIDSet(a.Config.GetString("WhitelistChatIDs"))
	whitelist := handler.NewWhitelist(a.Config.GetBool("EnableWhitelist"), whitelistIDs, a.AdminIDs, a.ConfigPath)

	adminCommands := make([]admincmd.Command, 0, len(a.AdminCommands)+2)
	adminCommands = append(adminCommands,
		handler.BuildAccountLoginCommand(a.PlatformManager),
		a.BuildProfileCommand(),
	)
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
	defaultLyricFormat := strings.TrimSpace(a.Config.GetString("DefaultLyricFormat"))
	if defaultLyricFormat == "" {
		defaultLyricFormat = "lrc"
	}
	pageSize := a.Config.GetInt("ListPageSize")
	inlinePageSize := a.Config.GetInt("InlineListPageSize")
	playlistHandler := &handler.PlaylistHandler{
		PlatformManager: a.PlatformManager,
		Repo:            a.DB,
		RateLimiter:     rateLimiter,
		ResourceLimiter: resourceLimiter,
		DefaultQuality:  defaultQuality,
		PageSize:        pageSize,
	}
	playlistCallback := &handler.PlaylistCallbackHandler{Playlist: playlistHandler, RateLimiter: rateLimiter}

	musicHandler := &handler.MusicHandler{
		Repo:                     a.DB,
		Pool:                     a.Pool,
		Logger:                   a.Logger,
		CacheDir:                 cacheDir,
		BotName:                  botName,
		DefaultQuality:           defaultQuality,
		DefaultLyricFormat:       defaultLyricFormat,
		InlineUploadChatID:       int64(a.Config.GetInt("InlineUploadChatID")),
		DefaultPlatform:          defaultPlatform,
		FallbackPlatform:         searchFallback,
		AdminIDs:                 a.adminSet,
		AdminCommands:            adminCommands,
		PlatformManager:          a.PlatformManager,
		DownloadService:          downloadService,
		ID3Service:               id3Service,
		TagProviders:             tagProviders,
		Limiter:                  downloadLimiter,
		UploadLimiter:            uploadLimiter,
		UploadWorkerCount:        uploadWorkerCount,
		UploadQueueSize:          uploadQueueSize,
		DownloadQueueWaitLimit:   downloadQueueWaitLimit,
		UploadBot:                a.Telegram.UploadClient(),
		RateLimiter:              rateLimiter,
		ResourceLimiter:          resourceLimiter,
		Playlist:                 playlistHandler,
		RecognizeEnabled:         a.Config.GetBool("EnableRecognize"),
		EnableQueueObservability: a.Config.GetBool("BotDebug"),
		PluginSettingDefinitions: a.PluginSettingDefinitions,
	}
	musicHandler.Artist = &handler.ArtistHandler{PlatformManager: a.PlatformManager, RateLimiter: rateLimiter, ResourceLimiter: resourceLimiter, Logger: a.Logger}
	musicHandler.StartWorker(ctx)

	settingsHandler := &handler.SettingsHandler{
		Repo:                     a.DB,
		PlatformManager:          a.PlatformManager,
		RateLimiter:              rateLimiter,
		DefaultPlatform:          defaultPlatform,
		DefaultQuality:           defaultQuality,
		DefaultLyricFormat:       defaultLyricFormat,
		PluginSettingDefinitions: a.PluginSettingDefinitions,
	}
	searchHandler := &handler.SearchHandler{PlatformManager: a.PlatformManager, Repo: a.DB, RateLimiter: rateLimiter, ResourceLimiter: resourceLimiter, DefaultPlatform: defaultPlatform, FallbackPlatform: searchFallback, PageSize: pageSize}
	favoritesHandler := &handler.FavoritesHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter, Music: musicHandler, BotName: botName, Logger: a.Logger, PageSize: pageSize}
	favoriteCallback := &handler.FavoriteCallbackHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter, Music: musicHandler, Favorites: favoritesHandler, BotName: botName, Logger: a.Logger, PageSize: pageSize}
	adminHandler := &handler.AdminCommandHandler{
		BotName:     botName,
		AdminIDs:    a.adminSet,
		RateLimiter: rateLimiter,
		Commands:    adminCommands,
	}
	searchCallback := &handler.SearchCallbackHandler{Search: searchHandler, RateLimiter: rateLimiter}
	reloadHandler := &handler.ReloadHandler{Reload: a.ReloadAll, RateLimiter: rateLimiter, Logger: a.Logger, AdminIDs: a.adminSet}

	enableRecognize := a.Config.GetBool("EnableRecognize")

	var recognizeHandler handler.MessageHandler
	if enableRecognize {
		recognizeHandler = &handler.RecognizeHandler{CacheDir: cacheDir, Music: musicHandler, RateLimiter: rateLimiter, ResourceLimiter: resourceLimiter, RecognizeService: a.RecognizeService, Logger: a.Logger, DownloadBot: a.Telegram.DownloadClient()}
	}
	chosenInlineHandler := &handler.ChosenInlineMusicHandler{Music: musicHandler, RateLimiter: rateLimiter, InlinePageSize: pageSize}

	guestLyricHandler := &handler.LyricHandler{PlatformManager: a.PlatformManager, RateLimiter: rateLimiter, ResourceLimiter: resourceLimiter, Repo: a.DB, DefaultPlatform: defaultPlatform, FallbackPlatform: searchFallback, SearchHandler: searchHandler, InlineUploadChatID: int64(a.Config.GetInt("InlineUploadChatID")), UploadBot: a.Telegram.UploadClient()}
	guestModeHandler := &handler.GuestModeHandler{
		PlatformManager:  a.PlatformManager,
		Music:            musicHandler,
		LyricHandler:     guestLyricHandler,
		SearchHandler:    searchHandler,
		Favorites:        favoritesHandler,
		RateLimiter:      rateLimiter,
		ResourceLimiter:  resourceLimiter,
		RecognizeService: a.RecognizeService,
		CacheDir:         cacheDir,
		DownloadBot:      a.Telegram.DownloadClient(),
		BotName:          botName,
		DefaultPlatform:  defaultPlatform,
		FallbackPlatform: searchFallback,
		DefaultQuality:   defaultQuality,
	}

	routerLyricHandler := &handler.LyricHandler{PlatformManager: a.PlatformManager, RateLimiter: rateLimiter, ResourceLimiter: resourceLimiter, Repo: a.DB, DefaultPlatform: defaultPlatform, FallbackPlatform: searchFallback, SearchHandler: searchHandler, InlineUploadChatID: int64(a.Config.GetInt("InlineUploadChatID")), UploadBot: a.Telegram.UploadClient()}
	musicHandler.LyricHandler = routerLyricHandler
	mentionRouter := &handler.MentionRouter{
		Music:           musicHandler,
		Search:          searchHandler,
		Lyric:           routerLyricHandler,
		Recognize:       recognizeHandler,
		Favorites:       favoritesHandler,
		PlatformManager: a.PlatformManager,
		BotName:         botName,
	}

	router := &handler.Router{
		Music:                    musicHandler,
		Playlist:                 playlistHandler,
		Artist:                   musicHandler.Artist,
		Search:                   searchHandler,
		Lyric:                    routerLyricHandler,
		Recognize:                recognizeHandler,
		GuestMode:                guestModeHandler,
		MentionRouter:            mentionRouter,
		GuestSearchCallback:      &handler.GuestSearchCallbackHandler{Guest: guestModeHandler, RateLimiter: rateLimiter},
		About:                    &handler.AboutHandler{RuntimeVer: a.Build.RuntimeVer, BinVersion: a.Build.BinVersion, CommitSHA: a.Build.CommitSHA, BuildTime: a.Build.BuildTime, BuildArch: a.Build.BuildArch, DynPlugins: a.DynPlugins, RateLimiter: rateLimiter},
		Status:                   &handler.StatusHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter, AdminIDs: a.adminSet},
		Settings:                 settingsHandler,
		Favorites:                favoritesHandler,
		RmCache:                  &handler.RmCacheHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter, AdminIDs: a.adminSet},
		Callback:                 &handler.CallbackMusicHandler{Music: musicHandler, BotName: botName, RateLimiter: rateLimiter},
		SettingsCallback:         &handler.SettingsCallbackHandler{Repo: a.DB, PlatformManager: a.PlatformManager, SettingsHandler: settingsHandler, RateLimiter: rateLimiter},
		SearchCallback:           searchCallback,
		PlaylistCallback:         playlistCallback,
		InlineCollectionCallback: &handler.InlineCollectionCallbackHandler{Chosen: chosenInlineHandler, RateLimiter: rateLimiter},
		LyricCallback:            &handler.LyricCallbackHandler{PlatformManager: a.PlatformManager, RateLimiter: rateLimiter, ResourceLimiter: resourceLimiter, Repo: a.DB, DefaultPlatform: defaultPlatform, FallbackPlatform: searchFallback, InlineUploadChatID: int64(a.Config.GetInt("InlineUploadChatID")), UploadBot: a.Telegram.UploadClient()},
		FavoriteCallback:         favoriteCallback,
		DownloadQueueCallback:    &handler.DownloadQueueCallbackHandler{Music: musicHandler, RateLimiter: rateLimiter},
		Reload:                   reloadHandler,
		Admin:                    adminHandler,
		Inline:                   &handler.InlineSearchHandler{Repo: a.DB, PlatformManager: a.PlatformManager, CollectionChosen: chosenInlineHandler, Favorites: favoritesHandler, BotName: botName, DefaultPlatform: defaultPlatform, DefaultQuality: defaultQuality, FallbackPlatform: searchFallback, PageSize: inlinePageSize, ResourceLimiter: resourceLimiter},
		ChosenInline:             chosenInlineHandler,
		PlatformManager:          a.PlatformManager,
		AdminCommands:            adminCommandNames,
		Whitelist:                whitelist,
		Logger:                   a.Logger,
		Repo:                     a.DB,
	}

	updates, err := a.Telegram.Client().UpdatesViaLongPolling(ctx, telegram.LongPollingParams())
	if err != nil {
		return fmt.Errorf("init telegram: %w", err)
	}
	botHandler, err := th.NewBotHandler(a.Telegram.Client(), updates)
	if err != nil {
		return fmt.Errorf("init telegram: %w", err)
	}
	a.botHandler = botHandler

	router.Register(botHandler, botName)

	a.registerLocalizedCommands(ctx, enableRecognize)

	go func() {
		if err := botHandler.Start(); err != nil && a.Logger != nil {
			a.Logger.Error("telegram bot handler stopped with error", "error", err)
		}
	}()
	return nil
}

// localizedCommandSpec describes one bot command whose description is resolved
// from the i18n catalog per language.
type localizedCommandSpec struct {
	command   string
	descKey   string
	recognize bool // only included when audio recognition is enabled
}

// botCommandSpecs is the menu shown in Telegram clients, in display order. The
// description text is looked up per language via the i18n catalog.
var botCommandSpecs = []localizedCommandSpec{
	{command: "help", descKey: "cmd_help"},
	{command: "music", descKey: "cmd_music"},
	{command: "search", descKey: "cmd_search"},
	{command: "lyric", descKey: "cmd_lyric"},
	{command: "fav", descKey: "cmd_fav"},
	{command: "settings", descKey: "cmd_settings"},
	{command: "recognize", descKey: "cmd_recognize", recognize: true},
	{command: "status", descKey: "cmd_status"},
	{command: "about", descKey: "cmd_about"},
}

// botProfileField maps a bot-profile config key to its i18n fallback key.
type botProfileField struct {
	configKey string // key inside [bot_profile.<lang>]
	i18nKey   string // embedded catalog key used when no override is set
}

var botProfileFields = []botProfileField{
	{configKey: "name", i18nKey: "bot_name"},
	{configKey: "description", i18nKey: "bot_description"},
	{configKey: "short_description", i18nKey: "bot_short_description"},
}

// effectiveBotProfile resolves the value for a profile field in a language:
// a [bot_profile.<lang>] config override wins over the embedded i18n default.
// The bool reports whether the value is safe to publish (non-empty and not the
// echoed i18n key that signals a missing catalog entry).
func (a *App) effectiveBotProfile(lang string, f botProfileField, loc *i18n.Localizer) (string, bool) {
	if a.Config != nil {
		if v := strings.TrimSpace(a.Config.GetBotProfileField(lang, f.configKey)); v != "" {
			return v, true
		}
	}
	v := loc.T(f.i18nKey)
	if strings.TrimSpace(v) == "" || v == f.i18nKey {
		return "", false
	}
	return v, true
}

// pushBotProfile publishes the bot name and (short) description for one
// language table to Telegram. profileLang is the language used to look up
// config overrides and the i18n catalog; langCode is the Telegram
// language_code to write under (empty = the default table).
func (a *App) pushBotProfile(ctx context.Context, langCode, profileLang string) {
	client := a.Telegram.Client()
	loc := i18n.For(profileLang)

	if name, ok := a.effectiveBotProfile(profileLang, botProfileFields[0], loc); ok {
		params := &telego.SetMyNameParams{Name: name}
		if langCode != "" {
			params.LanguageCode = langCode
		}
		if err := client.SetMyName(ctx, params); err != nil && a.Logger != nil {
			a.Logger.Debug("failed to set bot name", "lang", langCode, "error", err)
		}
	}
	if desc, ok := a.effectiveBotProfile(profileLang, botProfileFields[1], loc); ok {
		params := &telego.SetMyDescriptionParams{Description: desc}
		if langCode != "" {
			params.LanguageCode = langCode
		}
		if err := client.SetMyDescription(ctx, params); err != nil && a.Logger != nil {
			a.Logger.Debug("failed to set bot description", "lang", langCode, "error", err)
		}
	}
	if short, ok := a.effectiveBotProfile(profileLang, botProfileFields[2], loc); ok {
		params := &telego.SetMyShortDescriptionParams{ShortDescription: short}
		if langCode != "" {
			params.LanguageCode = langCode
		}
		if err := client.SetMyShortDescription(ctx, params); err != nil && a.Logger != nil {
			a.Logger.Debug("failed to set bot short description", "lang", langCode, "error", err)
		}
	}
}

// registerLocalizedCommands publishes the command menu, bot name, and bot
// descriptions to Telegram once per supported language. Telegram stores one
// table per (scope, language_code) and serves each client the table matching
// its UI language, falling back to the empty-language_code default table.
//
// We register the default table with the fallback language (English) so every
// user sees a usable menu, then one table per remaining supported language. The
// language_code Telegram accepts is a 2-letter ISO 639-1 code — exactly the
// codes i18n.SupportedLanguages already uses.
//
// Bot name / description / short description honor per-language overrides from
// the [bot_profile.<lang>] config sections, falling back to the embedded i18n
// catalog (see effectiveBotProfile).
func (a *App) registerLocalizedCommands(ctx context.Context, enableRecognize bool) {
	client := a.Telegram.Client()

	buildCommands := func(loc *i18n.Localizer) []telego.BotCommand {
		out := make([]telego.BotCommand, 0, len(botCommandSpecs))
		for _, spec := range botCommandSpecs {
			if spec.recognize && !enableRecognize {
				continue
			}
			out = append(out, telego.BotCommand{
				Command:     spec.command,
				Description: loc.T(spec.descKey),
			})
		}
		return out
	}

	// Apply one (scope=default) catalog per language. The fallback language is
	// also written with an empty language_code so it becomes the default table.
	apply := func(langCode, profileLang string) {
		params := &telego.SetMyCommandsParams{Commands: buildCommands(i18n.For(profileLang))}
		if langCode != "" {
			params.LanguageCode = langCode
		}
		if err := client.SetMyCommands(ctx, params); err != nil && a.Logger != nil {
			a.Logger.Warn("failed to set bot commands", "lang", langCode, "error", err)
		}
		a.pushBotProfile(ctx, langCode, profileLang)
	}

	// Default table (empty language_code) uses the fallback language so users
	// whose client language has no dedicated table still get a complete menu.
	apply("", i18n.DefaultLanguage)
	for _, lang := range i18n.SupportedLanguages {
		apply(lang, lang)
	}
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

// ReloadAll reloads config and reinitializes all platform plugins at runtime.
func (a *App) ReloadAll(ctx context.Context) error {
	if strings.TrimSpace(a.ConfigPath) == "" {
		return fmt.Errorf("config path missing")
	}
	conf, err := config.Load(a.ConfigPath)
	if err != nil {
		return err
	}
	a.Config = conf
	// 重建 admin 集合并原子发布：所有 handler 共享 a.adminSet，Replace 后立即对它们生效，
	// 且不与并发读冲突（handler 通过 AdminSet 的 atomic 快照读取，不再触碰 a.AdminIDs）。
	a.AdminIDs = parseIDSet(conf.GetString("BotAdmin"))
	a.adminSet.Replace(a.AdminIDs)

	if dm, ok := a.PlatformManager.(*platform.DefaultManager); ok {
		dm.Reset()
	} else {
		return fmt.Errorf("platform manager does not support reset")
	}

	dynManager := a.DynPlugins
	if dynManager == nil {
		dynManager = dynplugin.NewManager(a.Logger)
		a.DynPlugins = dynManager
	}
	pluginTagProviders := make(map[string]id3.ID3TagProvider)
	adminCommands := make([]admincmd.Command, 0)
	pluginSettingDefinitions := make([]botpkg.PluginSettingDefinition, 0)
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
			if a.Logger != nil {
				a.Logger.Info("plugin disabled by config", "plugin", name)
			}
			continue
		}
		factory, ok := platformplugins.Get(name)
		if !ok {
			continue
		}
		contrib, err := factory(conf, a.Logger)
		if err != nil {
			if a.Logger != nil {
				a.Logger.Error("plugin init failed", "plugin", name, "error", err)
			}
			continue
		}
		registerContribution(a.PlatformManager, pluginTagProviders, &recognizeService, &adminCommands, &pluginSettingDefinitions, contrib, a.Logger)
	}

	if err := dynManager.Reload(ctx, conf, a.PlatformManager); err != nil {
		if a.Logger != nil {
			a.Logger.Warn("dynamic plugin reload failed", "error", err)
		}
	}
	a.TagProviders = pluginTagProviders
	a.AdminCommands = adminCommands
	a.PluginSettingDefinitions = pluginSettingDefinitions
	if recognizeService != nil {
		if a.RecognizeService != nil {
			_ = a.RecognizeService.Stop()
		}
		a.RecognizeService = recognizeService
		if err := a.RecognizeService.Start(ctx); err != nil && a.Logger != nil {
			a.Logger.Warn("failed to start recognition service after reload", "error", err)
		}
	}
	return nil
}

// ReloadDynamicPlugins reloads script-based plugins from disk.
func (a *App) ReloadDynamicPlugins(ctx context.Context) error {
	return a.ReloadAll(ctx)
}

func parseIDSet(raw string) map[int64]struct{} {
	ids := make(map[int64]struct{})
	for _, value := range splitAdminIDs(raw) {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			ids[id] = struct{}{}
		}
	}
	return ids
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

	// 关闭平台插件持有的后台守护协程（如 bilibili/kugou 的 Cookie 自动续期）。
	if dm, ok := a.PlatformManager.(*platform.DefaultManager); ok {
		_ = dm.Close()
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

// buildResourceRateLimits assembles the per-action rate-limit rules for the
// ResourceRateLimiter from config. Each action has its own window/per-user/
// per-platform/global quota. A non-positive quota disables that dimension; an
// action whose three quotas are all zero is effectively unlimited. The defaults
// (set in config.go) throttle the abusable platform-API entry points: search,
// lyric fetch, download/decrypt, audio recognition, playlist/album paging,
// episode listing, and artist lookups.
func buildResourceRateLimits(c *config.Config) map[string]handler.ResourceLimit {
	win := func(key string) time.Duration {
		secs := c.GetInt(key)
		if secs <= 0 {
			secs = 60
		}
		return time.Duration(secs) * time.Second
	}
	window := win("ResourceRateLimitWindowSeconds")
	rule := func(prefix string) handler.ResourceLimit {
		return handler.ResourceLimit{
			Window:      window,
			PerUser:     c.GetInt(prefix + "PerUser"),
			PerPlatform: c.GetInt(prefix + "PerPlatform"),
			Global:      c.GetInt(prefix + "Global"),
		}
	}
	return map[string]handler.ResourceLimit{
		handler.ActionSearch:    rule("SearchRateLimit"),
		handler.ActionLyric:     rule("LyricRateLimit"),
		handler.ActionDownload:  rule("DownloadRateLimit"),
		handler.ActionRecognize: rule("RecognizeRateLimit"),
		handler.ActionPlaylist:  rule("PlaylistRateLimit"),
		handler.ActionEpisode:   rule("EpisodeRateLimit"),
		handler.ActionArtist:    rule("ArtistRateLimit"),
	}
}
