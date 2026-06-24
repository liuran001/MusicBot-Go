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

// Repository provides access to the song cache and configuration databases.
type Repository struct {
	cacheDB            *gorm.DB
	dataDB             *gorm.DB
	defaultPlatform    string
	defaultQuality     string
	defaultLyricFormat string
}

// NewSQLiteRepository creates a repository backed by SQLite.
func NewSQLiteRepository(cacheDSN, dataDSN string, gormLogger logger.Interface) (*Repository, error) {
	if cacheDSN == "" || dataDSN == "" {
		return nil, fmt.Errorf("dsns required")
	}

	if gormLogger == nil {
		gormLogger = logger.Default.LogMode(logger.Silent)
	}

	for _, dsn := range []string{cacheDSN, dataDSN} {
		dbDir := filepath.Dir(dsn)
		if dbDir != "" && dbDir != "." {
			if err := os.MkdirAll(dbDir, 0755); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
	}

	cacheDB, err := gorm.Open(sqlite.Open(cacheDSN), &gorm.Config{
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
		Logger:                 gormLogger,
	})
	if err != nil {
		return nil, err
	}
	if err := applySQLitePragmas(cacheDB); err != nil {
		return nil, err
	}

	dataDB, err := gorm.Open(sqlite.Open(dataDSN), &gorm.Config{
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
		Logger:                 gormLogger,
	})
	if err != nil {
		return nil, err
	}
	if err := applySQLitePragmas(dataDB); err != nil {
		return nil, err
	}

	if err := performDataMigration(cacheDB, dataDB, cacheDSN); err != nil {
		return nil, fmt.Errorf("data migration failed: %w", err)
	}

	if err := cacheDB.AutoMigrate(&SongInfoModel{}); err != nil {
		return nil, err
	}
	if err := dataDB.AutoMigrate(&UserSettingsModel{}, &BotStatModel{}, &GroupSettingsModel{}, &PluginSettingModel{}, &FavoriteModel{}); err != nil {
		return nil, err
	}

	if err := migrateSettingsAutoLinkDetect(dataDB); err != nil {
		return nil, err
	}
	if err := migrateSettingsDefaultLyricFormat(dataDB); err != nil {
		return nil, err
	}
	if err := migrateSettingsDefaultLyricFlags(dataDB); err != nil {
		return nil, err
	}
	if err := migratePluginSettingsFromLegacyBilibiliParse(dataDB); err != nil {
		return nil, err
	}

	if err := migrateToMultiPlatform(cacheDB); err != nil {
		return nil, err
	}
	if err := migrateToQualityBasedCache(cacheDB); err != nil {
		return nil, err
	}
	if err := ensureSQLiteIndexes(cacheDB); err != nil {
		return nil, err
	}

	maxOpen, maxIdle, maxLifetime := sqlitePoolDefaultsFromEnv()
	cacheSQLDB, _ := cacheDB.DB()
	cacheSQLDB.SetMaxOpenConns(maxOpen)
	cacheSQLDB.SetMaxIdleConns(maxIdle)
	cacheSQLDB.SetConnMaxLifetime(maxLifetime)

	dataSQLDB, _ := dataDB.DB()
	dataSQLDB.SetMaxOpenConns(maxOpen)
	dataSQLDB.SetMaxIdleConns(maxIdle)
	dataSQLDB.SetConnMaxLifetime(maxLifetime)

	return &Repository{
		cacheDB:            cacheDB,
		dataDB:             dataDB,
		defaultPlatform:    "netease",
		defaultQuality:     "hires",
		defaultLyricFormat: "lrc",
	}, nil
}

// performDataMigration checks if legacy tables exist in cacheDB, backs up the DB, migrates data to dataDB, and drops tables.
func performDataMigration(cacheDB, dataDB *gorm.DB, cacheDSN string) error {
	legacyTables := []string{"user_settings", "group_settings", "plugin_settings", "bot_stats"}
	var tablesToMigrate []string
	for _, table := range legacyTables {
		if cacheDB.Migrator().HasTable(table) {
			tablesToMigrate = append(tablesToMigrate, table)
		}
	}

	if len(tablesToMigrate) == 0 {
		return nil // Nothing to migrate
	}

	dir := filepath.Dir(cacheDSN)
	base := filepath.Base(cacheDSN)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".db"
	}
	backupPath := filepath.Join(dir, fmt.Sprintf("%s-%s%s.bak", name, time.Now().Format("20060102150405"), ext))

	input, err := os.ReadFile(cacheDSN)
	if err == nil {
		_ = os.WriteFile(backupPath, input, 0644)
	}

	for _, table := range tablesToMigrate {
		switch table {
		case "user_settings":
			_ = dataDB.AutoMigrate(&UserSettingsModel{})
			var records []UserSettingsModel
			if err := cacheDB.Find(&records).Error; err == nil && len(records) > 0 {
				if err := dataDB.CreateInBatches(records, 100).Error; err != nil {
					return fmt.Errorf("migrate %s: %w", table, err)
				}
			}
		case "group_settings":
			_ = dataDB.AutoMigrate(&GroupSettingsModel{})
			var records []GroupSettingsModel
			if err := cacheDB.Find(&records).Error; err == nil && len(records) > 0 {
				if err := dataDB.CreateInBatches(records, 100).Error; err != nil {
					return fmt.Errorf("migrate %s: %w", table, err)
				}
			}
		case "plugin_settings":
			_ = dataDB.AutoMigrate(&PluginSettingModel{})
			var records []PluginSettingModel
			if err := cacheDB.Find(&records).Error; err == nil && len(records) > 0 {
				if err := dataDB.CreateInBatches(records, 100).Error; err != nil {
					return fmt.Errorf("migrate %s: %w", table, err)
				}
			}
		case "bot_stats":
			_ = dataDB.AutoMigrate(&BotStatModel{})
			var records []BotStatModel
			if err := cacheDB.Find(&records).Error; err == nil && len(records) > 0 {
				if err := dataDB.CreateInBatches(records, 100).Error; err != nil {
					return fmt.Errorf("migrate %s: %w", table, err)
				}
			}
		}
		if err := cacheDB.Migrator().DropTable(table); err != nil {
			return fmt.Errorf("drop legacy table %s: %w", table, err)
		}
	}

	return nil
}

