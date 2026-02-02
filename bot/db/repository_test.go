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

	groupSettings, err := repo.GetGroupSettings(ctx, -1001)
	if err != nil {
		t.Fatalf("get group settings: %v", err)
	}
	if groupSettings.DefaultQuality != "hires" {
		t.Fatalf("unexpected default group quality: %s", groupSettings.DefaultQuality)
	}

	if err := repo.Delete(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
