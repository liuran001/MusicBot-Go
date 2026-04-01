package kugou

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	kugoulib "github.com/guohuiyuan/music-lib/kugou"
	"github.com/guohuiyuan/music-lib/model"
	"github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const (
	kugouGatewaySongInfoURL = "https://gateway.kugou.com/v3/album_audio/audio"
	kugouGatewayPlayURL     = "https://gateway.kugou.com/v5/url"
	kugouPlayDataURL        = "https://wwwapi.kugou.com/yy/index.php"
	kugouPlaylistInfoV2URL  = "https://mobiles.kugou.com/api/v5/special/info_v2"
	kugouPlaylistSongV2URL  = "https://mobiles.kugou.com/api/v5/special/song_v2"
	kugouPlaylistDecodeURL  = "https://t.kugou.com/v1/songlist/batch_decode"
	kugouPlaylistLegacyURL  = "https://pubsongscdn.kugou.com/v2/get_other_list_file"
	kugouPlaylistSpecialURL = "http://mobilecdnbj.kugou.com/api/v5/special/info"
	kugouGatewayAppID       = "1005"
	kugouGatewayClientVer   = "11451"
	kugouPlayClientVer      = "20349"
	kugouGatewayMid         = "211008"
	kugouGatewaySignKey     = "OIlwieks28dk2k092lksi2UIkp"
	kugouPlaySignKey        = "NVPh5oo715z5DIWAeQlhMDsWXXQV4hwt"
	kugouPlayPidVerSec      = "57ae12eb6890223e355ccfcb74edf70d"
	kugouPlaylistWebAppID   = "1058"
	kugouPlaylistSrcAppID   = "2919"
	kugouPlaylistClientVer  = "20000"
	kugouPlaylistAndroidCV  = "20109"
	kugouPlaylistAndroidUA  = "Mozilla/5.0 (Linux; Android 10; HUAWEI HMA-AL00) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/83.0.4103.106 Mobile Safari/537.36"
	kugouPlaylistWebUA      = "Mozilla/5.0 (iPhone; CPU iPhone OS 11_0 like Mac OS X) AppleWebKit/604.1.38 (KHTML, like Gecko) Version/11.0 Mobile/15A372 Safari/604.1"
	kugouPlaylistReferer    = "https://m3ws.kugou.com/share/index.php"
)

type Client struct {
	api              *kugoulib.Kugou
	cookie           string
	logger           bot.Logger
	concept          *ConceptSessionManager
	apiHTTPClient    *http.Client
	searchHTTPClient *http.Client
}

func (c *Client) HasCookie() bool {
	return c != nil && (strings.TrimSpace(c.baseCookie()) != "" || strings.TrimSpace(c.conceptCookie()) != "")
}

func (c *Client) HasVIPDownloadCookie() bool {
	if c == nil {
		return false
	}
	cookie := c.baseCookie()
	return parseCookieValue(cookie, "t") != "" && parseCookieValue(cookie, "KugooID") != ""
}

func NewClient(cookie string, logger bot.Logger) *Client {
	trimmed := strings.TrimSpace(cookie)
	return &Client{
		api:    kugoulib.New(trimmed),
		cookie: trimmed,
		logger: logger,
	}
}

func (c *Client) SetSearchProxy(rawProxy string) error {
	if c == nil {
		return nil
	}
	rawProxy = strings.TrimSpace(rawProxy)
	if rawProxy == "" {
		c.searchHTTPClient = nil
		return nil
	}
	proxyURL, err := url.Parse(rawProxy)
	if err != nil {
		return fmt.Errorf("invalid kugou search proxy: %w", err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	c.searchHTTPClient = &http.Client{Transport: transport, Timeout: 20 * time.Second}
	return nil
}

func (c *Client) SetAPIProxy(cfg httpproxy.Config) error {
	if c == nil {
		return nil
	}
	client, err := httpproxy.NewHTTPClient(cfg, defaultKugouProxyTimeout)
	if err != nil {
		return err
	}
	c.apiHTTPClient = client
	return nil
}

func (c *Client) AttachConcept(manager *ConceptSessionManager) {
	if c == nil {
		return
	}
	c.concept = manager
}

func (c *Client) Concept() *ConceptSessionManager {
	if c == nil {
		return nil
	}
	return c.concept
}

func (c *Client) effectiveCookie() string {
	if c == nil {
		return ""
	}
	if cookie := c.baseCookie(); cookie != "" {
		return cookie
	}
	return c.conceptCookie()
}

func (c *Client) baseCookie() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.cookie)
}

func (c *Client) conceptCookie() string {
	if c == nil || c.concept == nil {
		return ""
	}
	return strings.TrimSpace(c.concept.CookieString())
}

func (c *Client) Search(ctx context.Context, keyword string, limit int) ([]model.Song, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, platform.NewNotFoundError("kugou", "search", "")
	}
	songs, err := c.searchSongs(ctx, keyword, limit)
	if err == nil && len(songs) > 0 {
		return songs, nil
	}
	fallbackSongs, fallbackErr := c.api.Search(keyword)
	if fallbackErr != nil {
		if err != nil {
			return nil, wrapError("kugou", "search", "", err)
		}
		return nil, wrapError("kugou", "search", "", fallbackErr)
	}
	if limit > 0 && len(fallbackSongs) > limit {
		fallbackSongs = fallbackSongs[:limit]
	}
	return fallbackSongs, nil
}

