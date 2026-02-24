package netease

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/XiaoMengXinX/Music163Api-Go/api"
	"github.com/XiaoMengXinX/Music163Api-Go/types"
	"github.com/XiaoMengXinX/Music163Api-Go/utils"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/liuran001/MusicBot-Go/bot"
	"github.com/sony/gobreaker"
)

// Client provides resilient NetEase API calls.
type Client struct {
	baseData   utils.RequestData
	spoofIP    bool
	retry      *retryablehttp.Client
	breaker    *gobreaker.CircuitBreaker
	maxRetries int
	minBackoff time.Duration
	maxBackoff time.Duration
	logger     bot.Logger
}

var mainlandIPPrefixes = [][2]uint8{
	{113, 0}, {113, 64}, {113, 128}, {114, 214},
	{118, 122}, {119, 112}, {211, 161}, {221, 238},
	{116, 224}, {222, 128}, {183, 128}, {116, 128},
	{101, 226}, {61, 128},
}

type neteaseAlbumDetail struct {
	Code          int                    `json:"code"`
	ResourceState bool                   `json:"resourceState"`
	Album         neteaseAlbumMetadata   `json:"album"`
	Songs         []types.SongDetailData `json:"songs"`
}

type neteaseAlbumMetadata struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	PicURL      string `json:"picUrl"`
	Description string `json:"description"`
	BriefDesc   string `json:"briefDesc"`
	Size        int    `json:"size"`
	Artist      struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
	Artists []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"artists"`
}

// New creates a NetEase client with retry and circuit breaker.
func New(musicU string, spoofIP bool, logger bot.Logger) *Client {
	client := retryablehttp.NewClient()
	client.RetryMax = 3
	client.RetryWaitMin = 200 * time.Millisecond
	client.RetryWaitMax = 2 * time.Second
	client.Logger = nil

	settings := gobreaker.Settings{
		Name:        "netease-api",
		MaxRequests: 3,
		Interval:    10 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 5
		},
	}

	data := utils.RequestData{}
	if musicU != "" {
		data.Cookies = []*http.Cookie{{Name: "MUSIC_U", Value: musicU}}
		if logger != nil {
			logger.Info("netease client initialized with MUSIC_U cookie", "cookie_length", len(musicU))
		}
	} else {
		if logger != nil {
			logger.Warn("netease client initialized WITHOUT MUSIC_U cookie - lossless download may fail")
		}
	}

	return &Client{
		baseData:   data,
		spoofIP:    spoofIP,
		retry:      client,
		breaker:    gobreaker.NewCircuitBreaker(settings),
		maxRetries: client.RetryMax,
		minBackoff: client.RetryWaitMin,
		maxBackoff: client.RetryWaitMax,
		logger:     logger,
	}
}

// GetSongDetail retrieves song detail data.
func (c *Client) GetSongDetail(ctx context.Context, musicID int) (*bot.SongDetail, error) {
	if c.logger != nil {
		c.logger.Debug("fetching song detail", "music_id", musicID)
	}

	var result bot.SongDetail
	err := c.execute(ctx, func() error {
		data, err := api.GetSongDetail(c.requestData(), []int{musicID})
		if err != nil {
			if c.logger != nil {
				c.logger.Error("api.GetSongDetail failed", "music_id", musicID, "error", err)
			}
			return err
		}
		result = data
		if c.logger != nil {
			c.logger.Debug("song detail fetched successfully", "music_id", musicID, "songs_count", len(result.Songs))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetSongDetailBatch retrieves song detail data for multiple song IDs.
func (c *Client) GetSongDetailBatch(ctx context.Context, musicIDs []int) (*bot.SongDetail, error) {
	if len(musicIDs) == 0 {
		return nil, nil
	}
	if c.logger != nil {
		c.logger.Debug("fetching song detail batch", "count", len(musicIDs))
	}
	var result bot.SongDetail
	err := c.execute(ctx, func() error {
		data, err := api.GetSongDetail(c.requestData(), musicIDs)
		if err != nil {
			if c.logger != nil {
				c.logger.Error("api.GetSongDetail batch failed", "count", len(musicIDs), "error", err)
			}
			return err
		}
		result = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetPlaylistDetail retrieves playlist detail data.
func (c *Client) GetPlaylistDetail(ctx context.Context, playlistID int) (*bot.PlaylistDetail, error) {
	if c.logger != nil {
		c.logger.Debug("fetching playlist detail", "playlist_id", playlistID)
	}
	var result bot.PlaylistDetail
	err := c.execute(ctx, func() error {
		data, err := api.GetPlaylistDetail(c.requestData(), playlistID)
		if err != nil {
			if c.logger != nil {
				c.logger.Error("api.GetPlaylistDetail failed", "playlist_id", playlistID, "error", err)
			}
			return err
		}
		result = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAlbumDetail retrieves album detail data.
func (c *Client) GetAlbumDetail(ctx context.Context, albumID int) (*neteaseAlbumDetail, error) {
	if c.logger != nil {
		c.logger.Debug("fetching album detail", "album_id", albumID)
	}

	var result neteaseAlbumDetail
	err := c.execute(ctx, func() error {
		data, err := api.GetAlbumDetail(c.requestData(), albumID)
		if err != nil {
			if c.logger != nil {
				c.logger.Error("api.GetAlbumDetail failed", "album_id", albumID, "error", err)
			}
			return err
		}

		raw := strings.TrimSpace(data.RawJson)
		if raw == "" {
			rawBytes, marshalErr := json.Marshal(data)
			if marshalErr != nil {
				return fmt.Errorf("netease: marshal album detail: %w", marshalErr)
			}
			raw = string(rawBytes)
		}

		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return fmt.Errorf("netease: decode album detail: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetSongURL retrieves song URL data.
func (c *Client) GetSongURL(ctx context.Context, musicID int, quality string) (*bot.SongURL, error) {
	var result bot.SongURL
	err := c.execute(ctx, func() error {
		data, err := api.GetSongURL(c.requestData(), api.SongURLConfig{Ids: []int{musicID}, Level: quality})
		if err != nil {
			return err
		}
		result = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Search searches songs by keyword.
func (c *Client) Search(ctx context.Context, keyword string, limit int) (*bot.SearchResult, error) {
	var result bot.SearchResult
	err := c.execute(ctx, func() error {
		data, err := api.SearchSong(c.requestData(), api.SearchSongConfig{Keyword: keyword, Limit: limit})
		if err != nil {
			return err
		}
		result = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetLyric retrieves lyric data.
func (c *Client) GetLyric(ctx context.Context, musicID int) (*bot.Lyric, error) {
	var result bot.Lyric
	err := c.execute(ctx, func() error {
		data, err := api.GetSongLyric(c.requestData(), musicID)
		if err != nil {
			return err
		}
		result = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) execute(ctx context.Context, fn func() error) error {
	if fn == nil {
		return nil
	}

	_, err := c.breaker.Execute(func() (interface{}, error) {
		return nil, c.withRetry(ctx, fn)
	})
	return err
}

func (c *Client) withRetry(ctx context.Context, fn func() error) error {
	if fn == nil {
		return nil
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if attempt == c.maxRetries {
			break
		}

		wait := c.retry.Backoff(c.minBackoff, c.maxBackoff, attempt, nil)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}

	if lastErr == nil {
		lastErr = errors.New("netease: retry failed")
	}
	return lastErr
}

func (c *Client) requestData() utils.RequestData {
	data := c.baseData

	headers := make(utils.Headers, 0, len(c.baseData.Headers)+4)
	headers = append(headers, c.baseData.Headers...)

	if c.spoofIP {
		if ip, err := randomMainlandIPv4(); err == nil {
			headers = append(headers,
				struct {
					Name  string
					Value string
				}{Name: "X-Real-IP", Value: ip},
				struct {
					Name  string
					Value string
				}{Name: "X-Forwarded-For", Value: ip},
				struct {
					Name  string
					Value string
				}{Name: "HTTP_X_FORWARDED_FOR", Value: ip},
				struct {
					Name  string
					Value string
				}{Name: "CLIENT-IP", Value: ip},
			)
		} else if c.logger != nil {
			c.logger.Warn("failed to generate random spoof ip", "error", err)
		}
	}

	data.Headers = headers
	return data
}

func randomMainlandIPv4() (string, error) {
	prefixIdx, err := cryptoRandInt(len(mainlandIPPrefixes))
	if err != nil {
		return "", err
	}
	prefix := mainlandIPPrefixes[prefixIdx]

	third, err := cryptoRandInt(254)
	if err != nil {
		return "", err
	}
	fourth, err := cryptoRandInt(254)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%d.%d.%d.%d", prefix[0], prefix[1], third+1, fourth+1), nil
}

func cryptoRandInt(max int) (int, error) {
	if max <= 0 {
		return 0, fmt.Errorf("invalid max: %d", max)
	}
	n, err := crand.Int(crand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}
