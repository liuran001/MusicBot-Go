package db

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/liuran001/MusicBot-Go/bot"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Repository provides access to the song cache database.
type Repository struct {
	db              *gorm.DB
	defaultPlatform string
	defaultQuality  string
}

// NewSQLiteRepository creates a repository backed by SQLite.
func NewSQLiteRepository(dsn string, gormLogger logger.Interface) (*Repository, error) {
	if dsn == "" {
		return nil, fmt.Errorf("dsn required")
	}

	if gormLogger == nil {
		gormLogger = logger.Default.LogMode(logger.Silent)
	}

	dbDir := filepath.Dir(dsn)
	if dbDir != "" && dbDir != "." {
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
		Logger:                 gormLogger,
	})
	if err != nil {
		return nil, err
	}

	if err := applySQLitePragmas(db); err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&SongInfoModel{}, &UserSettingsModel{}, &BotStatModel{}); err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&GroupSettingsModel{}, &PluginSettingModel{}); err != nil {
		return nil, err
	}
	if err := migrateSettingsAutoLinkDetect(db); err != nil {
		return nil, err
	}
	if err := migratePluginSettingsFromLegacyBilibiliParse(db); err != nil {
		return nil, err
	}

	if err := migrateToMultiPlatform(db); err != nil {
		return nil, err
	}

	if err := migrateToQualityBasedCache(db); err != nil {
		return nil, err
	}

	if err := ensureSQLiteIndexes(db); err != nil {
		return nil, err
	}

	maxOpen, maxIdle, maxLifetime := sqlitePoolDefaultsFromEnv()
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxLifetime(maxLifetime)

	return &Repository{
		db:              db,
		defaultPlatform: "netease",
		defaultQuality:  "hires",
	}, nil
}

// ConfigurePool updates the database connection pool settings.
func (r *Repository) ConfigurePool(maxOpen, maxIdle int, maxLifetime time.Duration) error {
	if r == nil || r.db == nil {
		return errors.New("repository not configured")
	}
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	if maxOpen >= 0 {
		sqlDB.SetMaxOpenConns(maxOpen)
	}
	if maxIdle >= 0 {
		sqlDB.SetMaxIdleConns(maxIdle)
	}
	if maxLifetime >= 0 {
		sqlDB.SetConnMaxLifetime(maxLifetime)
	}
	return nil
}

// SetDefaults sets repository defaults for new settings records.
func (r *Repository) SetDefaults(defaultPlatform, defaultQuality string) {
	if r == nil {
		return
	}
	if strings.TrimSpace(defaultPlatform) != "" {
		r.defaultPlatform = defaultPlatform
	}
	if strings.TrimSpace(defaultQuality) != "" {
		r.defaultQuality = defaultQuality
	}
}

func migrateToMultiPlatform(db *gorm.DB) error {
	var columnExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('song_infos') WHERE name='platform'").Scan(&columnExists).Error; err != nil {
		return fmt.Errorf("check platform column: %w", err)
	}

	if columnExists {
		return nil
	}

	if err := db.Exec("ALTER TABLE song_infos ADD COLUMN platform TEXT NOT NULL DEFAULT 'netease'").Error; err != nil {
		return fmt.Errorf("add platform column: %w", err)
	}

	if err := db.Exec("ALTER TABLE song_infos ADD COLUMN track_id TEXT NOT NULL DEFAULT ''").Error; err != nil {
		return fmt.Errorf("add track_id column: %w", err)
	}

	if err := db.Exec("UPDATE song_infos SET track_id = CAST(music_id AS TEXT) WHERE track_id = ''").Error; err != nil {
		return fmt.Errorf("populate track_id from music_id: %w", err)
	}

	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_platform_track ON song_infos(platform, track_id)").Error; err != nil {
		return fmt.Errorf("create unique index: %w", err)
	}

	return nil
}