func (c *Client) GetTrack(ctx context.Context, trackID string) (*model.Song, error) {
	if chain, ok := decodeShareTrackID(trackID); ok {
		return c.fetchTrackByShareChain(ctx, chain)
	}
	hash := normalizeHash(trackID)
	if hash == "" {
		return nil, platform.NewNotFoundError("kugou", "track", trackID)
	}
	if song, err := c.fetchGatewayTrackInfo(ctx, hash); err == nil && song != nil {
		return song, nil
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
	song, err := c.GetTrack(ctx, trackID)
	if err != nil {
		return "", err
	}
	lyrics, err := c.api.GetLyrics(song)
	if err != nil {
		return "", wrapError("kugou", "lyrics", strings.TrimSpace(song.ID), err)
	}
	if strings.TrimSpace(lyrics) == "" {
		return "", platform.NewUnavailableError("kugou", "lyrics", strings.TrimSpace(song.ID))
	}
	return lyrics, nil
}

func (c *Client) GetDownloadInfo(ctx context.Context, trackID string) (*model.Song, error) {
	requested := platform.QualityHigh
	song, err := c.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if song == nil {
		return nil, platform.NewNotFoundError("kugou", "track", trackID)
	}
	resolved, err := c.ResolveDownloadByQuality(ctx, song, requested)
	if err == nil && resolved != nil && strings.TrimSpace(resolved.URL) != "" {
		return resolved, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, platform.NewUnavailableError("kugou", "track", normalizeHash(trackID))
}

func (c *Client) ResolveDownloadByQuality(ctx context.Context, song *model.Song, requested platform.Quality) (*model.Song, error) {
	if song == nil {
		return nil, platform.NewNotFoundError("kugou", "track", "")
	}
	plans := buildDownloadPlans(song, requested)
	var lastErr error
	if c.concept == nil || !c.concept.HasUsableSession() {
		return nil, platform.NewAuthRequiredError("kugou")
	}
	for _, plan := range plans {
		resolved, err := c.fetchConceptSongURL(ctx, song, plan)
		if err == nil && resolved != nil && strings.TrimSpace(resolved.URL) != "" {
			if c != nil && c.logger != nil {
				c.logger.Debug("kugou: download resolved via concept old", "track_id", strings.TrimSpace(song.ID), "requested", requested.String(), "resolved_quality", plan.Quality.String(), "hash", plan.Hash, "url", resolved.URL)
			}
			return resolved, nil
		}
		if err != nil {
			lastErr = preferKugouDownloadError(lastErr, wrapError("kugou", "track", strings.TrimSpace(song.ID), err))
		}
		if newResp, newErr := c.concept.FetchSongURLNew(ctx, song, plan); newErr != nil {
			lastErr = preferKugouDownloadError(lastErr, wrapError("kugou", "track", strings.TrimSpace(song.ID), newErr))
		} else if resolvedNew, ok := c.resolveConceptSongURLNew(song, plan, newResp); ok {
			if c != nil && c.logger != nil {
				c.logger.Debug("kugou: download resolved via concept new", "track_id", strings.TrimSpace(song.ID), "requested", requested.String(), "resolved_quality", plan.Quality.String(), "hash", plan.Hash, "url", resolvedNew.URL)
			}
			return resolvedNew, nil
		} else if authErr := conceptSongURLNewAuthError(newResp); authErr != nil {
			lastErr = preferKugouDownloadError(lastErr, authErr)
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, platform.NewUnavailableError("kugou", "track", strings.TrimSpace(song.ID))
}

func (c *Client) GetPlaylist(ctx context.Context, playlistID string) (*model.Playlist, []model.Song, error) {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return nil, nil, platform.NewNotFoundError("kugou", "playlist", "")
	}
	kind, rawID := parseCollectionID(playlistID)
	switch kind {
	case "album":
		return c.GetAlbumPlaylist(ctx, rawID)
	case "playlist_url":
		return c.parsePlaylistFromURL(ctx, rawID)
	}
	return c.parsePlaylistByID(ctx, rawID)
}

func (c *Client) GetAlbumPlaylist(ctx context.Context, albumID string) (*model.Playlist, []model.Song, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return nil, nil, platform.NewNotFoundError("kugou", "album", "")
	}
	meta, err := c.fetchAlbumInfo(ctx, albumID)
	if err != nil {
		return nil, nil, err
	}
	songs, total, err := c.fetchAlbumSongs(ctx, albumID)
	if err != nil {
		return nil, nil, err
	}
	playlist := &model.Playlist{
		ID:          albumID,
		Name:        strings.TrimSpace(meta.Data.AlbumName),
		Cover:       strings.TrimSpace(meta.Data.ImgURL),
		TrackCount:  total,
		Creator:     strings.TrimSpace(meta.Data.Author),
		Description: strings.TrimSpace(meta.Data.Intro),
		Source:      "kugou",
		Link:        buildAlbumURL(albumID),
		Extra: map[string]string{
			"collection_type": "album",
			"album_id":        albumID,
			"publish_time":    strings.TrimSpace(meta.Data.Publish),
		},
	}
	if playlist.Name == "" && len(songs) > 0 {
		playlist.Name = strings.TrimSpace(songs[0].Album)
	}
	for i := range songs {
		if strings.TrimSpace(songs[i].Album) == "" {
			songs[i].Album = playlist.Name
		}
		if strings.TrimSpace(songs[i].AlbumID) == "" {
			songs[i].AlbumID = albumID
		}
		if strings.TrimSpace(songs[i].Cover) == "" {
			songs[i].Cover = playlist.Cover
		}
	}
	if playlist.TrackCount <= 0 {
		playlist.TrackCount = len(songs)
	}
	return playlist, songs, nil
}

func (c *Client) parsePlaylistFromURL(ctx context.Context, rawURL string) (*model.Playlist, []model.Song, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil, platform.NewNotFoundError("kugou", "playlist", "")
	}
	if collectionID, ok := NewURLMatcher().MatchPlaylistURL(rawURL); ok {
		kind, id := parseCollectionID(collectionID)
		switch kind {
		case "album":
			return c.GetAlbumPlaylist(ctx, id)
		case "playlist":
			return c.parsePlaylistByID(ctx, id)
		}
	}
	if resolvedID, err := c.resolvePlaylistCollectionID(ctx, rawURL); err == nil && strings.TrimSpace(resolvedID) != "" {
		kind, id := parseCollectionID(resolvedID)
		switch kind {
		case "album":
			return c.GetAlbumPlaylist(ctx, id)
		case "playlist":
			return c.parsePlaylistByID(ctx, id)
		}
	}
	return c.parsePlaylistRaw(rawURL)
}

func (c *Client) parsePlaylistByID(ctx context.Context, playlistID string) (*model.Playlist, []model.Song, error) {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return nil, nil, platform.NewNotFoundError("kugou", "playlist", "")
	}
	if c != nil && c.logger != nil {
		c.logger.Debug("kugou: parse playlist by id", "playlist_id", playlistID)
	}
	identity := kugouPlaylistIdentity{InputID: playlistID}
	if isGlobalCollectionID(playlistID) {
		identity.GlobalCollectionID = playlistID
		identity.ResolvePath = "input:global_collection_id"
		if c != nil && c.logger != nil {
			c.logger.Debug("kugou: playlist resolved as global collection", "playlist_id", playlistID, "global_collection_id", identity.GlobalCollectionID)
		}
		return c.fetchGlobalCollectionPlaylist(ctx, identity)
	}
	if strings.HasPrefix(strings.ToLower(playlistID), "gcid_") {
		if decodedIdentity, err := c.decodePlaylistGCID(ctx, playlistID); err == nil && strings.TrimSpace(decodedIdentity.GlobalCollectionID) != "" {
			identity.GlobalCollectionID = strings.TrimSpace(decodedIdentity.GlobalCollectionID)
			identity.SpecialID = firstNonEmpty(identity.SpecialID, strings.TrimSpace(decodedIdentity.SpecialID))
			identity.GlobalSpecialID = firstNonEmpty(strings.TrimSpace(decodedIdentity.GlobalSpecialID), strings.TrimSpace(decodedIdentity.GlobalCollectionID))
			identity.ResolvePath = "gcid->global_collection_id"
			if c != nil && c.logger != nil {
				c.logger.Debug("kugou: playlist gcid decoded", "playlist_id", playlistID, "global_collection_id", identity.GlobalCollectionID, "global_specialid", identity.GlobalSpecialID, "specialid", identity.SpecialID, "resolve_path", identity.ResolvePath)
			}
			return c.fetchGlobalCollectionPlaylist(ctx, identity)
		} else {
			if c != nil && c.logger != nil {
				c.logger.Warn("kugou: playlist gcid decode failed, trying resolve fallback", "playlist_id", playlistID, "err", err)
			}
			if resolvedID, resolveErr := c.resolvePlaylistCollectionID(ctx, buildPlaylistLink(playlistID)); resolveErr == nil && strings.TrimSpace(resolvedID) != "" && !strings.EqualFold(strings.TrimSpace(resolvedID), strings.TrimSpace(playlistID)) {
				if c != nil && c.logger != nil {
					c.logger.Debug("kugou: playlist gcid resolved via URL fallback", "playlist_id", playlistID, "resolved_id", resolvedID)
				}
				return c.parsePlaylistByID(ctx, resolvedID)
			} else if c != nil && c.logger != nil {
				c.logger.Warn("kugou: playlist gcid resolve fallback failed", "playlist_id", playlistID, "resolved_id", resolvedID, "err", resolveErr)
			}
		}
	}
	if isNumericText(playlistID) {
		if globalID, err := c.resolveGlobalSpecialID(ctx, playlistID); err == nil && globalID != "" {
			identity.SpecialID = playlistID
			identity.GlobalSpecialID = globalID
			identity.ResolvePath = "specialid->global_specialid"
			if c != nil && c.logger != nil {
				c.logger.Debug("kugou: numeric playlist resolved to global specialid", "playlist_id", playlistID, "global_specialid", identity.GlobalSpecialID)
			}
			if playlist, songs, err := c.fetchGlobalCollectionPlaylist(ctx, identity); err == nil && playlist != nil {
				return playlist, songs, nil
			}
		}
		if playlist, songs, err := c.fetchLegacySpecialPlaylist(ctx, playlistID); err == nil && playlist != nil {
			if c != nil && c.logger != nil {
				c.logger.Debug("kugou: numeric playlist fell back to legacy special", "playlist_id", playlistID, "song_count", len(songs))
			}
			return playlist, songs, nil
		}
	}
	if c != nil && c.logger != nil {
		c.logger.Debug("kugou: playlist falling back to music-lib ParsePlaylist", "playlist_id", playlistID)
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

func (c *Client) parsePlaylistRaw(rawURL string) (*model.Playlist, []model.Song, error) {
	playlist, songs, err := c.api.ParsePlaylist(rawURL)
	if err != nil {
		return nil, nil, wrapError("kugou", "playlist", rawURL, err)
	}
	if playlist == nil {
		return nil, nil, platform.NewNotFoundError("kugou", "playlist", rawURL)
	}
	if playlist.Source == "" {
		playlist.Source = "kugou"
	}
	if strings.TrimSpace(playlist.ID) == "" {
		playlist.ID = rawURL
	}
	if strings.TrimSpace(playlist.Link) == "" {
		playlist.Link = rawURL
	}
	if playlist.Extra == nil {
		playlist.Extra = map[string]string{}
	}
	playlist.Extra["collection_type"] = "playlist"
	playlist.Extra["source_url"] = strings.TrimSpace(rawURL)
	playlist.Extra["resolved_url"] = strings.TrimSpace(playlist.Link)
	applyPlaylistSongContext(playlist, songs)
	return playlist, songs, nil
}

func (c *Client) resolvePlaylistCollectionID(ctx context.Context, rawURL string) (string, error) {
	if collectionID, ok := NewURLMatcher().MatchPlaylistURL(rawURL); ok {
		kind, id := parseCollectionID(collectionID)
		if kind == "playlist" && (isGlobalCollectionID(id) || strings.HasPrefix(strings.ToLower(id), "gcid_") || isNumericText(id)) {
			return id, nil
		}
	}
	if parsed, err := url.Parse(rawURL); err == nil {
		query := parsed.Query()
		if globalID := strings.TrimSpace(firstNonEmpty(query.Get("global_collection_id"), query.Get("global_specialid"))); globalID != "" {
			return globalID, nil
		}
		for _, key := range []string{"gcid", "encode_gic", "encode_src_gid"} {
			if gcid := strings.TrimSpace(query.Get(key)); strings.HasPrefix(strings.ToLower(gcid), "gcid_") {
				return strings.ToLower(gcid), nil
			}
		}
		for _, key := range []string{"specialid", "specialId"} {
			if value := strings.TrimSpace(query.Get(key)); value != "" && isNumericText(value) {
				return value, nil
			}
		}
	}
	if resolvedID, err := c.resolvePlaylistCollectionIDFromRedirect(ctx, rawURL); err == nil && strings.TrimSpace(resolvedID) != "" {
		return resolvedID, nil
	}
	return c.resolvePlaylistCollectionIDFromHTML(ctx, rawURL)
}

func (c *Client) resolvePlaylistCollectionIDFromRedirect(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := c.htmlHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		if collectionID, ok := NewURLMatcher().MatchPlaylistURL(resp.Request.URL.String()); ok {
			kind, _ := parseCollectionID(collectionID)
			if kind != "playlist_url" {
				return collectionID, nil
			}
		}
	}
	return "", nil
}

func (c *Client) resolvePlaylistCollectionIDFromHTML(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := c.htmlHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	text := string(bodyBytes)
	if globalID := extractPlaylistGlobalCollectionID(text); globalID != "" {
		return globalID, nil
	}
	if gcid := extractPlaylistGCID(text); gcid != "" {
		return gcid, nil
	}
	if specialID := extractPlaylistSpecialID(text); specialID != "" {
		return specialID, nil
	}
	return "", nil
}

func (c *Client) CheckCookie(ctx context.Context) (bool, error) {
	_ = ctx
	if c.concept != nil && c.concept.HasUsableSession() {
		_, _, err := c.concept.FetchAccountStatus(ctx)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	if strings.TrimSpace(c.cookie) == "" {
		return false, nil
	}
	return c.api.IsVipAccount()
}

func (c *Client) ManualRenew(ctx context.Context) (string, error) {
	if c == nil || c.concept == nil || !c.concept.Enabled() {
		return "", fmt.Errorf("kugou concept session not enabled")
	}
	return c.concept.ManualRenew(ctx)
}

func (c *Client) fetchConceptSongURL(ctx context.Context, song *model.Song, plan kugouDownloadPlan) (*model.Song, error) {
	if c == nil || c.concept == nil || !c.concept.HasUsableSession() {
		return nil, fmt.Errorf("kugou concept session unavailable")
	}
	urlResp, err := c.concept.FetchSongURL(ctx, song, plan)
	if err != nil {
		return nil, err
	}
	if urlResp == nil || len(urlResp.URL) == 0 || strings.TrimSpace(urlResp.URL[0]) == "" {
		return nil, fmt.Errorf("kugou concept song url empty")
	}
	resolved := cloneSongWithHash(song, plan.Hash)
	if resolved == nil {
		return nil, fmt.Errorf("kugou concept clone song failed")
	}
	resolved.URL = strings.TrimSpace(urlResp.URL[0])
	applyPlanMetadata(resolved, plan)
	if urlResp.TimeLength > 0 {
		resolved.Duration = normalizeGatewayDuration(int(urlResp.TimeLength))
	}
	if strings.TrimSpace(urlResp.ExtName) != "" {
		resolved.Ext = strings.TrimSpace(urlResp.ExtName)
	}
	extra := ensureSongExtra(resolved)
	extra["play_url"] = resolved.URL
	extra["resolved_quality"] = plan.Quality.String()
	extra["concept_source"] = "song/url"
	return resolved, nil
}

func wrapError(source, resource, id string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "too frequent") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "errcode=1002"):
		return platform.NewRateLimitedError(source)
	case strings.Contains(msg, "lyrics not found") || strings.Contains(msg, "hash not found"):
		return platform.NewNotFoundError(source, resource, id)
	case strings.Contains(msg, "invalid kugou") || strings.Contains(msg, "invalid hash") || strings.Contains(msg, "invalid link"):
		return platform.NewNotFoundError(source, resource, id)
	case strings.Contains(msg, "content is empty") || strings.Contains(msg, "download url not found") || strings.Contains(msg, "unavailable"):
		return platform.NewUnavailableError(source, resource, id)
	case strings.Contains(msg, "cookie required") || strings.Contains(msg, "requires cookie") || strings.Contains(msg, "missing encode_album_audio_id") || strings.Contains(msg, "requires cookie t and kugooid"):
		return platform.NewAuthRequiredError(source)
	default:
		return fmt.Errorf("%s: %s %s: %w", source, resource, id, err)
	}
}

type kugouGatewaySongInfoResponse struct {
	Status int `json:"status"`
	Data   [][]struct {
		AlbumAudioID string `json:"album_audio_id"`
		AuthorName   string `json:"author_name"`
		OriAudioName string `json:"ori_audio_name"`
		AudioInfo    struct {
			AudioID      interface{} `json:"audio_id"`
			Hash         string      `json:"hash"`
			Hash128      string      `json:"hash_128"`
			Hash320      string      `json:"hash_320"`
			HashFlac     string      `json:"hash_flac"`
			HashHigh     string      `json:"hash_high"`
			HashSuper    string      `json:"hash_super"`
			Filesize     interface{} `json:"filesize"`
			Filesize128  interface{} `json:"filesize_128"`
			Filesize320  interface{} `json:"filesize_320"`
			FilesizeFlac interface{} `json:"filesize_flac"`
			FilesizeHigh interface{} `json:"filesize_high"`
			Timelength   interface{} `json:"timelength"`
			Bitrate      interface{} `json:"bitrate"`
			Extname      string      `json:"extname"`
			Privilege    interface{} `json:"privilege"`
		} `json:"audio_info"`
		AlbumInfo struct {
			AlbumID      string `json:"album_id"`
			AlbumName    string `json:"album_name"`
			SizableCover string `json:"sizable_cover"`
		} `json:"album_info"`
	} `json:"data"`
}

type kugouSearchResponse struct {
	Data struct {
		Lists []struct {
			SongName    string      `json:"SongName"`
			SingerName  string      `json:"SingerName"`
			SingerID    interface{} `json:"SingerId"`
			AlbumName   string      `json:"AlbumName"`
			AlbumID     string      `json:"AlbumID"`
			AudioID     interface{} `json:"Audioid"`
			MixSongID   interface{} `json:"MixSongID"`
			Duration    int         `json:"Duration"`
			FileHash    string      `json:"FileHash"`
			SQFileHash  string      `json:"SQFileHash"`
			HQFileHash  string      `json:"HQFileHash"`
			ResFileHash string      `json:"ResFileHash"`
			FileSize    interface{} `json:"FileSize"`
			SQFileSize  int64       `json:"SQFileSize"`
			HQFileSize  int64       `json:"HQFileSize"`
			ResFileSize int64       `json:"ResFileSize"`
			Image       string      `json:"Image"`
			Privilege   int         `json:"Privilege"`
			TransParam  struct {
				Ogg320Hash     string      `json:"ogg_320_hash"`
				Ogg128Hash     string      `json:"ogg_128_hash"`
				Ogg320FileSize int64       `json:"ogg_320_filesize"`
				Ogg128FileSize int64       `json:"ogg_128_filesize"`
				SingerID       interface{} `json:"singerid"`
			} `json:"trans_param"`
		} `json:"lists"`
	} `json:"data"`
}

type kugouPlayURLResponse struct {
	Status int      `json:"status"`
	URL    []string `json:"url"`
}

