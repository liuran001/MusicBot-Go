package applemusic

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/platform"

	widevine "github.com/iyear/gowidevine"
)

const (
	appleMusicBaseURL  = "https://amp-api.music.apple.com"
	appleMusicOrigin   = "https://music.apple.com"
	appleMusicUA       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/110.0.0.0 Safari/537.36"
	defaultArtworkSize = 1200
)

// Client is the Apple Music API client.
type Client struct {
	httpClient         *http.Client
	developerToken     string
	mediaUserToken     string
	storefront         string
	language           string
	languageExplicit   bool             // user explicitly configured language; don't override on storefront auto-detect
	wrapperHost        string           // Host of the wrapper service (e.g. "127.0.0.1"), empty = disabled
	wvDevice           *widevine.Device // Widevine L3 device for native decryption
	storefrontDetected bool
	logger             *logpkg.Logger
	persistFunc        func(pairs map[string]string) error
	tokenMu            sync.RWMutex
}

// NewClient creates an Apple Music API client.
func NewClient(mediaUserToken, storefront, language string, timeout time.Duration, logger *logpkg.Logger) *Client {
	if storefront == "" {
		storefront = "us"
	}
	if language == "" {
		language = "en-US"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		httpClient:     &http.Client{Timeout: timeout},
		mediaUserToken: strings.TrimSpace(mediaUserToken),
		storefront:     storefront,
		language:       language,
		logger:         logger,
	}
}

// SetAPIProxy configures the HTTP client proxy.
func (c *Client) SetAPIProxy(cfg httpproxy.Config) error {
	if c == nil {
		return nil
	}
	timeout := 30 * time.Second
	if c.httpClient != nil && c.httpClient.Timeout > 0 {
		timeout = c.httpClient.Timeout
	}
	proxiedClient, err := httpproxy.NewHTTPClient(cfg, timeout)
	if err != nil {
		return err
	}
	if proxiedClient == nil {
		c.httpClient = &http.Client{Timeout: timeout}
		return nil
	}
	c.httpClient = proxiedClient
	return nil
}

// --- Developer Token ---

var (
	jsAssetPattern = regexp.MustCompile(`/assets/index[^"'\s]*\.js`)
	tokenPattern   = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{40,}\.[A-Za-z0-9_-]{40,}\.[A-Za-z0-9_-]{40,}`)
)

func (c *Client) ensureDeveloperToken(ctx context.Context) error {
	c.tokenMu.RLock()
	if c.developerToken != "" {
		c.tokenMu.RUnlock()
		return nil
	}
	c.tokenMu.RUnlock()

	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	// Double-check after acquiring write lock.
	if c.developerToken != "" {
		return nil
	}

	token, err := c.fetchDeveloperToken(ctx)
	if err != nil {
		return fmt.Errorf("applemusic: fetch developer token: %w", err)
	}
	c.developerToken = token
	if c.logger != nil {
		c.logger.Debug("applemusic: developer token fetched", "length", len(token))
	}
	return nil
}

func (c *Client) fetchDeveloperToken(ctx context.Context) (string, error) {
	// Step 1: Fetch the Apple Music homepage.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appleMusicOrigin, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", appleMusicUA)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}
	html := string(body)

	// Step 2: Find JS bundle URL.
	match := jsAssetPattern.FindString(html)
	if match == "" {
		return "", fmt.Errorf("js bundle URL not found in homepage")
	}
	jsURL := appleMusicOrigin + match

	// Step 3: Fetch the JS bundle.
	jsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, jsURL, nil)
	if err != nil {
		return "", err
	}
	jsReq.Header.Set("User-Agent", appleMusicUA)

	jsResp, err := c.httpClient.Do(jsReq)
	if err != nil {
		return "", err
	}
	defer jsResp.Body.Close()

	jsBody, err := io.ReadAll(io.LimitReader(jsResp.Body, 10*1024*1024))
	if err != nil {
		return "", err
	}

	// Step 4: Extract JWT token.
	token := tokenPattern.FindString(string(jsBody))
	if token == "" {
		return "", fmt.Errorf("JWT token not found in JS bundle")
	}
	return token, nil
}

func (c *Client) clearDeveloperToken() {
	c.tokenMu.Lock()
	c.developerToken = ""
	c.tokenMu.Unlock()
}

func (c *Client) getDeveloperToken() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.developerToken
}

// --- HTTP Requests ---

func (c *Client) doRequest(ctx context.Context, reqURL string) ([]byte, error) {
	if err := c.ensureDeveloperToken(ctx); err != nil {
		return nil, err
	}
	c.autoDetectStorefront(ctx)
	return c.doRequestInner(ctx, reqURL, true)
}

// autoDetectStorefront queries /v1/me/storefront to match the account region.
// Lyrics and some other endpoints require the storefront to match the account.
func (c *Client) autoDetectStorefront(ctx context.Context) {
	if c.storefrontDetected || strings.TrimSpace(c.mediaUserToken) == "" {
		return
	}
	c.storefrontDetected = true // only try once

	sfURL := appleMusicBaseURL + "/v1/me/storefront"
	body, err := c.doRequestInner(ctx, sfURL, false)
	if err != nil {
		if c.logger != nil {
			c.logger.Debug("applemusic: storefront auto-detect failed", "error", err)
		}
		return
	}

	var resp struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				DefaultLanguageTag string   `json:"defaultLanguageTag"`
				Name               string   `json:"name"`
				SupportedLangs     []string `json:"supportedLanguageTags"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Data) == 0 {
		return
	}

	detected := resp.Data[0].ID
	if detected != "" && detected != c.storefront {
		if c.logger != nil {
			c.logger.Info("applemusic: auto-detected storefront",
				"configured", c.storefront, "detected", detected,
				"name", resp.Data[0].Attributes.Name)
		}
		c.storefront = detected
		// Follow the storefront's default language ONLY when the user did not
		// explicitly configure one. An explicit language must always be honored
		// (as long as the storefront supports it).
		if !c.languageExplicit && resp.Data[0].Attributes.DefaultLanguageTag != "" {
			c.language = resp.Data[0].Attributes.DefaultLanguageTag
		}
	}
}

