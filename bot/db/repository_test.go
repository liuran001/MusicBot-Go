package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

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

	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	gormLogger := logpkg.NewGormLogger(base, logger.Silent)

	repo, err := NewSQLiteRepository(path, gormLogger)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	repo.SetDefaults("netease", "hires")

	ctx := context.Background()
	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty db")
	}

	song := &bot.SongInfo{
		MusicID:     1,
		SongName:    "Song",
		SongArtists: "Artist",
		SongAlbum:   "Album",
		FileExt:     "mp3",
		MusicSize:   123,
		Duration:    10,
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

	if err := repo.Delete(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
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

	repo, err := NewSQLiteRepository(path, gormLogger)
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
	if err := repo.db.Unscoped().
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

	repo, err := NewSQLiteRepository(path, gormLogger)
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