type kugouPlayDataResponse struct {
	Status       int         `json:"status"`
	ErrCode      int         `json:"err_code"`
	URL          string      `json:"play_url"`
	BackupURL    string      `json:"play_backup_url"`
	Bitrate      interface{} `json:"bitrate"`
	Timelength   interface{} `json:"timelength"`
	FileSize     interface{} `json:"filesize"`
	ExtName      string      `json:"extname"`
	AudioName    string      `json:"audio_name"`
	AuthorName   string      `json:"author_name"`
	Img          string      `json:"img"`
	AlbumID      string      `json:"album_id"`
	AlbumAudioID string      `json:"album_audio_id"`
	Hash         string      `json:"hash"`
}

type kugouMobilePlayInfoResponse struct {
	Status       int         `json:"status"`
	ErrCode      int         `json:"errcode"`
	Error        string      `json:"error"`
	URL          string      `json:"url"`
	BackupURL    interface{} `json:"backup_url"`
	Bitrate      interface{} `json:"bitRate"`
	Timelength   interface{} `json:"timeLength"`
	ExtName      string      `json:"extName"`
	FileName     string      `json:"fileName"`
	SongName     string      `json:"songName"`
	AuthorName   string      `json:"author_name"`
	AlbumID      interface{} `json:"albumid"`
	AlbumAudioID interface{} `json:"album_audio_id"`
	Privilege    interface{} `json:"privilege"`
	PayType      interface{} `json:"pay_type"`
}

type kugouDownloadPlan struct {
	Hash    string
	Quality platform.Quality
	Format  string
	Size    int64
}

type kugouAlbumInfoResponse struct {
	Data struct {
		SongCount int    `json:"songcount"`
		Intro     string `json:"intro"`
		ImgURL    string `json:"imgurl"`
		Publish   string `json:"publishtime"`
		AlbumName string `json:"albumname"`
		Author    string `json:"author_name"`
	} `json:"data"`
}

type kugouAlbumSongsResponse struct {
	Data struct {
		Total int `json:"total"`
		Info  []struct {
			Hash         string `json:"hash"`
			SQHash       string `json:"sqhash"`
			Hash320      string `json:"320hash"`
			AlbumID      string `json:"album_id"`
			AlbumAudioID any    `json:"album_audio_id"`
			AudioID      any    `json:"audio_id"`
			SongName     string `json:"filename"`
			SongTitle    string `json:"songname"`
			AuthorName   string `json:"author_name"`
			Duration     int    `json:"duration"`
			Bitrate      int    `json:"bitrate"`
			ExtName      string `json:"extname"`
			FileSize     int64  `json:"filesize"`
			Cover        string `json:"img"`
			Privilege    any    `json:"privilege"`
			TransParam   struct {
				Ogg320Hash string      `json:"ogg_320_hash"`
				Ogg128Hash string      `json:"ogg_128_hash"`
				SingerID   interface{} `json:"singerid"`
				UnionCover string      `json:"union_cover"`
				HashOffset struct {
					ClipHash string `json:"clip_hash"`
				} `json:"hash_offset"`
			} `json:"trans_param"`
		} `json:"info"`
	} `json:"data"`
}

