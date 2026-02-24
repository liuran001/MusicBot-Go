package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	autoRenew    bilibiliAutoRenewConfig
}

type bilibiliAutoRenewConfig struct {
	enabled  bool
	interval time.Duration
	persist  bool
	path     string
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
	Bvid      string      `json:"bvid"`
	Aid       int         `json:"aid"`
	Cid       int         `json:"cid"`
	Pages     []VideoPage `json:"pages"`
	Tid       int         `json:"tid"`
	Tname     string      `json:"tname"`
	TypeName  string      `json:"type_name"`
	TidV2     int         `json:"tid_v2"`
	TnameV2   string      `json:"tname_v2"`
	PidV2     int         `json:"pid_v2"`
	PidNameV2 string      `json:"pid_name_v2"`
	Title     string      `json:"title"`
	Pic       string      `json:"pic"`
	Desc      string      `json:"desc"`
	Duration  int         `json:"duration"`
	Owner     struct {
		Mid  int    `json:"mid"`
		Name string `json:"name"`
		Face string `json:"face"`
	} `json:"owner"`
}

type VideoPage struct {
	Cid      int    `json:"cid"`
	Page     int    `json:"page"`
	Part     string `json:"part"`
	Duration int    `json:"duration"`
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

type VideoSubtitleItem struct {
	SubtitleURL   string `json:"subtitle_url"`
	SubtitleURLV2 string `json:"subtitle_url_v2"`
	Lan           string `json:"lan"`
	LanDoc        string `json:"lan_doc"`
	AiType        int    `json:"ai_type"`
}

type VideoSubtitleData struct {
	Subtitle struct {
		Subtitles []VideoSubtitleItem `json:"subtitles"`
	} `json:"subtitle"`
}

type VideoSubtitleResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    *VideoSubtitleData `json:"data"`
}

type VideoSearchItem struct {
	TypeName string `json:"typename"`
	ArcURL   string `json:"arcurl"`
	AID      int    `json:"aid"`
	BVID     string `json:"bvid"`
	Title    string `json:"title"`
	Pic      string `json:"pic"`
	Duration string `json:"duration"`
	Author   string `json:"author"`
	Mid      int    `json:"mid"`
	TypeID   string `json:"typeid"`
}

type VideoSearchData struct {
	Result []VideoSearchItem `json:"result"`
}

type VideoSearchResponse struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    *VideoSearchData `json:"data"`
}

type SubtitleBodyLine struct {
	From    float64 `json:"from"`
	To      float64 `json:"to"`
	Content string  `json:"content"`
}

type SubtitleBodyResponse struct {
	Body []SubtitleBodyLine `json:"body"`
}