func migrateToQualityBasedCache(db *gorm.DB) error {
	var columnExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('song_infos') WHERE name='quality'").Scan(&columnExists).Error; err != nil {
		return fmt.Errorf("check quality column: %w", err)
	}

	if columnExists {
		return nil
	}

	if err := db.Exec("ALTER TABLE song_infos ADD COLUMN quality TEXT NOT NULL DEFAULT 'hires'").Error; err != nil {
		return fmt.Errorf("add quality column: %w", err)
	}

	if err := db.Exec("DROP INDEX IF EXISTS idx_platform_track").Error; err != nil {
		return fmt.Errorf("drop old index: %w", err)
	}

	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_platform_track_quality ON song_infos(platform, track_id, quality)").Error; err != nil {
		return fmt.Errorf("create new unique index: %w", err)
	}

	return nil
}

func migrateSettingsAutoLinkDetect(db *gorm.DB) error {
	var userColumnExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('user_settings') WHERE name='auto_link_detect'").Scan(&userColumnExists).Error; err != nil {
		return fmt.Errorf("check user_settings.auto_link_detect column: %w", err)
	}
	if !userColumnExists {
		if err := db.Exec("ALTER TABLE user_settings ADD COLUMN auto_link_detect NUMERIC NOT NULL DEFAULT 1").Error; err != nil {
			return fmt.Errorf("add user_settings.auto_link_detect column: %w", err)
		}
	}

	var groupColumnExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('group_settings') WHERE name='auto_link_detect'").Scan(&groupColumnExists).Error; err != nil {
		return fmt.Errorf("check group_settings.auto_link_detect column: %w", err)
	}
	if !groupColumnExists {
		if err := db.Exec("ALTER TABLE group_settings ADD COLUMN auto_link_detect NUMERIC NOT NULL DEFAULT 1").Error; err != nil {
			return fmt.Errorf("add group_settings.auto_link_detect column: %w", err)
		}
	}

	return nil
}

func migratePluginSettingsFromLegacyBilibiliParse(db *gorm.DB) error {
	var userColumnExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('user_settings') WHERE name='bilibili_parse_mode'").Scan(&userColumnExists).Error; err != nil {
		return fmt.Errorf("check legacy user bilibili_parse_mode column: %w", err)
	}
	if userColumnExists {
		if err := db.Exec(`
			INSERT INTO plugin_settings (scope_type, scope_id, plugin, setting_key, setting_value, created_at, updated_at)
			SELECT 'user', user_id, 'bilibili', 'parse_mode', bilibili_parse_mode, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
			FROM user_settings
			WHERE bilibili_parse_mode IS NOT NULL AND TRIM(bilibili_parse_mode) != ''
			ON CONFLICT(scope_type, scope_id, plugin, setting_key)
			DO UPDATE SET setting_value=excluded.setting_value, updated_at=CURRENT_TIMESTAMP
		`).Error; err != nil {
			return fmt.Errorf("migrate legacy user bilibili_parse_mode to plugin_settings: %w", err)
		}
	}

	var groupColumnExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('group_settings') WHERE name='bilibili_parse_mode'").Scan(&groupColumnExists).Error; err != nil {
		return fmt.Errorf("check legacy group bilibili_parse_mode column: %w", err)
	}
	if groupColumnExists {
		if err := db.Exec(`
			INSERT INTO plugin_settings (scope_type, scope_id, plugin, setting_key, setting_value, created_at, updated_at)
			SELECT 'group', chat_id, 'bilibili', 'parse_mode', bilibili_parse_mode, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
			FROM group_settings
			WHERE bilibili_parse_mode IS NOT NULL AND TRIM(bilibili_parse_mode) != ''
			ON CONFLICT(scope_type, scope_id, plugin, setting_key)
			DO UPDATE SET setting_value=excluded.setting_value, updated_at=CURRENT_TIMESTAMP
		`).Error; err != nil {
			return fmt.Errorf("migrate legacy group bilibili_parse_mode to plugin_settings: %w", err)
		}
	}

	return nil
}