type kugouPlaylistBaseResponse struct {
	Status  int    `json:"status"`
	ErrCode int    `json:"errcode,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

type kugouPlaylistInfoV2Response struct {
	kugouPlaylistBaseResponse
	Data struct {
		GlobalSpecialID string `json:"global_specialid"`
		SpecialName     string `json:"specialname"`
		ImgURL          string `json:"imgurl"`
		Intro           string `json:"intro"`
		Nickname        string `json:"nickname"`
		SongCount       int    `json:"songcount"`
		PlayCount       int    `json:"playcount"`
	} `json:"data"`
}

type kugouPlaylistSongsV2Response struct {
	kugouPlaylistBaseResponse
	Data struct {
		Total int              `json:"total"`
		Info  []map[string]any `json:"info"`
		List  []map[string]any `json:"list"`
		Data  []map[string]any `json:"data"`
	} `json:"data"`
}

type kugouPlaylistDecodeResponse struct {
	kugouPlaylistBaseResponse
	Data struct {
		List []struct {
			GlobalCollectionID string `json:"global_collection_id"`
			Info               struct {
				SpecialID any `json:"specialid"`
			} `json:"info"`
		} `json:"list"`
	} `json:"data"`
}

type kugouLegacyPlaylistSongsResponse struct {
	kugouPlaylistBaseResponse
	Info []map[string]any `json:"info"`
	Data struct {
		Info []map[string]any `json:"info"`
	} `json:"data"`
}

type kugouPlaylistSpecialInfoResponse struct {
	kugouPlaylistBaseResponse
	Data struct {
		GlobalSpecialID string `json:"global_specialid"`
	} `json:"data"`
}

type kugouPlaylistIdentity struct {
	InputID            string
	SpecialID          string
	GlobalSpecialID    string
	GlobalCollectionID string
	ResolvePath        string
}

func (c *Client) fetchGatewayTrackInfo(ctx context.Context, hash string) (*model.Song, error) {
	bodyMap := map[string]any{
		"area_code":       "1",
		"show_privilege":  "1",
		"show_album_info": "1",
		"is_publish":      "",
		"appid":           1005,
		"clientver":       11451,
		"mid":             kugouGatewayMid,
		"dfid":            "-",
		"clienttime":      time.Now().Unix(),
		"key":             kugouGatewaySignKey,
		"data":            []map[string]string{{"hash": hash}},
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, err
	}
	var resp kugouGatewaySongInfoResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, kugouGatewaySongInfoURL, nil, bytes.NewReader(body), map[string]string{
		"Content-Type": "application/json",
		"KG-THash":     "13a3164",
		"KG-RC":        "1",
		"KG-Fake":      "0",
		"KG-RF":        "00869891",
		"User-Agent":   "Android712-AndroidPhone-11451-376-0-FeeCacheUpdate-wifi",
		"x-router":     "kmr.service.kugou.com",
	}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 || len(resp.Data[0]) == 0 {
		return nil, fmt.Errorf("kugou gateway track info empty")
	}
	item := resp.Data[0][0]
	primaryHash := firstNonEmpty(item.AudioInfo.Hash, item.AudioInfo.Hash128, hash)
	shareChain := resolveSongShareChain(strings.TrimSpace(item.AlbumAudioID), formatAnyNumericString(item.AudioInfo.AudioID))
	trackLink := buildShareTrackLink(shareChain, primaryHash, item.AlbumInfo.AlbumID, strings.TrimSpace(item.AlbumAudioID))
	filesize128 := parseKugouInt64(item.AudioInfo.Filesize128)
	filesize := parseKugouInt64(item.AudioInfo.Filesize)
	song := &model.Song{
		Source:   "kugou",
		ID:       strings.ToLower(strings.TrimSpace(primaryHash)),
		Name:     strings.TrimSpace(item.OriAudioName),
		Artist:   strings.TrimSpace(item.AuthorName),
		Album:    strings.TrimSpace(item.AlbumInfo.AlbumName),
		AlbumID:  strings.TrimSpace(item.AlbumInfo.AlbumID),
		Duration: normalizeGatewayDuration(parseKugouInt(item.AudioInfo.Timelength)),
		Size:     choosePositive(filesize128, filesize),
		Bitrate:  normalizeGatewayBitrate(parseKugouInt(item.AudioInfo.Bitrate)),
		Ext:      strings.TrimSpace(item.AudioInfo.Extname),
		Cover:    normalizeSizedCover(item.AlbumInfo.SizableCover),
		Link:     trackLink,
		Extra: map[string]string{
			"hash":           strings.ToLower(strings.TrimSpace(primaryHash)),
			"file_hash":      strings.ToLower(strings.TrimSpace(item.AudioInfo.Hash128)),
			"hq_hash":        strings.ToLower(strings.TrimSpace(item.AudioInfo.Hash320)),
			"sq_hash":        strings.ToLower(strings.TrimSpace(item.AudioInfo.HashFlac)),
			"res_hash":       strings.ToLower(strings.TrimSpace(firstNonEmpty(item.AudioInfo.HashHigh, item.AudioInfo.HashSuper))),
			"album_id":       strings.TrimSpace(item.AlbumInfo.AlbumID),
			"album_audio_id": strings.TrimSpace(item.AlbumAudioID),
			"audio_id":       formatAnyNumericString(item.AudioInfo.AudioID),
			"share_chain":    shareChain,
			"privilege":      formatAnyNumericString(item.AudioInfo.Privilege),
		},
	}
	if enriched := c.enrichGatewaySongMeta(ctx, song); enriched != nil {
		song = enriched
	}
	return song, nil
}

func (c *Client) fetchAlbumInfo(ctx context.Context, albumID string) (*kugouAlbumInfoResponse, error) {
	apiURL := "http://mobilecdnbj.kugou.com/api/v3/album/info?albumid=" + url.QueryEscape(strings.TrimSpace(albumID))
	var resp kugouAlbumInfoResponse
	if err := c.doJSONRequest(ctx, http.MethodGet, apiURL, nil, nil, map[string]string{
		"User-Agent": "Mozilla/5.0",
	}, &resp); err != nil {
		return nil, wrapError("kugou", "album", albumID, err)
	}
	return &resp, nil
}

func (c *Client) fetchAlbumSongs(ctx context.Context, albumID string) ([]model.Song, int, error) {
	params := url.Values{}
	params.Set("albumid", strings.TrimSpace(albumID))
	params.Set("page", "1")
	params.Set("pagesize", "500")
	apiURL := "http://mobilecdnbj.kugou.com/api/v3/album/song?" + params.Encode()
	var resp kugouAlbumSongsResponse
	if err := c.doJSONRequest(ctx, http.MethodGet, apiURL, nil, nil, map[string]string{
		"User-Agent": "Mozilla/5.0",
	}, &resp); err != nil {
		return nil, 0, wrapError("kugou", "album", albumID, err)
	}
	results := make([]model.Song, 0, len(resp.Data.Info))
	for _, item := range resp.Data.Info {
		primaryHash := firstNonEmpty(item.Hash, item.Hash320, item.SQHash, item.TransParam.Ogg320Hash, item.TransParam.Ogg128Hash, item.TransParam.HashOffset.ClipHash)
		if normalizeHash(primaryHash) == "" {
			continue
		}
		songName := strings.TrimSpace(firstNonEmpty(item.SongTitle, item.SongName))
		artistName := strings.TrimSpace(item.AuthorName)
		if title, parsedArtist := splitKugouSongDisplayName(songName); title != "" {
			songName = title
			if artistName == "" {
				artistName = parsedArtist
			}
		}
		if artistName == "" {
			if title, parsedArtist := splitKugouSongDisplayName(strings.TrimSpace(item.SongName)); title != "" {
				if songName == "" {
					songName = title
				}
				artistName = parsedArtist
			}
		}
		shareChain := resolveSongShareChain(formatAnyNumericString(item.AlbumAudioID), formatAnyNumericString(item.AudioID))
		cover := normalizeSizedCover(strings.TrimSpace(firstNonEmpty(item.TransParam.UnionCover, item.Cover)))
		singerIDs := formatKugouIDList(firstNonEmpty(formatAnyIDList(item.TransParam.SingerID)))
		song := model.Song{
			Source:   "kugou",
			ID:       normalizeHash(primaryHash),
			Name:     songName,
			Artist:   artistName,
			AlbumID:  strings.TrimSpace(item.AlbumID),
			Duration: item.Duration,
			Size:     item.FileSize,
			Bitrate:  item.Bitrate,
			Ext:      strings.TrimSpace(item.ExtName),
			Cover:    cover,
			Link:     buildShareTrackLink(shareChain, primaryHash, item.AlbumID, formatAnyNumericString(item.AlbumAudioID)),
			Extra: map[string]string{
				"hash":           normalizeHash(primaryHash),
				"file_hash":      normalizeHash(item.Hash),
				"hq_hash":        normalizeHash(item.Hash320),
				"sq_hash":        normalizeHash(item.SQHash),
				"ogg_320_hash":   normalizeHash(item.TransParam.Ogg320Hash),
				"ogg_128_hash":   normalizeHash(item.TransParam.Ogg128Hash),
				"album_id":       strings.TrimSpace(item.AlbumID),
				"album_audio_id": formatAnyNumericString(item.AlbumAudioID),
				"audio_id":       formatAnyNumericString(item.AudioID),
				"share_chain":    shareChain,
				"privilege":      formatAnyNumericString(item.Privilege),
				"singer_ids":     singerIDs,
			},
		}
		if enriched := c.enrichGatewaySongMeta(ctx, &song); enriched != nil {
			song = *enriched
		}
		results = append(results, song)
	}
	return results, resp.Data.Total, nil
}

func (c *Client) enrichGatewaySongMeta(ctx context.Context, song *model.Song) *model.Song {
	if song == nil {
		return song
	}
	extra := ensureSongExtra(song)
	if strings.TrimSpace(extra["singer_ids"]) != "" {
		return song
	}
	query := strings.TrimSpace(strings.Join([]string{song.Name, song.Artist}, " "))
	if query == "" {
		return song
	}
	results, err := c.searchSongs(ctx, query, 10)
	if err != nil || len(results) == 0 {
		return song
	}
	primaryHash := normalizeHash(firstNonEmpty(extra["hash"], song.ID))
	best := findMatchingSearchSong(results, primaryHash, strings.TrimSpace(song.AlbumID), strings.TrimSpace(song.Name), strings.TrimSpace(song.Artist))
	if best == nil || best.Extra == nil {
		return song
	}
	if singerIDs := strings.TrimSpace(best.Extra["singer_ids"]); singerIDs != "" {
		extra["singer_ids"] = singerIDs
	}
	if strings.TrimSpace(extra["share_chain"]) == "" {
		if shareChain := strings.TrimSpace(best.Extra["share_chain"]); shareChain != "" {
			extra["share_chain"] = shareChain
			song.Link = buildShareTrackLink(shareChain, song.ID, song.AlbumID, extra["album_audio_id"])
		}
	}
	return song
}

func (c *Client) fetchGlobalCollectionPlaylist(ctx context.Context, identity kugouPlaylistIdentity) (*model.Playlist, []model.Song, error) {
	globalID := firstNonEmpty(identity.GlobalCollectionID, identity.GlobalSpecialID, identity.InputID)
	v2ID := firstNonEmpty(identity.GlobalCollectionID, identity.GlobalSpecialID, globalID)
	if c != nil && c.logger != nil {
		c.logger.Debug("kugou: fetch global collection playlist", "input_id", identity.InputID, "global_collection_id", identity.GlobalCollectionID, "global_specialid", identity.GlobalSpecialID, "specialid", identity.SpecialID, "global_id", globalID, "v2_id", v2ID, "resolve_path", identity.ResolvePath)
	}
	info, err := c.fetchPlaylistInfoV2(ctx, v2ID)
	if err != nil {
		if c != nil && c.logger != nil {
			c.logger.Warn("kugou: v2 playlist info fetch failed, continuing with fallback metadata", "v2_id", v2ID, "err", err)
		}
	}
	songs, total, err := c.fetchPlaylistSongsV2(ctx, v2ID)
	if err != nil {
		if c != nil && c.logger != nil {
			c.logger.Warn("kugou: v2 playlist song fetch failed, trying legacy", "v2_id", v2ID, "err", err)
		}
		legacy, legacySongs, legacyErr := c.fetchLegacyCollectionPlaylist(ctx, identity)
		if legacyErr == nil && legacy != nil {
			if c != nil && c.logger != nil {
				c.logger.Debug("kugou: legacy playlist fallback succeeded", "song_count", len(legacySongs), "track_count", legacy.TrackCount)
			}
			return legacy, legacySongs, nil
		}
		return nil, nil, err
	}
	metaSongCount := 0
	metaPlayCount := 0
	metaName := ""
	metaCover := ""
	metaCreator := ""
	metaIntro := ""
	metaGlobalSpecialID := ""
	if info != nil {
		metaSongCount = info.Data.SongCount
		metaPlayCount = info.Data.PlayCount
		metaName = strings.TrimSpace(info.Data.SpecialName)
		metaCover = strings.TrimSpace(info.Data.ImgURL)
		metaCreator = strings.TrimSpace(info.Data.Nickname)
		metaIntro = strings.TrimSpace(info.Data.Intro)
		metaGlobalSpecialID = strings.TrimSpace(info.Data.GlobalSpecialID)
	}
	if c != nil && c.logger != nil {
		c.logger.Debug("kugou: v2 playlist fetch result", "v2_id", v2ID, "song_count", len(songs), "track_count", choosePositiveInt(metaSongCount, total, len(songs)), "meta_song_count", metaSongCount, "api_total", total)
	}
	if supplemental, supplementErr := c.fetchLegacyCollectionSongsForMetadata(ctx, identity); supplementErr == nil && len(supplemental) > 0 {
		if c != nil && c.logger != nil {
			c.logger.Debug("kugou: fetched legacy supplemental playlist songs", "count", len(supplemental))
		}
		songs = mergePlaylistSongs(songs, supplemental)
	}
	playlist := &model.Playlist{
		ID:          firstNonEmpty(metaGlobalSpecialID, globalID),
		Name:        metaName,
		Cover:       metaCover,
		TrackCount:  choosePositiveInt(metaSongCount, total, len(songs)),
		PlayCount:   metaPlayCount,
		Creator:     metaCreator,
		Description: metaIntro,
		Source:      "kugou",
		Link:        buildPlaylistLink(firstNonEmpty(identity.GlobalCollectionID, identity.GlobalSpecialID, globalID)),
		Extra: map[string]string{
			"collection_type":      "playlist",
			"input_playlist_id":    strings.TrimSpace(identity.InputID),
			"global_collection_id": strings.TrimSpace(identity.GlobalCollectionID),
			"global_specialid":     strings.TrimSpace(identity.GlobalSpecialID),
			"specialid":            strings.TrimSpace(identity.SpecialID),
			"id_resolve_path":      strings.TrimSpace(identity.ResolvePath),
		},
	}
	if info == nil {
		applyPlaylistSongMetadataFallback(playlist, songs)
	}
	if strings.TrimSpace(playlist.Name) == "" && len(songs) > 0 {
		playlist.Name = strings.TrimSpace(songs[0].Album)
	}
	if strings.TrimSpace(playlist.Cover) == "" && len(songs) > 0 {
		playlist.Cover = strings.TrimSpace(songs[0].Cover)
	}
	for i := range songs {
		if strings.TrimSpace(songs[i].Album) == "" {
			songs[i].Album = playlist.Name
		}
		if strings.TrimSpace(songs[i].AlbumID) == "" {
			songs[i].AlbumID = firstNonEmpty(identity.GlobalCollectionID, identity.GlobalSpecialID)
		}
		if strings.TrimSpace(songs[i].Cover) == "" {
			songs[i].Cover = playlist.Cover
		}
	}
	applyPlaylistSongContext(playlist, songs)
	return playlist, songs, nil
}

func (c *Client) fetchLegacyCollectionPlaylist(ctx context.Context, identity kugouPlaylistIdentity) (*model.Playlist, []model.Song, error) {
	globalID := firstNonEmpty(identity.GlobalCollectionID, identity.GlobalSpecialID, identity.InputID)
	songs, err := c.fetchLegacyPlaylistSongs(ctx, map[string]string{
		"need_sort":            "1",
		"module":               "CloudMusic",
		"clientver":            "11589",
		"pagesize":             "500",
		"page":                 "1",
		"global_collection_id": globalID,
		"userid":               "0",
		"type":                 "0",
		"area_code":            "1",
		"appid":                kugouGatewayAppID,
	}, kugouGatewaySignKey, kugouPlaylistAndroidUA)
	if err != nil {
		return nil, nil, err
	}
	playlist := &model.Playlist{
		ID:     globalID,
		Name:   globalID,
		Source: "kugou",
		Link:   buildPlaylistLink(firstNonEmpty(identity.GlobalCollectionID, identity.GlobalSpecialID, globalID)),
		Extra: map[string]string{
			"collection_type":      "playlist",
			"input_playlist_id":    strings.TrimSpace(identity.InputID),
			"global_collection_id": strings.TrimSpace(identity.GlobalCollectionID),
			"global_specialid":     strings.TrimSpace(identity.GlobalSpecialID),
			"specialid":            strings.TrimSpace(identity.SpecialID),
			"id_resolve_path":      firstNonEmpty(strings.TrimSpace(identity.ResolvePath), "legacy:global_collection"),
		},
		TrackCount: len(songs),
	}
	if metaID := firstNonEmpty(identity.GlobalSpecialID, identity.GlobalCollectionID); metaID != "" {
		if info, infoErr := c.fetchPlaylistInfoV2(ctx, metaID); infoErr == nil && info != nil {
			applyPlaylistInfoMetadata(playlist, info)
		}
	}
	applyPlaylistSongMetadataFallback(playlist, songs)
	applyPlaylistSongContext(playlist, songs)
	return playlist, songs, nil
}

func (c *Client) fetchLegacyCollectionSongsForMetadata(ctx context.Context, identity kugouPlaylistIdentity) ([]model.Song, error) {
	globalID := firstNonEmpty(identity.GlobalCollectionID, identity.GlobalSpecialID, identity.InputID)
	if strings.TrimSpace(globalID) == "" {
		return nil, nil
	}
	return c.fetchLegacyPlaylistSongs(ctx, map[string]string{
		"need_sort":            "1",
		"module":               "CloudMusic",
		"clientver":            "11589",
		"pagesize":             "500",
		"page":                 "1",
		"global_collection_id": globalID,
		"userid":               "0",
		"type":                 "0",
		"area_code":            "1",
		"appid":                kugouGatewayAppID,
	}, kugouGatewaySignKey, kugouPlaylistAndroidUA)
}

func (c *Client) fetchLegacySpecialPlaylist(ctx context.Context, specialID string) (*model.Playlist, []model.Song, error) {
	identity := kugouPlaylistIdentity{InputID: specialID, SpecialID: specialID, ResolvePath: "specialid->legacy"}
	if globalID, err := c.resolveGlobalSpecialID(ctx, specialID); err == nil && globalID != "" {
		identity.GlobalSpecialID = globalID
	}
	songs, err := c.fetchLegacyPlaylistSongs(ctx, map[string]string{
		"srcappid":  kugouPlaylistSrcAppID,
		"clientver": kugouPlaylistClientVer,
		"appid":     kugouPlaylistWebAppID,
		"type":      "0",
		"module":    "playlist",
		"page":      "1",
		"pagesize":  "500",
		"specialid": specialID,
	}, kugouPlaySignKey, kugouPlaylistWebUA)
	if err != nil {
		return nil, nil, err
	}
	playlist := &model.Playlist{
		ID:     specialID,
		Name:   specialID,
		Source: "kugou",
		Link:   buildPlaylistLink(specialID),
		Extra: map[string]string{
			"collection_type":      "playlist",
			"input_playlist_id":    strings.TrimSpace(identity.InputID),
			"specialid":            strings.TrimSpace(identity.SpecialID),
			"global_specialid":     strings.TrimSpace(identity.GlobalSpecialID),
			"global_collection_id": strings.TrimSpace(identity.GlobalCollectionID),
			"id_resolve_path":      strings.TrimSpace(identity.ResolvePath),
		},
		TrackCount: len(songs),
	}
	if identity.GlobalSpecialID != "" {
		if info, infoErr := c.fetchPlaylistInfoV2(ctx, identity.GlobalSpecialID); infoErr == nil && info != nil {
			applyPlaylistInfoMetadata(playlist, info)
		}
	}
	applyPlaylistSongMetadataFallback(playlist, songs)
	applyPlaylistSongContext(playlist, songs)
	return playlist, songs, nil
}

func (c *Client) decodePlaylistGCID(ctx context.Context, gcid string) (kugouPlaylistIdentity, error) {
	gcid = strings.TrimSpace(strings.ToLower(gcid))
	if !strings.HasPrefix(gcid, "gcid_") {
		return kugouPlaylistIdentity{}, nil
	}
	type decodeItem struct {
		ID     string `json:"id"`
		IDType int    `json:"id_type"`
	}
	type decodeBody struct {
		RetInfo int          `json:"ret_info"`
		Data    []decodeItem `json:"data"`
	}
	bodyJSON, err := json.Marshal(decodeBody{
		RetInfo: 1,
		Data: []decodeItem{{
			ID:     gcid,
			IDType: 2,
		}},
	})
	if err != nil {
		return kugouPlaylistIdentity{}, err
	}
	query := url.Values{}
	query.Set("dfid", "-")
	query.Set("appid", kugouGatewayAppID)
	query.Set("mid", "0")
	query.Set("clientver", kugouPlaylistAndroidCV)
	query.Set("clienttime", "640612895")
	query.Set("uuid", "-")
	query.Set("signature", signPlaylistQuery(query, kugouGatewaySignKey, string(bodyJSON)))
	var resp kugouPlaylistDecodeResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, kugouPlaylistDecodeURL, query, bytes.NewReader(bodyJSON), map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   kugouPlaylistAndroidUA,
		"Referer":      "https://m.kugou.com/",
	}, &resp); err != nil {
		return kugouPlaylistIdentity{}, err
	}
	if len(resp.Data.List) == 0 {
		return kugouPlaylistIdentity{}, fmt.Errorf("kugou batch_decode empty for %s", gcid)
	}
	decoded := resp.Data.List[0]
	identity := kugouPlaylistIdentity{
		InputID:            gcid,
		GlobalCollectionID: strings.TrimSpace(decoded.GlobalCollectionID),
		SpecialID:          formatAnyNumericString(decoded.Info.SpecialID),
	}
	identity.GlobalSpecialID = strings.TrimSpace(decoded.GlobalCollectionID)
	return identity, nil
}

func (c *Client) resolveGlobalSpecialID(ctx context.Context, specialID string) (string, error) {
	specialID = strings.TrimSpace(specialID)
	if !isNumericText(specialID) {
		return "", nil
	}
	query := url.Values{}
	query.Set("specialid", specialID)
	var resp kugouPlaylistSpecialInfoResponse
	if err := c.doJSONRequest(ctx, http.MethodGet, kugouPlaylistSpecialURL, query, nil, map[string]string{
		"User-Agent": "Mozilla/5.0",
	}, &resp); err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Data.GlobalSpecialID), nil
}

func (c *Client) fetchPlaylistInfoV2(ctx context.Context, globalID string) (*kugouPlaylistInfoV2Response, error) {
	query := buildPlaylistWebQuery(time.Now())
	query.Set("specialid", "0")
	query.Set("global_specialid", strings.TrimSpace(globalID))
	query.Set("format", "jsonp")
	query.Set("signature", signPlaylistQuery(query, kugouPlaySignKey, ""))
	var resp kugouPlaylistInfoV2Response
	if err := c.doJSONRequest(ctx, http.MethodGet, kugouPlaylistInfoV2URL, query, nil, map[string]string{
		"User-Agent": kugouPlaylistWebUA,
		"Referer":    kugouPlaylistReferer,
		"mid":        query.Get("mid"),
		"clienttime": query.Get("clienttime"),
		"dfid":       query.Get("dfid"),
	}, &resp); err != nil {
		return nil, err
	}
	if resp.Status != 1 && strings.TrimSpace(resp.Data.SpecialName) == "" {
		return nil, fmt.Errorf("kugou playlist info unavailable: %s", firstNonEmpty(resp.Error, resp.Message, strconv.Itoa(resp.ErrCode)))
	}
	return &resp, nil
}

func (c *Client) fetchPlaylistSongsV2(ctx context.Context, globalID string) ([]model.Song, int, error) {
	const pageSize = 300
	allEntries := make([]map[string]any, 0, pageSize)
	total := 0
	for page := 1; ; page++ {
		query := buildPlaylistWebQuery(time.Now())
		query.Set("global_specialid", strings.TrimSpace(globalID))
		query.Set("specialid", "0")
		query.Set("plat", "0")
		query.Set("version", "8000")
		query.Set("page", strconv.Itoa(page))
		query.Set("pagesize", strconv.Itoa(pageSize))
		query.Set("signature", signPlaylistQuery(query, kugouPlaySignKey, ""))
		var resp kugouPlaylistSongsV2Response
		if err := c.doJSONRequest(ctx, http.MethodGet, kugouPlaylistSongV2URL, query, nil, map[string]string{
			"User-Agent": kugouPlaylistWebUA,
			"Referer":    kugouPlaylistReferer,
			"mid":        query.Get("mid"),
			"clienttime": query.Get("clienttime"),
			"dfid":       query.Get("dfid"),
		}, &resp); err != nil {
			return nil, 0, err
		}
		entries := resp.Data.Info
		if len(entries) == 0 {
			entries = resp.Data.List
		}
		if len(entries) == 0 {
			entries = resp.Data.Data
		}
		if total <= 0 {
			total = resp.Data.Total
		}
		if c != nil && c.logger != nil {
			c.logger.Debug("kugou: fetched playlist songs v2 page", "global_specialid", globalID, "page", page, "entries_info", len(resp.Data.Info), "entries_list", len(resp.Data.List), "entries_data", len(resp.Data.Data), "entries_selected", len(entries), "api_total", resp.Data.Total)
		}
		if len(entries) == 0 {
			if page == 1 {
				return nil, 0, fmt.Errorf("kugou playlist songs empty: %s", firstNonEmpty(resp.Error, resp.Message, strconv.Itoa(resp.ErrCode)))
			}
			break
		}
		allEntries = append(allEntries, entries...)
		if total > 0 && len(allEntries) >= total {
			break
		}
		if total <= 0 && len(entries) < pageSize {
			break
		}
	}
	songs := convertPlaylistSongEntries(allEntries)
	if c != nil && c.logger != nil {
		c.logger.Debug("kugou: converted playlist songs v2", "global_specialid", globalID, "raw_entries", len(allEntries), "songs_after_convert", len(songs), "total", choosePositiveInt(total, len(allEntries)))
	}
	return songs, choosePositiveInt(total, len(allEntries)), nil
}

func (c *Client) fetchLegacyPlaylistSongs(ctx context.Context, params map[string]string, secret, userAgent string) ([]model.Song, error) {
	page := 1
	if rawPage := strings.TrimSpace(params["page"]); rawPage != "" {
		if parsed, err := strconv.Atoi(rawPage); err == nil && parsed > 0 {
			page = parsed
		}
	}
	pageSize := 500
	if rawPageSize := strings.TrimSpace(params["pagesize"]); rawPageSize != "" {
		if parsed, err := strconv.Atoi(rawPageSize); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	allEntries := make([]map[string]any, 0, pageSize)
	seenPageKeys := make(map[string]struct{})
	for {
		query := url.Values{}
		for key, value := range params {
			query.Set(key, value)
		}
		query.Set("page", strconv.Itoa(page))
		query.Set("pagesize", strconv.Itoa(pageSize))
		query.Set("signature", signPlaylistQuery(query, secret, ""))
		var resp kugouLegacyPlaylistSongsResponse
		if err := c.doJSONRequest(ctx, http.MethodGet, kugouPlaylistLegacyURL, query, nil, map[string]string{
			"User-Agent": userAgent,
			"Referer":    kugouPlaylistReferer,
			"dfid":       "-",
		}, &resp); err != nil {
			return nil, err
		}
		entries := resp.Info
		if len(entries) == 0 {
			entries = resp.Data.Info
		}
		if c != nil && c.logger != nil {
			c.logger.Debug("kugou: fetched legacy playlist page", "page", page, "entries_info", len(resp.Info), "entries_data_info", len(resp.Data.Info), "entries_selected", len(entries))
		}
		if len(entries) == 0 {
			if page == 1 {
				return nil, fmt.Errorf("kugou legacy playlist empty: %s", firstNonEmpty(resp.Error, resp.Message, strconv.Itoa(resp.ErrCode)))
			}
			break
		}
		pageKey := playlistEntryPageKey(entries)
		if _, exists := seenPageKeys[pageKey]; exists {
			if c != nil && c.logger != nil {
				c.logger.Warn("kugou: legacy playlist page repeated, stopping pagination", "page", page, "entries", len(entries))
			}
			break
		}
		seenPageKeys[pageKey] = struct{}{}
		allEntries = append(allEntries, entries...)
		if len(entries) < pageSize {
			break
		}
		page++
	}
	songs := convertPlaylistSongEntries(allEntries)
	if c != nil && c.logger != nil {
		c.logger.Debug("kugou: converted legacy playlist songs", "raw_entries", len(allEntries), "songs_after_convert", len(songs))
	}
	return songs, nil
}

func buildPlaylistWebQuery(now time.Time) url.Values {
	ts := strconv.FormatInt(now.UnixMilli(), 10)
	query := url.Values{}
	query.Set("appid", kugouPlaylistWebAppID)
	query.Set("srcappid", kugouPlaylistSrcAppID)
	query.Set("clientver", kugouPlaylistClientVer)
	query.Set("clienttime", ts)
	query.Set("mid", ts)
	query.Set("uuid", ts)
	query.Set("dfid", "-")
	return query
}

func signPlaylistQuery(query url.Values, secret, body string) string {
	keys := sortedQueryKeys(query)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.EqualFold(key, "signature") {
			continue
		}
		for _, value := range query[key] {
			parts = append(parts, key+"="+value)
		}
	}
	sort.Strings(parts)
	return conceptMD5(secret + strings.Join(parts, "") + body + secret)
}

func isGlobalCollectionID(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.HasPrefix(value, "collection_")
}

func extractPlaylistGlobalCollectionID(text string) string {
	patterns := []string{
		`global_collection_id["'=: ]+(collection_[A-Za-z0-9_\-]+)`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		match := re.FindStringSubmatch(text)
		if len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func convertPlaylistSongEntries(entries []map[string]any) []model.Song {
	results := make([]model.Song, 0, len(entries))
	skippedWithoutID := 0
	for _, entry := range entries {
		song := convertPlaylistSongEntry(entry)
		if strings.TrimSpace(song.ID) == "" {
			skippedWithoutID++
			continue
		}
		results = append(results, song)
	}
	_ = skippedWithoutID
	return results
}

func convertPlaylistSongEntry(entry map[string]any) model.Song {
	hash := normalizeHash(firstNonEmpty(
		valueString(entry["hash"]),
		valueString(entry["audio_id"]),
		valueString(entry["320hash"]),
		valueString(entry["sqhash"]),
	))
	relateGoods := mapRelateGoods(entry["relate_goods"])
	hash = firstNonEmpty(hash, normalizeHash(relateGoods["128"]), normalizeHash(relateGoods["320"]), normalizeHash(relateGoods["flac"]), normalizeHash(relateGoods["high"]))
	albumInfo := mapValueAny(entry, "albuminfo")
	albumID := firstNonEmpty(valueString(entry["album_id"]), valueString(albumInfo["id"]))
	albumName := firstNonEmpty(valueString(entry["album_name"]), valueString(albumInfo["name"]))
	singerInfo := mapSlice(entry["singerinfo"])
	artistNames, singerIDs := extractSingerInfo(singerInfo)
	artist := firstNonEmpty(strings.Join(artistNames, "、"), valueString(entry["author_name"]), valueString(entry["singername"]))
	name := strings.TrimSpace(valueString(entry["songname"]))
	if name == "" {
		name = strings.TrimSpace(valueString(entry["name"]))
	}
	if name == "" {
		name = strings.TrimSpace(valueString(entry["filename"]))
	}
	if title, parsedArtist := splitKugouSongDisplayName(name); title != "" {
		name = title
		if strings.TrimSpace(artist) == "" {
			artist = parsedArtist
		}
	}
	if strings.TrimSpace(artist) == "" {
		if title, parsedArtist := splitKugouSongDisplayName(strings.TrimSpace(valueString(entry["filename"]))); title != "" {
			if strings.TrimSpace(name) == "" {
				name = title
			}
			artist = parsedArtist
		}
	}
	albumAudioID := firstNonEmpty(valueString(entry["album_audio_id"]), valueString(entry["mixsongid"]), valueString(entry["mix_song_id"]))
	audioID := firstNonEmpty(valueString(entry["audio_id"]))
	shareChain := resolveSongShareChain(albumAudioID, audioID)
	bitrate := parseKugouInt(firstNonEmptyAny(entry["bitrate"], entry["bit_rate"]))
	extName := firstNonEmpty(valueString(entry["extname"]), valueString(entry["ext_name"]))
	if extName == "" {
		extName = guessPlaylistEntryFormat(relateGoods)
	}
	cover := normalizeSizedCover(firstNonEmpty(valueString(entry["imgurl"]), valueString(entry["cover"]), valueString(entry["img"]), valueString(albumInfo["imgurl"])))
	extra := map[string]string{
		"hash":           hash,
		"file_hash":      hash,
		"hq_hash":        normalizeHash(firstNonEmpty(relateGoods["320"], valueString(entry["320hash"]))),
		"sq_hash":        normalizeHash(firstNonEmpty(relateGoods["flac"], valueString(entry["sqhash"]))),
		"res_hash":       normalizeHash(relateGoods["high"]),
		"album_id":       albumID,
		"album_audio_id": albumAudioID,
		"audio_id":       audioID,
		"mix_song_id":    firstNonEmpty(valueString(entry["mixsongid"]), valueString(entry["mix_song_id"])),
		"share_chain":    shareChain,
		"singer_ids":     strings.Join(singerIDs, ","),
		"privilege":      valueString(entry["privilege"]),
	}
	for level, qualityHash := range relateGoods {
		switch level {
		case "128":
			extra["hash"] = firstNonEmpty(extra["hash"], qualityHash)
		case "320":
			extra["hq_hash"] = firstNonEmpty(extra["hq_hash"], qualityHash)
		case "flac":
			extra["sq_hash"] = firstNonEmpty(extra["sq_hash"], qualityHash)
		case "high", "flac24bit":
			extra["res_hash"] = firstNonEmpty(extra["res_hash"], qualityHash)
		}
		if size := valueString(relateGoodsSize(entry["relate_goods"], level)); size != "" {
			extra[level+"_filesize"] = size
		}
	}
	return model.Song{
		Source:   "kugou",
		ID:       hash,
		Name:     name,
		Artist:   artist,
		Album:    albumName,
		AlbumID:  albumID,
		Duration: normalizeGatewayDuration(parseKugouInt(firstNonEmptyAny(entry["timelen"], entry["duration"]))),
		Size:     parseKugouInt64(firstNonEmptyAny(entry["filesize"], entry["size"])),
		Bitrate:  bitrate,
		Ext:      extName,
		Cover:    cover,
		Link:     buildShareTrackLink(shareChain, hash, albumID, albumAudioID),
		Extra:    extra,
	}
}

func mapRelateGoods(value any) map[string]string {
	result := map[string]string{}
	for _, item := range mapSlice(value) {
		level := strings.ToLower(strings.TrimSpace(valueString(item["level"])))
		hash := normalizeHash(valueString(item["hash"]))
		if level == "" || hash == "" {
			continue
		}
		result[level] = hash
	}
	return result
}

func relateGoodsSize(value any, level string) string {
	for _, item := range mapSlice(value) {
		itemLevel := strings.ToLower(strings.TrimSpace(valueString(item["level"])))
		if itemLevel != strings.ToLower(strings.TrimSpace(level)) {
			continue
		}
		return valueString(firstNonEmptyAny(item["size"], item["filesize"]))
	}
	return ""
}

func mapValueAny(entry map[string]any, key string) map[string]any {
	if entry == nil {
		return nil
	}
	if value, ok := entry[key].(map[string]any); ok {
		return value
	}
	return nil
}

func mapSlice(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped, ok := item.(map[string]any); ok {
			result = append(result, mapped)
		}
	}
	return result
}

func extractSingerInfo(items []map[string]any) ([]string, []string) {
	names := make([]string, 0, len(items))
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if name := strings.TrimSpace(firstNonEmpty(valueString(item["name"]), valueString(item["author_name"]))); name != "" {
			names = append(names, name)
		}
		if id := strings.TrimSpace(firstNonEmpty(valueString(item["id"]), valueString(item["author_id"]), valueString(item["singerid"]))); id != "" {
			ids = append(ids, id)
		}
	}
	return names, ids
}