// ConfigurePool updates the database connection pool settings.
func (r *Repository) ConfigurePool(maxOpen, maxIdle int, maxLifetime time.Duration) error {
	if r == nil || r.cacheDB == nil || r.dataDB == nil {
		return errors.New("repository not configured")
	}
	for _, db := range []*gorm.DB{r.cacheDB, r.dataDB} {
		sqlDB, err := db.DB()
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
	}
	return nil
}

// SetDefaults sets repository defaults for new settings records.
func (r *Repository) SetDefaults(defaultPlatform, defaultQuality, defaultLyricFormat string) {
	if r == nil {
		return
	}
	if strings.TrimSpace(defaultPlatform) != "" {
		r.defaultPlatform = defaultPlatform
	}
	if strings.TrimSpace(defaultQuality) != "" {
		r.defaultQuality = defaultQuality
	}
	if strings.TrimSpace(defaultLyricFormat) != "" {
		r.defaultLyricFormat = defaultLyricFormat
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

// migrateSettingsDefaultLyricFormat adds the default_lyric_format column to the
// user/group settings tables for installs created before the per-scope default
// lyric format setting existed.
func migrateSettingsDefaultLyricFormat(db *gorm.DB) error {
	var userColumnExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('user_settings') WHERE name='default_lyric_format'").Scan(&userColumnExists).Error; err != nil {
		return fmt.Errorf("check user_settings.default_lyric_format column: %w", err)
	}
	if !userColumnExists {
		if err := db.Exec("ALTER TABLE user_settings ADD COLUMN default_lyric_format TEXT NOT NULL DEFAULT 'lrc'").Error; err != nil {
			return fmt.Errorf("add user_settings.default_lyric_format column: %w", err)
		}
	}

	var groupColumnExists bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('group_settings') WHERE name='default_lyric_format'").Scan(&groupColumnExists).Error; err != nil {
		return fmt.Errorf("check group_settings.default_lyric_format column: %w", err)
	}
	if !groupColumnExists {
		if err := db.Exec("ALTER TABLE group_settings ADD COLUMN default_lyric_format TEXT NOT NULL DEFAULT 'lrc'").Error; err != nil {
			return fmt.Errorf("add group_settings.default_lyric_format column: %w", err)
		}
	}

	return nil
}

