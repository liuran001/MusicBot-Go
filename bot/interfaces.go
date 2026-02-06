package bot

import "context"

// Logger is the minimal logging abstraction used across modules.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(args ...any) Logger
}

// Config provides typed access to configuration values.
type Config interface {
	GetString(key string) string
	GetInt(key string) int
	GetBool(key string) bool
	GetIntSlice(key string) []int
}

// SongRepository defines storage operations for cached songs.
type SongRepository interface {
	FindByMusicID(ctx context.Context, musicID int) (*SongInfo, error)
	FindByPlatformTrackID(ctx context.Context, platform, trackID, quality string) (*SongInfo, error)
	FindByFileID(ctx context.Context, fileID string) (*SongInfo, error)
	Create(ctx context.Context, song *SongInfo) error
	Update(ctx context.Context, song *SongInfo) error
	Delete(ctx context.Context, musicID int) error
	DeleteAll(ctx context.Context) error
	DeleteAllByPlatform(ctx context.Context, platform string) error
	DeleteByPlatformTrackID(ctx context.Context, platform, trackID, quality string) error
	DeleteAllQualitiesByPlatformTrackID(ctx context.Context, platform, trackID string) error
	Count(ctx context.Context) (int64, error)
	CountByUserID(ctx context.Context, userID int64) (int64, error)
	CountByChatID(ctx context.Context, chatID int64) (int64, error)
	CountByPlatform(ctx context.Context) (map[string]int64, error)
	GetSendCount(ctx context.Context) (int64, error)
	IncrementSendCount(ctx context.Context) error
	Last(ctx context.Context) (*SongInfo, error)
	GetUserSettings(ctx context.Context, userID int64) (*UserSettings, error)
	UpdateUserSettings(ctx context.Context, settings *UserSettings) error
	GetGroupSettings(ctx context.Context, chatID int64) (*GroupSettings, error)
	UpdateGroupSettings(ctx context.Context, settings *GroupSettings) error
}

// NeteaseClient defines the NetEase API operations used by the bot.
type NeteaseClient interface {
	GetSongDetail(ctx context.Context, musicID int) (*SongDetail, error)
	GetSongURL(ctx context.Context, musicID int, quality string) (*SongURL, error)
	Search(ctx context.Context, keyword string, limit int) (*SearchResult, error)
	GetLyric(ctx context.Context, musicID int) (*Lyric, error)
}

// WorkerPool limits concurrency for background tasks.
type WorkerPool interface {
	Submit(task func()) error
	SubmitWait(task func() error) error
	Shutdown(ctx context.Context) error
	Size() int
}

// Updater abstracts dynamic update operations (yaegi-based).
type Updater interface {
	CheckUpdate(ctx context.Context) (bool, error)
	LoadEntry(entry string) (func(), error)
	Reload(ctx context.Context) error
}