func splitKugouSongDisplayName(value string) (title, artist string) {
	value = strings.TrimSpace(value)
	for _, sep := range []string{" - ", "-", "－", "—", "–"} {
		parts := strings.SplitN(value, sep, 2)
		if len(parts) != 2 {
			continue
		}
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		if left == "" || right == "" {
			continue
		}
		return right, left
	}
	return "", ""
}

func guessPlaylistEntryFormat(relateGoods map[string]string) string {
	if normalizeHash(relateGoods["flac"]) != "" || normalizeHash(relateGoods["high"]) != "" {
		return "flac"
	}
	if normalizeHash(relateGoods["320"]) != "" {
		return "mp3"
	}
	return "mp3"
}

func firstNonEmptyAny(values ...any) any {
	for _, value := range values {
		if strings.TrimSpace(valueString(value)) != "" {
			return value
		}
	}
	return nil
}

func choosePositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func playlistEntryPageKey(entries []map[string]any) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, firstNonEmpty(
			normalizeHash(valueString(entry["hash"])),
			strings.TrimSpace(valueString(entry["mixsongid"])),
			strings.TrimSpace(valueString(entry["mix_song_id"])),
			strings.TrimSpace(valueString(entry["audio_id"])),
			strings.TrimSpace(valueString(entry["songname"])),
			strings.TrimSpace(valueString(entry["filename"])),
		))
	}
	return strings.Join(parts, "|")
}