func (c *Client) doRequestInner(ctx context.Context, reqURL string, retry bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.getDeveloperToken())
	req.Header.Set("Origin", appleMusicOrigin)
	req.Header.Set("User-Agent", appleMusicUA)
	if strings.TrimSpace(c.mediaUserToken) != "" {
		req.Header.Set("media-user-token", c.mediaUserToken)
		// Also set as cookie — some endpoints (lyrics) require cookie-based auth.
		req.AddCookie(&http.Cookie{Name: "media-user-token", Value: c.mediaUserToken})
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusUnauthorized:
		if retry {
			c.clearDeveloperToken()
			if err := c.ensureDeveloperToken(ctx); err != nil {
				return nil, err
			}
			return c.doRequestInner(ctx, reqURL, false)
		}
		return nil, &platform.PlatformError{Platform: "applemusic", Resource: "api", Err: platform.ErrAuthRequired}
	case http.StatusTooManyRequests:
		return nil, platform.NewRateLimitedError("applemusic")
	case http.StatusNotFound:
		return nil, platform.NewNotFoundError("applemusic", "resource", reqURL)
	default:
		return nil, fmt.Errorf("applemusic: HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
}

// --- Search ---

func (c *Client) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 25 {
		limit = 25
	}

	reqURL := fmt.Sprintf("%s/v1/catalog/%s/search?term=%s&types=songs&limit=%d&l=%s",
		appleMusicBaseURL, c.storefront, url.QueryEscape(query), limit, c.language)

	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	var resp appleMusicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("applemusic: parse search response: %w", err)
	}
	if resp.Results == nil || resp.Results.Songs == nil {
		return nil, nil
	}

	tracks := make([]platform.Track, 0, len(resp.Results.Songs.Data))
	for _, song := range resp.Results.Songs.Data {
		tracks = append(tracks, convertSong(song))
	}
	return tracks, nil
}

// --- Track ---

func (c *Client) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	reqURL := fmt.Sprintf("%s/v1/catalog/%s/songs/%s?include=albums,artists&extend=extendedAssetUrls&l=%s",
		appleMusicBaseURL, c.storefront, trackID, c.language)

	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	var resp appleMusicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("applemusic: parse track response: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, platform.NewNotFoundError("applemusic", "track", trackID)
	}

	track := convertSong(resp.Data[0])
	return &track, nil
}

// --- Album ---

func (c *Client) GetAlbum(ctx context.Context, albumID string) (*platform.Album, []platform.Track, error) {
	reqURL := fmt.Sprintf("%s/v1/catalog/%s/albums/%s?include=tracks,artists&l=%s",
		appleMusicBaseURL, c.storefront, albumID, c.language)

	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return nil, nil, err
	}

	var resp appleMusicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("applemusic: parse album response: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, nil, platform.NewNotFoundError("applemusic", "album", albumID)
	}

	album := convertAlbum(resp.Data[0])

	var tracks []platform.Track
	if rel := resp.Data[0].Relationships; rel != nil && rel.Tracks != nil {
		for _, t := range rel.Tracks.Data {
			tracks = append(tracks, convertSong(t))
		}
	}
	return &album, tracks, nil
}

// --- Artist ---

func (c *Client) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	reqURL := fmt.Sprintf("%s/v1/catalog/%s/artists/%s?l=%s",
		appleMusicBaseURL, c.storefront, artistID, c.language)

	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	var resp appleMusicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("applemusic: parse artist response: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, platform.NewNotFoundError("applemusic", "artist", artistID)
	}

	artist := convertArtist(resp.Data[0])
	return &artist, nil
}

