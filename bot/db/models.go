package db

import (
	"time"

	"github.com/liuran001/MusicBot-Go/bot"
	"gorm.io/gorm"
)

// SongInfoModel mirrors the song_infos schema with multi-platform support.
type SongInfoModel struct {
	gorm.Model
	Platform        string `gorm:"not null;default:'netease';index:idx_platform_track_quality,unique"`
	TrackID         string `gorm:"not null;default:'';index:idx_platform_track_quality,unique"`
	Quality         string `gorm:"not null;default:'hires';index:idx_platform_track_quality,unique"`
	QualityVerified bool   `gorm:"not null;default:false"`
	QualityRevision int    `gorm:"not null;default:0"`
	AudioCodec      string
	SampleRate      int
	BitDepth        int
	MusicID         int // Deprecated: Legacy NetEase music ID (kept for backward compatibility)
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
	AudioValidated  bool `gorm:"not null;default:false"`
	FileID          string
	ThumbFileID     string
	FromUserID      int64
	FromUserName    string
	FromChatID      int64
	FromChatName    string
	LyricsAvailable *bool

	MetadataLanguage string
	AudioLanguage    string `gorm:"not null;default:''"`
}

// LocalizedSongMetadataModel keeps metadata variants separate from the audio
// cache row, whose key also includes quality.
type LocalizedSongMetadataModel struct {
	gorm.Model
	Platform    string `gorm:"not null;index:idx_localized_song_metadata,unique"`
	TrackID     string `gorm:"not null;index:idx_localized_song_metadata,unique"`
	Language    string `gorm:"not null;index:idx_localized_song_metadata,unique"`
	SongName    string
	SongArtists string
	SongAlbum   string
}

func (LocalizedSongMetadataModel) TableName() string {
	return "localized_song_metadata"
}

// LocalizedAudioModel preserves reusable Apple Music audio files whose tags
// were written in different bot languages while the primary cache row changes.
type LocalizedAudioModel struct {
	gorm.Model
	Platform     string `gorm:"not null;index:idx_localized_audio,unique"`
	TrackID      string `gorm:"not null;index:idx_localized_audio,unique"`
	Quality      string `gorm:"not null;index:idx_localized_audio,unique"`
	Language     string `gorm:"not null;index:idx_localized_audio,unique"`
	FileID       string `gorm:"not null;index"`
	SongInfoJSON []byte `gorm:"not null"`
}

func (LocalizedAudioModel) TableName() string {
	return "localized_audio"
}

func (SongInfoModel) TableName() string {
	return "song_infos"
}

// BotStatModel stores aggregated bot statistics.
type BotStatModel struct {
	gorm.Model
	Key   string `gorm:"uniqueIndex;not null"`
	Value int64
}

func (BotStatModel) TableName() string {
	return "bot_stats"
}

func toInternal(model SongInfoModel) *bot.SongInfo {
	return &bot.SongInfo{
		ID:              model.ID,
		CreatedAt:       model.CreatedAt,
		UpdatedAt:       model.UpdatedAt,
		DeletedAt:       deletedAtPtr(model.DeletedAt),
		Platform:        model.Platform,
		TrackID:         model.TrackID,
		Quality:         model.Quality,
		QualityVerified: model.QualityVerified,
		QualityRevision: model.QualityRevision,
		AudioCodec:      model.AudioCodec,
		SampleRate:      model.SampleRate,
		BitDepth:        model.BitDepth,
		MusicID:         model.MusicID,
		SongName:        model.SongName,
		SongArtists:     model.SongArtists,
		SongArtistsIDs:  model.SongArtistsIDs,
		SongAlbum:       model.SongAlbum,
		AlbumID:         model.AlbumID,
		TrackURL:        model.TrackURL,
		AlbumURL:        model.AlbumURL,
		SongArtistsURLs: model.SongArtistsURLs,
		FileExt:         model.FileExt,
		MusicSize:       model.MusicSize,
		PicSize:         model.PicSize,
		EmbPicSize:      model.EmbPicSize,
		BitRate:         model.BitRate,
		Duration:        model.Duration,
		AudioValidated:  model.AudioValidated,
		FileID:          model.FileID,
		ThumbFileID:     model.ThumbFileID,
		FromUserID:      model.FromUserID,
		FromUserName:    model.FromUserName,
		FromChatID:      model.FromChatID,
		FromChatName:    model.FromChatName,
		LyricsAvailable: model.LyricsAvailable,

		MetadataLanguage: model.MetadataLanguage,
		AudioLanguage:    model.AudioLanguage,
	}
}

