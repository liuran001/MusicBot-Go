package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	if err := db.AutoMigrate(&GroupSettingsModel{}); err != nil {
		return nil, err
	}
	if err := migrateSettingsAutoLinkDetect(db); err != nil {
		return nil, err
	}

	if err := migrateToMultiPlatform(db); err != nil {
		return nil, err
	}

	if err := migrateToQualityBasedCache(db); err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)

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

	if err := db.Exec("ALTER TABLE song_infos ADD COLUMN quality TEXT NOT NULL DEFAULT 'high'").Error; err != nil {
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
		"PRAGMA foreign_keys=ON;",
	}
	for _, stmt := range pragmas {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetUserSettings retrieves settings for a user, creating default if not exists.
func (r *Repository) GetUserSettings(ctx context.Context, userID int64) (*bot.UserSettings, error) {
	var settings UserSettingsModel
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&settings).Error
	if err == gorm.ErrRecordNotFound {
		settings = UserSettingsModel{
			UserID:          userID,
			DefaultPlatform: r.defaultPlatform,
			DefaultQuality:  r.defaultQuality,
			AutoDeleteList:  false,
			AutoLinkDetect:  true,
		}
		if createErr := r.db.WithContext(ctx).Create(&settings).Error; createErr != nil {
			if errors.Is(createErr, gorm.ErrDuplicatedKey) {
				reloadErr := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&settings).Error
				if reloadErr != nil {
					return nil, reloadErr
				}
			} else {
				return nil, createErr
			}
		}
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
		}, nil
	}
	if err != nil {
		return nil, err
	}
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
	}, nil
}

// GetGroupSettings retrieves settings for a group, creating default if not exists.
func (r *Repository) GetGroupSettings(ctx context.Context, chatID int64) (*bot.GroupSettings, error) {
	var settings GroupSettingsModel
	err := r.db.WithContext(ctx).Where("chat_id = ?", chatID).First(&settings).Error
	if err == gorm.ErrRecordNotFound {
		settings = GroupSettingsModel{
			ChatID:          chatID,
			DefaultPlatform: r.defaultPlatform,
			DefaultQuality:  r.defaultQuality,
			AutoDeleteList:  true,
			AutoLinkDetect:  true,
		}
		if createErr := r.db.WithContext(ctx).Create(&settings).Error; createErr != nil {
			if errors.Is(createErr, gorm.ErrDuplicatedKey) {
				reloadErr := r.db.WithContext(ctx).Where("chat_id = ?", chatID).First(&settings).Error
				if reloadErr != nil {
					return nil, reloadErr
				}
			} else {
				return nil, createErr
			}
		}
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
		}, nil
	}
	if err != nil {
		return nil, err
	}
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
	}, nil
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