// --- Playlist ---

func (c *Client) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	reqURL := fmt.Sprintf("%s/v1/catalog/%s/playlists/%s?include=tracks&l=%s",
		appleMusicBaseURL, c.storefront, playlistID, c.language)

	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	var resp appleMusicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("applemusic: parse playlist response: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, platform.NewNotFoundError("applemusic", "playlist", playlistID)
	}

	res := resp.Data[0]
	attrs := res.Attributes

	var tracks []platform.Track
	if rel := res.Relationships; rel != nil && rel.Tracks != nil {
		for _, t := range rel.Tracks.Data {
			tracks = append(tracks, convertSong(t))
		}
	}

	return &platform.Playlist{
		ID:          res.ID,
		Platform:    "applemusic",
		Title:       attrs.Name,
		Description: descriptionText(attrs.Description),
		CoverURL:    formatArtworkURL(attrs.Artwork, defaultArtworkSize),
		Creator:     attrs.CuratorName,
		TrackCount:  maxInt(attrs.TrackCount, len(tracks)),
		Tracks:      tracks,
		URL:         attrs.URL,
	}, nil
}

// --- Lyrics ---

func (c *Client) GetLyrics(ctx context.Context, trackID string) (string, error) {
	reqURL := fmt.Sprintf("%s/v1/catalog/%s/songs/%s/lyrics?l=%s",
		appleMusicBaseURL, c.storefront, trackID, c.language)

	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return "", err
	}

	var resp appleMusicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("applemusic: parse lyrics response: %w", err)
	}
	if len(resp.Data) == 0 {
		return "", platform.NewUnavailableError("applemusic", "lyrics", trackID)
	}

	ttml := resp.Data[0].Attributes.TTML
	if ttml == "" {
		return "", platform.NewUnavailableError("applemusic", "lyrics", trackID)
	}
	return parseTTMLToLRC(ttml), nil
}

// --- Download ---

func (c *Client) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	// Fetch song details to confirm the track exists / is available.
	reqURL := fmt.Sprintf("%s/v1/catalog/%s/songs/%s?include=albums,artists&extend=extendedAssetUrls&l=%s",
		appleMusicBaseURL, c.storefront, trackID, c.language)

	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	var resp appleMusicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("applemusic: parse song response: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, platform.NewNotFoundError("applemusic", "track", trackID)
	}

	// Decide whether the request wants a lossless/Hi-Res/Atmos tier. Those are
	// ONLY obtainable through the external FairPlay wrapper — Apple refuses
	// Widevine licenses for enhancedHls (server-side, returns -1002), so the
	// built-in native path tops out at AAC 256k.
	wantsLossless := quality >= platform.QualityLossless
	hasWrapper := strings.TrimSpace(c.wrapperHost) != ""

	// Priority 1 (lossless tiers): external wrapper via enhancedHls.
	if wantsLossless && hasWrapper {
		info, err := c.fetchViaWrapper(ctx, trackID, quality, false)
		if err == nil && info != nil {
			return info, nil
		}
		if c.logger != nil {
			c.logger.Warn("applemusic: wrapper lossless failed, falling back to AAC",
				"track_id", trackID, "quality", quality.String(), "error", err)
		}
	} else if wantsLossless && !hasWrapper && c.logger != nil {
		c.logger.Info("applemusic: lossless requested but no wrapper configured; serving AAC 256k",
			"track_id", trackID, "quality", quality.String())
	}

	// Priority 2: Native Widevine decryption (built-in, AAC 256k, zero-config).
	if c.wvDevice != nil && strings.TrimSpace(c.mediaUserToken) != "" {
		info, err := c.fetchDecrypted(ctx, trackID)
		if err == nil && info != nil {
			return info, nil
		}
		if c.logger != nil {
			c.logger.Warn("applemusic: native decrypt failed, trying fallback", "track_id", trackID, "error", err)
		}
	}

	// Priority 3: External wrapper for non-lossless requests (if available).
	if !wantsLossless && hasWrapper {
		info, err := c.fetchViaWrapper(ctx, trackID, quality, false)
		if err == nil && info != nil {
			return info, nil
		}
		if c.logger != nil {
			c.logger.Warn("applemusic: wrapper decrypt failed, trying fallback", "track_id", trackID, "error", err)
		}
	}

	// Priority 4: WebPlayback encrypted mp4 (full track, DRM-encrypted).
	if strings.TrimSpace(c.mediaUserToken) != "" {
		info, err := c.fetchWebPlayback(ctx, trackID)
		if err == nil && info != nil {
			return info, nil
		}
		if c.logger != nil {
			c.logger.Debug("applemusic: webplayback failed", "track_id", trackID, "error", err)
		}
	}

	// No full-quality source succeeded. We intentionally do NOT fall back to the
	// 30-second preview clip — a preview is not a real download, so treat it as
	// "track unavailable".
	return nil, platform.NewUnavailableError("applemusic", "track", trackID)
}