func toModel(info *bot.SongInfo) *SongInfoModel {
	if info == nil {
		return &SongInfoModel{}
	}

	model := &SongInfoModel{
		Platform:        info.Platform,
		TrackID:         info.TrackID,
		Quality:         info.Quality,
		QualityVerified: info.QualityVerified,
		QualityRevision: info.QualityRevision,
		AudioCodec:      info.AudioCodec,
		SampleRate:      info.SampleRate,
		BitDepth:        info.BitDepth,
		MusicID:         info.MusicID,
		SongName:        info.SongName,
		SongArtists:     info.SongArtists,
		SongArtistsIDs:  info.SongArtistsIDs,
		SongAlbum:       info.SongAlbum,
		AlbumID:         info.AlbumID,
		TrackURL:        info.TrackURL,
		AlbumURL:        info.AlbumURL,
		SongArtistsURLs: info.SongArtistsURLs,
		FileExt:         info.FileExt,
		MusicSize:       info.MusicSize,
		PicSize:         info.PicSize,
		EmbPicSize:      info.EmbPicSize,
		BitRate:         info.BitRate,
		Duration:        info.Duration,
		AudioValidated:  info.AudioValidated,
		FileID:          info.FileID,
		ThumbFileID:     info.ThumbFileID,
		FromUserID:      info.FromUserID,
		FromUserName:    info.FromUserName,
		FromChatID:      info.FromChatID,
		FromChatName:    info.FromChatName,
		LyricsAvailable: info.LyricsAvailable,

		MetadataLanguage: info.MetadataLanguage,
		AudioLanguage:    info.AudioLanguage,
	}

	if info.ID != 0 {
		model.ID = info.ID
	}
	if !info.CreatedAt.IsZero() {
		model.CreatedAt = info.CreatedAt
	}
	if !info.UpdatedAt.IsZero() {
		model.UpdatedAt = info.UpdatedAt
	}
	if info.DeletedAt != nil {
		model.DeletedAt = gorm.DeletedAt{Time: *info.DeletedAt, Valid: true}
	}

	return model
}

func deletedAtPtr(value gorm.DeletedAt) *time.Time {
	if value.Valid {
		return &value.Time
	}
	return nil
}

// UserSettingsModel stores user preferences for the bot.
type UserSettingsModel struct {
	gorm.Model
	UserID             int64  `gorm:"uniqueIndex;not null"`
	DefaultPlatform    string `gorm:"not null;default:'netease'"`
	DefaultQuality     string `gorm:"not null;default:'hires'"`
	AutoDeleteList     bool   `gorm:"not null;default:false"`
	AutoLinkDetect     bool   `gorm:"not null;default:true"`
	DefaultLyricFormat string `gorm:"not null;default:'lrc'"`
	// Language is the persisted UI-language override (2-letter ISO 639-1, e.g.
	// "zh"/"en"/"ja"). Empty means "auto-detect from the Telegram client".
	Language string `gorm:"not null;default:''"`
	// Nullable side-track defaults: NULL means "unset" (use the per-format
	// default); a non-NULL value is the user's explicit choice.
	DefaultLyricIncludeTranslation *bool
	DefaultLyricIncludeRoma        *bool
}

func (UserSettingsModel) TableName() string {
	return "user_settings"
}

