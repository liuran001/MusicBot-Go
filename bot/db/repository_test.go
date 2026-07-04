package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"gorm.io/gorm/logger"
)

func TestRepositoryCRUD(t *testing.T) {
	file, err := os.CreateTemp("", "music163bot-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)

	file2, err := os.CreateTemp("", "music163bot-data-*.db")
	if err != nil {
		t.Fatalf("create temp data db: %v", err)
	}
	dataPath := file2.Name()
	_ = file2.Close()
	defer os.Remove(dataPath)

	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	gormLogger := logpkg.NewGormLogger(base, logger.Silent)

	repo, err := NewSQLiteRepository(path, dataPath, gormLogger)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	repo.SetDefaults("netease", "hires", "lrc")

	ctx := context.Background()
	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty db")
	}

	noLyrics := false
	song := &bot.SongInfo{
		MusicID:         1,
		SongName:        "Song",
		SongArtists:     "Artist",
		SongAlbum:       "Album",
		FileExt:         "mp3",
		MusicSize:       123,
		Duration:        10,
		LyricsAvailable: &noLyrics,
	}
	if err := repo.Create(ctx, song); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := repo.Last(ctx); err != nil {
		t.Fatalf("last: %v", err)
	}

	if _, err := repo.CountByUserID(ctx, song.FromUserID); err != nil {
		t.Fatalf("count by user: %v", err)
	}

	if _, err := repo.CountByChatID(ctx, song.FromChatID); err != nil {
		t.Fatalf("count by chat: %v", err)
	}

	loaded, err := repo.FindByMusicID(ctx, 1)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if loaded.SongName != "Song" {
		t.Fatalf("unexpected song name: %s", loaded.SongName)
	}
	if loaded.Quality != "hires" {
		t.Fatalf("unexpected default song quality: %s", loaded.Quality)
	}
	if loaded.LyricsAvailable == nil || *loaded.LyricsAvailable {
		t.Fatalf("expected lyrics availability false, got %v", loaded.LyricsAvailable)
	}

	loaded.SongName = "Song Updated"
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("update: %v", err)
	}

	loaded, err = repo.FindByMusicID(ctx, 1)
	if err != nil {
		t.Fatalf("find after update: %v", err)
	}
	if loaded.SongName != "Song Updated" {
		t.Fatalf("update not persisted")
	}

	userSettings, err := repo.GetUserSettings(ctx, 123)
	if err != nil {
		t.Fatalf("get user settings: %v", err)
	}
	if userSettings.DefaultQuality != "hires" {
		t.Fatalf("unexpected default user quality: %s", userSettings.DefaultQuality)
	}
	if userSettings.AutoDeleteList {
		t.Fatalf("unexpected default user auto delete: %v", userSettings.AutoDeleteList)
	}
	if !userSettings.AutoLinkDetect {
		t.Fatalf("unexpected default user auto link detect: %v", userSettings.AutoLinkDetect)
	}

	groupSettings, err := repo.GetGroupSettings(ctx, -1001)
	if err != nil {
		t.Fatalf("get group settings: %v", err)
	}
	if groupSettings.DefaultQuality != "hires" {
		t.Fatalf("unexpected default group quality: %s", groupSettings.DefaultQuality)
	}
	if !groupSettings.AutoDeleteList {
		t.Fatalf("unexpected default group auto delete: %v", groupSettings.AutoDeleteList)
	}
	if !groupSettings.AutoLinkDetect {
		t.Fatalf("unexpected default group auto link detect: %v", groupSettings.AutoLinkDetect)
	}

	if err := repo.Delete(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestRepositoryDefaultLyricFlagsPersistence(t *testing.T) {
	file, err := os.CreateTemp("", "music163bot-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)

	file2, err := os.CreateTemp("", "music163bot-data-*.db")
	if err != nil {
		t.Fatalf("create temp data db: %v", err)
	}
	dataPath := file2.Name()
	_ = file2.Close()
	defer os.Remove(dataPath)

	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	gormLogger := logpkg.NewGormLogger(base, logger.Silent)
	repo, err := NewSQLiteRepository(path, dataPath, gormLogger)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	ctx := context.Background()

	// Freshly-created settings must leave the flags unset (nil), so /lyric keeps
	// the per-format defaults instead of being forced on/off.
	settings, err := repo.GetUserSettings(ctx, 555)
	if err != nil {
		t.Fatalf("get user settings: %v", err)
	}
	if settings.DefaultLyricIncludeTranslation != nil || settings.DefaultLyricIncludeRoma != nil {
		t.Fatalf("new settings should have nil lyric flags, got (%v,%v)",
			settings.DefaultLyricIncludeTranslation, settings.DefaultLyricIncludeRoma)
	}

	// Set explicit values and persist.
	on := true
	off := false
	settings.DefaultLyricIncludeTranslation = &off
	settings.DefaultLyricIncludeRoma = &on
	if err := repo.UpdateUserSettings(ctx, settings); err != nil {
		t.Fatalf("update user settings: %v", err)
	}

	reloaded, err := repo.GetUserSettings(ctx, 555)
	if err != nil {
		t.Fatalf("reget user settings: %v", err)
	}
	if reloaded.DefaultLyricIncludeTranslation == nil || *reloaded.DefaultLyricIncludeTranslation {
		t.Errorf("translation flag should persist as false, got %v", reloaded.DefaultLyricIncludeTranslation)
	}
	if reloaded.DefaultLyricIncludeRoma == nil || !*reloaded.DefaultLyricIncludeRoma {
		t.Errorf("roma flag should persist as true, got %v", reloaded.DefaultLyricIncludeRoma)
	}

	// Same for group settings.
	group, err := repo.GetGroupSettings(ctx, -1009)
	if err != nil {
		t.Fatalf("get group settings: %v", err)
	}
	if group.DefaultLyricIncludeTranslation != nil {
		t.Fatalf("new group settings should have nil translation flag")
	}
	group.DefaultLyricIncludeTranslation = &on
	if err := repo.UpdateGroupSettings(ctx, group); err != nil {
		t.Fatalf("update group settings: %v", err)
	}
	regroup, err := repo.GetGroupSettings(ctx, -1009)
	if err != nil {
		t.Fatalf("reget group settings: %v", err)
	}
	if regroup.DefaultLyricIncludeTranslation == nil || !*regroup.DefaultLyricIncludeTranslation {
		t.Errorf("group translation flag should persist as true, got %v", regroup.DefaultLyricIncludeTranslation)
	}
	if regroup.DefaultLyricIncludeRoma != nil {
		t.Errorf("group roma flag should stay nil, got %v", regroup.DefaultLyricIncludeRoma)
	}
}

func TestRepositoryCreateAfterSoftDeleteByTrackQuality(t *testing.T) {
	file, err := os.CreateTemp("", "music163bot-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)

	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	gormLogger := logpkg.NewGormLogger(base, logger.Silent)

	file2, err2 := os.CreateTemp("", "music163bot-data-*.db")
	if err2 != nil {
		t.Fatalf("create temp data db: %v", err2)
	}
	dataPath := file2.Name()
	_ = file2.Close()
	defer os.Remove(dataPath)

	repo, err := NewSQLiteRepository(path, dataPath, gormLogger)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	ctx := context.Background()
	song := &bot.SongInfo{
		Platform:    "qqmusic",
		TrackID:     "004HmEBY3HfyE5",
		Quality:     "hires",
		SongName:    "Test Song",
		SongArtists: "Test Artist",
		SongAlbum:   "Test Album",
		FileExt:     "flac",
		MusicSize:   12345,
		Duration:    200,
		FileID:      "file-id-1",
	}
	if err := repo.Create(ctx, song); err != nil {
		t.Fatalf("create before delete: %v", err)
	}

	if err := repo.DeleteAllQualitiesByPlatformTrackID(ctx, song.Platform, song.TrackID); err != nil {
		t.Fatalf("delete track cache: %v", err)
	}

	recreated := &bot.SongInfo{
		Platform:    song.Platform,
		TrackID:     song.TrackID,
		Quality:     song.Quality,
		SongName:    "Test Song Recreated",
		SongArtists: "Test Artist",
		SongAlbum:   "Test Album",
		FileExt:     "flac",
		MusicSize:   23456,
		Duration:    201,
		FileID:      "file-id-2",
	}
	if err := repo.Create(ctx, recreated); err != nil {
		t.Fatalf("create after soft delete: %v", err)
	}

	loaded, err := repo.FindByPlatformTrackID(ctx, song.Platform, song.TrackID, song.Quality)
	if err != nil {
		t.Fatalf("find recreated song: %v", err)
	}
	if loaded == nil || loaded.FileID != "file-id-2" {
		t.Fatalf("unexpected recreated song file id: %+v", loaded)
	}

	var softDeletedCount int64
	if err := repo.cacheDB.Unscoped().
		Model(&SongInfoModel{}).
		Where("platform = ? AND track_id = ? AND quality = ? AND deleted_at IS NOT NULL", song.Platform, song.TrackID, song.Quality).
		Count(&softDeletedCount).Error; err != nil {
		t.Fatalf("count soft-deleted rows: %v", err)
	}
	if softDeletedCount != 0 {
		t.Fatalf("expected no soft-deleted rows after recreate, got %d", softDeletedCount)
	}

}

func TestRepositoryDeleteAllByPlatform(t *testing.T) {
	file, err := os.CreateTemp("", "music163bot-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)

	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	gormLogger := logpkg.NewGormLogger(base, logger.Silent)

	file2, err2 := os.CreateTemp("", "music163bot-data-*.db")
	if err2 != nil {
		t.Fatalf("create temp data db: %v", err2)
	}
	dataPath := file2.Name()
	_ = file2.Close()
	defer os.Remove(dataPath)

	repo, err := NewSQLiteRepository(path, dataPath, gormLogger)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	ctx := context.Background()
	songs := []*bot.SongInfo{
		{Platform: "netease", TrackID: "1", Quality: "high", SongName: "N1", FileID: "f1"},
		{Platform: "qqmusic", TrackID: "2", Quality: "high", SongName: "Q1", FileID: "f2"},
		{Platform: "qqmusic", TrackID: "3", Quality: "hires", SongName: "Q2", FileID: "f3"},
	}
	for _, song := range songs {
		if err := repo.Create(ctx, song); err != nil {
			t.Fatalf("create song: %v", err)
		}
	}

	if err := repo.DeleteAllByPlatform(ctx, "qqmusic"); err != nil {
		t.Fatalf("delete all by platform: %v", err)
	}

	if _, err := repo.FindByPlatformTrackID(ctx, "qqmusic", "2", "high"); err == nil {
		t.Fatalf("expected qqmusic high record deleted")
	}
	if _, err := repo.FindByPlatformTrackID(ctx, "qqmusic", "3", "hires"); err == nil {
		t.Fatalf("expected qqmusic hires record deleted")
	}
	if _, err := repo.FindByPlatformTrackID(ctx, "netease", "1", "high"); err != nil {
		t.Fatalf("expected netease record kept: %v", err)
	}
}

func TestRepositoryGetUserSettingsConcurrentCreate(t *testing.T) {
	file, err := os.CreateTemp("", "music163bot-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)

	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	gormLogger := logpkg.NewGormLogger(base, logger.Silent)

	file2, err2 := os.CreateTemp("", "music163bot-data-*.db")
	if err2 != nil {
		t.Fatalf("create temp data db: %v", err2)
	}
	dataPath := file2.Name()
	_ = file2.Close()
	defer os.Remove(dataPath)

	repo, err := NewSQLiteRepository(path, dataPath, gormLogger)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	ctx := context.Background()
	const workers = 12
	results := make(chan *bot.UserSettings, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			settings, getErr := repo.GetUserSettings(ctx, 777)
			if getErr != nil {
				errs <- getErr
				return
			}
			results <- settings
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for getErr := range errs {
		t.Fatalf("unexpected get user settings error: %v", getErr)
	}

	var baseID uint
	count := 0
	for settings := range results {
		count++
		if settings == nil {
			t.Fatal("nil settings")
		}
		if baseID == 0 {
			baseID = settings.ID
			continue
		}
		if settings.ID != baseID {
			t.Fatalf("expected same settings row id, got %d and %d", baseID, settings.ID)
		}
	}
	if count != workers {
		t.Fatalf("expected %d results, got %d", workers, count)
	}
}

func TestSQLitePoolDefaultsFromEnv(t *testing.T) {
	t.Setenv("MUSICBOT_DB_SQLITE_MAX_OPEN_CONNS", "")
	t.Setenv("MUSICBOT_DB_SQLITE_MAX_IDLE_CONNS", "")
	t.Setenv("MUSICBOT_DB_SQLITE_CONN_MAX_LIFETIME", "")

	maxOpen, maxIdle, maxLifetime := sqlitePoolDefaultsFromEnv()
	if maxOpen != 4 {
		t.Fatalf("unexpected default maxOpen: %d", maxOpen)
	}
	if maxIdle != 2 {
		t.Fatalf("unexpected default maxIdle: %d", maxIdle)
	}
	if maxLifetime != time.Hour {
		t.Fatalf("unexpected default maxLifetime: %s", maxLifetime)
	}
}

func TestSQLitePoolDefaultsFromEnv_OverrideAndClamp(t *testing.T) {
	t.Setenv("MUSICBOT_DB_SQLITE_MAX_OPEN_CONNS", "3")
	t.Setenv("MUSICBOT_DB_SQLITE_MAX_IDLE_CONNS", "10")
	t.Setenv("MUSICBOT_DB_SQLITE_CONN_MAX_LIFETIME", "30m")

	maxOpen, maxIdle, maxLifetime := sqlitePoolDefaultsFromEnv()
	if maxOpen != 3 {
		t.Fatalf("unexpected maxOpen: %d", maxOpen)
	}
	if maxIdle != 3 {
		t.Fatalf("expected maxIdle clamped to 3, got %d", maxIdle)
	}
	if maxLifetime != 30*time.Minute {
		t.Fatalf("unexpected maxLifetime: %s", maxLifetime)
	}
}

func TestSQLitePoolDefaultsFromEnv_InvalidValuesFallback(t *testing.T) {
	t.Setenv("MUSICBOT_DB_SQLITE_MAX_OPEN_CONNS", "0")
	t.Setenv("MUSICBOT_DB_SQLITE_MAX_IDLE_CONNS", "-1")
	t.Setenv("MUSICBOT_DB_SQLITE_CONN_MAX_LIFETIME", "bad-duration")

	maxOpen, maxIdle, maxLifetime := sqlitePoolDefaultsFromEnv()
	if maxOpen != 4 {
		t.Fatalf("unexpected fallback maxOpen: %d", maxOpen)
	}
	if maxIdle != 2 {
		t.Fatalf("unexpected fallback maxIdle: %d", maxIdle)
	}
	if maxLifetime != time.Hour {
		t.Fatalf("unexpected fallback maxLifetime: %s", maxLifetime)
	}
}

func newTempRepo(t *testing.T) *Repository {
	t.Helper()
	file, err := os.CreateTemp("", "music163bot-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := file.Name()
	_ = file.Close()
	t.Cleanup(func() { os.Remove(path) })

	file2, err := os.CreateTemp("", "music163bot-data-*.db")
	if err != nil {
		t.Fatalf("create temp data db: %v", err)
	}
	dataPath := file2.Name()
	_ = file2.Close()
	t.Cleanup(func() { os.Remove(dataPath) })

	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	gormLogger := logpkg.NewGormLogger(base, logger.Silent)
	repo, err := NewSQLiteRepository(path, dataPath, gormLogger)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	repo.SetDefaults("netease", "hires", "lrc")
	return repo
}

// TestVerifyAndUpdateQuality covers the three branches of VerifyAndUpdateQuality:
// same label (only flag verified), relabel an existing row, and merge into a row
// that already exists under the target quality (drop the stale row).
func TestVerifyAndUpdateQuality(t *testing.T) {
	ctx := context.Background()

	t.Run("same label marks verified", func(t *testing.T) {
		repo := newTempRepo(t)
		song := &bot.SongInfo{Platform: "netease", TrackID: "100", Quality: "lossless", SongName: "S", FileID: "f1", MusicSize: 1000}
		if err := repo.Create(ctx, song); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := repo.VerifyAndUpdateQuality(ctx, "netease", "100", "lossless", "lossless"); err != nil {
			t.Fatalf("verify: %v", err)
		}
		loaded, err := repo.FindByPlatformTrackID(ctx, "netease", "100", "lossless")
		if err != nil || loaded == nil {
			t.Fatalf("find: %v", err)
		}
		if !loaded.QualityVerified {
			t.Fatalf("expected QualityVerified=true")
		}
		if loaded.Quality != "lossless" {
			t.Fatalf("quality changed unexpectedly: %s", loaded.Quality)
		}
	})

	t.Run("relabel when target row absent", func(t *testing.T) {
		repo := newTempRepo(t)
		song := &bot.SongInfo{Platform: "netease", TrackID: "200", Quality: "high", SongName: "S", FileID: "f2", MusicSize: 1000}
		if err := repo.Create(ctx, song); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := repo.VerifyAndUpdateQuality(ctx, "netease", "200", "high", "lossless"); err != nil {
			t.Fatalf("verify: %v", err)
		}
		// Old label gone, new label present and verified.
		if old, _ := repo.FindByPlatformTrackID(ctx, "netease", "200", "high"); old != nil {
			t.Fatalf("expected old-quality row gone, got %+v", old)
		}
		loaded, err := repo.FindByPlatformTrackID(ctx, "netease", "200", "lossless")
		if err != nil || loaded == nil {
			t.Fatalf("find relabeled: %v", err)
		}
		if !loaded.QualityVerified || loaded.FileID != "f2" {
			t.Fatalf("unexpected relabeled row: %+v", loaded)
		}
	})

	t.Run("merge drops stale row when target exists", func(t *testing.T) {
		repo := newTempRepo(t)
		// Correct row already cached under lossless.
		correct := &bot.SongInfo{Platform: "netease", TrackID: "300", Quality: "lossless", SongName: "S", FileID: "correct", MusicSize: 1000}
		if err := repo.Create(ctx, correct); err != nil {
			t.Fatalf("create correct: %v", err)
		}
		// Stale mislabeled row under high.
		stale := &bot.SongInfo{Platform: "netease", TrackID: "300", Quality: "high", SongName: "S", FileID: "stale", MusicSize: 1000}
		if err := repo.Create(ctx, stale); err != nil {
			t.Fatalf("create stale: %v", err)
		}
		if err := repo.VerifyAndUpdateQuality(ctx, "netease", "300", "high", "lossless"); err != nil {
			t.Fatalf("verify: %v", err)
		}
		// Stale high row dropped; correct lossless row kept and verified.
		if old, _ := repo.FindByPlatformTrackID(ctx, "netease", "300", "high"); old != nil {
			t.Fatalf("expected stale row dropped, got %+v", old)
		}
		loaded, err := repo.FindByPlatformTrackID(ctx, "netease", "300", "lossless")
		if err != nil || loaded == nil {
			t.Fatalf("find correct: %v", err)
		}
		if loaded.FileID != "correct" || !loaded.QualityVerified {
			t.Fatalf("unexpected merged row: %+v", loaded)
		}
	})
}

func TestRepositoryFavorites(t *testing.T) {
	file, err := os.CreateTemp("", "music163bot-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)

	file2, err := os.CreateTemp("", "music163bot-data-*.db")
	if err != nil {
		t.Fatalf("create temp data db: %v", err)
	}
	dataPath := file2.Name()
	_ = file2.Close()
	defer os.Remove(dataPath)

	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	gormLogger := logpkg.NewGormLogger(base, logger.Silent)
	repo, err := NewSQLiteRepository(path, dataPath, gormLogger)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	ctx := context.Background()

	const uid int64 = 1001
	const gid int64 = -200300

	// Not favorited initially.
	if ok, err := repo.IsFavorited(ctx, bot.FavoriteScopeUser, uid, "netease", "111"); err != nil || ok {
		t.Fatalf("expected not favorited, got ok=%v err=%v", ok, err)
	}

	// Add a personal favorite.
	if err := repo.AddFavorite(ctx, &bot.Favorite{ScopeType: bot.FavoriteScopeUser, ScopeID: uid, Platform: "netease", TrackID: "111", AddedByUserID: uid, SongName: "Song A", SongArtists: "Artist A"}); err != nil {
		t.Fatalf("add personal favorite: %v", err)
	}
	if ok, _ := repo.IsFavorited(ctx, bot.FavoriteScopeUser, uid, "netease", "111"); !ok {
		t.Fatalf("expected favorited after add")
	}

	// Group favorite is a different scope: same track is independent.
	if err := repo.AddFavorite(ctx, &bot.Favorite{ScopeType: bot.FavoriteScopeGroup, ScopeID: gid, Platform: "netease", TrackID: "111", AddedByUserID: uid, AddedByName: "Alice", SongName: "Song A"}); err != nil {
		t.Fatalf("add group favorite: %v", err)
	}
	if n, _ := repo.CountFavorites(ctx, bot.FavoriteScopeUser, uid); n != 1 {
		t.Fatalf("expected 1 personal favorite, got %d", n)
	}
	if n, _ := repo.CountFavorites(ctx, bot.FavoriteScopeGroup, gid); n != 1 {
		t.Fatalf("expected 1 group favorite, got %d", n)
	}

	// Idempotent add refreshes metadata but does not duplicate.
	if err := repo.AddFavorite(ctx, &bot.Favorite{ScopeType: bot.FavoriteScopeUser, ScopeID: uid, Platform: "netease", TrackID: "111", AddedByUserID: uid, SongName: "Song A v2"}); err != nil {
		t.Fatalf("re-add personal favorite: %v", err)
	}
	if n, _ := repo.CountFavorites(ctx, bot.FavoriteScopeUser, uid); n != 1 {
		t.Fatalf("expected still 1 personal favorite after re-add, got %d", n)
	}

	// Remove then re-add must work (hard delete vacates the unique index).
	if err := repo.RemoveFavorite(ctx, bot.FavoriteScopeUser, uid, "netease", "111"); err != nil {
		t.Fatalf("remove favorite: %v", err)
	}
	if ok, _ := repo.IsFavorited(ctx, bot.FavoriteScopeUser, uid, "netease", "111"); ok {
		t.Fatalf("expected not favorited after remove")
	}
	if err := repo.AddFavorite(ctx, &bot.Favorite{ScopeType: bot.FavoriteScopeUser, ScopeID: uid, Platform: "netease", TrackID: "111", AddedByUserID: uid, SongName: "Song A"}); err != nil {
		t.Fatalf("re-add after remove: %v", err)
	}
	if ok, _ := repo.IsFavorited(ctx, bot.FavoriteScopeUser, uid, "netease", "111"); !ok {
		t.Fatalf("expected favorited after re-add")
	}

	// List + Random for the group scope.
	favs, err := repo.ListFavorites(ctx, bot.FavoriteScopeGroup, gid, 8, 0)
	if err != nil || len(favs) != 1 || favs[0].AddedByName != "Alice" {
		t.Fatalf("unexpected group list: %+v err=%v", favs, err)
	}
	rnd, err := repo.RandomFavorite(ctx, bot.FavoriteScopeGroup, gid)
	if err != nil || rnd == nil || rnd.TrackID != "111" {
		t.Fatalf("unexpected random favorite: %+v err=%v", rnd, err)
	}

	// Empty scope returns nil random without error.
	if rnd, err := repo.RandomFavorite(ctx, bot.FavoriteScopeUser, 999999); err != nil || rnd != nil {
		t.Fatalf("expected nil random for empty scope, got %+v err=%v", rnd, err)
	}
}