// New returns an instance of Bilibili client.
func New(logger bot.Logger, cookie string, refreshToken string, autoRenewEnabled bool, autoRenewInterval time.Duration, autoRenewPersist bool, autoRenewPersistPath string) *Client {
	c := &Client{
		httpClient:   retryablehttp.NewClient(),
		maxRetries:   3,
		minBackoff:   1 * time.Second,
		maxBackoff:   5 * time.Second,
		logger:       logger,
		cookie:       cookie,
		refreshToken: refreshToken,
		autoRenew: bilibiliAutoRenewConfig{
			enabled:  autoRenewEnabled,
			interval: autoRenewInterval,
			persist:  autoRenewPersist,
			path:     strings.TrimSpace(autoRenewPersistPath),
		},
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

// GetAudioSongLyric fetches lyric content directly by bilibili audio sid.
func (c *Client) GetAudioSongLyric(ctx context.Context, sid int) (string, error) {
	if sid <= 0 {
		return "", errors.New("bilibili: invalid audio sid")
	}

	if c.logger != nil {
		c.logger.Debug("bilibili: fetching audio lyric by sid", "sid", sid)
	}

	apiURL := fmt.Sprintf("https://www.bilibili.com/audio/music-service-c/web/song/lyric?sid=%d", sid)

	var result struct {
		Code    int     `json:"code"`
		Message string  `json:"msg"`
		Data    *string `json:"data"`
	}

	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
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
			return fmt.Errorf("bilibili: decode song lyric: %w", err)
		}

		if result.Code != 0 {
			return fmt.Errorf("bilibili: API error code %d: %s", result.Code, result.Message)
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	if result.Data == nil {
		return "", nil
	}

	return strings.TrimSpace(*result.Data), nil
}

func pickBestSubtitleURL(items []VideoSubtitleItem) string {
	if len(items) == 0 {
		return ""
	}

	type subtitleCandidate struct {
		url   string
		score int
		lan   string
	}

	candidates := make([]subtitleCandidate, 0, len(items)*2)
	for _, item := range items {
		score := subtitleLanguagePriority(item.Lan, item.LanDoc)
		if item.AiType != 0 {
			score += 5
		}

		for _, raw := range []string{item.SubtitleURL, item.SubtitleURLV2} {
			u := strings.TrimSpace(raw)
			if u == "" {
				continue
			}
			if strings.HasPrefix(u, "//") {
				u = "https:" + u
			}
			candidates = append(candidates, subtitleCandidate{
				url:   u,
				score: score,
				lan:   strings.ToLower(strings.TrimSpace(item.Lan)),
			})
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score < candidates[j].score
		}
		if candidates[i].lan != candidates[j].lan {
			return candidates[i].lan < candidates[j].lan
		}
		return candidates[i].url < candidates[j].url
	})

	return candidates[0].url
}

func subtitleLanguagePriority(lan, lanDoc string) int {
	lanNorm := strings.ToLower(strings.TrimSpace(lan))
	lanDocNorm := strings.ToLower(strings.TrimSpace(lanDoc))

	switch {
	case lanNorm == "zh-cn" || lanNorm == "zh-hans" || lanNorm == "zh":
		return 0
	case strings.HasPrefix(lanNorm, "zh"):
		return 1
	case strings.Contains(lanDocNorm, "中文") || strings.Contains(lanDocNorm, "汉") || strings.Contains(lanDocNorm, "漢"):
		return 2
	default:
		return 10
	}
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

func (c *Client) SearchVideo(ctx context.Context, keyword string, page int) ([]VideoSearchItem, error) {
	if c.logger != nil {
		c.logger.Debug("bilibili: searching video", "keyword", keyword, "page", page)
	}
	if page <= 0 {
		page = 1
	}

	query := url.Values{}
	query.Set("search_type", "video")
	query.Set("keyword", keyword)
	query.Set("page", fmt.Sprintf("%d", page))

	apiURL := "https://api.bilibili.com/x/web-interface/search/type?" + query.Encode()

	var result VideoSearchResponse
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
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
			return fmt.Errorf("bilibili: decode video search: %w", err)
		}

		if result.Code != 0 {
			return fmt.Errorf("bilibili: API error code %d: %s", result.Code, result.Message)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if result.Data == nil {
		return nil, nil
	}
	return result.Data.Result, nil
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

// GetVideoSubtitleURL fetches an available subtitle URL for a video.
func (c *Client) GetVideoSubtitleURL(ctx context.Context, bvid string, cid int) (string, error) {
	if c.logger != nil {
		c.cookieMutex.RLock()
		cookieLen := len(c.cookie)
		hasSESSDATA := strings.Contains(c.cookie, "SESSDATA=")
		c.cookieMutex.RUnlock()

		c.logger.Debug("bilibili: fetching video subtitle list", "bvid", bvid, "cid", cid, "cookie_len", cookieLen, "cookie_has_sessdata", hasSESSDATA)
	}

	type subtitleEndpoint struct {
		Name string
		URL  string
	}

	endpoints := []subtitleEndpoint{
		{
			Name: "player.wbi.v2",
			URL:  fmt.Sprintf("https://api.bilibili.com/x/player/wbi/v2?bvid=%s&cid=%d", bvid, cid),
		},
		{
			Name: "player.v2",
			URL:  fmt.Sprintf("https://api.bilibili.com/x/player/v2?bvid=%s&cid=%d", bvid, cid),
		},
	}

	var lastErr error
	hasSuccessResponse := false

	for _, ep := range endpoints {
		var result VideoSubtitleResponse
		err := c.execute(ctx, func() error {
			req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, ep.URL, nil)
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
				return fmt.Errorf("bilibili: decode subtitle list: %w", err)
			}

			if result.Code != 0 {
				return fmt.Errorf("bilibili: API error code %d: %s", result.Code, result.Message)
			}

			return nil
		})

		if err != nil {
			lastErr = err
			if c.logger != nil {
				c.logger.Debug("bilibili: subtitle list fetch failed", "bvid", bvid, "cid", cid, "endpoint", ep.Name, "err", err)
			}
			continue
		}

		hasSuccessResponse = true

		subtitleCount := 0
		if result.Data != nil {
			subtitleCount = len(result.Data.Subtitle.Subtitles)
		}
		if c.logger != nil {
			c.logger.Debug("bilibili: subtitle list fetched", "bvid", bvid, "cid", cid, "endpoint", ep.Name, "api_code", result.Code, "subtitle_count", subtitleCount)
		}

		if subtitleCount == 0 {
			continue
		}

		if selected := pickBestSubtitleURL(result.Data.Subtitle.Subtitles); selected != "" {
			return selected, nil
		}
	}

	if !hasSuccessResponse && lastErr != nil {
		return "", lastErr
	}

	return "", nil
}

// GetVideoSubtitleLines fetches subtitle body lines from subtitle URL.
func (c *Client) GetVideoSubtitleLines(ctx context.Context, subtitleURL string) ([]SubtitleBodyLine, error) {
	subtitleURL = strings.TrimSpace(subtitleURL)
	if subtitleURL == "" {
		return nil, errors.New("bilibili: empty subtitle url")
	}

	if c.logger != nil {
		c.logger.Debug("bilibili: fetching subtitle body", "url", subtitleURL)
	}

	var result SubtitleBodyResponse
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, subtitleURL, nil)
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
			return fmt.Errorf("bilibili: unexpected status code %d fetching subtitle body", resp.StatusCode)
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("bilibili: decode subtitle body: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result.Body, nil
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