// migrateSettingsDefaultLyricFlags adds the nullable default_lyric_include_*
// columns to the user/group settings tables. They are intentionally NULLABLE:
// a NULL means "unset" (fall back to the per-format default), so existing rows
// keep the historical behavior instead of being forced to a concrete on/off.
func migrateSettingsDefaultLyricFlags(db *gorm.DB) error {
	type column struct{ table, name string }
	columns := []column{
		{"user_settings", "default_lyric_include_translation"},
		{"user_settings", "default_lyric_include_roma"},
		{"group_settings", "default_lyric_include_translation"},
		{"group_settings", "default_lyric_include_roma"},
	}
	for _, col := range columns {
		var exists bool
		if err := db.Raw(
			fmt.Sprintf("SELECT COUNT(*) > 0 FROM pragma_table_info('%s') WHERE name='%s'", col.table, col.name),
		).Scan(&exists).Error; err != nil {
			return fmt.Errorf("check %s.%s column: %w", col.table, col.name, err)
		}
		if exists {
			continue
		}
		if err := db.Exec(
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s NUMERIC", col.table, col.name),
		).Error; err != nil {
			return fmt.Errorf("add %s.%s column: %w", col.table, col.name, err)
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
	err := r.cacheDB.WithContext(ctx).Where("platform = ? AND music_id = ?", "netease", musicID).First(&model).Error
	if err != nil {
		return nil, err
	}
	return toInternal(model), nil
}

// FindByPlatformTrackID returns a cached song by platform, track ID and quality.
func (r *Repository) FindByPlatformTrackID(ctx context.Context, platform, trackID, quality string) (*bot.SongInfo, error) {
	var model SongInfoModel
	err := r.cacheDB.WithContext(ctx).Where("platform = ? AND track_id = ? AND quality = ?", platform, trackID, quality).First(&model).Error
	if err != nil {
		return nil, err
	}
	return toInternal(model), nil
}

// SearchCachedSongs searches cached songs by keyword with optional platform/quality filters.
func (r *Repository) SearchCachedSongs(ctx context.Context, keyword, platformName, quality string, limit int) ([]*bot.SongInfo, error) {
	if r == nil || r.cacheDB == nil {
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

	query := r.cacheDB.WithContext(ctx).Model(&SongInfoModel{}).
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
	if r == nil || r.cacheDB == nil {
		return nil, errors.New("repository not configured")
	}
	query := r.cacheDB.WithContext(ctx).
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
	err := r.cacheDB.WithContext(ctx).
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
	err := r.cacheDB.WithContext(ctx).Where("file_id = ?", fileID).First(&model).Error
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
	return r.cacheDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	return r.cacheDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model := toModel(song)
		return tx.Save(model).Error
	})
}

// VerifyAndUpdateQuality reconciles a cached track's stored quality label with
// the platform-reported quality, then flags the record as verified.
//
//   - oldQuality == newQuality: the label was already correct, just set
//     quality_verified = true.
//   - oldQuality != newQuality: the cached label was wrong. The unique index on
//     (platform, track_id, quality) forbids two colliding rows, so if a record
//     already exists under newQuality we flag it verified and drop the stale
//     oldQuality row; otherwise we relabel the existing row to newQuality.
func (r *Repository) VerifyAndUpdateQuality(ctx context.Context, platform, trackID, oldQuality, newQuality string) error {
	if r == nil || r.cacheDB == nil {
		return errors.New("repository not configured")
	}
	return r.cacheDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if oldQuality == newQuality {
			return tx.Model(&SongInfoModel{}).
				Where("platform = ? AND track_id = ? AND quality = ?", platform, trackID, oldQuality).
				Update("quality_verified", true).Error
		}

		var existing SongInfoModel
		err := tx.Where("platform = ? AND track_id = ? AND quality = ?", platform, trackID, newQuality).First(&existing).Error
		if err == nil {
			// A record already exists under the correct quality: keep it, mark it
			// verified, and drop the stale oldQuality row.
			if updErr := tx.Model(&SongInfoModel{}).
				Where("platform = ? AND track_id = ? AND quality = ?", platform, trackID, newQuality).
				Update("quality_verified", true).Error; updErr != nil {
				return updErr
			}
			return tx.Delete(&SongInfoModel{}, "platform = ? AND track_id = ? AND quality = ?", platform, trackID, oldQuality).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// No record under newQuality: relabel the existing oldQuality row.
		return tx.Model(&SongInfoModel{}).
			Where("platform = ? AND track_id = ? AND quality = ?", platform, trackID, oldQuality).
			Updates(map[string]any{"quality": newQuality, "quality_verified": true}).Error
	})
}