// FindByMusicID returns a cached song by MusicID (legacy NetEase support).
func (r *Repository) FindByMusicID(ctx context.Context, musicID int) (*bot.SongInfo, error) {
	var model SongInfoModel
	err := r.db.WithContext(ctx).Where("platform = ? AND music_id = ?", "netease", musicID).First(&model).Error
	if err != nil {
		return nil, err
	}
	return toInternal(model), nil
}

// FindByPlatformTrackID returns a cached song by platform, track ID and quality.
func (r *Repository) FindByPlatformTrackID(ctx context.Context, platform, trackID, quality string) (*bot.SongInfo, error) {
	var model SongInfoModel
	err := r.db.WithContext(ctx).Where("platform = ? AND track_id = ? AND quality = ?", platform, trackID, quality).First(&model).Error
	if err != nil {
		return nil, err
	}
	return toInternal(model), nil
}

// SearchCachedSongs searches cached songs by keyword with optional platform/quality filters.
func (r *Repository) SearchCachedSongs(ctx context.Context, keyword, platformName, quality string, limit int) ([]*bot.SongInfo, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not configured")
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 3
	}
	if limit > 20 {
		limit = 20
	}

	lowerKeyword := strings.ToLower(keyword)
	likeValue := "%" + lowerKeyword + "%"

	query := r.db.WithContext(ctx).Model(&SongInfoModel{}).
		Where("file_id <> ''").
		Where("song_name <> ''").
		Where("(LOWER(song_name) LIKE ? OR LOWER(song_artists) LIKE ? OR LOWER(song_album) LIKE ?)", likeValue, likeValue, likeValue)

	if strings.TrimSpace(platformName) != "" {
		query = query.Where("platform = ?", strings.TrimSpace(platformName))
	}
	if strings.TrimSpace(quality) != "" {
		query = query.Where("quality = ?", strings.TrimSpace(quality))
	}

	// Basic relevance: song_name exact > song_name contains > artists contains > album contains.
	query = query.Order(clause.Expr{
		SQL: "CASE " +
			"WHEN LOWER(song_name) = ? THEN 0 " +
			"WHEN LOWER(song_name) LIKE ? THEN 1 " +
			"WHEN LOWER(song_artists) LIKE ? THEN 2 " +
			"WHEN LOWER(song_album) LIKE ? THEN 3 " +
			"ELSE 4 END",
		Vars: []any{lowerKeyword, likeValue, likeValue, likeValue},
	}).Order("updated_at DESC").Limit(limit)

	var models []SongInfoModel
	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, nil
	}
	results := make([]*bot.SongInfo, 0, len(models))
	for _, model := range models {
		results = append(results, toInternal(model))
	}
	return results, nil
}

// FindRandomCachedSong returns a random cached song with valid file payload.
func (r *Repository) FindRandomCachedSong(ctx context.Context) (*bot.SongInfo, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not configured")
	}
	query := r.db.WithContext(ctx).
		Model(&SongInfoModel{}).
		Where("file_id <> ''").
		Where("song_name <> ''")

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, nil
	}
	offset := rand.Int63n(count)

	var model SongInfoModel
	err := r.db.WithContext(ctx).
		Model(&SongInfoModel{}).
		Where("file_id <> ''").
		Where("song_name <> ''").
		Offset(int(offset)).
		Limit(1).
		Take(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toInternal(model), nil
}

// FindByFileID returns a cached song by FileID.
func (r *Repository) FindByFileID(ctx context.Context, fileID string) (*bot.SongInfo, error) {
	var model SongInfoModel
	err := r.db.WithContext(ctx).Where("file_id = ?", fileID).First(&model).Error
	if err != nil {
		return nil, err
	}
	return toInternal(model), nil
}