// GroupSettingsModel stores group preferences for the bot.
type GroupSettingsModel struct {
	gorm.Model
	ChatID             int64  `gorm:"uniqueIndex;not null"`
	DefaultPlatform    string `gorm:"not null;default:'netease'"`
	DefaultQuality     string `gorm:"not null;default:'hires'"`
	AutoDeleteList     bool   `gorm:"not null;default:true"`
	AutoLinkDetect     bool   `gorm:"not null;default:true"`
	DefaultLyricFormat string `gorm:"not null;default:'lrc'"`
	// Language is the persisted UI-language override (2-letter ISO 639-1, e.g.
	// "zh"/"en"/"ja"). Empty means "auto-detect from the Telegram client".
	Language string `gorm:"not null;default:''"`
	// Nullable side-track defaults: NULL means "unset" (use the per-format
	// default); a non-NULL value is the group's explicit choice.
	DefaultLyricIncludeTranslation *bool
	DefaultLyricIncludeRoma        *bool
}

func (GroupSettingsModel) TableName() string {
	return "group_settings"
}

type PluginSettingModel struct {
	gorm.Model
	ScopeType    string `gorm:"uniqueIndex:idx_plugin_scope_key,priority:1;not null"`
	ScopeID      int64  `gorm:"uniqueIndex:idx_plugin_scope_key,priority:2;not null"`
	Plugin       string `gorm:"uniqueIndex:idx_plugin_scope_key,priority:3;not null"`
	SettingKey   string `gorm:"uniqueIndex:idx_plugin_scope_key,priority:4;not null"`
	SettingValue string `gorm:"type:text;not null"`
}

func (PluginSettingModel) TableName() string {
	return "plugin_settings"
}

// FavoriteModel stores a favorited track for a user or a group. The four-column
// unique index guarantees a track is favorited at most once per scope; a second
// "add" is an idempotent upsert. It lives in data.db (durable user data), never
// in the volatile song cache, so song metadata is denormalized here.
type FavoriteModel struct {
	gorm.Model
	ScopeType       string `gorm:"uniqueIndex:idx_fav_scope_track,priority:1;not null"`
	ScopeID         int64  `gorm:"uniqueIndex:idx_fav_scope_track,priority:2;not null"`
	Platform        string `gorm:"uniqueIndex:idx_fav_scope_track,priority:3;not null"`
	TrackID         string `gorm:"uniqueIndex:idx_fav_scope_track,priority:4;not null"`
	AddedByUserID   int64  `gorm:"index"`
	AddedByName     string
	AddedByUsername string
	SongName        string
	SongArtists     string
	SongAlbum       string
	TrackURL        string
	SongArtistsURLs string
}

func (FavoriteModel) TableName() string {
	return "favorites"
}

func toFavorite(model FavoriteModel) *bot.Favorite {
	return &bot.Favorite{
		ID:              model.ID,
		CreatedAt:       model.CreatedAt,
		UpdatedAt:       model.UpdatedAt,
		DeletedAt:       deletedAtPtr(model.DeletedAt),
		ScopeType:       model.ScopeType,
		ScopeID:         model.ScopeID,
		Platform:        model.Platform,
		TrackID:         model.TrackID,
		AddedByUserID:   model.AddedByUserID,
		AddedByName:     model.AddedByName,
		AddedByUsername: model.AddedByUsername,
		SongName:        model.SongName,
		SongArtists:     model.SongArtists,
		SongAlbum:       model.SongAlbum,
		TrackURL:        model.TrackURL,
		SongArtistsURLs: model.SongArtistsURLs,
	}
}

func toFavoriteModel(fav *bot.Favorite) *FavoriteModel {
	if fav == nil {
		return &FavoriteModel{}
	}
	model := &FavoriteModel{
		ScopeType:       fav.ScopeType,
		ScopeID:         fav.ScopeID,
		Platform:        fav.Platform,
		TrackID:         fav.TrackID,
		AddedByUserID:   fav.AddedByUserID,
		AddedByName:     fav.AddedByName,
		AddedByUsername: fav.AddedByUsername,
		SongName:        fav.SongName,
		SongArtists:     fav.SongArtists,
		SongAlbum:       fav.SongAlbum,
		TrackURL:        fav.TrackURL,
		SongArtistsURLs: fav.SongArtistsURLs,
	}
	if fav.ID != 0 {
		model.ID = fav.ID
	}
	return model
}
