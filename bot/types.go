package bot

import (
	"time"
)

// SongInfo represents cached song metadata.
// It supports multi-platform architecture with Platform and TrackID fields.
type SongInfo struct {
	ID              uint
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
	Platform        string // Platform identifier (e.g., "netease", "spotify")
	TrackID         string // Platform-specific track identifier
	Quality         string // Quality level (e.g., "standard", "high", "lossless")
	MusicID         int    // Deprecated: Legacy NetEase music ID (kept for backward compatibility)
	SongName        string
	SongArtists     string
	SongArtistsIDs  string
	SongAlbum       string
	AlbumID         int
	TrackURL        string
	AlbumURL        string
	SongArtistsURLs string
	FileExt         string
	MusicSize       int
	PicSize         int
	EmbPicSize      int
	BitRate         int
	Duration        int
	FileID          string
	ThumbFileID     string
	FromUserID      int64
	FromUserName    string
	FromChatID      int64
	FromChatName    string
}

// UserSettings represents user preferences for the bot.
type UserSettings struct {
	ID              uint
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
	UserID          int64
	DefaultPlatform string
	DefaultQuality  string
	AutoDeleteList  bool
	AutoLinkDetect  bool
}

// GroupSettings represents group-level preferences for the bot.
type GroupSettings struct {
	ID              uint
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
	ChatID          int64
	DefaultPlatform string
	DefaultQuality  string
	AutoDeleteList  bool
	AutoLinkDetect  bool
}