// Create inserts a new song record.
func (r *Repository) Create(ctx context.Context, song *bot.SongInfo) error {
	if song != nil && song.ID != 0 {
		return r.Update(ctx, song)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model := toModel(song)
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "platform"},
				{Name: "track_id"},
				{Name: "quality"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"deleted_at",
				"updated_at",
				"music_id",
				"song_name",
				"song_artists",
				"song_artists_ids",
				"song_album",
				"album_id",
				"track_url",
				"album_url",
				"song_artists_urls",
				"file_ext",
				"music_size",
				"pic_size",
				"emb_pic_size",
				"bit_rate",
				"duration",
				"file_id",
				"thumb_file_id",
				"from_user_id",
				"from_user_name",
				"from_chat_id",
				"from_chat_name",
			}),
		}).Create(model).Error; err != nil {
			return err
		}
		if err := tx.Where("platform = ? AND track_id = ? AND quality = ?", model.Platform, model.TrackID, model.Quality).First(model).Error; err != nil {
			return err
		}
		song.ID = model.ID
		song.CreatedAt = model.CreatedAt
		song.UpdatedAt = model.UpdatedAt
		return nil
	})
}

// Update updates an existing song record.
func (r *Repository) Update(ctx context.Context, song *bot.SongInfo) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model := toModel(song)
		return tx.Save(model).Error
	})
}

// Delete removes a song by MusicID (legacy NetEase support).
func (r *Repository) Delete(ctx context.Context, musicID int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Delete(&SongInfoModel{}, "platform = ? AND music_id = ?", "netease", musicID).Error
	})
}

// DeleteAll clears all cached songs.
func (r *Repository) DeleteAll(ctx context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("repository not configured")
	}
	return r.db.WithContext(ctx).
		Session(&gorm.Session{AllowGlobalUpdate: true}).
		Delete(&SongInfoModel{}).Error
}

// DeleteAllByPlatform clears cached songs for a specific platform.
func (r *Repository) DeleteAllByPlatform(ctx context.Context, platform string) error {
	if r == nil || r.db == nil {
		return errors.New("repository not configured")
	}
	return r.db.WithContext(ctx).
		Where("platform = ?", platform).
		Delete(&SongInfoModel{}).Error
}

// DeleteByPlatformTrackID removes a song by platform, track ID and quality.
func (r *Repository) DeleteByPlatformTrackID(ctx context.Context, platform, trackID, quality string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Delete(&SongInfoModel{}, "platform = ? AND track_id = ? AND quality = ?", platform, trackID, quality).Error
	})
}

func (r *Repository) DeleteAllQualitiesByPlatformTrackID(ctx context.Context, platform, trackID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Delete(&SongInfoModel{}, "platform = ? AND track_id = ?", platform, trackID).Error
	})
}

// Count returns total cached songs.
func (r *Repository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&SongInfoModel{}).Count(&count).Error
	return count, err
}

// CountByUserID returns cached count by user ID.
func (r *Repository) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&SongInfoModel{}).Where("from_user_id = ?", userID).Count(&count).Error
	return count, err
}

// CountByChatID returns cached count by chat ID.
func (r *Repository) CountByChatID(ctx context.Context, chatID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&SongInfoModel{}).Where("from_chat_id = ?", chatID).Count(&count).Error
	return count, err
}

// CountByPlatform returns cached counts grouped by platform.
func (r *Repository) CountByPlatform(ctx context.Context) (map[string]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not configured")
	}
	rows := make([]struct {
		Platform string
		Count    int64
	}, 0)
	err := r.db.WithContext(ctx).Model(&SongInfoModel{}).
		Select("platform, COUNT(*) as count").
		Group("platform").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, row := range rows {
		result[row.Platform] = row.Count
	}
	return result, nil
}

// GetSendCount returns total successful send count.
func (r *Repository) GetSendCount(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("repository not configured")
	}
	var stat BotStatModel
	err := r.db.WithContext(ctx).Where("key = ?", "send_count").First(&stat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return stat.Value, nil
}

// IncrementSendCount increments total successful send count.
func (r *Repository) IncrementSendCount(ctx context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("repository not configured")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&BotStatModel{}).Where("key = ?", "send_count").UpdateColumn("value", gorm.Expr("value + ?", 1))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			return nil
		}
		return tx.Create(&BotStatModel{Key: "send_count", Value: 1}).Error
	})
}