// fetchDecrypted performs native Go Widevine decryption for a full quality track.
// Uses a custom DownloadFunc to run the decryption pipeline and write to disk.
func (c *Client) fetchDecrypted(_ context.Context, trackID string) (*platform.DownloadInfo, error) {
	if c.wvDevice == nil {
		return nil, fmt.Errorf("widevine device not configured")
	}

	downloadFn := func(ctx context.Context, info *platform.DownloadInfo, destPath string, progress func(written, total int64)) (int64, error) {
		decrypted, err := c.decryptTrack(ctx, trackID, c.wvDevice)
		if err != nil {
			return 0, fmt.Errorf("applemusic: decrypt track %s: %w", trackID, err)
		}

		if err := func() error {
			f, err := createFile(destPath)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.Write(decrypted)
			return err
		}(); err != nil {
			return 0, err
		}

		// Decryption yields a fragmented MP4, which won't show a progress bar /
		// seek in Telegram or many desktop players. Remux to progressive MP4.
		if err := remuxToProgressive(ctx, destPath); err != nil {
			if c.logger != nil {
				c.logger.Warn("applemusic: remux to progressive failed (file may not seek)",
					"track_id", trackID, "error", err)
			}
		}

		fi, statErr := os.Stat(destPath)
		var n int64
		if statErr == nil {
			n = fi.Size()
		}
		if progress != nil {
			progress(n, n)
		}
		return n, nil
	}

	if c.logger != nil {
		c.logger.Debug("applemusic: using native widevine decrypt", "track_id", trackID)
	}

	return &platform.DownloadInfo{
		URL:        "widevine://" + trackID,
		Format:     "m4a",
		Bitrate:    256,
		Quality:    platform.QualityHigh,
		Downloader: downloadFn,
	}, nil
}

const webPlaybackURL = "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/webPlayback"

// webPlaybackResponse is the response from the WebPlayback API.
type webPlaybackResponse struct {
	SongList []webPlaybackSong `json:"songList"`
}

type webPlaybackSong struct {
	SongID string             `json:"songId"`
	Assets []webPlaybackAsset `json:"assets"`
}

type webPlaybackAsset struct {
	Flavor   string              `json:"flavor"`
	URL      string              `json:"URL"`
	FileSize int64               `json:"file-size"`
	Metadata webPlaybackMetadata `json:"metadata"`
}

type webPlaybackMetadata struct {
	FileExtension string `json:"fileExtension"`
	BitRate       int    `json:"bitRate"`
	SampleRate    int    `json:"sampleRate"`
	ItemName      string `json:"itemName"`
	Duration      int    `json:"duration"` // millis
}

func (c *Client) fetchWebPlayback(ctx context.Context, trackID string) (*platform.DownloadInfo, error) {
	if err := c.ensureDeveloperToken(ctx); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(map[string]string{"salableAdamId": trackID})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webPlaybackURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.getDeveloperToken())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", appleMusicOrigin)
	req.Header.Set("User-Agent", appleMusicUA)
	req.Header.Set("media-user-token", c.mediaUserToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("applemusic: webplayback HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, err
	}

	var wpResp webPlaybackResponse
	if err := json.Unmarshal(body, &wpResp); err != nil {
		return nil, fmt.Errorf("applemusic: parse webplayback response: %w", err)
	}
	if len(wpResp.SongList) == 0 || len(wpResp.SongList[0].Assets) == 0 {
		return nil, fmt.Errorf("applemusic: no assets in webplayback response")
	}

	// Select best asset: prefer ctrp256 (Widevine, highest quality downloadable).
	assets := wpResp.SongList[0].Assets
	selected := assets[0]
	for _, a := range assets {
		if a.Flavor == "28:ctrp256" {
			selected = a
			break
		}
	}
	// Fallback preference order if ctrp256 not found.
	if selected.Flavor != "28:ctrp256" {
		for _, a := range assets {
			if a.Metadata.BitRate > selected.Metadata.BitRate {
				selected = a
			}
		}
	}

	hlsURL := strings.TrimSpace(selected.URL)
	if hlsURL == "" {
		return nil, fmt.Errorf("applemusic: empty HLS URL in webplayback response")
	}

	// Parse the m3u8 to find the actual mp4 segment URL.
	mp4URL, mp4Size, err := c.resolveHLSToMP4(ctx, hlsURL)
	if err != nil {
		return nil, fmt.Errorf("applemusic: resolve HLS: %w", err)
	}

	bitrate := selected.Metadata.BitRate
	if bitrate <= 0 {
		bitrate = 256
	}

	quality := platform.QualityHigh
	if bitrate >= 256 {
		quality = platform.QualityHigh
	}

	if c.logger != nil {
		c.logger.Debug("applemusic: webplayback resolved",
			"track_id", trackID, "flavor", selected.Flavor,
			"bitrate", bitrate, "size", mp4Size)
	}

	return &platform.DownloadInfo{
		URL:     mp4URL,
		Format:  "m4a",
		Size:    mp4Size,
		Bitrate: bitrate,
		Quality: quality,
	}, nil
}

