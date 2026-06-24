package spotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const (
	spotifyTokenURL = "https://accounts.spotify.com/api/token"
	spotifyAPIBase  = "https://api.spotify.com/v1"
)

// Client talks to the Spotify Web API using the Client Credentials flow (no user
// login needed — sufficient for search + metadata). It is safe for concurrent
// use; the cached app token is refreshed under a mutex.
type Client struct {
	httpClient   *http.Client
	clientID     string
	clientSecret string
	market       string
	logger       bot.Logger

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// NewClient builds a Spotify client. market is an ISO 3166-1 alpha-2 code used
// to scope availability (defaults to "US" when empty).
func NewClient(clientID, clientSecret, market string, timeout time.Duration, logger bot.Logger) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	market = strings.TrimSpace(market)
	if market == "" {
		market = "US"
	}
	return &Client{
		httpClient:   &http.Client{Timeout: timeout},
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
		market:       market,
		logger:       logger,
	}
}

// Configured reports whether credentials are present.
func (c *Client) Configured() bool {
	return c != nil && c.clientID != "" && c.clientSecret != ""
}

// SetAPIProxy routes API calls through the configured platform proxy.
func (c *Client) SetAPIProxy(cfg httpproxy.Config) error {
	if c == nil {
		return nil
	}
	timeout := 15 * time.Second
	if c.httpClient != nil && c.httpClient.Timeout > 0 {
		timeout = c.httpClient.Timeout
	}
	proxied, err := httpproxy.NewHTTPClient(cfg, timeout)
	if err != nil {
		return err
	}
	if proxied == nil {
		c.httpClient = &http.Client{Timeout: timeout}
		return nil
	}
	c.httpClient = proxied
	return nil
}

// accessToken returns a valid app token, fetching/refreshing it as needed.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	if !c.Configured() {
		return "", platform.NewAuthRequiredError(platformName)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return c.token, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, spotifyTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	basic := base64.StdEncoding.EncodeToString([]byte(c.clientID + ":" + c.clientSecret))
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusBadRequest {
			return "", platform.NewAuthRequiredError(platformName)
		}
		return "", fmt.Errorf("spotify: token status %d", resp.StatusCode)
	}
	var tok tokenResponse
	if err := json.Unmarshal(data, &tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", platform.NewAuthRequiredError(platformName)
	}
	c.token = tok.AccessToken
	// Refresh a minute early to avoid edge-of-expiry failures.
	ttl := tok.ExpiresIn
	if ttl <= 0 {
		ttl = 3600
	}
	c.tokenExpiry = time.Now().Add(time.Duration(ttl-60) * time.Second)
	return c.token, nil
}

// apiGet performs an authenticated GET against the Spotify API and decodes JSON
// into out. It maps common HTTP errors to the platform sentinel errors.
func (c *Client) apiGet(ctx context.Context, path string, query url.Values, out any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	full := spotifyAPIBase + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return platform.ErrNotFound
	case http.StatusTooManyRequests:
		return platform.ErrRateLimited
	case http.StatusUnauthorized:
		// Token may have been revoked; drop it so the next call refreshes.
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return platform.NewAuthRequiredError(platformName)
	default:
		return fmt.Errorf("spotify: GET %s status %d", path, resp.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return err
		}
	}
	return nil
}

// Search returns up to limit tracks matching query.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("type", "track")
	q.Set("limit", strconv.Itoa(limit))
	q.Set("market", c.market)
	var resp spotifySearchResponse
	if err := c.apiGet(ctx, "/search", q, &resp); err != nil {
		return nil, err
	}
	tracks := make([]platform.Track, 0, len(resp.Tracks.Items))
	for _, it := range resp.Tracks.Items {
		if strings.TrimSpace(it.ID) == "" {
			continue
		}
		tracks = append(tracks, convertTrack(it))
	}
	return tracks, nil
}

// GetTrack fetches a single track's full metadata (including ISRC).
func (c *Client) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	q := url.Values{}
	q.Set("market", c.market)
	var t spotifyTrack
	if err := c.apiGet(ctx, "/tracks/"+url.PathEscape(trackID), q, &t); err != nil {
		return nil, err
	}
	if strings.TrimSpace(t.ID) == "" {
		return nil, platform.ErrNotFound
	}
	track := convertTrack(t)
	return &track, nil
}

// GetAlbum fetches album metadata and its track list.
func (c *Client) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	q := url.Values{}
	q.Set("market", c.market)
	var a spotifyAlbum
	if err := c.apiGet(ctx, "/albums/"+url.PathEscape(albumID), q, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.ID) == "" {
		return nil, platform.ErrNotFound
	}
	album := convertAlbum(a)
	return &album, nil
}

// GetPlaylist fetches playlist metadata and its tracks.
func (c *Client) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	q := url.Values{}
	q.Set("market", c.market)
	var p spotifyPlaylist
	if err := c.apiGet(ctx, "/playlists/"+url.PathEscape(playlistID), q, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, platform.ErrNotFound
	}
	pl := platform.Playlist{
		ID:          p.ID,
		Platform:    platformName,
		Title:       p.Name,
		Description: p.Description,
		CoverURL:    firstImage(p.Images),
		Creator:     p.Owner.DisplayName,
		TrackCount:  p.Tracks.Total,
		URL:         p.ExternalURLs["spotify"],
	}
	for _, item := range p.Tracks.Items {
		if strings.TrimSpace(item.Track.ID) == "" {
			continue
		}
		pl.Tracks = append(pl.Tracks, convertTrack(item.Track))
	}
	return &pl, nil
}

// GetArtist fetches basic artist info.
func (c *Client) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	var a spotifyArtist
	if err := c.apiGet(ctx, "/artists/"+url.PathEscape(artistID), nil, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.ID) == "" {
		return nil, platform.ErrNotFound
	}
	return &platform.Artist{
		ID:        a.ID,
		Platform:  platformName,
		Name:      a.Name,
		AvatarURL: firstImage(a.Images),
		URL:       a.ExternalURLs["spotify"],
	}, nil
}