// Last returns the last cached record.
func (r *Repository) Last(ctx context.Context) (*bot.SongInfo, error) {
	var model SongInfoModel
	if err := r.db.WithContext(ctx).Last(&model).Error; err != nil {
		return nil, err
	}
	return toInternal(model), nil
}

func applySQLitePragmas(db *gorm.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA cache_size=-64000;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, stmt := range pragmas {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureSQLiteIndexes(db *gorm.DB) error {
	indexStatements := []string{
		"CREATE INDEX IF NOT EXISTS idx_song_infos_platform_music_id ON song_infos(platform, music_id)",
		"CREATE INDEX IF NOT EXISTS idx_song_infos_file_id ON song_infos(file_id)",
		"CREATE INDEX IF NOT EXISTS idx_song_infos_from_user_id ON song_infos(from_user_id)",
		"CREATE INDEX IF NOT EXISTS idx_song_infos_from_chat_id ON song_infos(from_chat_id)",
		"CREATE INDEX IF NOT EXISTS idx_song_infos_platform_quality_updated_at ON song_infos(platform, quality, updated_at DESC)",
	}
	for _, stmt := range indexStatements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func sqlitePoolDefaultsFromEnv() (maxOpen, maxIdle int, maxLifetime time.Duration) {
	maxOpen = 4
	maxIdle = 2
	maxLifetime = time.Hour

	if value := strings.TrimSpace(os.Getenv("MUSICBOT_DB_SQLITE_MAX_OPEN_CONNS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			maxOpen = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("MUSICBOT_DB_SQLITE_MAX_IDLE_CONNS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			maxIdle = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("MUSICBOT_DB_SQLITE_CONN_MAX_LIFETIME")); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed >= 0 {
			maxLifetime = parsed
		}
	}

	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}

	return maxOpen, maxIdle, maxLifetime
}

func userSettingsToInternal(settings UserSettingsModel) *bot.UserSettings {
	var deletedAt *time.Time
	if settings.DeletedAt.Valid {
		deletedAt = &settings.DeletedAt.Time
	}
	return &bot.UserSettings{
		ID:              settings.ID,
		CreatedAt:       settings.CreatedAt,
		UpdatedAt:       settings.UpdatedAt,
		DeletedAt:       deletedAt,
		UserID:          settings.UserID,
		DefaultPlatform: settings.DefaultPlatform,
		DefaultQuality:  settings.DefaultQuality,
		AutoDeleteList:  settings.AutoDeleteList,
		AutoLinkDetect:  settings.AutoLinkDetect,
	}
}

func groupSettingsToInternal(settings GroupSettingsModel) *bot.GroupSettings {
	var deletedAt *time.Time
	if settings.DeletedAt.Valid {
		deletedAt = &settings.DeletedAt.Time
	}
	return &bot.GroupSettings{
		ID:              settings.ID,
		CreatedAt:       settings.CreatedAt,
		UpdatedAt:       settings.UpdatedAt,
		DeletedAt:       deletedAt,
		ChatID:          settings.ChatID,
		DefaultPlatform: settings.DefaultPlatform,
		DefaultQuality:  settings.DefaultQuality,
		AutoDeleteList:  settings.AutoDeleteList,
		AutoLinkDetect:  settings.AutoLinkDetect,
	}
}

// GetUserSettings retrieves settings for a user, creating default if not exists.
func (r *Repository) GetUserSettings(ctx context.Context, userID int64) (*bot.UserSettings, error) {
	var settings UserSettingsModel
	err := r.db.WithContext(ctx).
		Where(UserSettingsModel{UserID: userID}).
		Attrs(UserSettingsModel{
			DefaultPlatform: r.defaultPlatform,
			DefaultQuality:  r.defaultQuality,
			AutoDeleteList:  false,
			AutoLinkDetect:  true,
		}).
		FirstOrCreate(&settings).Error
	if isSQLiteUniqueConstraint(err) {
		err = r.db.WithContext(ctx).Where("user_id = ?", userID).First(&settings).Error
	}
	if err != nil {
		return nil, err
	}
	return userSettingsToInternal(settings), nil
}

// GetGroupSettings retrieves settings for a group, creating default if not exists.
func (r *Repository) GetGroupSettings(ctx context.Context, chatID int64) (*bot.GroupSettings, error) {
	var settings GroupSettingsModel
	err := r.db.WithContext(ctx).
		Where(GroupSettingsModel{ChatID: chatID}).
		Attrs(GroupSettingsModel{
			DefaultPlatform: r.defaultPlatform,
			DefaultQuality:  r.defaultQuality,
			AutoDeleteList:  true,
			AutoLinkDetect:  true,
		}).
		FirstOrCreate(&settings).Error
	if isSQLiteUniqueConstraint(err) {
		err = r.db.WithContext(ctx).Where("chat_id = ?", chatID).First(&settings).Error
	}
	if err != nil {
		return nil, err
	}
	return groupSettingsToInternal(settings), nil
}

func isSQLiteUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "unique constraint failed")
}

