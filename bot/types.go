package bot

import (
	"time"
)

// AppleMusicQualityRevision identifies the current Apple Music classifier and
// cache layout. Bump it whenever the Lossless/Hi-Res/Atmos mapping changes in a
// way that makes previously cached enhanced renditions unsafe to reuse.
const AppleMusicQualityRevision = 1

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
	QualityVerified bool   // true if Quality has been verified against the platform API
	QualityRevision int    // classifier revision used when the cached quality was verified
	AudioCodec      string // verified audio codec when available (e.g. alac, eac3)
	SampleRate      int    // verified audio sample rate in Hz when available
	BitDepth        int    // verified audio bit depth when available
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
	// AudioValidated is true only after the downloaded audio duration has been
	// checked against the platform catalog duration. Catalog metadata and a
	// Telegram FileID alone do not establish this flag.
	AudioValidated bool
	FileID         string
	ThumbFileID    string
	FromUserID     int64
	FromUserName   string
	FromChatID     int64
	FromChatName   string
	// LyricsAvailable is nil when unknown. When false, the platform explicitly
	// reported that this track has no lyrics.
	LyricsAvailable *bool

	MetadataLanguage string // language used for the cached title/artist/album metadata
	AudioLanguage    string // language embedded in the cached audio file's tags
}

// LocalizedSongMetadata stores one immutable metadata snapshot for a track and
// language. Audio identifiers and lyrics deliberately remain in SongInfo.
type LocalizedSongMetadata struct {
	Platform    string
	TrackID     string
	Language    string
	SongName    string
	SongArtists string
	SongAlbum   string
}

// Favorite scope constants identify whether a favorite belongs to a single user
// or is shared within a group chat. They reuse the same string values as the
// plugin-setting scopes so callers can pass either interchangeably.
const (
	FavoriteScopeUser  = PluginScopeUser
	FavoriteScopeGroup = PluginScopeGroup
)

// Favorite represents a favorited track. A favorite is keyed by
// (ScopeType, ScopeID, Platform, TrackID): personal favorites use
// ScopeType="user" with ScopeID=userID, group favorites use ScopeType="group"
// with ScopeID=chatID. AddedByUserID records who created it (for group lists the
// collector is shown; for personal favorites it equals ScopeID). Song metadata
// is denormalized so the list renders without touching the volatile song cache.
type Favorite struct {
	ID              uint
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
	ScopeType       string
	ScopeID         int64
	Platform        string
	TrackID         string
	AddedByUserID   int64
	AddedByName     string
	AddedByUsername string
	SongName        string
	SongArtists     string
	SongAlbum       string
	TrackURL        string
	SongArtistsURLs string
}

// UserSettings represents user preferences for the bot.
type UserSettings struct {
	ID                 uint
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
	UserID             int64
	DefaultPlatform    string
	DefaultQuality     string
	AutoDeleteList     bool
	AutoLinkDetect     bool
	DefaultLyricFormat string
	// Language is the persisted UI-language override (2-letter ISO 639-1, e.g.
	// "zh"/"en"/"ja"). Empty means "auto-detect from the Telegram client".
	Language string
	// DefaultLyricIncludeTranslation / DefaultLyricIncludeRoma are the persisted
	// default translation/roma side-track toggles for /lyric. A nil pointer means
	// "unset" — fall back to the per-format default. Only meaningful for formats
	// that support side tracks.
	DefaultLyricIncludeTranslation *bool
	DefaultLyricIncludeRoma        *bool
}

// GroupSettings represents group-level preferences for the bot.
type GroupSettings struct {
	ID                 uint
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
	ChatID             int64
	DefaultPlatform    string
	DefaultQuality     string
	AutoDeleteList     bool
	AutoLinkDetect     bool
	DefaultLyricFormat string
	// Language is the persisted UI-language override (2-letter ISO 639-1). Empty
	// means "auto-detect from the Telegram client".
	Language string
	// DefaultLyricIncludeTranslation / DefaultLyricIncludeRoma mirror the
	// UserSettings fields for groups. A nil pointer means "unset" — fall back to
	// the per-format default.
	DefaultLyricIncludeTranslation *bool
	DefaultLyricIncludeRoma        *bool
}
