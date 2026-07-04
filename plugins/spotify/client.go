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
	"github.com/liuran001/MusicBot-Go/plugins/spotify/native"
)

const (
	spotifyTokenURL          = "https://accounts.spotify.com/api/token"
	spotifyAPIBase           = "https://api.spotify.com/v1"
	spotifyPathfinderURL     = "https://api-partner.spotify.com/pathfinder/v2/query"
	getTrackOperation        = "getTrack"
	getTrackPersistedHash    = "612585ae06ba435ad26369870deaae23b5c8800a256cd8a57e08eddc25a37294"
	findTracksOperation      = "findTracks"
	findTracksPersistHash    = "755858df4daab8d212980b02a81dcf8c9a58447de318b59d07c4651a1d0450b9"
	queryAlbumOperation      = "queryAlbum"
	queryAlbumPersistHash    = "ce390dbf7ca6b61a23aec210619e1094fe9d23d7f101ff773ce1146f84d4dd10"
	queryPlaylistOperation   = "queryPlaylist"
	queryPlaylistPersistHash = "908a5597b4d0af0489a9ad6a2d41bc3b416ff47c0884016d92bbd6822d0eb6d8"
	queryArtistOperation     = "queryArtist"
	queryArtistPersistHash   = "3d5c331d43374c565bbccc51325785054226e7535167c0e18ce0932dc9c79021"
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
	webAuth      func(context.Context) (native.WebAuth, error)

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

// WithWebAuthProvider enables web-player token fallback for direct track
// metadata. It is used when no Spotify developer client credentials are set.
func (c *Client) WithWebAuthProvider(provider func(context.Context) (native.WebAuth, error)) *Client {
	if c != nil {
		c.webAuth = provider
	}
	return c
}

func (c *Client) hasClientCredentials() bool {
	return c != nil && c.clientID != "" && c.clientSecret != ""
}

// Configured reports whether credentials are present.
func (c *Client) Configured() bool {
	return c != nil && (c.hasClientCredentials() || c.webAuth != nil)
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
	if !c.hasClientCredentials() {
		auth, err := c.webAuth(ctx)
		if err != nil || auth.Bearer == "" {
			return "", platform.NewAuthRequiredError(platformName)
		}
		return auth.Bearer, nil
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
	if !c.hasClientCredentials() && c.webAuth != nil {
		return c.searchPathfinder(ctx, query, limit)
	}

	q := url.Values{}
	q.Set("q", query)
	q.Set("type", "track")
	q.Set("limit", strconv.Itoa(limit))
	q.Set("market", c.market)
	var resp spotifySearchResponse
	if err := c.apiGet(ctx, "/search", q, &resp); err != nil {
		if c.webAuth != nil {
			if tracks, fallbackErr := c.searchPathfinder(ctx, query, limit); fallbackErr == nil {
				return tracks, nil
			}
		}
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

func (c *Client) pathfinderQuery(ctx context.Context, operation, hash string, variables map[string]any, out any) error {
	if c.webAuth == nil {
		return platform.NewAuthRequiredError(platformName)
	}
	auth, err := c.webAuth(ctx)
	if err != nil || auth.Bearer == "" {
		return platform.NewAuthRequiredError(platformName)
	}
	payload := map[string]any{
		"variables":     variables,
		"operationName": operation,
		"extensions": map[string]any{
			"persistedQuery": map[string]any{
				"version":    1,
				"sha256Hash": hash,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, spotifyPathfinderURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	native.ApplyWebAuthHeaders(req, auth)
	req.Header.Set("Content-Type", "application/json")

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
	case http.StatusUnauthorized, http.StatusForbidden:
		return platform.NewAuthRequiredError(platformName)
	default:
		return fmt.Errorf("spotify: pathfinder %s status %d", operation, resp.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) searchPathfinder(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	var result pathfinderSearchResponse
	err := c.pathfinderQuery(ctx, findTracksOperation, findTracksPersistHash, map[string]any{
		"query":  strings.TrimSpace(query),
		"limit":  limit,
		"offset": 0,
	}, &result)
	if err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("spotify: pathfinder findTracks returned errors")
	}

	items := result.Data.SearchV2.TracksV2.Items
	tracks := make([]platform.Track, 0, len(items))
	for _, item := range items {
		t := item.Item.Data
		if pathfinderID(t.ID, t.URI) == "" {
			continue
		}
		tracks = append(tracks, convertPathfinderTrack(t))
	}
	return tracks, nil
}

// GetTrack fetches a single track's metadata. Developer credentials use the
// Web API; otherwise the authenticated web-player Pathfinder query is used.
func (c *Client) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	if !c.hasClientCredentials() && c.webAuth != nil {
		return c.getTrackPathfinder(ctx, trackID)
	}

	q := url.Values{}
	q.Set("market", c.market)
	var t spotifyTrack
	if err := c.apiGet(ctx, "/tracks/"+url.PathEscape(trackID), q, &t); err != nil {
		if c.webAuth != nil {
			if track, fallbackErr := c.getTrackPathfinder(ctx, trackID); fallbackErr == nil {
				return track, nil
			}
		}
		return nil, err
	}
	if strings.TrimSpace(t.ID) == "" {
		return nil, platform.ErrNotFound
	}
	track := convertTrack(t)
	return &track, nil
}

func (c *Client) getTrackPathfinder(ctx context.Context, trackID string) (*platform.Track, error) {
	var result pathfinderTrackResponse
	if err := c.pathfinderQuery(ctx, getTrackOperation, getTrackPersistedHash, map[string]any{"uri": "spotify:track:" + trackID}, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("spotify: pathfinder getTrack returned errors")
	}
	if pathfinderID(result.Data.Track.ID, result.Data.Track.URI) == "" {
		return nil, platform.ErrNotFound
	}
	track := convertPathfinderTrack(result.Data.Track)
	return &track, nil
}

// GetAlbum fetches album metadata and its track list.
func (c *Client) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	if !c.hasClientCredentials() && c.webAuth != nil {
		return c.getAlbumPathfinder(ctx, albumID)
	}

	q := url.Values{}
	q.Set("market", c.market)
	var a spotifyAlbum
	if err := c.apiGet(ctx, "/albums/"+url.PathEscape(albumID), q, &a); err != nil {
		if c.webAuth != nil {
			if album, fallbackErr := c.getAlbumPathfinder(ctx, albumID); fallbackErr == nil {
				return album, nil
			}
		}
		return nil, err
	}
	if strings.TrimSpace(a.ID) == "" {
		return nil, platform.ErrNotFound
	}
	album := convertAlbum(a)
	return &album, nil
}

func (c *Client) getAlbumPathfinder(ctx context.Context, albumID string) (*platform.Album, error) {
	var result pathfinderAlbumResponse
	if err := c.pathfinderQuery(ctx, queryAlbumOperation, queryAlbumPersistHash, map[string]any{
		"uri":    "spotify:album:" + albumID,
		"offset": 0,
	}, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("spotify: pathfinder queryAlbum returned errors")
	}
	if result.Data.Album.TypeName == "NotFound" {
		return nil, platform.ErrNotFound
	}
	album := convertPathfinderAlbum(result.Data.Album)
	if strings.TrimSpace(album.ID) == "" {
		return nil, platform.ErrNotFound
	}
	return &album, nil
}

// GetAlbumAsPlaylist fetches an album and returns it as a Playlist with its
// track list populated. Used when an album link is browsed through the
// collection (playlist) UI, where platform.Album (which has no Tracks field)
// would otherwise lose the track list.
func (c *Client) GetAlbumAsPlaylist(ctx context.Context, albumID string) (*platform.Playlist, error) {
	if !c.hasClientCredentials() && c.webAuth != nil {
		return c.getAlbumAsPlaylistPathfinder(ctx, albumID)
	}

	q := url.Values{}
	q.Set("market", c.market)
	var a spotifyAlbum
	if err := c.apiGet(ctx, "/albums/"+url.PathEscape(albumID), q, &a); err != nil {
		if c.webAuth != nil {
			if playlist, fallbackErr := c.getAlbumAsPlaylistPathfinder(ctx, albumID); fallbackErr == nil {
				return playlist, nil
			}
		}
		return nil, err
	}
	if strings.TrimSpace(a.ID) == "" {
		return nil, platform.ErrNotFound
	}
	creator := ""
	if len(a.Artists) > 0 {
		creator = a.Artists[0].Name
	}
	pl := &platform.Playlist{
		ID:         "album:" + a.ID,
		Platform:   platformName,
		Title:      a.Name,
		CoverURL:   firstImage(a.Images),
		Creator:    creator,
		TrackCount: a.TotalTracks,
		URL:        a.ExternalURLs["spotify"],
	}
	cover := firstImage(a.Images)
	for _, t := range a.Tracks.Items {
		if strings.TrimSpace(t.ID) == "" {
			continue
		}
		track := convertTrack(t)
		// Album-tracks endpoint omits per-track album art; backfill from the album.
		if track.CoverURL == "" {
			track.CoverURL = cover
		}
		pl.Tracks = append(pl.Tracks, track)
	}
	return pl, nil
}

func (c *Client) getAlbumAsPlaylistPathfinder(ctx context.Context, albumID string) (*platform.Playlist, error) {
	var result pathfinderAlbumResponse
	if err := c.pathfinderQuery(ctx, queryAlbumOperation, queryAlbumPersistHash, map[string]any{
		"uri":    "spotify:album:" + albumID,
		"offset": 0,
	}, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("spotify: pathfinder queryAlbum returned errors")
	}
	if result.Data.Album.TypeName == "NotFound" {
		return nil, platform.ErrNotFound
	}
	pl := convertPathfinderAlbumAsPlaylist(result.Data.Album)
	if pl == nil || strings.TrimSpace(pl.ID) == "album:" {
		return nil, platform.ErrNotFound
	}
	return pl, nil
}

// GetPlaylist fetches playlist metadata and its tracks.
func (c *Client) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	if !c.hasClientCredentials() && c.webAuth != nil {
		return c.getPlaylistPathfinder(ctx, playlistID)
	}

	q := url.Values{}
	q.Set("market", c.market)
	var p spotifyPlaylist
	if err := c.apiGet(ctx, "/playlists/"+url.PathEscape(playlistID), q, &p); err != nil {
		if c.webAuth != nil {
			if playlist, fallbackErr := c.getPlaylistPathfinder(ctx, playlistID); fallbackErr == nil {
				return playlist, nil
			}
		}
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

func (c *Client) getPlaylistPathfinder(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	var result pathfinderPlaylistResponse
	if err := c.pathfinderQuery(ctx, queryPlaylistOperation, queryPlaylistPersistHash, map[string]any{
		"uri":    "spotify:playlist:" + playlistID,
		"limit":  100,
		"offset": 0,
	}, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("spotify: pathfinder queryPlaylist returned errors")
	}
	switch result.Data.Playlist.TypeName {
	case "NotFound":
		return nil, platform.ErrNotFound
	case "GenericError":
		return nil, platform.ErrUnavailable
	}
	pl := convertPathfinderPlaylist(result.Data.Playlist)
	if pl == nil || strings.TrimSpace(pl.ID) == "" {
		return nil, platform.ErrNotFound
	}
	return pl, nil
}

// GetArtist fetches basic artist info.
func (c *Client) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	if !c.hasClientCredentials() && c.webAuth != nil {
		return c.getArtistPathfinder(ctx, artistID)
	}

	var a spotifyArtist
	if err := c.apiGet(ctx, "/artists/"+url.PathEscape(artistID), nil, &a); err != nil {
		if c.webAuth != nil {
			if artist, fallbackErr := c.getArtistPathfinder(ctx, artistID); fallbackErr == nil {
				return artist, nil
			}
		}
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

func (c *Client) getArtistPathfinder(ctx context.Context, artistID string) (*platform.Artist, error) {
	var result pathfinderArtistResponse
	if err := c.pathfinderQuery(ctx, queryArtistOperation, queryArtistPersistHash, map[string]any{"uri": "spotify:artist:" + artistID}, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("spotify: pathfinder queryArtist returned errors")
	}
	if result.Data.Artist.TypeName == "NotFound" {
		return nil, platform.ErrNotFound
	}
	artist := convertPathfinderArtistUnion(result.Data.Artist)
	if strings.TrimSpace(artist.ID) == "" {
		return nil, platform.ErrNotFound
	}
	return &artist, nil
}