// Delete removes a song by MusicID (legacy NetEase support).
func (r *Repository) Delete(ctx context.Context, musicID int) error {
	return r.cacheDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Delete(&SongInfoModel{}, "platform = ? AND music_id = ?", "netease", musicID).Error
	})
}

// DeleteAll clears all cached songs.
func (r *Repository) DeleteAll(ctx context.Context) error {
	if r == nil || r.cacheDB == nil {
		return errors.New("repository not configured")
	}
	return r.cacheDB.WithContext(ctx).
		Session(&gorm.Session{AllowGlobalUpdate: true}).
		Delete(&SongInfoModel{}).Error
}

// DeleteAllByPlatform clears cached songs for a specific platform.
func (r *Repository) DeleteAllByPlatform(ctx context.Context, platform string) error {
	if r == nil || r.cacheDB == nil {
		return errors.New("repository not configured")
	}
	return r.cacheDB.WithContext(ctx).
		Where("platform = ?", platform).
		Delete(&SongInfoModel{}).Error
}

// DeleteByPlatformTrackID removes a song by platform, track ID and quality.
func (r *Repository) DeleteByPlatformTrackID(ctx context.Context, platform, trackID, quality string) error {
	return r.cacheDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Delete(&SongInfoModel{}, "platform = ? AND track_id = ? AND quality = ?", platform, trackID, quality).Error
	})
}

func (r *Repository) DeleteAllQualitiesByPlatformTrackID(ctx context.Context, platform, trackID string) error {
	return r.cacheDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Delete(&SongInfoModel{}, "platform = ? AND track_id = ?", platform, trackID).Error
	})
}

// Count returns total cached songs.
func (r *Repository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.cacheDB.WithContext(ctx).Model(&SongInfoModel{}).Count(&count).Error
	return count, err
}

// CountByUserID returns cached count by user ID.
func (r *Repository) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := r.cacheDB.WithContext(ctx).Model(&SongInfoModel{}).Where("from_user_id = ?", userID).Count(&count).Error
	return count, err
}

// CountByChatID returns cached count by chat ID.
func (r *Repository) CountByChatID(ctx context.Context, chatID int64) (int64, error) {
	var count int64
	err := r.cacheDB.WithContext(ctx).Model(&SongInfoModel{}).Where("from_chat_id = ?", chatID).Count(&count).Error
	return count, err
}

// CountByPlatform returns cached counts grouped by platform.
func (r *Repository) CountByPlatform(ctx context.Context) (map[string]int64, error) {
	if r == nil || r.cacheDB == nil {
		return nil, errors.New("repository not configured")
	}
	rows := make([]struct {
		Platform string
		Count    int64
	}, 0)
	err := r.cacheDB.WithContext(ctx).Model(&SongInfoModel{}).
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
	if r == nil || r.dataDB == nil {
		return 0, errors.New("repository not configured")
	}
	var stat BotStatModel
	err := r.dataDB.WithContext(ctx).Where("key = ?", "send_count").First(&stat).Error
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
	if r == nil || r.dataDB == nil {
		return errors.New("repository not configured")
	}
	return r.dataDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	if err := r.cacheDB.WithContext(ctx).Last(&model).Error; err != nil {
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
		ID:                             settings.ID,
		CreatedAt:                      settings.CreatedAt,
		UpdatedAt:                      settings.UpdatedAt,
		DeletedAt:                      deletedAt,
		UserID:                         settings.UserID,
		DefaultPlatform:                settings.DefaultPlatform,
		DefaultQuality:                 settings.DefaultQuality,
		AutoDeleteList:                 settings.AutoDeleteList,
		AutoLinkDetect:                 settings.AutoLinkDetect,
		DefaultLyricFormat:             settings.DefaultLyricFormat,
		DefaultLyricIncludeTranslation: settings.DefaultLyricIncludeTranslation,
		DefaultLyricIncludeRoma:        settings.DefaultLyricIncludeRoma,
	}
}