func applyPlaylistInfoMetadata(playlist *model.Playlist, info *kugouPlaylistInfoV2Response) {
	if playlist == nil || info == nil {
		return
	}
	if strings.TrimSpace(playlist.Name) == "" {
		playlist.Name = strings.TrimSpace(info.Data.SpecialName)
	}
	if strings.TrimSpace(playlist.Cover) == "" {
		playlist.Cover = strings.TrimSpace(info.Data.ImgURL)
	}
	if strings.TrimSpace(playlist.Description) == "" {
		playlist.Description = strings.TrimSpace(info.Data.Intro)
	}
	if strings.TrimSpace(playlist.Creator) == "" {
		playlist.Creator = strings.TrimSpace(info.Data.Nickname)
	}
	playlist.PlayCount = choosePositiveInt(playlist.PlayCount, info.Data.PlayCount)
	playlist.TrackCount = choosePositiveInt(playlist.TrackCount, info.Data.SongCount)
	if playlist.Extra == nil {
		playlist.Extra = map[string]string{}
	}
	if strings.TrimSpace(playlist.Extra["global_specialid"]) == "" {
		playlist.Extra["global_specialid"] = strings.TrimSpace(info.Data.GlobalSpecialID)
	}
}

func applyPlaylistSongMetadataFallback(playlist *model.Playlist, songs []model.Song) {
	if playlist == nil || len(songs) == 0 {
		return
	}
	if strings.TrimSpace(playlist.Name) == "" {
		playlist.Name = strings.TrimSpace(firstNonEmpty(songs[0].Album, songs[0].Name))
	}
	if strings.TrimSpace(playlist.Cover) == "" {
		playlist.Cover = strings.TrimSpace(songs[0].Cover)
	}
	playlist.TrackCount = choosePositiveInt(playlist.TrackCount, len(songs))
	for i := range songs {
		if strings.TrimSpace(songs[i].Album) == "" {
			songs[i].Album = playlist.Name
		}
		if strings.TrimSpace(songs[i].Cover) == "" {
			songs[i].Cover = playlist.Cover
		}
	}
}

func applyPlaylistSongContext(playlist *model.Playlist, songs []model.Song) {
	if playlist == nil {
		return
	}
	playlistURL := strings.TrimSpace(playlist.Link)
	playlistName := strings.TrimSpace(playlist.Name)
	playlistID := strings.TrimSpace(playlist.ID)
	for i := range songs {
		extra := ensureSongExtra(&songs[i])
		extra["playlist_id"] = playlistID
		extra["playlist_url"] = playlistURL
		extra["playlist_name"] = playlistName
	}
}

func mergePlaylistSongs(primary, supplemental []model.Song) []model.Song {
	if len(primary) == 0 || len(supplemental) == 0 {
		return primary
	}
	index := make(map[string]model.Song, len(supplemental))
	for _, song := range supplemental {
		key := normalizeHash(firstNonEmpty(song.ID, song.Extra["hash"], song.Extra["file_hash"]))
		if key == "" {
			continue
		}
		index[key] = song
	}
	for i := range primary {
		key := normalizeHash(firstNonEmpty(primary[i].ID, primary[i].Extra["hash"], primary[i].Extra["file_hash"]))
		if key == "" {
			continue
		}
		supplement, ok := index[key]
		if !ok {
			continue
		}
		if strings.TrimSpace(primary[i].Artist) == "" {
			primary[i].Artist = strings.TrimSpace(supplement.Artist)
		}
		if strings.TrimSpace(primary[i].Album) == "" {
			primary[i].Album = strings.TrimSpace(supplement.Album)
		}
		if strings.TrimSpace(primary[i].AlbumID) == "" {
			primary[i].AlbumID = strings.TrimSpace(supplement.AlbumID)
		}
		if strings.TrimSpace(primary[i].Cover) == "" {
			primary[i].Cover = strings.TrimSpace(supplement.Cover)
		}
		if strings.TrimSpace(primary[i].Link) == "" {
			primary[i].Link = strings.TrimSpace(supplement.Link)
		}
		primaryExtra := ensureSongExtra(&primary[i])
		for key, value := range supplement.Extra {
			if strings.TrimSpace(primaryExtra[key]) == "" && strings.TrimSpace(value) != "" {
				primaryExtra[key] = value
			}
		}
	}
	return primary
}

func findMatchingSearchSong(results []model.Song, primaryHash, albumID, name, artist string) *model.Song {
	for i := range results {
		candidate := &results[i]
		candidateHash := normalizeHash(firstNonEmpty(candidate.Extra["hash"], candidate.ID))
		if primaryHash != "" && candidateHash != "" && candidateHash == primaryHash {
			return candidate
		}
	}
	for i := range results {
		candidate := &results[i]
		if strings.TrimSpace(candidate.AlbumID) == "" || albumID == "" {
			continue
		}
		if strings.TrimSpace(candidate.AlbumID) == albumID && strings.EqualFold(strings.TrimSpace(candidate.Name), name) && strings.EqualFold(strings.TrimSpace(candidate.Artist), artist) {
			return candidate
		}
	}
	for i := range results {
		candidate := &results[i]
		if strings.EqualFold(strings.TrimSpace(candidate.Name), name) && strings.EqualFold(strings.TrimSpace(candidate.Artist), artist) {
			return candidate
		}
	}
	return nil
}

