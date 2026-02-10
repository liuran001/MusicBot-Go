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
	Quality         string `gorm:"not null;default:'high';index:idx_platform_track_quality,unique"`
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
		FileID:          model.FileID,
		ThumbFileID:     model.ThumbFileID,
		FromUserID:      model.FromUserID,
		FromUserName:    model.FromUserName,
		FromChatID:      model.FromChatID,
		FromChatName:    model.FromChatName,
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
		FileID:          info.FileID,
		ThumbFileID:     info.ThumbFileID,
		FromUserID:      info.FromUserID,
		FromUserName:    info.FromUserName,
		FromChatID:      info.FromChatID,
		FromChatName:    info.FromChatName,
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
	UserID          int64  `gorm:"uniqueIndex;not null"`
	DefaultPlatform string `gorm:"not null;default:'netease'"`
	DefaultQuality  string `gorm:"not null;default:'hires'"`
	AutoDeleteList  bool   `gorm:"not null;default:false"`
	AutoLinkDetect  bool   `gorm:"not null;default:true"`
}

func (UserSettingsModel) TableName() string {
	return "user_settings"
}

// GroupSettingsModel stores group preferences for the bot.
type GroupSettingsModel struct {
	gorm.Model
	ChatID          int64  `gorm:"uniqueIndex;not null"`
	DefaultPlatform string `gorm:"not null;default:'netease'"`
	DefaultQuality  string `gorm:"not null;default:'hires'"`
	AutoDeleteList  bool   `gorm:"not null;default:true"`
	AutoLinkDetect  bool   `gorm:"not null;default:true"`
}

func (GroupSettingsModel) TableName() string {
	return "group_settings"
}