func groupSettingsToInternal(settings GroupSettingsModel) *bot.GroupSettings {
	var deletedAt *time.Time
	if settings.DeletedAt.Valid {
		deletedAt = &settings.DeletedAt.Time
	}
	return &bot.GroupSettings{
		ID:                             settings.ID,
		CreatedAt:                      settings.CreatedAt,
		UpdatedAt:                      settings.UpdatedAt,
		DeletedAt:                      deletedAt,
		ChatID:                         settings.ChatID,
		DefaultPlatform:                settings.DefaultPlatform,
		DefaultQuality:                 settings.DefaultQuality,
		AutoDeleteList:                 settings.AutoDeleteList,
		AutoLinkDetect:                 settings.AutoLinkDetect,
		DefaultLyricFormat:             settings.DefaultLyricFormat,
		DefaultLyricIncludeTranslation: settings.DefaultLyricIncludeTranslation,
		DefaultLyricIncludeRoma:        settings.DefaultLyricIncludeRoma,
	}
}

// GetUserSettings retrieves settings for a user, creating default if not exists.
func (r *Repository) GetUserSettings(ctx context.Context, userID int64) (*bot.UserSettings, error) {
	var settings UserSettingsModel
	err := r.dataDB.WithContext(ctx).
		Where(UserSettingsModel{UserID: userID}).
		Attrs(UserSettingsModel{
			DefaultPlatform:    r.defaultPlatform,
			DefaultQuality:     r.defaultQuality,
			AutoDeleteList:     false,
			AutoLinkDetect:     true,
			DefaultLyricFormat: r.defaultLyricFormat,
		}).
		FirstOrCreate(&settings).Error
	if isSQLiteUniqueConstraint(err) {
		err = r.dataDB.WithContext(ctx).Where("user_id = ?", userID).First(&settings).Error
	}
	if err != nil {
		return nil, err
	}
	return userSettingsToInternal(settings), nil
}

// GetGroupSettings retrieves settings for a group, creating default if not exists.
func (r *Repository) GetGroupSettings(ctx context.Context, chatID int64) (*bot.GroupSettings, error) {
	var settings GroupSettingsModel
	err := r.dataDB.WithContext(ctx).
		Where(GroupSettingsModel{ChatID: chatID}).
		Attrs(GroupSettingsModel{
			DefaultPlatform:    r.defaultPlatform,
			DefaultQuality:     r.defaultQuality,
			AutoDeleteList:     true,
			AutoLinkDetect:     true,
			DefaultLyricFormat: r.defaultLyricFormat,
		}).
		FirstOrCreate(&settings).Error
	if isSQLiteUniqueConstraint(err) {
		err = r.dataDB.WithContext(ctx).Where("chat_id = ?", chatID).First(&settings).Error
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
		UserID:             settings.UserID,
		DefaultPlatform:    settings.DefaultPlatform,
		DefaultQuality:     settings.DefaultQuality,
		AutoDeleteList:     settings.AutoDeleteList,
		AutoLinkDetect:     settings.AutoLinkDetect,
		DefaultLyricFormat: settings.DefaultLyricFormat,

		DefaultLyricIncludeTranslation: settings.DefaultLyricIncludeTranslation,
		DefaultLyricIncludeRoma:        settings.DefaultLyricIncludeRoma,
	}
	if settings.DeletedAt != nil {
		model.DeletedAt = gorm.DeletedAt{Time: *settings.DeletedAt, Valid: true}
	}
	return r.dataDB.WithContext(ctx).Save(&model).Error
}