// UpdateUserSettings updates user settings.
func (r *Repository) UpdateUserSettings(ctx context.Context, settings *bot.UserSettings) error {
	model := UserSettingsModel{
		Model: gorm.Model{
			ID:        settings.ID,
			CreatedAt: settings.CreatedAt,
			UpdatedAt: settings.UpdatedAt,
		},
		UserID:          settings.UserID,
		DefaultPlatform: settings.DefaultPlatform,
		DefaultQuality:  settings.DefaultQuality,
		AutoDeleteList:  settings.AutoDeleteList,
		AutoLinkDetect:  settings.AutoLinkDetect,
	}
	if settings.DeletedAt != nil {
		model.DeletedAt = gorm.DeletedAt{Time: *settings.DeletedAt, Valid: true}
	}
	return r.db.WithContext(ctx).Save(&model).Error
}

// UpdateGroupSettings updates group settings.
func (r *Repository) UpdateGroupSettings(ctx context.Context, settings *bot.GroupSettings) error {
	model := GroupSettingsModel{
		Model: gorm.Model{
			ID:        settings.ID,
			CreatedAt: settings.CreatedAt,
			UpdatedAt: settings.UpdatedAt,
		},
		ChatID:          settings.ChatID,
		DefaultPlatform: settings.DefaultPlatform,
		DefaultQuality:  settings.DefaultQuality,
		AutoDeleteList:  settings.AutoDeleteList,
		AutoLinkDetect:  settings.AutoLinkDetect,
	}
	if settings.DeletedAt != nil {
		model.DeletedAt = gorm.DeletedAt{Time: *settings.DeletedAt, Valid: true}
	}
	return r.db.WithContext(ctx).Save(&model).Error
}

// GetPluginSetting returns plugin setting value by scope/plugin/key.
func (r *Repository) GetPluginSetting(ctx context.Context, scopeType string, scopeID int64, plugin string, key string) (string, error) {
	var model PluginSettingModel
	err := r.db.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ? AND plugin = ? AND setting_key = ?", scopeType, scopeID, plugin, key).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return model.SettingValue, nil
}

// SetPluginSetting upserts plugin setting value by scope/plugin/key.
func (r *Repository) SetPluginSetting(ctx context.Context, scopeType string, scopeID int64, plugin string, key string, value string) error {
	model := PluginSettingModel{
		ScopeType:    strings.TrimSpace(scopeType),
		ScopeID:      scopeID,
		Plugin:       strings.TrimSpace(plugin),
		SettingKey:   strings.TrimSpace(key),
		SettingValue: strings.TrimSpace(value),
	}
	if model.ScopeType == "" || model.ScopeID == 0 || model.Plugin == "" || model.SettingKey == "" {
		return fmt.Errorf("invalid plugin setting key")
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}, {Name: "plugin"}, {Name: "setting_key"}},
		DoUpdates: clause.Assignments(map[string]any{
			"setting_value": model.SettingValue,
			"updated_at":    time.Now(),
		}),
	}).Create(&model).Error
}

// Close closes the database connection.
func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	sqlDB, err := r.db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}
	return sqlDB.Close()
}