// resolveHLSToMP4 fetches an HLS m3u8 manifest and resolves the underlying mp4 segment URL.
// Apple Music HLS playlists reference a single mp4 file with byte-range segments.
func (c *Client) resolveHLSToMP4(ctx context.Context, m3u8URL string) (mp4URL string, size int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m3u8URL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", appleMusicUA)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return "", 0, err
	}

	m3u8Content := string(body)
	baseURL := m3u8URL[:strings.LastIndex(m3u8URL, "/")+1]

	// Find the mp4 segment filename (non-comment line ending in .mp4).
	var mp4Name string
	for line := range strings.SplitSeq(m3u8Content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ".mp4") || strings.HasSuffix(line, ".m4a") || strings.HasSuffix(line, ".m4s") {
			mp4Name = line
			break
		}
	}
	if mp4Name == "" {
		return "", 0, fmt.Errorf("no mp4 segment found in m3u8")
	}

	// Build absolute URL.
	if strings.HasPrefix(mp4Name, "http") {
		mp4URL = mp4Name
	} else {
		mp4URL = baseURL + mp4Name
	}

	// Get the file size via HEAD request.
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, mp4URL, nil)
	if err != nil {
		return mp4URL, 0, nil
	}
	headReq.Header.Set("User-Agent", appleMusicUA)

	headResp, err := c.httpClient.Do(headReq)
	if err != nil {
		return mp4URL, 0, nil
	}
	headResp.Body.Close()

	if cl := headResp.Header.Get("Content-Length"); cl != "" {
		size, _ = strconv.ParseInt(cl, 10, 64)
	}

	return mp4URL, size, nil
}

// --- Wrapper Decryption ---