// UpdateGroupSettings updates group settings.
func (r *Repository) UpdateGroupSettings(ctx context.Context, settings *bot.GroupSettings) error {
	model := GroupSettingsModel{
		Model: gorm.Model{
			ID:        settings.ID,
			CreatedAt: settings.CreatedAt,
			UpdatedAt: settings.UpdatedAt,
		},
		ChatID:             settings.ChatID,
		DefaultPlatform:    settings.DefaultPlatform,
		DefaultQuality:     settings.DefaultQuality,
		AutoDeleteList:     settings.AutoDeleteList,
		AutoLinkDetect:     settings.AutoLinkDetect,
		DefaultLyricFormat: settings.DefaultLyricFormat,

		DefaultLyricIncludeTranslation: settings.DefaultLyricIncludeTranslation,
		DefaultLyricIncludeRoma:        settings.DefaultLyricIncludeRoma,
	}
	if settings.DeletedAt != nil {
		model.DeletedAt = gorm.DeletedAt{Time: *settings.DeletedAt, Valid: true}
	}
	return r.dataDB.WithContext(ctx).Save(&model).Error
}

// GetPluginSetting returns plugin setting value by scope/plugin/key.
func (r *Repository) GetPluginSetting(ctx context.Context, scopeType string, scopeID int64, plugin string, key string) (string, error) {
	var model PluginSettingModel
	err := r.dataDB.WithContext(ctx).
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

	return r.dataDB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}, {Name: "plugin"}, {Name: "setting_key"}},
		DoUpdates: clause.Assignments(map[string]any{
			"setting_value": model.SettingValue,
			"updated_at":    time.Now(),
		}),
	}).Create(&model).Error
}

// favoriteScope normalizes a scope type, returning "" when invalid so callers
// can reject the request before touching the database.
func favoriteScope(scopeType string) string {
	switch strings.TrimSpace(strings.ToLower(scopeType)) {
	case bot.FavoriteScopeUser:
		return bot.FavoriteScopeUser
	case bot.FavoriteScopeGroup:
		return bot.FavoriteScopeGroup
	default:
		return ""
	}
}

// IsFavorited reports whether (scope, platform, trackID) is currently favorited.
func (r *Repository) IsFavorited(ctx context.Context, scopeType string, scopeID int64, platform, trackID string) (bool, error) {
	if r == nil || r.dataDB == nil {
		return false, errors.New("repository not configured")
	}
	scope := favoriteScope(scopeType)
	if scope == "" || scopeID == 0 {
		return false, nil
	}
	var count int64
	err := r.dataDB.WithContext(ctx).Model(&FavoriteModel{}).
		Where("scope_type = ? AND scope_id = ? AND platform = ? AND track_id = ?", scope, scopeID, strings.TrimSpace(platform), strings.TrimSpace(trackID)).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetFavorite returns a single favorite, or (nil, nil) when not found.
func (r *Repository) GetFavorite(ctx context.Context, scopeType string, scopeID int64, platform, trackID string) (*bot.Favorite, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	scope := favoriteScope(scopeType)
	if scope == "" || scopeID == 0 {
		return nil, nil
	}
	var model FavoriteModel
	err := r.dataDB.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ? AND platform = ? AND track_id = ?", scope, scopeID, strings.TrimSpace(platform), strings.TrimSpace(trackID)).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toFavorite(model), nil
}

// AddFavorite inserts a favorite, idempotently refreshing the denormalized song
// metadata on conflict. The original collector (AddedByUserID) is preserved.
func (r *Repository) AddFavorite(ctx context.Context, fav *bot.Favorite) error {
	if r == nil || r.dataDB == nil {
		return errors.New("repository not configured")
	}
	if fav == nil {
		return errors.New("nil favorite")
	}
	scope := favoriteScope(fav.ScopeType)
	if scope == "" || fav.ScopeID == 0 || strings.TrimSpace(fav.Platform) == "" || strings.TrimSpace(fav.TrackID) == "" {
		return errors.New("invalid favorite key")
	}
	model := toFavoriteModel(fav)
	model.ScopeType = scope
	model.Platform = strings.TrimSpace(fav.Platform)
	model.TrackID = strings.TrimSpace(fav.TrackID)
	return r.dataDB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}, {Name: "platform"}, {Name: "track_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"song_name":         model.SongName,
			"song_artists":      model.SongArtists,
			"song_album":        model.SongAlbum,
			"track_url":         model.TrackURL,
			"song_artists_urls": model.SongArtistsURLs,
			"updated_at":        time.Now(),
		}),
	}).Create(model).Error
}

