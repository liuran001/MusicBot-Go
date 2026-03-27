package kugou

import (
	"context"
	"fmt"
	"strings"

	kugoulib "github.com/guohuiyuan/music-lib/kugou"
	"github.com/guohuiyuan/music-lib/model"
	"github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type Client struct {
	api    *kugoulib.Kugou
	cookie string
	logger bot.Logger
}

func NewClient(cookie string, logger bot.Logger) *Client {
	trimmed := strings.TrimSpace(cookie)
	return &Client{
		api:    kugoulib.New(trimmed),
		cookie: trimmed,
		logger: logger,
	}
}

func (c *Client) Search(ctx context.Context, keyword string, limit int) ([]model.Song, error) {
	_ = ctx
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, platform.NewNotFoundError("kugou", "search", "")
	}
	songs, err := c.api.Search(keyword)
	if err != nil {
		return nil, wrapError("kugou", "search", "", err)
	}
	if limit > 0 && len(songs) > limit {
		songs = songs[:limit]
	}
	return songs, nil
}

func (c *Client) GetTrack(ctx context.Context, trackID string) (*model.Song, error) {
	_ = ctx
	hash := normalizeHash(trackID)
	if hash == "" {
		return nil, platform.NewNotFoundError("kugou", "track", trackID)
	}
	song, err := c.api.Parse(buildTrackLink(hash))
	if err != nil {
		return nil, wrapError("kugou", "track", hash, err)
	}
	if song == nil {
		return nil, platform.NewNotFoundError("kugou", "track", hash)
	}
	if song.Source == "" {
		song.Source = "kugou"
	}
	if song.Extra == nil {
		song.Extra = map[string]string{}
	}
	if strings.TrimSpace(song.Extra["hash"]) == "" {
		song.Extra["hash"] = hash
	}
	if strings.TrimSpace(song.ID) == "" {
		song.ID = hash
	}
	if strings.TrimSpace(song.Link) == "" {
		song.Link = buildTrackLink(hash)
	}
	return song, nil
}

func (c *Client) GetLyrics(ctx context.Context, trackID string) (string, error) {
	_ = ctx
	hash := normalizeHash(trackID)
	if hash == "" {
		return "", platform.NewNotFoundError("kugou", "lyrics", trackID)
	}
	lyrics, err := c.api.GetLyrics(&model.Song{
		Source: "kugou",
		ID:     hash,
		Extra:  map[string]string{"hash": hash},
	})
	if err != nil {
		return "", wrapError("kugou", "lyrics", hash, err)
	}
	if strings.TrimSpace(lyrics) == "" {
		return "", platform.NewUnavailableError("kugou", "lyrics", hash)
	}
	return lyrics, nil
}

func (c *Client) GetDownloadInfo(ctx context.Context, trackID string) (*model.Song, error) {
	_ = ctx
	song, err := c.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if song == nil {
		return nil, platform.NewNotFoundError("kugou", "track", trackID)
	}
	if strings.TrimSpace(song.URL) == "" {
		url, songInfoErr := c.api.GetDownloadURLBySonginfo(song)
		if songInfoErr == nil && strings.TrimSpace(url) != "" {
			song.URL = strings.TrimSpace(url)
			if strings.TrimSpace(song.Ext) == "" {
				song.Ext = detectExtFromURL(song.URL)
			}
		}
	}
	if strings.TrimSpace(song.URL) == "" {
		url, downloadErr := c.api.GetDownloadURL(song)
		if downloadErr != nil {
			return nil, wrapError("kugou", "track", normalizeHash(trackID), downloadErr)
		}
		if strings.TrimSpace(url) == "" {
			return nil, platform.NewUnavailableError("kugou", "track", normalizeHash(trackID))
		}
		song.URL = strings.TrimSpace(url)
		if strings.TrimSpace(song.Ext) == "" {
			song.Ext = detectExtFromURL(song.URL)
		}
	}
	return song, nil
}

func (c *Client) GetPlaylist(ctx context.Context, playlistID string) (*model.Playlist, []model.Song, error) {
	_ = ctx
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return nil, nil, platform.NewNotFoundError("kugou", "playlist", "")
	}
	playlist, songs, err := c.api.ParsePlaylist(buildPlaylistLink(playlistID))
	if err != nil {
		return nil, nil, wrapError("kugou", "playlist", playlistID, err)
	}
	if playlist == nil {
		return nil, nil, platform.NewNotFoundError("kugou", "playlist", playlistID)
	}
	if playlist.Source == "" {
		playlist.Source = "kugou"
	}
	if strings.TrimSpace(playlist.ID) == "" {
		playlist.ID = playlistID
	}
	if strings.TrimSpace(playlist.Link) == "" {
		playlist.Link = buildPlaylistLink(playlist.ID)
	}
	return playlist, songs, nil
}

func (c *Client) CheckCookie(ctx context.Context) (bool, error) {
	_ = ctx
	if strings.TrimSpace(c.cookie) == "" {
		return false, nil
	}
	return c.api.IsVipAccount()
}

func wrapError(source, resource, id string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "lyrics not found") || strings.Contains(msg, "hash not found"):
		return platform.NewNotFoundError(source, resource, id)
	case strings.Contains(msg, "invalid kugou") || strings.Contains(msg, "invalid hash") || strings.Contains(msg, "invalid link"):
		return platform.NewNotFoundError(source, resource, id)
	case strings.Contains(msg, "content is empty") || strings.Contains(msg, "download url not found") || strings.Contains(msg, "unavailable"):
		return platform.NewUnavailableError(source, resource, id)
	case strings.Contains(msg, "cookie required") || strings.Contains(msg, "requires cookie"):
		return platform.NewAuthRequiredError(source)
	default:
		return fmt.Errorf("%s: %s %s: %w", source, resource, id, err)
	}
}
