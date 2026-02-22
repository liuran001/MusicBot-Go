package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sync"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/liuran001/MusicBot-Go/bot"
	"github.com/sony/gobreaker"
)

// Client provides resilient Bilibili API calls.
type Client struct {
	httpClient   *retryablehttp.Client
	breaker      *gobreaker.CircuitBreaker
	maxRetries   int
	minBackoff   time.Duration
	maxBackoff   time.Duration
	logger       bot.Logger
	cookie       string
	refreshToken string
	cookieMutex  sync.RWMutex
}

// AudioSongInfoRequestParams for requesting Audio song info
type AudioSongInfoRequestParams struct {
	Sid int `json:"sid"`
}

// AudioSongInfoData represents the Bilibili song metadata info
type AudioSongInfoData struct {
	ID       int    `json:"id"`
	UID      int    `json:"uid"`
	UName    string `json:"uname"`
	Author   string `json:"author"`
	Title    string `json:"title"`
	Cover    string `json:"cover"`
	Intro    string `json:"intro"`
	Lyric    string `json:"lyric"`
	Duration int    `json:"duration"` // in seconds
	Bvid     string `json:"bvid"`
}

// AudioSongInfoResponse is the top level structure for song info API
type AudioSongInfoResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"msg"`
	Data    *AudioSongInfoData `json:"data"`
}

// AudioStreamUrlRequestParams defines the request parameters for stream URL
type AudioStreamUrlRequestParams struct {
	SongID    int    `json:"songid"`
	Quality   int    `json:"quality"`
	Privilege int    `json:"privilege"`
	Mid       int    `json:"mid"`
	Platform  string `json:"platform"`
}

// AudioStreamUrlData holds the actual stream URL data
type AudioStreamUrlData struct {
	Sid     int      `json:"sid"`
	Type    int      `json:"type"`
	Timeout int      `json:"timeout"`
	Size    int      `json:"size"`
	Cdns    []string `json:"cdns"`
	Title   string   `json:"title"`
	Cover   string   `json:"cover"`
}

// AudioStreamUrlResponse is the top level structure for stream URL API
type AudioStreamUrlResponse struct {
	Code    int                 `json:"code"`
	Message string              `json:"msg"`
	Data    *AudioStreamUrlData `json:"data"`
}

// VideoInfoData contains metadata for a video
type VideoInfoData struct {
	Bvid     string `json:"bvid"`
	Aid      int    `json:"aid"`
	Cid      int    `json:"cid"`
	Title    string `json:"title"`
	Pic      string `json:"pic"`
	Desc     string `json:"desc"`
	Duration int    `json:"duration"`
	Owner    struct {
		Mid  int    `json:"mid"`
		Name string `json:"name"`
		Face string `json:"face"`
	} `json:"owner"`
}

type VideoInfoResponse struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    *VideoInfoData `json:"data"`
}

// VideoDashAudio represents an audio stream within the DASH format
type VideoDashAudio struct {
	ID        int    `json:"id"`
	BaseURL   string `json:"baseUrl"`
	Bandwidth int    `json:"bandwidth"`
	MimeType  string `json:"mimeType"`
	Codecs    string `json:"codecs"`
}

type VideoPlayUrlData struct {
	Dash struct {
		Duration int              `json:"duration"`
		Audio    []VideoDashAudio `json:"audio"`
		Dolby    *struct {
			Type  int              `json:"type"`
			Audio []VideoDashAudio `json:"audio"`
		} `json:"dolby"`
		Flac *struct {
			Display bool            `json:"display"`
			Audio   *VideoDashAudio `json:"audio"`
		} `json:"flac"`
	} `json:"dash"`
}

type VideoPlayUrlResponse struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    *VideoPlayUrlData `json:"data"`
}

// New returns an instance of Bilibili client.
func New(logger bot.Logger, cookie string, refreshToken string) *Client {
	c := &Client{
		httpClient:   retryablehttp.NewClient(),
		maxRetries:   3,
		minBackoff:   1 * time.Second,
		maxBackoff:   5 * time.Second,
		logger:       logger,
		cookie:       cookie,
		refreshToken: refreshToken,
	}

	c.httpClient.RetryMax = c.maxRetries
	c.httpClient.RetryWaitMin = c.minBackoff
	c.httpClient.RetryWaitMax = c.maxBackoff
	c.httpClient.Logger = nil

	settings := gobreaker.Settings{
		Name:        "bilibili-api",
		MaxRequests: 3,
		Interval:    10 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 5
		},
	}

	c.breaker = gobreaker.NewCircuitBreaker(settings)
	return c
}

func (c *Client) setHeaders(req *retryablehttp.Request, explicitCookie ...string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.bilibili.com/")

	if len(explicitCookie) > 0 && explicitCookie[0] != "" {
		req.Header.Set("Cookie", explicitCookie[0])
		return
	}

	c.cookieMutex.RLock()
	currentCookie := c.cookie
	c.cookieMutex.RUnlock()

	if currentCookie != "" {
		req.Header.Set("Cookie", currentCookie)
	}
}

// GetAudioSongInfo fetches metadata for an audio track using its auid.
func (c *Client) GetAudioSongInfo(ctx context.Context, sid int) (*AudioSongInfoData, error) {
	if c.logger != nil {
		c.logger.Debug("bilibili: fetching audio song info", "sid", sid)
	}

	url := fmt.Sprintf("https://www.bilibili.com/audio/music-service-c/web/song/info?sid=%d", sid)

	var result AudioSongInfoResponse
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		// Set headers, including cookie if available
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("bilibili: unexpected status code %d: %s", resp.StatusCode, string(body))
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("bilibili: decode song info: %w", err)
		}

		if result.Code != 0 {
			return fmt.Errorf("bilibili: API error code %d: %s", result.Code, result.Message)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// GetAudioStreamUrl fetches the actual playback URL for an audio track.
func (c *Client) GetAudioStreamUrl(ctx context.Context, sid int, quality int) (*AudioStreamUrlData, error) {
	if c.logger != nil {
		c.logger.Debug("bilibili: fetching audio stream url", "sid", sid, "quality", quality)
	}

	url := fmt.Sprintf("https://api.bilibili.com/audio/music-service-c/url?songid=%d&quality=%d&privilege=2&mid=1&platform=pc", sid, quality)

	var result AudioStreamUrlResponse
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("bilibili: unexpected status code %d: %s", resp.StatusCode, string(body))
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("bilibili: decode stream url info: %w", err)
		}

		if result.Code != 0 {
			// Specific handling for common bilibili errors could be added here
			// 7201006 = Not Found / Taken Down
			return fmt.Errorf("bilibili: API error code %d: %s", result.Code, result.Message)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// GetLyric fetches the lyric string from the provided URL (from GetAudioSongInfo)
func (c *Client) GetLyric(ctx context.Context, lyricUrl string) (string, error) {
	if lyricUrl == "" {
		return "", errors.New("bilibili: empty lyric url")
	}

	if c.logger != nil {
		c.logger.Debug("bilibili: fetching lyric", "url", lyricUrl)
	}

	var lyric string
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, lyricUrl, nil)
		if err != nil {
			return err
		}

		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("bilibili: unexpected status code %d fetching lyrics", resp.StatusCode)
		}

		lyric = string(body)
		return nil
	})

	if err != nil {
		return "", err
	}
	return lyric, nil
}

// ResolveB23ID follows a b23.tv shortlink and finds the actual track ID
func (c *Client) ResolveB23ID(ctx context.Context, shortID string) (string, error) {
	if c.logger != nil {
		c.logger.Debug("bilibili: resolving b23.tv shortlink", "shortID", shortID)
	}

	urlStr := fmt.Sprintf("https://b23.tv/%s", shortID)

	var finalUrl string
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
		if err != nil {
			return err
		}

		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		finalUrl = resp.Request.URL.String()
		return nil
	})

	if err != nil {
		return "", err
	}

	matcher := NewURLMatcher()
	id, ok := matcher.MatchURL(finalUrl)
	if !ok || strings.HasPrefix(id, "b23:") {
		return "", fmt.Errorf("could not resolve b23 link or it did not resolve to a media track (resolved to %s)", finalUrl)
	}

	return id, nil
}

// GetVideoInfo fetches metadata for a video track using its id (bvid or av).
func (c *Client) GetVideoInfo(ctx context.Context, id string) (*VideoInfoData, error) {
	if c.logger != nil {
		c.logger.Debug("bilibili: fetching video info", "id", id)
	}

	lowerId := strings.ToLower(id)
	var url string
	if strings.HasPrefix(lowerId, "av") {
		url = fmt.Sprintf("https://api.bilibili.com/x/web-interface/view?aid=%s", id[2:])
	} else {
		url = fmt.Sprintf("https://api.bilibili.com/x/web-interface/view?bvid=%s", id)
	}

	var result VideoInfoResponse
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("bilibili: unexpected status code %d: %s", resp.StatusCode, string(body))
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("bilibili: decode video info: %w", err)
		}

		if result.Code != 0 {
			return fmt.Errorf("bilibili: API error code %d: %s", result.Code, result.Message)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// GetVideoPlayUrl fetches the actual raw dash audio streams for a video track.
func (c *Client) GetVideoPlayUrl(ctx context.Context, bvid string, cid int) ([]VideoDashAudio, error) {
	if c.logger != nil {
		c.logger.Debug("bilibili: fetching video play url", "bvid", bvid, "cid", cid)
	}

	// qn=16 and fnval=16 returns DASH format containing raw audio streams
	url := fmt.Sprintf("https://api.bilibili.com/x/player/playurl?bvid=%s&cid=%d&qn=16&fnval=16", bvid, cid)

	var result VideoPlayUrlResponse
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("bilibili: unexpected status code %d: %s", resp.StatusCode, string(body))
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("bilibili: decode video play url info: %w", err)
		}

		if result.Code != 0 {
			return fmt.Errorf("bilibili: API error code %d: %s", result.Code, result.Message)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if result.Data == nil || len(result.Data.Dash.Audio) == 0 {
		return nil, fmt.Errorf("bilibili: no audio stream found in response")
	}

	// Collect all available audio streams
	var allAudio []VideoDashAudio
	allAudio = append(allAudio, result.Data.Dash.Audio...)

	// Also append FLAC and Dolby if available
	if result.Data.Dash.Flac != nil && result.Data.Dash.Flac.Audio != nil {
		allAudio = append(allAudio, *result.Data.Dash.Flac.Audio)
	}

	if result.Data.Dash.Dolby != nil && len(result.Data.Dash.Dolby.Audio) > 0 {
		allAudio = append(allAudio, result.Data.Dash.Dolby.Audio...)
	}

	return allAudio, nil
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

		wait := c.httpClient.Backoff(c.minBackoff, c.maxBackoff, attempt, nil)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}

	if lastErr == nil {
		lastErr = errors.New("bilibili: retry failed")
	}
	return lastErr
}
