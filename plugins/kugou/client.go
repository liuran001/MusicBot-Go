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
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const (
	kugouGatewaySongInfoURL = "https://gateway.kugou.com/v3/album_audio/audio"
	kugouGatewayPlayURL     = "https://gateway.kugou.com/v5/url"
	kugouPlayDataURL        = "https://wwwapi.kugou.com/yy/index.php"
	kugouGatewayAppID       = "1005"
	kugouGatewayClientVer   = "11451"
	kugouPlayClientVer      = "20349"
	kugouGatewayMid         = "211008"
	kugouGatewaySignKey     = "OIlwlieks28dk2k092lksi2UIkp"
	kugouPlaySignKey        = "NVPh5oo715z5DIWAeQlhMDsWXXQV4hwt"
	kugouPlayPidVerSec      = "57ae12eb6890223e355ccfcb74edf70d"
)

type Client struct {
	api     *kugoulib.Kugou
	cookie  string
	logger  bot.Logger
	concept *ConceptSessionManager
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
	if strings.TrimSpace(song.URL) == "" {
		url, songInfoErr := c.api.GetDownloadURLBySonginfo(song)
		if songInfoErr == nil && strings.TrimSpace(url) != "" {
			song.URL = strings.TrimSpace(url)
			ensureSongExtra(song)["play_url"] = song.URL
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
		ensureSongExtra(song)["play_url"] = song.URL
		if strings.TrimSpace(song.Ext) == "" {
			song.Ext = detectExtFromURL(song.URL)
		}
	}
	return song, nil
}

func (c *Client) ResolveDownloadByQuality(ctx context.Context, song *model.Song, requested platform.Quality) (*model.Song, error) {
	if song == nil {
		return nil, platform.NewNotFoundError("kugou", "track", "")
	}
	plans := buildDownloadPlans(song, requested)
	var lastErr error
	if c.concept != nil && c.concept.HasUsableSession() {
		for _, plan := range plans {
			resolved, err := c.fetchConceptSongURL(ctx, song, plan)
			if err == nil && resolved != nil && strings.TrimSpace(resolved.URL) != "" {
				return resolved, nil
			}
			if err != nil {
				lastErr = preferKugouDownloadError(lastErr, wrapError("kugou", "track", strings.TrimSpace(song.ID), err))
			}
			if newResp, newErr := c.concept.FetchSongURLNew(ctx, song, plan); newErr != nil {
				lastErr = preferKugouDownloadError(lastErr, wrapError("kugou", "track", strings.TrimSpace(song.ID), newErr))
			} else if resolvedNew, ok := c.resolveConceptSongURLNew(song, plan, newResp); ok {
				return resolvedNew, nil
			} else if authErr := conceptSongURLNewAuthError(newResp); authErr != nil {
				lastErr = preferKugouDownloadError(lastErr, authErr)
			}
		}
	}
	for _, plan := range plans {
		resolved := cloneSongWithHash(song, plan.Hash)
		if resolved == nil {
			continue
		}
		ensureSongExtra(resolved)["resolved_quality"] = plan.Quality.String()
		if urlValue, err := c.fetchSignedPlayURL(ctx, resolved, plan); err == nil && strings.TrimSpace(urlValue) != "" {
			resolved.URL = strings.TrimSpace(urlValue)
			applyPlanMetadata(resolved, plan)
			ensureSongExtra(resolved)["play_url"] = resolved.URL
			return resolved, nil
		} else if err != nil {
			lastErr = preferKugouDownloadError(lastErr, wrapError("kugou", "track", strings.TrimSpace(song.ID), err))
		}
		if info, err := c.fetchPlayData(ctx, resolved, plan); err == nil && info != nil && strings.TrimSpace(info.URL) != "" {
			applyResolvedSongMetadata(resolved, info, plan)
			return resolved, nil
		} else if err != nil {
			lastErr = preferKugouDownloadError(lastErr, wrapError("kugou", "track", strings.TrimSpace(song.ID), err))
		}
		if info, err := c.fetchMobilePlayInfo(ctx, resolved, plan); err == nil && info != nil {
			if strings.TrimSpace(info.URL) != "" {
				applyMobilePlayInfoMetadata(resolved, info, plan)
				return resolved, nil
			}
			if mobilePlayInfoRequiresAuth(info) {
				lastErr = preferKugouDownloadError(lastErr, platform.NewAuthRequiredError("kugou"))
			}
		} else if err != nil {
			lastErr = preferKugouDownloadError(lastErr, wrapError("kugou", "track", strings.TrimSpace(song.ID), err))
		}
	}
	if strings.TrimSpace(song.URL) != "" {
		clone := cloneSongWithHash(song, strings.TrimSpace(song.ID))
		ensureSongExtra(clone)["resolved_quality"] = requested.String()
		return clone, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, platform.NewUnavailableError("kugou", "track", strings.TrimSpace(song.ID))
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
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
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
	resp, err := http.DefaultClient.Do(req)
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