// fetchViaWrapper decrypts an enhancedHls stream (ALAC lossless / Hi-Res /
// Atmos) through the WorldObservationLog/wrapper FairPlay service.
//
// The full flow:
//  1. Resolve the enhancedHls master playlist. For Hi-Res/Atmos, prefer the
//     wrapper's device m3u8 (port 20020) — the catalog enhancedHls is a trimmed
//     "_lossless" variant (ALAC ≤48kHz/24bit, no true Hi-Res/Atmos). Fall back
//     to the catalog enhancedHls when the device m3u8 is unavailable.
//  2. Pick the stream variant matching the requested quality.
//  3. Fetch the variant media playlist (per-segment skd:// keys + mp4 URL).
//  4. Download the single byte-range mp4 from CDN.
//  5. Stream each cbcs sample through wrapper port 10020 to decrypt in place,
//     then (ALAC) run alacfix and remux to a progressive MP4.
func (c *Client) fetchViaWrapper(ctx context.Context, trackID string, quality platform.Quality, wantAtmos bool) (*platform.DownloadInfo, error) {
	host := strings.TrimSpace(c.wrapperHost)
	if host == "" {
		return nil, fmt.Errorf("wrapper host not configured")
	}

	wrapper := NewWrapperClient(host)

	// Resolve the enhancedHls master playlist. The catalog API's enhancedHls is
	// a trimmed "_lossless" variant: ALAC capped at 48kHz/24bit, no true Hi-Res
	// (96/192kHz) and no Atmos. The wrapper's device m3u8 port (20020) returns
	// the full "_default" master with those streams. For Hi-Res/Atmos requests
	// we must use the device m3u8; for AAC/standard the catalog is enough.
	wantBest := wantAtmos || quality >= platform.QualityHiRes
	var masterURL string
	if wantBest {
		if devURL, err := wrapper.GetM3U8URL(ctx, trackID); err == nil && strings.HasSuffix(devURL, ".m3u8") {
			masterURL = devURL
		} else if c.logger != nil {
			c.logger.Warn("applemusic: device m3u8 unavailable, falling back to catalog enhancedHls",
				"track_id", trackID, "error", err)
		}
	}
	if masterURL == "" {
		catalogURL, err := c.enhancedHLSMasterURL(ctx, trackID)
		if err != nil {
			return nil, fmt.Errorf("enhancedHls master: %w", err)
		}
		masterURL = catalogURL
	}

	masterBody, err := c.downloadURL(ctx, masterURL)
	if err != nil {
		return nil, fmt.Errorf("fetch enhancedHls master: %w", err)
	}
	variants, err := parseEnhancedHLSMaster(string(masterBody))
	if err != nil {
		return nil, err
	}
	variant, ok := selectVariantForQuality(variants, quality, wantAtmos)
	if !ok {
		return nil, fmt.Errorf("no suitable enhancedHls variant for quality %s", quality)
	}

	mediaURL := variant.URI
	if !strings.HasPrefix(mediaURL, "http") {
		base := masterURL[:strings.LastIndex(masterURL, "/")+1]
		mediaURL = base + mediaURL
	}

	resolvedQuality := variantQuality(variant)
	resolvedBitrate := variant.AvgBW / 1000
	if resolvedBitrate <= 0 {
		resolvedBitrate = 256
	}
	format := "m4a"
	isALAC := variant.isALAC()

	downloadFn := func(ctx context.Context, info *platform.DownloadInfo, destPath string, progress func(written, total int64)) (int64, error) {
		// Fetch the variant's media playlist (per-segment keys + mp4 URL).
		mediaBody, err := c.downloadURL(ctx, mediaURL)
		if err != nil {
			return 0, fmt.Errorf("fetch media playlist: %w", err)
		}
		media, err := parseEnhancedHLSMedia(mediaURL, string(mediaBody))
		if err != nil {
			return 0, err
		}

		// Download the single byte-range mp4.
		encData, err := c.downloadURL(ctx, media.MP4URL)
		if err != nil {
			return 0, fmt.Errorf("download encrypted mp4: %w", err)
		}
		if progress != nil {
			progress(int64(len(encData))/2, int64(len(encData)))
		}

		f, err := createFile(destPath)
		if err != nil {
			return 0, err
		}

		// Decrypt via the wrapper (FairPlay cbcs, per-fragment/per-sample).
		if err := wrapper.DecryptEnhancedHLS(ctx, trackID, bytes.NewReader(encData), media.SegKeys, f); err != nil {
			f.Close()
			return 0, fmt.Errorf("wrapper decrypt: %w", err)
		}
		if err := f.Close(); err != nil {
			return 0, fmt.Errorf("close output: %w", err)
		}

		// ALAC packets from Apple sometimes lack the TYPE_END terminator, which
		// trips ffmpeg ("invalid element channel count"). Patch in place. This
		// is a no-op for files without the defect; failures are non-fatal.
		if isALAC {
			if err := fixALACFile(destPath); err != nil && c.logger != nil {
				c.logger.Warn("applemusic: alac fix failed (file may still play)",
					"track_id", trackID, "error", err)
			}
		}

		// The wrapper emits a fragmented MP4; remux to progressive so it shows a
		// progress bar / seeks in Telegram and desktop players.
		if err := remuxToProgressive(ctx, destPath); err != nil {
			if c.logger != nil {
				c.logger.Warn("applemusic: remux to progressive failed (file may not seek)",
					"track_id", trackID, "error", err)
			}
		}

		fi, statErr := os.Stat(destPath)
		var n int64
		if statErr == nil {
			n = fi.Size()
		}
		if progress != nil {
			progress(n, n)
		}
		return n, nil
	}

	if c.logger != nil {
		c.logger.Debug("applemusic: using wrapper for decrypted download",
			"track_id", trackID, "wrapper_host", host,
			"codecs", variant.Codecs, "quality", resolvedQuality.String())
	}

	return &platform.DownloadInfo{
		URL:        fmt.Sprintf("wrapper://%s/%s", host, trackID),
		Format:     format,
		Bitrate:    resolvedBitrate,
		Quality:    resolvedQuality,
		Downloader: downloadFn,
	}, nil
}

// variantQuality maps a selected enhancedHls variant to the unified Quality.
func variantQuality(v enhancedHLSVariant) platform.Quality {
	switch {
	case v.isALAC():
		if v.BitDepth >= 24 || v.SampleRate > 44100 {
			return platform.QualityHiRes
		}
		return platform.QualityLossless
	case v.isAtmos():
		return platform.QualityHiRes
	case v.isAAC():
		if v.AvgBW >= 200000 {
			return platform.QualityHigh
		}
		return platform.QualityStandard
	default:
		return platform.QualityHigh
	}
}

// enhancedHLSMasterURL fetches the catalog song and returns its enhancedHls
// master playlist URL (the source of lossless/Hi-Res/Atmos streams).
func (c *Client) enhancedHLSMasterURL(ctx context.Context, trackID string) (string, error) {
	reqURL := fmt.Sprintf("%s/v1/catalog/%s/songs/%s?extend=extendedAssetUrls&l=%s",
		appleMusicBaseURL, c.storefront, trackID, c.language)
	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return "", err
	}
	var resp appleMusicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse song: %w", err)
	}
	if len(resp.Data) == 0 || resp.Data[0].Attributes.ExtendedAssetUrls == nil {
		return "", fmt.Errorf("no extendedAssetUrls (track may lack lossless entitlement)")
	}
	url := strings.TrimSpace(resp.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls)
	if url == "" {
		return "", fmt.Errorf("empty enhancedHls URL")
	}
	return url, nil
}