func (c *Client) searchSongs(ctx context.Context, keyword string, limit int) ([]model.Song, error) {
	params := url.Values{}
	params.Set("keyword", keyword)
	params.Set("platform", "WebFilter")
	params.Set("format", "json")
	params.Set("page", "1")
	if limit > 0 {
		params.Set("pagesize", strconv.Itoa(limit))
	} else {
		params.Set("pagesize", "10")
	}
	apiURL := "http://songsearch.kugou.com/song_search_v2?" + params.Encode()
	var resp kugouSearchResponse
	if err := c.doJSONRequest(ctx, http.MethodGet, apiURL, nil, nil, map[string]string{
		"User-Agent": "Mozilla/5.0 (Linux; Android 10; SM-G981B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.162 Mobile Safari/537.36",
		"Cookie":     c.baseCookie(),
	}, &resp); err != nil {
		return nil, err
	}
	results := make([]model.Song, 0, len(resp.Data.Lists))
	for _, item := range resp.Data.Lists {
		primaryHash := firstNonEmpty(item.FileHash, item.HQFileHash, item.SQFileHash, item.ResFileHash, item.TransParam.Ogg320Hash, item.TransParam.Ogg128Hash)
		if normalizeHash(primaryHash) == "" {
			continue
		}
		size := parseKugouInt64(item.FileSize)
		switch normalizeHash(primaryHash) {
		case normalizeHash(item.SQFileHash):
			if item.SQFileSize > 0 {
				size = item.SQFileSize
			}
		case normalizeHash(item.HQFileHash):
			if item.HQFileSize > 0 {
				size = item.HQFileSize
			}
		case normalizeHash(item.ResFileHash):
			if item.ResFileSize > 0 {
				size = item.ResFileSize
			}
		case normalizeHash(item.TransParam.Ogg320Hash):
			if item.TransParam.Ogg320FileSize > 0 {
				size = item.TransParam.Ogg320FileSize
			}
		case normalizeHash(item.TransParam.Ogg128Hash):
			if item.TransParam.Ogg128FileSize > 0 {
				size = item.TransParam.Ogg128FileSize
			}
		}
		bitrate := 0
		if item.Duration > 0 && size > 0 {
			bitrate = int(size * 8 / 1000 / int64(item.Duration))
		}
		singerIDs := formatKugouIDList(firstNonEmpty(formatAnyIDList(item.SingerID), formatAnyIDList(item.TransParam.SingerID)))
		shareChain := resolveSongShareChain(formatAnyNumericString(item.MixSongID), formatAnyNumericString(item.AudioID))
		song := model.Song{
			Source:   "kugou",
			ID:       normalizeHash(primaryHash),
			Name:     strings.TrimSpace(item.SongName),
			Artist:   strings.TrimSpace(item.SingerName),
			Album:    strings.TrimSpace(item.AlbumName),
			AlbumID:  strings.TrimSpace(item.AlbumID),
			Duration: item.Duration,
			Size:     size,
			Bitrate:  bitrate,
			Cover:    normalizeSizedCover(item.Image),
			Link:     buildShareTrackLink(shareChain, primaryHash, item.AlbumID, formatAnyNumericString(item.MixSongID)),
			Extra: map[string]string{
				"hash":         normalizeHash(primaryHash),
				"file_hash":    normalizeHash(item.FileHash),
				"hq_hash":      normalizeHash(item.HQFileHash),
				"sq_hash":      normalizeHash(item.SQFileHash),
				"res_hash":     normalizeHash(item.ResFileHash),
				"ogg_320_hash": normalizeHash(item.TransParam.Ogg320Hash),
				"ogg_128_hash": normalizeHash(item.TransParam.Ogg128Hash),
				"audio_id":     formatAnyNumericString(item.AudioID),
				"mix_song_id":  formatAnyNumericString(item.MixSongID),
				"share_chain":  shareChain,
				"album_id":     strings.TrimSpace(item.AlbumID),
				"privilege":    strconv.Itoa(item.Privilege),
				"singer_ids":   singerIDs,
			},
		}
		results = append(results, song)
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (c *Client) fetchTrackByShareChain(ctx context.Context, chain string) (*model.Song, error) {
	chain = normalizeShareChain(chain)
	if chain == "" {
		return nil, platform.NewNotFoundError("kugou", "track", "")
	}
	resolvedURL, err := c.resolveShareChainURL(ctx, chain)
	if err != nil {
		return nil, wrapError("kugou", "track", chain, err)
	}
	parsed, err := url.Parse(strings.TrimSpace(resolvedURL))
	if err != nil {
		return nil, wrapError("kugou", "track", chain, err)
	}
	hash := normalizeHash(parsed.Query().Get("hash"))
	if hash == "" {
		return nil, platform.NewNotFoundError("kugou", "track", chain)
	}
	song, err := c.fetchGatewayTrackInfo(ctx, hash)
	if err != nil {
		return nil, err
	}
	if song == nil {
		return nil, platform.NewNotFoundError("kugou", "track", chain)
	}
	extra := ensureSongExtra(song)
	extra["share_chain"] = chain
	if albumID := strings.TrimSpace(parsed.Query().Get("album_id")); albumID != "" {
		song.AlbumID = firstNonEmpty(song.AlbumID, albumID)
		extra["album_id"] = firstNonEmpty(extra["album_id"], albumID)
	}
	if albumAudioID := strings.TrimSpace(parsed.Query().Get("album_audio_id")); albumAudioID != "" {
		extra["album_audio_id"] = firstNonEmpty(extra["album_audio_id"], albumAudioID)
	}
	song.Link = buildShareTrackLink(chain, song.ID, song.AlbumID, firstNonEmpty(extra["album_audio_id"], strings.TrimSpace(parsed.Query().Get("album_audio_id"))))
	return song, nil
}

func (c *Client) resolveShareChainURL(ctx context.Context, chain string) (string, error) {
	shareURL := "https://www.kugou.com/share/" + chain + ".html"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shareURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client := c.htmlHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	text := string(bodyBytes)
	hash := extractHTMLValue(text, `"hash":"([A-Fa-f0-9]{32})"`)
	albumID := extractHTMLValue(text, `"album_id":"?(\d+)"?`)
	albumAudioID := extractHTMLValue(text, `"(?:encode_)?album_audio_id":"?([A-Za-z0-9]+)"?`)
	values := url.Values{}
	if hash != "" {
		values.Set("hash", strings.ToLower(hash))
	}
	if albumID != "" {
		values.Set("album_id", albumID)
	}
	if albumAudioID != "" {
		values.Set("album_audio_id", albumAudioID)
	}
	if len(values) > 0 {
		return "https://h5.kugou.com/v2/v-5a15aeb1/index.html?" + values.Encode(), nil
	}
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String(), nil
	}
	return shareURL, nil
}

func resolveSongShareChain(candidates ...string) string {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if len(candidate) <= 12 && normalizeShareChain(candidate) != "" {
			return candidate
		}
	}
	return ""
}

func buildShareTrackLink(chain, hash, albumID, albumAudioID string) string {
	if chain = normalizeShareChain(chain); chain != "" {
		return "https://www.kugou.com/share/" + chain + ".html"
	}
	return buildTrackLinkWithAlbum(hash, albumID)
}