// RemoveFavorite hard-deletes a favorite. A hard delete (not soft) is required
// so the row vacates the unique index and the same track can be re-favorited.
func (r *Repository) RemoveFavorite(ctx context.Context, scopeType string, scopeID int64, platform, trackID string) error {
	if r == nil || r.dataDB == nil {
		return errors.New("repository not configured")
	}
	scope := favoriteScope(scopeType)
	if scope == "" || scopeID == 0 {
		return errors.New("invalid favorite key")
	}
	return r.dataDB.WithContext(ctx).Unscoped().
		Where("scope_type = ? AND scope_id = ? AND platform = ? AND track_id = ?", scope, scopeID, strings.TrimSpace(platform), strings.TrimSpace(trackID)).
		Delete(&FavoriteModel{}).Error
}

// ListFavorites returns favorites for a scope, newest first, with paging.
func (r *Repository) ListFavorites(ctx context.Context, scopeType string, scopeID int64, limit, offset int) ([]*bot.Favorite, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	scope := favoriteScope(scopeType)
	if scope == "" || scopeID == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	if offset < 0 {
		offset = 0
	}
	var models []FavoriteModel
	err := r.dataDB.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ?", scope, scopeID).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, err
	}
	results := make([]*bot.Favorite, 0, len(models))
	for _, model := range models {
		results = append(results, toFavorite(model))
	}
	return results, nil
}

// CountFavorites returns how many favorites a scope holds.
func (r *Repository) CountFavorites(ctx context.Context, scopeType string, scopeID int64) (int64, error) {
	if r == nil || r.dataDB == nil {
		return 0, errors.New("repository not configured")
	}
	scope := favoriteScope(scopeType)
	if scope == "" || scopeID == 0 {
		return 0, nil
	}
	var count int64
	err := r.dataDB.WithContext(ctx).Model(&FavoriteModel{}).
		Where("scope_type = ? AND scope_id = ?", scope, scopeID).
		Count(&count).Error
	return count, err
}

// RandomFavorite returns a random favorite from a scope, or (nil, nil) if empty.
func (r *Repository) RandomFavorite(ctx context.Context, scopeType string, scopeID int64) (*bot.Favorite, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	scope := favoriteScope(scopeType)
	if scope == "" || scopeID == 0 {
		return nil, nil
	}
	var model FavoriteModel
	err := r.dataDB.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ?", scope, scopeID).
		Order("RANDOM()").
		Limit(1).
		Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toFavorite(model), nil
}

// FindCachedSongMeta returns cached metadata for a track regardless of quality,
// preferring the most recently updated row. Used to denormalize song name/artist
// into a favorite when only (platform, trackID) is known (e.g. a button click).
func (r *Repository) FindCachedSongMeta(ctx context.Context, platform, trackID string) (*bot.SongInfo, error) {
	if r == nil || r.cacheDB == nil {
		return nil, errors.New("repository not configured")
	}
	platform = strings.TrimSpace(platform)
	trackID = strings.TrimSpace(trackID)
	if platform == "" || trackID == "" {
		return nil, nil
	}
	var model SongInfoModel
	err := r.cacheDB.WithContext(ctx).
		Where("platform = ? AND track_id = ?", platform, trackID).
		Order("updated_at DESC").
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toInternal(model), nil
}

// Close closes the database connection.
func (r *Repository) Close() error {
	if r == nil || r.cacheDB == nil || r.dataDB == nil {
		return nil
	}
	if sqlDB, err := r.cacheDB.DB(); err == nil {
		_ = sqlDB.Close()
	}
	if sqlDB, err := r.dataDB.DB(); err == nil {
		_ = sqlDB.Close()
	}
	return nil
}