// downloadURL downloads a URL and returns its content as bytes.
func (c *Client) downloadURL(ctx context.Context, targetURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", appleMusicUA)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, targetURL)
	}
	return io.ReadAll(resp.Body)
}

// createFile creates a file for writing, creating parent directories as needed.
func createFile(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return os.Create(path)
}

// --- API Types ---

type appleMusicResponse struct {
	Results *appleMusicSearchResults `json:"results,omitempty"`
	Data    []appleMusicResource     `json:"data,omitempty"`
	Errors  []appleMusicError        `json:"errors,omitempty"`
}

type appleMusicSearchResults struct {
	Songs   *appleMusicResourceList `json:"songs,omitempty"`
	Albums  *appleMusicResourceList `json:"albums,omitempty"`
	Artists *appleMusicResourceList `json:"artists,omitempty"`
}

type appleMusicResourceList struct {
	Data []appleMusicResource `json:"data"`
	Next string               `json:"next,omitempty"`
}

type appleMusicResource struct {
	ID            string                   `json:"id"`
	Type          string                   `json:"type"`
	Attributes    appleMusicAttributes     `json:"attributes"`
	Relationships *appleMusicRelationships `json:"relationships,omitempty"`
}

type appleMusicAttributes struct {
	Name              string                    `json:"name"`
	ArtistName        string                    `json:"artistName"`
	AlbumName         string                    `json:"albumName"`
	DurationInMillis  int                       `json:"durationInMillis"`
	TrackNumber       int                       `json:"trackNumber"`
	DiscNumber        int                       `json:"discNumber"`
	ISRC              string                    `json:"isrc"`
	ReleaseDate       string                    `json:"releaseDate"`
	GenreNames        []string                  `json:"genreNames"`
	Artwork           *appleMusicArtwork        `json:"artwork,omitempty"`
	Previews          []appleMusicPreview       `json:"previews,omitempty"`
	PlayParams        *appleMusicPlayParams     `json:"playParams,omitempty"`
	TrackCount        int                       `json:"trackCount"`
	Description       *appleMusicDescription    `json:"description,omitempty"`
	URL               string                    `json:"url"`
	CuratorName       string                    `json:"curatorName"`
	ExtendedAssetUrls *appleMusicExtendedAssets `json:"extendedAssetUrls,omitempty"`
	TTML              string                    `json:"ttml"`
}

type appleMusicArtwork struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type appleMusicPreview struct {
	URL string `json:"url"`
}

type appleMusicPlayParams struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type appleMusicDescription struct {
	Standard string `json:"standard"`
	Short    string `json:"short"`
}

type appleMusicExtendedAssets struct {
	EnhancedHls string `json:"enhancedHls,omitempty"`
}

type appleMusicRelationships struct {
	Albums  *appleMusicResourceList `json:"albums,omitempty"`
	Artists *appleMusicResourceList `json:"artists,omitempty"`
	Tracks  *appleMusicResourceList `json:"tracks,omitempty"`
}