func extractHTMLValue(text, pattern string) string {
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(text)
	if len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func extractPlaylistGCID(text string) string {
	patterns := []string{
		`gcid_[A-Za-z0-9]+`,
		`"encode_gic"\s*:\s*"(gcid_[A-Za-z0-9]+)"`,
		`"encode_src_gid"\s*:\s*"(gcid_[A-Za-z0-9]+)"`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		match := re.FindStringSubmatch(text)
		if len(match) == 1 {
			return strings.TrimSpace(match[0])
		}
		if len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func extractPlaylistSpecialID(text string) string {
	patterns := []string{
		`specialid[^\d]{0,16}(\d{3,})`,
		`"special_id"\s*:\s*"?(\d{3,})"?`,
		`"specialid"\s*:\s*"?(\d{3,})"?`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		match := re.FindStringSubmatch(text)
		if len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func (c *Client) fetchSignedPlayURL(ctx context.Context, song *model.Song, plan kugouDownloadPlan) (string, error) {
	if !c.HasVIPDownloadCookie() {
		return "", fmt.Errorf("kugou v5 url requires cookie t and KugooID")
	}
	extra := ensureSongExtra(song)
	albumID := firstNonEmpty(extra["album_id"], song.AlbumID)
	albumAudioID := firstNonEmpty(extra["album_audio_id"])
	if albumID == "" || albumAudioID == "" || plan.Hash == "" {
		return "", fmt.Errorf("kugou v5 url missing required metadata")
	}
	cookie := c.baseCookie()
	userID := parseCookieValue(cookie, "KugooID")
	token := parseCookieValue(cookie, "t")
	params := map[string]string{
		"album_id":       albumID,
		"userid":         userID,
		"area_code":      "1",
		"hash":           plan.Hash,
		"mid":            kugouGatewayMid,
		"appid":          kugouGatewayAppID,
		"ssa_flag":       "is_fromtrack",
		"clientver":      kugouPlayClientVer,
		"token":          token,
		"album_audio_id": albumAudioID,
		"behavior":       "play",
		"clienttime":     strconv.FormatInt(time.Now().Unix(), 10),
		"pid":            "2",
		"key":            buildPlayKey(plan.Hash, userID),
		"quality":        qualityCode(plan.Quality),
		"version":        kugouPlayClientVer,
		"dfid":           "-",
		"pidversion":     "3001",
	}
	requestURL := signKugouRequestURL(kugouGatewayPlayURL, params)
	var resp kugouPlayURLResponse
	if err := c.doJSONRequest(ctx, http.MethodGet, requestURL, nil, nil, map[string]string{
		"User-Agent": "Android12-AndroidCar-20089-46-0-NetMusic-wifi",
		"KG-THash":   "255d751",
		"KG-Rec":     "1",
		"KG-RC":      "1",
		"x-router":   "tracker.kugou.com",
	}, &resp); err != nil {
		return "", err
	}
	if resp.Status != 1 || len(resp.URL) == 0 {
		return "", fmt.Errorf("kugou v5 url unavailable, status=%d", resp.Status)
	}
	return strings.TrimSpace(resp.URL[len(resp.URL)-1]), nil
}

func (c *Client) fetchPlayData(ctx context.Context, song *model.Song, plan kugouDownloadPlan) (*kugouPlayDataResponse, error) {
	extra := ensureSongExtra(song)
	params := url.Values{}
	params.Set("r", "play/getdata")
	params.Set("hash", plan.Hash)
	params.Set("album_id", firstNonEmpty(extra["album_id"], song.AlbumID))
	params.Set("mid", kugouGatewayMid)
	apiURL := kugouPlayDataURL + "?" + params.Encode()
	var resp kugouPlayDataResponse
	if err := c.doJSONRequest(ctx, http.MethodGet, apiURL, nil, nil, map[string]string{
		"User-Agent": "Mozilla/5.0",
		"Referer":    "https://www.kugou.com/",
		"Cookie":     c.baseCookie(),
	}, &resp); err != nil {
		return nil, err
	}
	if strings.TrimSpace(resp.URL) == "" && strings.TrimSpace(resp.BackupURL) == "" {
		return nil, fmt.Errorf("kugou play/getdata unavailable, status=%d err=%d", resp.Status, resp.ErrCode)
	}
	return &resp, nil
}

func (c *Client) fetchMobilePlayInfo(ctx context.Context, song *model.Song, plan kugouDownloadPlan) (*kugouMobilePlayInfoResponse, error) {
	extra := ensureSongExtra(song)
	params := url.Values{}
	params.Set("cmd", "playInfo")
	params.Set("hash", plan.Hash)
	if albumID := firstNonEmpty(extra["album_id"], song.AlbumID); albumID != "" {
		params.Set("album_id", albumID)
	}
	apiURL := "https://m.kugou.com/app/i/getSongInfo.php?" + params.Encode()
	var resp kugouMobilePlayInfoResponse
	if err := c.doJSONRequest(ctx, http.MethodGet, apiURL, nil, nil, map[string]string{
		"User-Agent": "Mozilla/5.0",
		"Referer":    "https://www.kugou.com/",
		"Cookie":     c.baseCookie(),
	}, &resp); err != nil {
		return nil, err
	}
	if strings.TrimSpace(resp.URL) == "" && !mobilePlayInfoRequiresAuth(&resp) {
		return nil, fmt.Errorf("kugou mobile playInfo unavailable, status=%d err=%d", resp.Status, resp.ErrCode)
	}
	return &resp, nil
}

func (c *Client) doJSONRequest(ctx context.Context, method, rawURL string, query url.Values, body io.Reader, headers map[string]string, out any) error {
	if query != nil && len(query) > 0 {
		if strings.Contains(rawURL, "?") {
			rawURL += "&" + query.Encode()
		} else {
			rawURL += "?" + query.Encode()
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	client := c.apiRequestHTTPClient(rawURL)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) apiRequestHTTPClient(rawURL string) *http.Client {
	if c != nil {
		if c.apiHTTPClient != nil && shouldProxyKugouAPIRequest(rawURL) {
			return c.apiHTTPClient
		}
		if c.searchHTTPClient != nil && isKugouSearchRequest(rawURL) {
			return c.searchHTTPClient
		}
	}
	return http.DefaultClient
}

func (c *Client) htmlHTTPClient() *http.Client {
	if c != nil && c.apiHTTPClient != nil {
		return c.apiHTTPClient
	}
	return &http.Client{Timeout: 8 * time.Second}
}

func shouldProxyKugouAPIRequest(rawURL string) bool {
	return isKugouSearchRequest(rawURL) ||
		strings.Contains(rawURL, "www.kugou.com/share/") ||
		strings.Contains(rawURL, "www2.kugou.kugou.com/share/") ||
		strings.Contains(rawURL, "www.kugou.com/yy/special/single/") ||
		strings.Contains(rawURL, "mobilecdnbj.kugou.com/api/v3/album/info") ||
		strings.Contains(rawURL, "mobilecdnbj.kugou.com/api/v3/album/song") ||
		strings.Contains(rawURL, "wwwapi.kugou.com/yy/index.php") ||
		strings.Contains(rawURL, "m.kugou.com/app/i/getSongInfo.php") ||
		strings.Contains(rawURL, "gateway.kugou.com/") ||
		strings.Contains(rawURL, "kugouvip.kugou.com/") ||
		strings.Contains(rawURL, "tracker.kugou.com/") ||
		strings.Contains(rawURL, "userservice.kugou.com/") ||
		strings.Contains(rawURL, "login-user.kugou.com/") ||
		strings.Contains(rawURL, "login.user.kugou.com/")
}

func isKugouSearchRequest(rawURL string) bool {
	return strings.Contains(rawURL, "songsearch.kugou.com/song_search_v2") ||
		strings.Contains(rawURL, "complexsearch.kugou.com/v2/search/song") ||
		strings.Contains(rawURL, "mobilecdn.kugou.com/api/v3/search/special") ||
		strings.Contains(rawURL, "mobiles.kugou.com/api/v5/special/info_v2") ||
		strings.Contains(rawURL, "mobiles.kugou.com/api/v5/special/song_v2") ||
		strings.Contains(rawURL, "mobilecdnbj.kugou.com/api/v5/special/info") ||
		strings.Contains(rawURL, "pubsongscdn.kugou.com/v2/get_other_list_file") ||
		strings.Contains(rawURL, "t.kugou.com/v1/songlist/batch_decode")
}

func buildDownloadPlans(song *model.Song, requested platform.Quality) []kugouDownloadPlan {
	extra := ensureSongExtra(song)
	plans := []kugouDownloadPlan{}
	appendPlan := func(hash string, quality platform.Quality, format string, size int64) {
		hash = normalizeHash(hash)
		if hash == "" {
			return
		}
		for _, plan := range plans {
			if plan.Hash == hash {
				return
			}
		}
		plans = append(plans, kugouDownloadPlan{Hash: hash, Quality: quality, Format: format, Size: size})
	}
	appendPlan(extra["res_hash"], platform.QualityHiRes, "flac", 0)
	appendPlan(extra["sq_hash"], platform.QualityLossless, "flac", 0)
	appendPlan(extra["hq_hash"], platform.QualityHigh, "mp3", 0)
	appendPlan(extra["ogg_320_hash"], platform.QualityHigh, "ogg", 0)
	appendPlan(firstNonEmpty(extra["file_hash"], extra["hash"], song.ID), platform.QualityStandard, firstNonEmpty(song.Ext, "mp3"), song.Size)
	appendPlan(extra["ogg_128_hash"], platform.QualityStandard, "ogg", 0)
	if len(plans) == 0 {
		return nil
	}
	start := 0
	for i, plan := range plans {
		if plan.Quality <= requested {
			start = i
			break
		}
	}
	ordered := make([]kugouDownloadPlan, 0, len(plans))
	ordered = append(ordered, plans[start:]...)
	ordered = append(ordered, plans[:start]...)
	return ordered
}

func cloneSongWithHash(song *model.Song, hash string) *model.Song {
	if song == nil {
		return nil
	}
	clone := *song
	if clone.Extra != nil {
		cloneMap := make(map[string]string, len(clone.Extra))
		for key, value := range clone.Extra {
			cloneMap[key] = value
		}
		clone.Extra = cloneMap
	}
	hash = normalizeHash(hash)
	if hash != "" {
		clone.ID = hash
		ensureSongExtra(&clone)["hash"] = hash
		if strings.TrimSpace(clone.Link) == "" {
			clone.Link = buildTrackLink(hash)
		}
	}
	return &clone
}

func applyPlanMetadata(song *model.Song, plan kugouDownloadPlan) {
	if song == nil {
		return
	}
	if strings.TrimSpace(song.Ext) == "" {
		song.Ext = strings.TrimSpace(plan.Format)
	}
	if song.Size <= 0 && plan.Size > 0 {
		song.Size = plan.Size
	}
	if song.Bitrate <= 0 {
		switch plan.Quality {
		case platform.QualityHiRes:
			song.Bitrate = 2400
		case platform.QualityLossless:
			song.Bitrate = 1411
		case platform.QualityHigh:
			song.Bitrate = 320
		default:
			song.Bitrate = 128
		}
	}
}

func applyResolvedSongMetadata(song *model.Song, info *kugouPlayDataResponse, plan kugouDownloadPlan) {
	if song == nil || info == nil {
		return
	}
	song.URL = firstNonEmpty(info.URL, info.BackupURL)
	if fileSize := parseKugouInt64(info.FileSize); fileSize > 0 {
		song.Size = fileSize
	}
	if bitrate := parseKugouInt(info.Bitrate); bitrate > 0 {
		song.Bitrate = bitrate
	}
	if strings.TrimSpace(info.ExtName) != "" {
		song.Ext = strings.TrimSpace(info.ExtName)
	} else {
		applyPlanMetadata(song, plan)
	}
	if strings.TrimSpace(info.AudioName) != "" {
		song.Name = strings.TrimSpace(info.AudioName)
	}
	if strings.TrimSpace(info.AuthorName) != "" {
		song.Artist = strings.TrimSpace(info.AuthorName)
	}
	if strings.TrimSpace(info.AlbumID) != "" {
		song.AlbumID = strings.TrimSpace(info.AlbumID)
	}
	if cover := normalizeSizedCover(info.Img); cover != "" {
		song.Cover = cover
	}
	extra := ensureSongExtra(song)
	extra["play_url"] = song.URL
	if strings.TrimSpace(info.BackupURL) != "" {
		extra["play_backup_url"] = strings.TrimSpace(info.BackupURL)
	}
	if strings.TrimSpace(info.AlbumAudioID) != "" {
		extra["album_audio_id"] = strings.TrimSpace(info.AlbumAudioID)
	}
	extra["resolved_quality"] = plan.Quality.String()
}

func applyMobilePlayInfoMetadata(song *model.Song, info *kugouMobilePlayInfoResponse, plan kugouDownloadPlan) {
	if song == nil || info == nil {
		return
	}
	song.URL = strings.TrimSpace(info.URL)
	if bitrate := parseKugouInt(info.Bitrate); bitrate > 0 {
		song.Bitrate = bitrate
	}
	if duration := normalizeGatewayDuration(parseKugouInt(info.Timelength)); duration > 0 {
		song.Duration = duration
	}
	if strings.TrimSpace(info.ExtName) != "" {
		song.Ext = strings.TrimSpace(info.ExtName)
	} else {
		applyPlanMetadata(song, plan)
	}
	if strings.TrimSpace(info.SongName) != "" {
		song.Name = strings.TrimSpace(info.SongName)
	} else if strings.TrimSpace(info.FileName) != "" {
		song.Name = strings.TrimSpace(info.FileName)
	}
	if strings.TrimSpace(info.AuthorName) != "" {
		song.Artist = strings.TrimSpace(info.AuthorName)
	}
	if albumID := formatAnyNumericString(info.AlbumID); albumID != "" {
		song.AlbumID = albumID
	}
	extra := ensureSongExtra(song)
	extra["play_url"] = song.URL
	extra["resolved_quality"] = plan.Quality.String()
	if albumAudioID := formatAnyNumericString(info.AlbumAudioID); albumAudioID != "" {
		extra["album_audio_id"] = albumAudioID
	}
	if privilege := formatAnyNumericString(info.Privilege); privilege != "" {
		extra["privilege"] = privilege
	}
	if payType := formatAnyNumericString(info.PayType); payType != "" {
		extra["pay_type"] = payType
	}
}

func mobilePlayInfoRequiresAuth(info *kugouMobilePlayInfoResponse) bool {
	if info == nil {
		return false
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(info.Error)), "cookie") {
		return true
	}
	if strings.Contains(strings.TrimSpace(info.Error), "付费") || strings.Contains(strings.TrimSpace(info.Error), "会员") {
		return true
	}
	if parseKugouInt(info.Privilege) > 0 || parseKugouInt(info.PayType) > 0 {
		return true
	}
	return false
}

func preferKugouDownloadError(current, candidate error) error {
	if candidate == nil {
		return current
	}
	if current == nil {
		return candidate
	}
	if errors.Is(candidate, platform.ErrAuthRequired) {
		return candidate
	}
	if errors.Is(current, platform.ErrAuthRequired) {
		return current
	}
	if errors.Is(candidate, platform.ErrRateLimited) && !errors.Is(current, platform.ErrRateLimited) {
		return candidate
	}
	if errors.Is(candidate, platform.ErrUnavailable) && !errors.Is(current, platform.ErrUnavailable) {
		return candidate
	}
	return current
}

func (c *Client) resolveConceptSongURLNew(song *model.Song, plan kugouDownloadPlan, resp *conceptSongURLNewResponse) (*model.Song, bool) {
	if song == nil || resp == nil || len(resp.Data) == 0 {
		return nil, false
	}
	var entries []map[string]any
	if err := json.Unmarshal(resp.Data, &entries); err != nil || len(entries) == 0 {
		return nil, false
	}
	for _, entry := range entries {
		trackerURL := strings.TrimSpace(valueString(entry["tracker_url"]))
		if trackerURL == "" {
			trackerURL = strings.TrimSpace(valueString(entry["url"]))
		}
		if trackerURL == "" {
			continue
		}
		extName := strings.TrimSpace(firstNonEmpty(valueString(entry["extname"]), valueString(entry["ext_name"]), valueString(entry["format"])))
		if extName == "" {
			extName = detectExtFromURL(trackerURL)
		}
		if extName == "" {
			extName = strings.TrimSpace(plan.Format)
		}
		lowerExt := strings.ToLower(extName)
		if lowerExt == "mflac" || lowerExt == "mgg" || lowerExt == "mmp3" || lowerExt == "mogg" {
			continue
		}
		resolved := cloneSongWithHash(song, plan.Hash)
		if resolved == nil {
			return nil, false
		}
		resolved.URL = trackerURL
		applyPlanMetadata(resolved, plan)
		if extName != "" {
			resolved.Ext = extName
		}
		extra := ensureSongExtra(resolved)
		extra["play_url"] = trackerURL
		extra["resolved_quality"] = plan.Quality.String()
		extra["concept_source"] = "song/url/new"
		if token := strings.TrimSpace(valueString(entry["token"])); token != "" {
			extra["concept_tracker_token"] = token
		}
		if ekey := strings.TrimSpace(valueString(entry["en_ekey"])); ekey != "" {
			extra["concept_en_ekey"] = ekey
		}
		return resolved, true
	}
	return nil, false
}

func conceptSongURLNewAuthError(resp *conceptSongURLNewResponse) error {
	if resp == nil {
		return nil
	}
	joined := strings.ToLower(strings.TrimSpace(resp.Error + " " + string(resp.Data)))
	if strings.Contains(joined, "vip") || strings.Contains(joined, "付费") || strings.Contains(joined, "会员") || strings.Contains(joined, "auth") {
		return platform.NewAuthRequiredError("kugou")
	}
	if resp.ErrCode == 20018 || resp.ErrCode == 20010 {
		return platform.NewAuthRequiredError("kugou")
	}
	return nil
}

func signKugouRequestURL(baseURL string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var sigBuilder strings.Builder
	var queryBuilder strings.Builder
	for i, key := range keys {
		value := params[key]
		sigBuilder.WriteString(key)
		sigBuilder.WriteByte('=')
		sigBuilder.WriteString(value)
		if i > 0 {
			queryBuilder.WriteByte('&')
		}
		queryBuilder.WriteString(url.QueryEscape(key))
		queryBuilder.WriteByte('=')
		queryBuilder.WriteString(url.QueryEscape(value))
	}
	sum := md5.Sum([]byte(kugouPlaySignKey + sigBuilder.String() + kugouPlaySignKey))
	queryBuilder.WriteString("&signature=")
	queryBuilder.WriteString(hex.EncodeToString(sum[:]))
	return baseURL + "?" + queryBuilder.String()
}

func buildPlayKey(hash, userID string) string {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(hash)) + kugouPlayPidVerSec + kugouGatewayAppID + kugouGatewayMid + strings.TrimSpace(userID)))
	return hex.EncodeToString(sum[:])
}

func qualityCode(q platform.Quality) string {
	switch q {
	case platform.QualityHiRes:
		return "high"
	case platform.QualityLossless:
		return "flac"
	case platform.QualityHigh:
		return "320"
	default:
		return "128"
	}
}

func normalizeGatewayDuration(value int) int {
	if value <= 0 {
		return 0
	}
	if value > 1000 {
		return value / 1000
	}
	return value
}

func normalizeGatewayBitrate(value int) int {
	if value <= 0 {
		return 0
	}
	if value > 1000 {
		return value / 1000
	}
	return value
}

func normalizeSizedCover(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.Replace(value, "{size}", "480", 1)
}

func choosePositive(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func formatAnyNumericString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}

func parseKugouInt64(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func parseKugouInt(value any) int {
	return int(parseKugouInt64(value))
}

func formatAnyIDList(value any) string {
	switch v := value.(type) {
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := formatAnyNumericString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ",")
	case string:
		return strings.TrimSpace(v)
	default:
		return formatAnyNumericString(value)
	}
}

func formatKugouIDList(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '/', '、', '[', ']', ' ':
			return true
		default:
			return false
		}
	})
	result := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, err := strconv.ParseInt(part, 10, 64); err != nil {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return strings.Join(result, ",")
}

func ensureSongExtra(song *model.Song) map[string]string {
	if song == nil {
		return nil
	}
	if song.Extra == nil {
		song.Extra = make(map[string]string)
	}
	return song.Extra
}

func parseCookieValue(cookie, key string) string {
	if strings.TrimSpace(cookie) == "" || strings.TrimSpace(key) == "" {
		return ""
	}
	parts := strings.Split(cookie, ";")
	for _, part := range parts {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 {
			continue
		}
		if http.CanonicalHeaderKey(strings.TrimSpace(pair[0])) == http.CanonicalHeaderKey(strings.TrimSpace(key)) {
			return strings.TrimSpace(pair[1])
		}
	}
	return ""
}

func randomHex(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}