type appleMusicError struct {
	Status string `json:"status"`
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// --- Conversion ---

func convertSong(res appleMusicResource) platform.Track {
	attrs := res.Attributes

	var artists []platform.Artist
	if rel := res.Relationships; rel != nil && rel.Artists != nil {
		for _, a := range rel.Artists.Data {
			artists = append(artists, convertArtist(a))
		}
	}
	if len(artists) == 0 && attrs.ArtistName != "" {
		artists = []platform.Artist{{Name: attrs.ArtistName, Platform: "applemusic"}}
	}

	var album *platform.Album
	if rel := res.Relationships; rel != nil && rel.Albums != nil && len(rel.Albums.Data) > 0 {
		a := convertAlbum(rel.Albums.Data[0])
		album = &a
	} else if attrs.AlbumName != "" {
		album = &platform.Album{Title: attrs.AlbumName, Platform: "applemusic"}
	}

	year := 0
	if attrs.ReleaseDate != "" {
		if t, err := time.Parse("2006-01-02", attrs.ReleaseDate); err == nil {
			year = t.Year()
		} else if t, err := time.Parse("2006", attrs.ReleaseDate); err == nil {
			year = t.Year()
		}
	}

	return platform.Track{
		ID:          res.ID,
		Platform:    "applemusic",
		Title:       attrs.Name,
		Artists:     artists,
		Album:       album,
		Duration:    time.Duration(attrs.DurationInMillis) * time.Millisecond,
		CoverURL:    formatArtworkURL(attrs.Artwork, defaultArtworkSize),
		URL:         attrs.URL,
		ISRC:        attrs.ISRC,
		Year:        year,
		TrackNumber: attrs.TrackNumber,
		DiscNumber:  attrs.DiscNumber,
	}
}

func convertAlbum(res appleMusicResource) platform.Album {
	attrs := res.Attributes

	var artists []platform.Artist
	if rel := res.Relationships; rel != nil && rel.Artists != nil {
		for _, a := range rel.Artists.Data {
			artists = append(artists, convertArtist(a))
		}
	}
	if len(artists) == 0 && attrs.ArtistName != "" {
		artists = []platform.Artist{{Name: attrs.ArtistName, Platform: "applemusic"}}
	}

	var releaseDate *time.Time
	year := 0
	if attrs.ReleaseDate != "" {
		if t, err := time.Parse("2006-01-02", attrs.ReleaseDate); err == nil {
			releaseDate = &t
			year = t.Year()
		}
	}

	return platform.Album{
		ID:          res.ID,
		Platform:    "applemusic",
		Title:       attrs.Name,
		Artists:     artists,
		CoverURL:    formatArtworkURL(attrs.Artwork, defaultArtworkSize),
		Description: descriptionText(attrs.Description),
		ReleaseDate: releaseDate,
		TrackCount:  attrs.TrackCount,
		URL:         attrs.URL,
		Year:        year,
	}
}

func convertArtist(res appleMusicResource) platform.Artist {
	attrs := res.Attributes
	return platform.Artist{
		ID:        res.ID,
		Platform:  "applemusic",
		Name:      attrs.Name,
		AvatarURL: formatArtworkURL(attrs.Artwork, 300),
		URL:       attrs.URL,
	}
}

// --- Helpers ---

func formatArtworkURL(artwork *appleMusicArtwork, size int) string {
	if artwork == nil || artwork.URL == "" {
		return ""
	}
	u := artwork.URL
	u = strings.Replace(u, "{w}", strconv.Itoa(size), 1)
	u = strings.Replace(u, "{h}", strconv.Itoa(size), 1)
	return u
}

func descriptionText(desc *appleMusicDescription) string {
	if desc == nil {
		return ""
	}
	if desc.Standard != "" {
		return desc.Standard
	}
	return desc.Short
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// --- TTML to LRC ---

// ttmlDoc represents a minimal TTML document structure.
type ttmlDoc struct {
	XMLName xml.Name  `xml:"tt"`
	Body    *ttmlBody `xml:"body"`
}

type ttmlBody struct {
	Divs []ttmlDiv `xml:"div"`
}

type ttmlDiv struct {
	Paragraphs []ttmlP `xml:"p"`
}

type ttmlP struct {
	Begin string     `xml:"begin,attr"`
	End   string     `xml:"end,attr"`
	Text  string     `xml:",chardata"`
	Spans []ttmlSpan `xml:"span"`
}

type ttmlSpan struct {
	Text string `xml:",chardata"`
}

func parseTTMLToLRC(ttml string) string {
	var doc ttmlDoc
	if err := xml.Unmarshal([]byte(ttml), &doc); err != nil {
		// If parsing fails, return raw TTML as plain text fallback.
		return ttml
	}

	if doc.Body == nil {
		return ""
	}

	var lines []string
	for _, div := range doc.Body.Divs {
		for _, p := range div.Paragraphs {
			text := p.Text
			if text == "" {
				var parts []string
				for _, span := range p.Spans {
					if t := strings.TrimSpace(span.Text); t != "" {
						parts = append(parts, t)
					}
				}
				text = strings.Join(parts, "")
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}

			if p.Begin != "" {
				millis := parseTimeToMillis(p.Begin)
				min := millis / 60000
				sec := (millis % 60000) / 1000
				cs := (millis % 1000) / 10
				lines = append(lines, fmt.Sprintf("[%02d:%02d.%02d]%s", min, sec, cs, text))
			} else {
				lines = append(lines, text)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func parseTimeToMillis(timeStr string) int64 {
	timeStr = strings.TrimSpace(timeStr)
	if timeStr == "" {
		return 0
	}
	parts := strings.Split(timeStr, ":")
	var hours, minutes int64
	var secStr string

	switch len(parts) {
	case 3: // HH:MM:SS.mmm
		hours, _ = strconv.ParseInt(parts[0], 10, 64)
		minutes, _ = strconv.ParseInt(parts[1], 10, 64)
		secStr = parts[2]
	case 2: // MM:SS.mmm or M:SS.mmm
		minutes, _ = strconv.ParseInt(parts[0], 10, 64)
		secStr = parts[1]
	case 1: // SS.mmm (plain seconds, common in Apple Music TTML)
		secStr = parts[0]
	default:
		return 0
	}

	secParts := strings.Split(secStr, ".")
	seconds, _ := strconv.ParseInt(secParts[0], 10, 64)
	var millis int64
	if len(secParts) > 1 {
		ms := secParts[1]
		// Normalize to 3 digits.
		for len(ms) < 3 {
			ms += "0"
		}
		if len(ms) > 3 {
			ms = ms[:3]
		}
		millis, _ = strconv.ParseInt(ms, 10, 64)
	}
	return (hours*3600+minutes*60+seconds)*1000 + millis
}
