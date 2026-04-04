package soda

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const (
	sodaUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"
	sodaPCChannel = "pc_web"
	sodaAid       = "386088"
)

var (
	sodaExtractLosslessFLAC = extractSodaLosslessFLAC
	sodaRewriteLosslessFLAC = rewriteSodaLosslessFLAC
	sodaValidateAudioFile   = validateSodaAudioFile
	sodaEnsurePlayableFLAC  = ensureSodaPlayableLosslessFLAC
	sodaProbeAudioCodec     = probeAudioCodec
	sodaDecryptAudio        = decryptSodaAudio
	sodaDecryptAudioWithLog = decryptSodaAudioWithLogger
)

type Client struct {
	httpClient  *http.Client
	cookie      string
	logger      bot.Logger
	persistFunc func(map[string]string) error
}

type sodaSearchResponse struct {
	ResultGroups []struct {
		Data []struct {
			Entity struct {
				Track sodaTrack `json:"track"`
			} `json:"entity"`
		} `json:"data"`
	} `json:"result_groups"`
}

type sodaTrackV2Response struct {
	TrackInfo   sodaTrack `json:"track_info"`
	Track       sodaTrack `json:"track"`
	TrackPlayer struct {
		URLPlayerInfo string `json:"url_player_info"`
	} `json:"track_player"`
	Lyric struct {
		Content string `json:"content"`
	} `json:"lyric"`
}

type sodaPlayInfoResponse struct {
	Result struct {
		Data struct {
			PlayInfoList []sodaPlayInfo `json:"PlayInfoList"`
		} `json:"Data"`
	} `json:"Result"`
}

type sodaPlaylistDetailResponse struct {
	Playlist       sodaPlaylistMeta    `json:"playlist"`
	MediaResources []sodaPlaylistEntry `json:"media_resources"`
}

type sodaPlaylistSearchResponse struct {
	ResultGroups []struct {
		Data []struct {
			Entity struct {
				Playlist sodaPlaylistMeta `json:"playlist"`
			} `json:"entity"`
		} `json:"data"`
	} `json:"result_groups"`
}

type sodaSharePageData struct {
	LoaderData map[string]json.RawMessage `json:"loaderData"`
}

type sodaShareAlbumPayload struct {
	AlbumInfo sodaAlbumMeta `json:"albumInfo"`
	TrackList []sodaTrack   `json:"trackList"`
}

type sodaShareArtistPayload struct {
	ArtistInfo sodaArtistMeta `json:"artistInfo"`
	TrackList  []sodaTrack    `json:"trackList"`
}

type sodaAlbumMeta struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Intro       string           `json:"intro"`
	Desc        string           `json:"desc"`
	ReleaseDate sodaFlexibleDate `json:"release_date"`
	CountTracks int              `json:"count_tracks"`
	TrackCount  int              `json:"track_count"`
	Artists     []struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"artists"`
	URLCover struct {
		URLs []string `json:"urls"`
		URI  string   `json:"uri"`
	} `json:"url_cover"`
}

type sodaFlexibleDate string

func (d *sodaFlexibleDate) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		*d = ""
		return nil
	}
	if len(data) >= 2 && data[0] == '"' && data[len(data)-1] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*d = sodaFlexibleDate(strings.TrimSpace(value))
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*d = sodaFlexibleDate(number.String())
	return nil
}

func (d sodaFlexibleDate) String() string {
	return strings.TrimSpace(string(d))
}

type sodaArtistMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CountTracks int    `json:"count_tracks"`
	TrackCount  int    `json:"track_count"`
	URLCover    struct {
		URLs []string `json:"urls"`
		URI  string   `json:"uri"`
	} `json:"url_cover"`
	Avatar struct {
		URLs []string `json:"urls"`
		URI  string   `json:"uri"`
	} `json:"avatar"`
	AvatarThumb struct {
		URLs []string `json:"urls"`
		URI  string   `json:"uri"`
	} `json:"avatar_thumb"`
	AvatarMedium struct {
		URLs []string `json:"urls"`
		URI  string   `json:"uri"`
	} `json:"avatar_medium"`
}

type sodaTrack struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Duration int    `json:"duration"`
	Artists  []struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"artists"`
	Album struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		URLCover struct {
			URLs []string `json:"urls"`
			URI  string   `json:"uri"`
		} `json:"url_cover"`
	} `json:"album"`
	BitRates []struct {
		Size    int64  `json:"size"`
		Quality string `json:"quality"`
	} `json:"bit_rates"`
	AudioInfo struct {
		PlayInfoList []sodaPlayInfo `json:"play_info_list"`
	} `json:"audio_info"`
}

type sodaPlayInfo struct {
	MainPlayURL   string  `json:"MainPlayUrl"`
	BackupPlayURL string  `json:"BackupPlayUrl"`
	PlayAuth      string  `json:"PlayAuth"`
	Size          int64   `json:"Size"`
	Bitrate       int     `json:"Bitrate"`
	Format        string  `json:"Format"`
	Quality       string  `json:"Quality"`
	Duration      float64 `json:"Duration"`
}

type sodaPlaylistMeta struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Desc        string `json:"desc"`
	CountTracks int    `json:"count_tracks"`
	TrackCount  int    `json:"track_count"`
	Owner       struct {
		Nickname   string `json:"nickname"`
		PublicName string `json:"public_name"`
	} `json:"owner"`
	URLCover struct {
		URLs []string `json:"urls"`
		URI  string   `json:"uri"`
	} `json:"url_cover"`
}

type sodaPlaylistEntry struct {
	Type   string `json:"type"`
	Entity struct {
		TrackWrapper struct {
			Track sodaTrack `json:"track"`
		} `json:"track_wrapper"`
	} `json:"entity"`
}

func NewClient(cookie string, timeout time.Duration, logger bot.Logger) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		cookie:     strings.TrimSpace(cookie),
		logger:     logger,
	}
}

func (c *Client) SetAPIProxy(cfg httpproxy.Config) error {
	if c == nil {
		return nil
	}
	timeout := 15 * time.Second
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

func (c *Client) Search(ctx context.Context, keyword string, limit int) ([]platform.Track, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, platform.NewNotFoundError("soda", "search", "")
	}
	if limit <= 0 {
		limit = 10
	}
	params := url.Values{}
	params.Set("q", keyword)
	params.Set("cursor", "0")
	params.Set("search_method", "input")
	params.Set("aid", sodaAid)
	params.Set("device_platform", "web")
	params.Set("channel", sodaPCChannel)
	body, err := c.getJSON(ctx, "https://api.qishui.com/luna/pc/search/track?"+params.Encode())
	if err != nil {
		return nil, err
	}
	var resp sodaSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("soda: parse search response: %w", err)
	}
	if len(resp.ResultGroups) == 0 {
		return nil, nil
	}
	tracks := make([]platform.Track, 0, limit)
	for _, item := range resp.ResultGroups[0].Data {
		track := convertSodaTrack(item.Entity.Track)
		if track.ID == "" {
			continue
		}
		tracks = append(tracks, track)
		if len(tracks) >= limit {
			break
		}
	}
	return tracks, nil
}

func (c *Client) GetTrack(ctx context.Context, trackID string) (*platform.Track, string, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return nil, "", platform.NewNotFoundError("soda", "track", trackID)
	}
	params := url.Values{}
	params.Set("track_id", trackID)
	params.Set("media_type", "track")
	params.Set("aid", sodaAid)
	params.Set("device_platform", "web")
	params.Set("channel", sodaPCChannel)
	body, err := c.getJSON(ctx, "https://api.qishui.com/luna/pc/track_v2?"+params.Encode())
	if err != nil {
		return nil, "", err
	}
	var resp sodaTrackV2Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", fmt.Errorf("soda: parse track_v2 response: %w", err)
	}
	trackData := resp.TrackInfo
	if strings.TrimSpace(trackData.ID) == "" {
		trackData = resp.Track
	}
	track := convertSodaTrack(trackData)
	if track.ID == "" {
		return nil, "", platform.NewNotFoundError("soda", "track", trackID)
	}
	return &track, parseSodaLyric(resp.Lyric.Content), nil
}

func (c *Client) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return nil, platform.NewNotFoundError("soda", "playlist", playlistID)
	}
	offset := platform.PlaylistOffsetFromContext(ctx)
	if offset < 0 {
		offset = 0
	}
	limit := platform.PlaylistLimitFromContext(ctx)
	const defaultChunkSize = 20
	cursor := offset
	if limit <= 0 {
		cursor = 0
	}
	var (
		playlist *platform.Playlist
		tracks   []platform.Track
		seen     = map[string]struct{}{}
	)
	for {
		cnt := defaultChunkSize
		if limit > 0 {
			remaining := limit - len(tracks)
			if remaining <= 0 {
				break
			}
			if remaining < cnt {
				cnt = remaining
			}
		}
		params := url.Values{}
		params.Set("playlist_id", playlistID)
		params.Set("cursor", strconv.Itoa(cursor))
		params.Set("cnt", strconv.Itoa(cnt))
		params.Set("aid", sodaAid)
		params.Set("device_platform", "web")
		params.Set("channel", sodaPCChannel)
		body, err := c.getJSON(ctx, "https://api.qishui.com/luna/pc/playlist/detail?"+params.Encode())
		if err != nil {
			return nil, err
		}
		var resp sodaPlaylistDetailResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("soda: parse playlist response: %w", err)
		}
		if playlist == nil {
			playlist = convertSodaPlaylist(resp.Playlist)
			playlist.ID = playlistID
		}
		if playlist.TrackCount <= 0 {
			playlist.TrackCount = maxInt(resp.Playlist.TrackCount, resp.Playlist.CountTracks)
		}
		pageAdded := 0
		for _, item := range resp.MediaResources {
			if item.Type != "track" {
				continue
			}
			track := convertSodaTrack(item.Entity.TrackWrapper.Track)
			if track.ID == "" {
				continue
			}
			if _, ok := seen[track.ID]; ok {
				continue
			}
			seen[track.ID] = struct{}{}
			tracks = append(tracks, track)
			pageAdded++
		}
		if playlist == nil {
			break
		}
		cursor += cnt
		if pageAdded == 0 {
			break
		}
		if limit > 0 && len(tracks) >= limit {
			break
		}
		if playlist.TrackCount > 0 && cursor >= playlist.TrackCount {
			break
		}
	}
	if playlist == nil {
		return nil, platform.NewNotFoundError("soda", "playlist", playlistID)
	}
	playlist.Tracks = tracks
	if playlist.TrackCount <= 0 {
		if offset > 0 {
			playlist.TrackCount = offset + len(tracks)
		} else {
			playlist.TrackCount = len(tracks)
		}
	}
	return playlist, nil
}

func (c *Client) SearchPlaylist(ctx context.Context, keyword string, limit int) ([]platform.Playlist, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	params := url.Values{}
	params.Set("q", keyword)
	params.Set("cursor", "0")
	params.Set("search_method", "input")
	params.Set("aid", sodaAid)
	params.Set("device_platform", "web")
	params.Set("channel", sodaPCChannel)
	body, err := c.getJSON(ctx, "https://api.qishui.com/luna/pc/search/playlist?"+params.Encode())
	if err != nil {
		return nil, err
	}
	var resp sodaPlaylistSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("soda: parse playlist search response: %w", err)
	}
	if len(resp.ResultGroups) == 0 {
		return nil, nil
	}
	playlists := make([]platform.Playlist, 0, limit)
	for _, item := range resp.ResultGroups[0].Data {
		pl := convertSodaPlaylist(item.Entity.Playlist)
		if pl.ID == "" {
			continue
		}
		playlists = append(playlists, *pl)
		if len(playlists) >= limit {
			break
		}
	}
	return playlists, nil
}

func (c *Client) GetAlbum(ctx context.Context, albumID string) (*platform.Album, []platform.Track, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return nil, nil, platform.NewNotFoundError("soda", "album", albumID)
	}
	offset := platform.PlaylistOffsetFromContext(ctx)
	if offset < 0 {
		offset = 0
	}
	limit := platform.PlaylistLimitFromContext(ctx)
	rawURL := "https://music.douyin.com/qishui/share/album?album_id=" + url.QueryEscape(albumID)
	page, err := c.fetchHTML(ctx, rawURL)
	if err != nil {
		return nil, nil, err
	}
	routerData, err := extractSodaRouterData(page)
	if err != nil {
		return nil, nil, err
	}
	var payload sodaShareAlbumPayload
	if len(routerData.LoaderData) == 0 {
		return nil, nil, platform.NewNotFoundError("soda", "album", albumID)
	}
	if raw, ok := routerData.LoaderData["album_page"]; ok {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, nil, fmt.Errorf("soda: parse album page payload: %w", err)
		}
	} else {
		for _, raw := range routerData.LoaderData {
			if err := json.Unmarshal(raw, &payload); err == nil && (payload.AlbumInfo.ID != "" || len(payload.TrackList) > 0) {
				break
			}
		}
	}
	if payload.AlbumInfo.ID == "" {
		payload.AlbumInfo.ID = albumID
	}
	album := convertSodaAlbum(payload.AlbumInfo)
	if album == nil {
		return nil, nil, platform.NewNotFoundError("soda", "album", albumID)
	}
	tracks := make([]platform.Track, 0, len(payload.TrackList))
	for _, item := range payload.TrackList {
		track := convertSodaTrack(item)
		if track.ID == "" {
			continue
		}
		if track.Album == nil {
			track.Album = album
		}
		tracks = append(tracks, track)
	}
	album.TrackCount = maxInt(payload.AlbumInfo.TrackCount, payload.AlbumInfo.CountTracks)
	if album.TrackCount <= 0 {
		album.TrackCount = len(tracks)
	}
	return album, sliceTracksByOffsetLimit(tracks, offset, limit), nil
}

func (c *Client) GetArtist(ctx context.Context, artistID string) (*platform.Artist, int, error) {
	artistID = strings.TrimSpace(artistID)
	if artistID == "" {
		return nil, 0, platform.NewNotFoundError("soda", "artist", artistID)
	}
	rawURL := "https://music.douyin.com/qishui/share/artist?artist_id=" + url.QueryEscape(artistID)
	page, err := c.fetchHTML(ctx, rawURL)
	if err != nil {
		return nil, 0, err
	}
	routerData, err := extractSodaRouterData(page)
	if err != nil {
		return nil, 0, err
	}
	var payload sodaShareArtistPayload
	if len(routerData.LoaderData) == 0 {
		return nil, 0, platform.NewNotFoundError("soda", "artist", artistID)
	}
	if raw, ok := routerData.LoaderData["artist_page"]; ok {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, 0, fmt.Errorf("soda: parse artist page payload: %w", err)
		}
	} else {
		for _, raw := range routerData.LoaderData {
			if err := json.Unmarshal(raw, &payload); err == nil && (payload.ArtistInfo.ID != "" || payload.ArtistInfo.Name != "" || len(payload.TrackList) > 0) {
				break
			}
		}
	}
	if payload.ArtistInfo.ID == "" {
		payload.ArtistInfo.ID = artistID
	}
	artist, trackCount := convertSodaArtist(payload.ArtistInfo)
	if artist == nil {
		return nil, 0, platform.NewNotFoundError("soda", "artist", artistID)
	}
	if trackCount <= 0 {
		trackCount = len(payload.TrackList)
	}
	return artist, trackCount, nil
}

func (c *Client) DownloadAndDecrypt(ctx context.Context, info *platform.DownloadInfo, destPath string, progress func(written, total int64)) (int64, error) {
	if info == nil || strings.TrimSpace(info.URL) == "" {
		return 0, fmt.Errorf("download info missing")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return 0, err
	}
	urls := append([]string{info.URL}, info.CandidateURLs...)
	var lastErr error
	for _, rawURL := range urls {
		written, err := c.downloadAndDecryptOnce(ctx, rawURL, info, destPath, progress)
		if err == nil {
			return written, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return 0, fmt.Errorf("soda: no download url available")
}

func (c *Client) FetchDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return nil, platform.NewNotFoundError("soda", "track", trackID)
	}
	params := url.Values{}
	params.Set("track_id", trackID)
	params.Set("media_type", "track")
	params.Set("aid", sodaAid)
	params.Set("device_platform", "web")
	params.Set("channel", sodaPCChannel)
	body, err := c.getJSON(ctx, "https://api.qishui.com/luna/pc/track_v2?"+params.Encode())
	if err != nil {
		return nil, err
	}
	var resp sodaTrackV2Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("soda: parse track_v2 response: %w", err)
	}
	trackData := resp.TrackInfo
	if strings.TrimSpace(trackData.ID) == "" {
		trackData = resp.Track
	}
	if strings.TrimSpace(trackData.ID) == "" {
		return nil, platform.NewNotFoundError("soda", "track", trackID)
	}
	playerInfoURL := strings.TrimSpace(resp.TrackPlayer.URLPlayerInfo)
	if playerInfoURL == "" {
		return nil, fmt.Errorf("soda: player info url missing")
	}
	playerInfoURL = strings.TrimSpace(resp.TrackPlayer.URLPlayerInfo)
	playInfos, err := c.fetchPlayInfos(ctx, playerInfoURL)
	if err != nil {
		return nil, fmt.Errorf("soda: fetch play infos: %w", err)
	}
	if (quality == platform.QualityLossless || quality == platform.QualityHiRes) && len(playInfos) == 1 && strings.EqualFold(strings.TrimSpace(playInfos[0].Quality), "higher") {
		if c.logger != nil {
			c.logger.Debug("soda: single higher stream returned for high-tier request, probing signed url directly", "track_id", trackID, "requested_quality", quality.String())
		}
		if fallbackInfos, fallbackErr := c.fetchPlayInfosBySignedURL(ctx, playerInfoURL); fallbackErr == nil && len(fallbackInfos) > 0 {
			playInfos = fallbackInfos
		}
	}
	if len(playInfos) == 0 {
		return nil, platform.NewUnavailableError("soda", "track", trackID)
	}
	for i := range playInfos {
		playInfos[i].Quality = strings.ToLower(strings.TrimSpace(playInfos[i].Quality))
	}
	if c.logger != nil {
		choices := make([]string, 0, len(playInfos))
		for _, item := range playInfos {
			choices = append(choices, fmt.Sprintf("%s/%s/%d/%d", strings.TrimSpace(item.Quality), strings.TrimSpace(item.Format), item.Bitrate, item.Size))
		}
		c.logger.Debug("soda: available play infos", "track_id", trackID, "choices", strings.Join(choices, ","), "requested_quality", quality.String())
	}
	playInfo := selectSodaPlayInfo(playInfos, quality)
	if playInfo == nil {
		return nil, platform.NewUnavailableError("soda", "track", trackID)
	}
	if c.logger != nil {
		c.logger.Debug("soda: selected play info", "track_id", trackID, "quality_label", playInfo.Quality, "format", playInfo.Format, "bitrate", playInfo.Bitrate, "size", playInfo.Size, "requested_quality", quality.String())
	}
	rawURL := firstNonEmptyString(playInfo.MainPlayURL, playInfo.BackupPlayURL)
	if strings.TrimSpace(rawURL) == "" {
		return nil, platform.NewUnavailableError("soda", "track", trackID)
	}
	bitrate := playInfo.Bitrate
	if bitrate <= 0 && playInfo.Duration > 0 && playInfo.Size > 0 {
		bitrate = int(playInfo.Size * 8 / int64(playInfo.Duration) / 1000)
	}
	format := strings.TrimSpace(strings.ToLower(playInfo.Format))
	if format == "" {
		format = "m4a"
	}
	headers := map[string]string{"User-Agent": sodaUserAgent, "X-Soda-Play-Auth": playInfo.PlayAuth}
	qualityLevel := mapSodaQuality(playInfo, bitrate)
	candidates := make([]string, 0, 1)
	if backup := strings.TrimSpace(playInfo.BackupPlayURL); backup != "" && backup != rawURL {
		candidates = append(candidates, backup)
	}
	return &platform.DownloadInfo{
		URL:           rawURL,
		CandidateURLs: candidates,
		Headers:       headers,
		Size:          playInfo.Size,
		Format:        format,
		Bitrate:       bitrate,
		Quality:       qualityLevel,
		Downloader:    c.DownloadAndDecrypt,
	}, nil
}

func forceSodaPlayerInfoLossless(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set("format_type", "8")
	query.Set("codec_type", "5")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (c *Client) fetchPlayInfosBySignedURL(ctx context.Context, playerInfoURL string) ([]sodaPlayInfo, error) {
	parsed, err := url.Parse(strings.TrimSpace(playerInfoURL))
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	videoID := strings.TrimSpace(query.Get("video_id"))
	if videoID == "" {
		return nil, fmt.Errorf("soda: video_id missing")
	}
	base := url.Values{}
	base.Set("Action", "GetPlayInfo")
	base.Set("Version", "2019-03-15")
	base.Set("aid", query.Get("aid"))
	base.Set("ssl", query.Get("ssl"))
	base.Set("stream_type", query.Get("stream_type"))
	base.Set("video_id", videoID)
	base.Set("ptoken", query.Get("ptoken"))
	base.Set("codec_type", "5")
	base.Set("format_type", "8")
	raw := parsed.Scheme + "://" + parsed.Host + "/?" + base.Encode()
	body, err := c.getJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	var resp sodaPlayInfoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("soda: parse forced player info response: %w", err)
	}
	list := append([]sodaPlayInfo(nil), resp.Result.Data.PlayInfoList...)
	if len(list) == 0 {
		return nil, platform.NewUnavailableError("soda", "track", "")
	}
	return list, nil
}

func (c *Client) bestTrackURL(ctx context.Context, playerInfoURL string) string {
	playInfos, err := c.fetchPlayInfos(ctx, playerInfoURL)
	if err != nil || len(playInfos) == 0 {
		return ""
	}
	return firstNonEmptyString(playInfos[0].MainPlayURL, playInfos[0].BackupPlayURL)
}

func (c *Client) downloadAndDecryptOnce(ctx context.Context, rawURL string, info *platform.DownloadInfo, destPath string, progress func(written, total int64)) (int64, error) {
	if c != nil && c.logger != nil {
		c.logger.Debug("soda: download begin", "format", info.Format, "url", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	for k, v := range info.Headers {
		if strings.EqualFold(k, "X-Soda-Play-Auth") {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	encryptedData, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	decrypted := encryptedData
	playAuth := strings.TrimSpace(info.Headers["X-Soda-Play-Auth"])
	decryptSucceeded := false
	if playAuth != "" {
		if decoded, decodeErr := sodaDecryptAudioWithLog(encryptedData, playAuth, c.logger); decodeErr == nil {
			decrypted = decoded
			decryptSucceeded = true
			if c != nil && c.logger != nil {
				c.logger.Debug("soda: decrypt succeeded", "format", info.Format, "input_size", len(encryptedData), "output_size", len(decrypted))
			}
		} else if c.logger != nil {
			c.logger.Debug("soda: decrypt failed, fallback to raw media", "err", decodeErr)
		}
	}
	if strings.EqualFold(strings.TrimSpace(info.Format), "mp4") && looksLikeLosslessAudioContainer(encryptedData) {
		if c != nil && c.logger != nil {
			c.logger.Debug("soda: lossless container detected from raw media", "size", len(encryptedData), "decrypt_succeeded", decryptSucceeded)
		}
	}
	outputPath := destPath
	outputData := decrypted
	if err := os.WriteFile(outputPath, outputData, 0o644); err != nil {
		return 0, err
	}
	codecName, codecErr := sodaProbeAudioCodec(outputPath)
	codecName = strings.ToLower(strings.TrimSpace(codecName))
	if codecErr == nil {
		if c != nil && c.logger != nil {
			c.logger.Debug("soda: probed audio codec", "codec", codecName, "path", outputPath)
		}
		switch codecName {
		case "flac":
			extractedPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".flac"
			if err := sodaEnsurePlayableFLAC(ctx, outputPath, extractedPath); err != nil {
				if c != nil && c.logger != nil {
					c.logger.Warn("soda: failed to extract playable flac from lossless container", "src_path", outputPath, "dst_path", extractedPath, "error", err)
				}
				return 0, fmt.Errorf("soda: extract playable flac from lossless container: %w", err)
			}
			extracted, readErr := os.ReadFile(extractedPath)
			if readErr != nil {
				if c != nil && c.logger != nil {
					c.logger.Warn("soda: failed to read extracted flac after successful extraction", "path", extractedPath, "error", readErr)
				}
				return 0, fmt.Errorf("soda: read extracted flac: %w", readErr)
			}
			if len(extracted) == 0 {
				err := errors.New("extracted flac is empty")
				if c != nil && c.logger != nil {
					c.logger.Warn("soda: extracted flac is empty", "path", extractedPath)
				}
				return 0, fmt.Errorf("soda: %w", err)
			}
			_ = os.Remove(outputPath)
			outputPath = extractedPath
			outputData = extracted
			info.Format = "flac"
			info.Headers["X-Soda-Container"] = "mp4(flac)"
			if c != nil && c.logger != nil {
				c.logger.Debug("soda: extracted playable flac from audio container", "path", outputPath, "size", len(outputData))
			}
		case "aac", "alac":
			repackedPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".m4a"
			if repackedPath != outputPath {
				if repackedData, changed, err := repackSodaM4AIfNeeded(ctx, outputPath, repackedPath); err == nil && changed {
					_ = os.Remove(outputPath)
					outputPath = repackedPath
					outputData = repackedData
					info.Format = "m4a"
				}
			}
		}
	} else if c != nil && c.logger != nil {
		c.logger.Debug("soda: ffprobe codec detection failed", "err", codecErr)
	}
	if err := os.WriteFile(outputPath, outputData, 0o644); err != nil {
		return 0, err
	}
	if c != nil && c.logger != nil {
		c.logger.Debug("soda: download finished", "final_path", outputPath, "final_format", info.Format, "final_size", len(outputData))
	}
	if progress != nil {
		progress(int64(len(outputData)), int64(len(outputData)))
	}
	return int64(len(outputData)), nil
}

func (c *Client) fetchPlayInfos(ctx context.Context, playerInfoURL string) ([]sodaPlayInfo, error) {
	playerInfoURL = strings.TrimSpace(playerInfoURL)
	if playerInfoURL == "" {
		return nil, fmt.Errorf("soda: player info url missing")
	}
	body, err := c.getJSON(ctx, playerInfoURL)
	if err != nil {
		return nil, err
	}
	var resp sodaPlayInfoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("soda: parse player info response: %w", err)
	}
	list := append([]sodaPlayInfo(nil), resp.Result.Data.PlayInfoList...)
	if len(list) == 0 {
		return nil, platform.NewUnavailableError("soda", "track", "")
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Size != list[j].Size {
			return list[i].Size > list[j].Size
		}
		return list[i].Bitrate > list[j].Bitrate
	})
	best := list[0]
	if strings.TrimSpace(best.MainPlayURL) == "" && strings.TrimSpace(best.BackupPlayURL) == "" {
		return nil, platform.NewUnavailableError("soda", "track", "")
	}
	return list, nil
}

func (c *Client) getJSON(ctx context.Context, rawURL string) ([]byte, error) {
	return c.doRequest(ctx, rawURL, "application/json, text/plain, */*")
}

func (c *Client) fetchHTML(ctx context.Context, rawURL string) ([]byte, error) {
	return c.doRequest(ctx, rawURL, "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
}

func (c *Client) doRequest(ctx context.Context, rawURL string, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", sodaUserAgent)
	if strings.TrimSpace(accept) == "" {
		accept = "*/*"
	}
	req.Header.Set("Accept", accept)
	if strings.TrimSpace(c.cookie) != "" {
		req.Header.Set("Cookie", c.cookie)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("soda: request failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func extractSodaRouterData(page []byte) (*sodaSharePageData, error) {
	text := string(page)
	marker := "_ROUTER_DATA"
	idx := strings.Index(text, marker)
	if idx < 0 {
		return nil, fmt.Errorf("soda: router data not found")
	}
	start := strings.Index(text[idx:], "{")
	if start < 0 {
		return nil, fmt.Errorf("soda: router data start not found")
	}
	start += idx
	depth := 0
	end := -1
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
		if end > 0 {
			break
		}
	}
	if end <= start {
		return nil, fmt.Errorf("soda: router data end not found")
	}
	var result sodaSharePageData
	if err := json.Unmarshal([]byte(text[start:end]), &result); err != nil {
		return nil, fmt.Errorf("soda: parse router data: %w", err)
	}
	return &result, nil
}

func convertSodaTrack(track sodaTrack) platform.Track {
	trackID := strings.TrimSpace(track.ID)
	artists := make([]platform.Artist, 0, len(track.Artists))
	for _, artist := range track.Artists {
		if strings.TrimSpace(artist.Name) == "" {
			continue
		}
		artistID := strings.TrimSpace(artist.ID)
		artists = append(artists, platform.Artist{ID: artistID, Platform: "soda", Name: artist.Name, URL: buildSodaArtistURL(artistID)})
	}
	albumID := strings.TrimSpace(track.Album.ID)
	albumName := strings.TrimSpace(track.Album.Name)
	coverURL := buildSodaCoverURL(track.Album.URLCover.URLs, track.Album.URLCover.URI)
	var album *platform.Album
	if albumID != "" || albumName != "" {
		album = &platform.Album{ID: albumID, Platform: "soda", Title: albumName, Artists: artists, CoverURL: coverURL, URL: buildSodaAlbumURL(albumID)}
	}
	return platform.Track{
		ID:       trackID,
		Platform: "soda",
		Title:    strings.TrimSpace(track.Name),
		Artists:  artists,
		Album:    album,
		Duration: time.Duration(track.Duration/1000) * time.Second,
		CoverURL: coverURL,
		URL:      buildSodaTrackURL(trackID),
	}
}

func convertSodaPlaylist(meta sodaPlaylistMeta) *platform.Playlist {
	id := strings.TrimSpace(meta.ID)
	creator := firstNonEmptyString(strings.TrimSpace(meta.Owner.PublicName), strings.TrimSpace(meta.Owner.Nickname), "汽水音乐")
	trackCount := maxInt(meta.TrackCount, meta.CountTracks)
	title := firstNonEmptyString(strings.TrimSpace(meta.Title), id)
	return &platform.Playlist{
		ID:          id,
		Platform:    "soda",
		Title:       title,
		Description: strings.TrimSpace(meta.Desc),
		CoverURL:    buildSodaCoverURL(meta.URLCover.URLs, meta.URLCover.URI),
		Creator:     creator,
		TrackCount:  trackCount,
		URL:         buildSodaPlaylistURL(id),
	}
}

func convertSodaAlbum(meta sodaAlbumMeta) *platform.Album {
	id := strings.TrimSpace(meta.ID)
	artists := make([]platform.Artist, 0, len(meta.Artists))
	for _, artist := range meta.Artists {
		if strings.TrimSpace(artist.Name) == "" {
			continue
		}
		artistID := strings.TrimSpace(artist.ID)
		artists = append(artists, platform.Artist{ID: artistID, Platform: "soda", Name: artist.Name, URL: buildSodaArtistURL(artistID)})
	}
	var releaseDate *time.Time
	if ts := parseSodaDate(meta.ReleaseDate.String()); !ts.IsZero() {
		releaseDate = &ts
	}
	trackCount := maxInt(meta.TrackCount, meta.CountTracks)
	title := firstNonEmptyString(strings.TrimSpace(meta.Name), id)
	return &platform.Album{
		ID:          id,
		Platform:    "soda",
		Title:       title,
		Artists:     artists,
		CoverURL:    buildSodaCoverURL(meta.URLCover.URLs, meta.URLCover.URI),
		Description: firstNonEmptyString(strings.TrimSpace(meta.Intro), strings.TrimSpace(meta.Desc)),
		ReleaseDate: releaseDate,
		TrackCount:  trackCount,
		URL:         buildSodaAlbumURL(id),
		Year:        yearFromSodaDate(meta.ReleaseDate.String()),
	}
}

func sliceTracksByOffsetLimit(tracks []platform.Track, offset, limit int) []platform.Track {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(tracks) {
		return nil
	}
	end := len(tracks)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return tracks[offset:end]
}

func maxInt(values ...int) int {
	best := 0
	for _, value := range values {
		if value > best {
			best = value
		}
	}
	return best
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func convertSodaArtist(meta sodaArtistMeta) (*platform.Artist, int) {
	id := strings.TrimSpace(meta.ID)
	name := strings.TrimSpace(meta.Name)
	if id == "" && name == "" {
		return nil, 0
	}
	avatarURL := buildSodaCoverURL(meta.Avatar.URLs, meta.Avatar.URI)
	if avatarURL == "" {
		avatarURL = buildSodaCoverURL(meta.AvatarMedium.URLs, meta.AvatarMedium.URI)
	}
	if avatarURL == "" {
		avatarURL = buildSodaCoverURL(meta.AvatarThumb.URLs, meta.AvatarThumb.URI)
	}
	if avatarURL == "" {
		avatarURL = buildSodaCoverURL(meta.URLCover.URLs, meta.URLCover.URI)
	}
	trackCount := meta.TrackCount
	if trackCount <= 0 {
		trackCount = meta.CountTracks
	}
	return &platform.Artist{
		ID:        id,
		Platform:  "soda",
		Name:      name,
		URL:       buildSodaArtistURL(id),
		AvatarURL: avatarURL,
	}, trackCount
}

func buildSodaCoverURL(urls []string, uri string) string {
	base := ""
	if len(urls) > 0 {
		base = strings.TrimSpace(urls[0])
	}
	uri = strings.TrimSpace(uri)
	if base == "" {
		return ""
	}
	if uri != "" && !strings.Contains(base, uri) {
		base += uri
	}
	if !strings.Contains(base, "~") {
		base += "~c5_375x375.jpg"
	}
	return base
}

func buildSodaTrackURL(trackID string) string {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return ""
	}
	return "https://music.douyin.com/qishui/share/track?track_id=" + trackID
}

func buildSodaPlaylistURL(playlistID string) string {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return ""
	}
	return "https://music.douyin.com/qishui/share/playlist?playlist_id=" + playlistID
}

func buildSodaAlbumURL(albumID string) string {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return ""
	}
	return "https://music.douyin.com/qishui/share/album?album_id=" + albumID
}

func buildSodaArtistURL(artistID string) string {
	artistID = strings.TrimSpace(artistID)
	if artistID == "" {
		return ""
	}
	return "https://music.douyin.com/qishui/share/artist?artist_id=" + artistID
}

func parseSodaDate(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if ts, ok := parseSodaNumericDate(value); ok {
		return ts
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006/01/02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func parseSodaNumericDate(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return time.Time{}, false
		}
	}
	if len(value) == 8 {
		if parsed, err := time.Parse("20060102", value); err == nil {
			return parsed, true
		}
	}
	if l := len(value); l != 10 && l != 13 && l != 16 && l != 19 {
		return time.Time{}, false
	}
	iv, err := strconv.ParseInt(value, 10, 64)
	if err != nil || iv <= 0 {
		return time.Time{}, false
	}
	var ts time.Time
	switch {
	case iv >= 1e18:
		ts = time.Unix(0, iv)
	case iv >= 1e15:
		ts = time.UnixMicro(iv)
	case iv >= 1e12:
		ts = time.UnixMilli(iv)
	default:
		ts = time.Unix(iv, 0)
	}
	return ts.UTC(), true
}

func yearFromSodaDate(value string) int {
	if ts := parseSodaDate(value); !ts.IsZero() {
		return ts.Year()
	}
	return 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

var (
	sodaLinePattern = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)
	sodaWordPattern = regexp.MustCompile(`<[^>]+>`)
)

func parseSodaLyric(raw string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		match := sodaLinePattern.FindStringSubmatch(line)
		if len(match) < 4 {
			continue
		}
		startMS, _ := strconv.Atoi(match[1])
		text := sodaWordPattern.ReplaceAllString(match[3], "")
		minutes := startMS / 60000
		seconds := (startMS % 60000) / 1000
		centis := (startMS % 1000) / 10
		lines = append(lines, fmt.Sprintf("[%02d:%02d.%02d]%s", minutes, seconds, centis, text))
	}
	return strings.Join(lines, "\n")
}

func parseSodaLyricLines(lrc string) []platform.LyricLine {
	lines := strings.Split(strings.TrimSpace(lrc), "\n")
	result := make([]platform.LyricLine, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) < 11 || line[0] != '[' {
			continue
		}
		end := strings.IndexByte(line, ']')
		if end <= 1 {
			continue
		}
		stamp := line[1:end]
		parts := strings.Split(stamp, ":")
		if len(parts) != 2 {
			continue
		}
		min, err1 := strconv.Atoi(parts[0])
		secParts := strings.SplitN(parts[1], ".", 2)
		if len(secParts) != 2 {
			continue
		}
		sec, err2 := strconv.Atoi(secParts[0])
		centi, err3 := strconv.Atoi(secParts[1])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		result = append(result, platform.LyricLine{
			Time: time.Duration(min)*time.Minute + time.Duration(sec)*time.Second + time.Duration(centi)*10*time.Millisecond,
			Text: strings.TrimSpace(line[end+1:]),
		})
	}
	return result
}

func decryptSodaAudio(fileData []byte, playAuth string) ([]byte, error) {
	return decryptSodaAudioWithLogger(fileData, playAuth, nil)
}

func decryptSodaAudioWithLogger(fileData []byte, playAuth string, logger bot.Logger) ([]byte, error) {
	hexKey, err := extractSodaKey(playAuth)
	if err != nil {
		return nil, err
	}
	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, err
	}
	moov, err := findSodaBox(fileData, "moov", 0, len(fileData))
	if err != nil {
		return nil, errors.New("moov box not found")
	}
	trak, stbl, err := findSodaAudioTrack(fileData, moov)
	if stbl == nil {
		return nil, errors.New("stbl box not found")
	}
	if logger != nil {
		logger.Debug("soda: selected mp4 track for decrypt", "trak_offset", trak.offset, "stbl_offset", stbl.offset)
	}
	sampleRanges, err := parseSodaSampleRanges(stbl, fileData)
	if err != nil {
		return nil, err
	}
	senc, err := findSodaBox(fileData, "senc", trak.offset+8, trak.offset+trak.size)
	if err != nil {
		senc, err = findSodaBox(fileData, "senc", stbl.offset+8, stbl.offset+stbl.size)
	}
	if err != nil {
		senc, err = findSodaBox(fileData, "senc", moov.offset+8, moov.offset+moov.size)
	}
	if err != nil {
		return nil, errors.New("senc box not found")
	}
	sencSamples, ivSize, hasSubsamples := parseSodaSenc(senc.data)
	if logger != nil {
		logger.Debug("soda: parsed senc metadata", "sample_count", len(sencSamples), "iv_size", ivSize, "has_subsamples", hasSubsamples)
	}
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}
	decryptedData := make([]byte, len(fileData))
	copy(decryptedData, fileData)
	for i, sampleRange := range sampleRanges {
		chunk := decryptedData[sampleRange.Offset : sampleRange.Offset+sampleRange.Size]
		if i < len(sencSamples) {
			dst, decryptErr := decryptSodaSample(block, chunk, sencSamples[i])
			if decryptErr != nil {
				return nil, fmt.Errorf("decrypt sample %d: %w", i, decryptErr)
			}
			if logger != nil && len(sencSamples[i].Subsamples) > 0 {
				logger.Debug("soda: decrypted sample with subsamples", "sample_index", i, "sample_size", sampleRange.Size, "subsample_count", len(sencSamples[i].Subsamples))
			}
			copy(decryptedData[sampleRange.Offset:sampleRange.Offset+sampleRange.Size], dst)
		}
	}
	stsd, err := findSodaBox(fileData, "stsd", stbl.offset+8, stbl.offset+stbl.size)
	if err == nil {
		rewriteSodaStsdAudioSampleEntry(decryptedData[stsd.offset : stsd.offset+stsd.size])
	}
	return decryptedData, nil
}

type sodaMP4Box struct {
	offset int
	size   int
	data   []byte
}

type sodaSencSubsample struct {
	ClearBytes     uint16
	EncryptedBytes uint32
}

type sodaSencSample struct {
	IV         []byte
	Subsamples []sodaSencSubsample
}

type sodaChunkMapEntry struct {
	FirstChunk      uint32
	SamplesPerChunk uint32
	SampleDescID    uint32
}

type sodaSampleRange struct {
	Offset int
	Size   int
}

func findSodaBox(data []byte, boxType string, start, end int) (*sodaMP4Box, error) {
	boxes, err := findSodaBoxes(data, boxType, start, end)
	if err != nil {
		return nil, err
	}
	return boxes[0], nil
}

func findSodaBoxes(data []byte, boxType string, start, end int) ([]*sodaMP4Box, error) {
	if end > len(data) {
		end = len(data)
	}
	pos := start
	target := []byte(boxType)
	boxes := make([]*sodaMP4Box, 0, 1)
	for pos+8 <= end {
		size, headerSize, ok := parseSodaBoxHeader(data, pos, end)
		if !ok || size < headerSize {
			break
		}
		if pos+size > end {
			break
		}
		if bytes.Equal(data[pos+4:pos+8], target) {
			boxes = append(boxes, &sodaMP4Box{offset: pos, size: size, data: data[pos+headerSize : pos+size]})
		}
		pos += size
	}
	if len(boxes) > 0 {
		return boxes, nil
	}
	return nil, fmt.Errorf("box not found")
}

func parseSodaBoxHeader(data []byte, pos, end int) (size int, headerSize int, ok bool) {
	if pos+8 > end || pos+8 > len(data) {
		return 0, 0, false
	}
	boxSize := binary.BigEndian.Uint32(data[pos : pos+4])
	switch boxSize {
	case 0:
		return end - pos, 8, true
	case 1:
		if pos+16 > end || pos+16 > len(data) {
			return 0, 0, false
		}
		size64 := binary.BigEndian.Uint64(data[pos+8 : pos+16])
		if size64 > uint64(end-pos) || size64 > uint64(^uint(0)>>1) {
			return 0, 0, false
		}
		return int(size64), 16, true
	default:
		return int(boxSize), 8, true
	}
}

func findSodaAudioTrack(data []byte, moov *sodaMP4Box) (*sodaMP4Box, *sodaMP4Box, error) {
	traks, err := findSodaBoxes(data, "trak", moov.offset+8, moov.offset+moov.size)
	if err != nil || len(traks) == 0 {
		return nil, nil, errors.New("trak box not found")
	}
	for _, trak := range traks {
		if isSodaAudioTrack(data, trak) {
			stbl, stblErr := findSodaTrackStbl(data, trak)
			if stblErr == nil {
				return trak, stbl, nil
			}
		}
	}
	stbl, stblErr := findSodaTrackStbl(data, traks[0])
	if stblErr != nil {
		return traks[0], nil, stblErr
	}
	return traks[0], stbl, nil
}

func findSodaTrackStbl(data []byte, trak *sodaMP4Box) (*sodaMP4Box, error) {
	mdia, err := findSodaBox(data, "mdia", trak.offset+8, trak.offset+trak.size)
	if err != nil {
		return nil, err
	}
	minf, err := findSodaBox(data, "minf", mdia.offset+8, mdia.offset+mdia.size)
	if err != nil {
		return nil, err
	}
	return findSodaBox(data, "stbl", minf.offset+8, minf.offset+minf.size)
}

func isSodaAudioTrack(data []byte, trak *sodaMP4Box) bool {
	mdia, err := findSodaBox(data, "mdia", trak.offset+8, trak.offset+trak.size)
	if err == nil {
		hdlr, hdlrErr := findSodaBox(data, "hdlr", mdia.offset+8, mdia.offset+mdia.size)
		if hdlrErr == nil && len(hdlr.data) >= 12 && bytes.Equal(hdlr.data[8:12], []byte("soun")) {
			return true
		}
	}
	stbl, err := findSodaTrackStbl(data, trak)
	if err != nil {
		return false
	}
	stsd, err := findSodaBox(data, "stsd", stbl.offset+8, stbl.offset+stbl.size)
	if err != nil {
		return false
	}
	return bytes.Contains(stsd.data, []byte("enca")) || bytes.Contains(stsd.data, []byte("mp4a"))
}

func parseSodaStsz(data []byte) []uint32 {
	if len(data) < 12 {
		return nil
	}
	sampleSizeFixed := binary.BigEndian.Uint32(data[4:8])
	sampleCount := int(binary.BigEndian.Uint32(data[8:12]))
	sizes := make([]uint32, sampleCount)
	if sampleSizeFixed != 0 {
		for i := 0; i < sampleCount; i++ {
			sizes[i] = sampleSizeFixed
		}
		return sizes
	}
	for i := 0; i < sampleCount; i++ {
		if 12+i*4+4 <= len(data) {
			sizes[i] = binary.BigEndian.Uint32(data[12+i*4 : 12+i*4+4])
		}
	}
	return sizes
}

func parseSodaStsc(data []byte) []sodaChunkMapEntry {
	if len(data) < 8 {
		return nil
	}
	entryCount := int(binary.BigEndian.Uint32(data[4:8]))
	entries := make([]sodaChunkMapEntry, 0, entryCount)
	ptr := 8
	for i := 0; i < entryCount; i++ {
		if ptr+12 > len(data) {
			break
		}
		entries = append(entries, sodaChunkMapEntry{
			FirstChunk:      binary.BigEndian.Uint32(data[ptr : ptr+4]),
			SamplesPerChunk: binary.BigEndian.Uint32(data[ptr+4 : ptr+8]),
			SampleDescID:    binary.BigEndian.Uint32(data[ptr+8 : ptr+12]),
		})
		ptr += 12
	}
	return entries
}

func parseSodaStco(data []byte) []uint64 {
	if len(data) < 8 {
		return nil
	}
	entryCount := int(binary.BigEndian.Uint32(data[4:8]))
	offsets := make([]uint64, 0, entryCount)
	ptr := 8
	for i := 0; i < entryCount; i++ {
		if ptr+4 > len(data) {
			break
		}
		offsets = append(offsets, uint64(binary.BigEndian.Uint32(data[ptr:ptr+4])))
		ptr += 4
	}
	return offsets
}

func parseSodaCo64(data []byte) []uint64 {
	if len(data) < 8 {
		return nil
	}
	entryCount := int(binary.BigEndian.Uint32(data[4:8]))
	offsets := make([]uint64, 0, entryCount)
	ptr := 8
	for i := 0; i < entryCount; i++ {
		if ptr+8 > len(data) {
			break
		}
		offsets = append(offsets, binary.BigEndian.Uint64(data[ptr:ptr+8]))
		ptr += 8
	}
	return offsets
}

func parseSodaSampleRanges(stbl *sodaMP4Box, fileData []byte) ([]sodaSampleRange, error) {
	if stbl == nil {
		return nil, errors.New("stbl box not found")
	}
	stsz, err := findSodaBox(fileData, "stsz", stbl.offset+8, stbl.offset+stbl.size)
	if err != nil {
		return nil, errors.New("stsz box not found")
	}
	sampleSizes := parseSodaStsz(stsz.data)
	if len(sampleSizes) == 0 {
		return nil, errors.New("stsz sample sizes empty")
	}
	stsc, err := findSodaBox(fileData, "stsc", stbl.offset+8, stbl.offset+stbl.size)
	if err != nil {
		return nil, errors.New("stsc box not found")
	}
	chunkMap := parseSodaStsc(stsc.data)
	if len(chunkMap) == 0 {
		return nil, errors.New("stsc entries empty")
	}
	var chunkOffsets []uint64
	if stco, stcoErr := findSodaBox(fileData, "stco", stbl.offset+8, stbl.offset+stbl.size); stcoErr == nil {
		chunkOffsets = parseSodaStco(stco.data)
	} else if co64, co64Err := findSodaBox(fileData, "co64", stbl.offset+8, stbl.offset+stbl.size); co64Err == nil {
		chunkOffsets = parseSodaCo64(co64.data)
	}
	if len(chunkOffsets) == 0 {
		return nil, errors.New("chunk offsets not found")
	}
	ranges := make([]sodaSampleRange, 0, len(sampleSizes))
	sampleIndex := 0
	for mapIndex, entry := range chunkMap {
		if entry.FirstChunk == 0 || entry.SamplesPerChunk == 0 {
			continue
		}
		chunkStart := int(entry.FirstChunk) - 1
		chunkEnd := len(chunkOffsets)
		if mapIndex+1 < len(chunkMap) && chunkMap[mapIndex+1].FirstChunk > 0 {
			chunkEnd = int(chunkMap[mapIndex+1].FirstChunk) - 1
		}
		if chunkStart >= len(chunkOffsets) {
			break
		}
		if chunkEnd > len(chunkOffsets) {
			chunkEnd = len(chunkOffsets)
		}
		for chunkIndex := chunkStart; chunkIndex < chunkEnd && sampleIndex < len(sampleSizes); chunkIndex++ {
			chunkOffset := chunkOffsets[chunkIndex]
			if chunkOffset > uint64(len(fileData)) {
				return nil, fmt.Errorf("chunk offset %d out of range", chunkOffset)
			}
			offset := int(chunkOffset)
			for sampleInChunk := uint32(0); sampleInChunk < entry.SamplesPerChunk && sampleIndex < len(sampleSizes); sampleInChunk++ {
				size := int(sampleSizes[sampleIndex])
				if offset+size > len(fileData) {
					return nil, fmt.Errorf("sample %d out of range", sampleIndex)
				}
				ranges = append(ranges, sodaSampleRange{Offset: offset, Size: size})
				offset += size
				sampleIndex++
			}
		}
	}
	if sampleIndex != len(sampleSizes) {
		return nil, fmt.Errorf("sample layout incomplete: mapped=%d total=%d", sampleIndex, len(sampleSizes))
	}
	return ranges, nil
}

func parseSodaSenc(data []byte) ([]sodaSencSample, int, bool) {
	if len(data) < 8 {
		return nil, 0, false
	}
	flags := binary.BigEndian.Uint32(data[0:4]) & 0x00FFFFFF
	sampleCount := int(binary.BigEndian.Uint32(data[4:8]))
	hasSubsamples := (flags & 0x02) != 0
	ivSize := inferSodaSencIVSize(data[8:], sampleCount, hasSubsamples)
	if ivSize == 0 {
		return nil, 0, hasSubsamples
	}
	samples := make([]sodaSencSample, 0, sampleCount)
	ptr := 8
	for i := 0; i < sampleCount; i++ {
		if ptr+ivSize > len(data) {
			break
		}
		sample := sodaSencSample{IV: append([]byte(nil), data[ptr:ptr+ivSize]...)}
		ptr += ivSize
		if hasSubsamples {
			if ptr+2 > len(data) {
				break
			}
			subCount := int(binary.BigEndian.Uint16(data[ptr : ptr+2]))
			ptr += 2
			sample.Subsamples = make([]sodaSencSubsample, 0, subCount)
			for j := 0; j < subCount; j++ {
				if ptr+6 > len(data) {
					break
				}
				sample.Subsamples = append(sample.Subsamples, sodaSencSubsample{
					ClearBytes:     binary.BigEndian.Uint16(data[ptr : ptr+2]),
					EncryptedBytes: binary.BigEndian.Uint32(data[ptr+2 : ptr+6]),
				})
				ptr += 6
			}
		}
		samples = append(samples, sample)
	}
	return samples, ivSize, hasSubsamples
}

func rewriteSodaStsdAudioSampleEntry(stsdData []byte) {
	if len(stsdData) < 16 {
		return
	}
	entryCount := int(binary.BigEndian.Uint32(stsdData[12:16]))
	ptr := 16
	for i := 0; i < entryCount; i++ {
		if ptr+8 > len(stsdData) {
			return
		}
		entrySize := int(binary.BigEndian.Uint32(stsdData[ptr : ptr+4]))
		if entrySize < 8 || ptr+entrySize > len(stsdData) {
			return
		}
		entryType := stsdData[ptr+4 : ptr+8]
		if bytes.Equal(entryType, []byte("enca")) {
			replacement := []byte("mp4a")
			entryData := stsdData[ptr : ptr+entrySize]
			if idx := bytes.Index(entryData, []byte("frma")); idx >= 0 && idx+8 <= len(entryData) {
				candidate := entryData[idx+4 : idx+8]
				if len(candidate) == 4 {
					replacement = append([]byte(nil), candidate...)
				}
			}
			copy(entryType, replacement)
		}
		ptr += entrySize
	}
}

func inferSodaSencIVSize(data []byte, sampleCount int, hasSubsamples bool) int {
	if sampleCount <= 0 {
		return 0
	}
	for _, candidate := range []int{8, 16} {
		if consumed, ok := sodaSencConsumedBytes(data, sampleCount, candidate, hasSubsamples); ok && consumed == len(data) {
			return candidate
		}
	}
	for _, candidate := range []int{8, 16} {
		if _, ok := sodaSencConsumedBytes(data, sampleCount, candidate, hasSubsamples); ok {
			return candidate
		}
	}
	return 0
}

func sodaSencConsumedBytes(data []byte, sampleCount, ivSize int, hasSubsamples bool) (int, bool) {
	ptr := 0
	for i := 0; i < sampleCount; i++ {
		if ptr+ivSize > len(data) {
			return 0, false
		}
		ptr += ivSize
		if !hasSubsamples {
			continue
		}
		if ptr+2 > len(data) {
			return 0, false
		}
		subCount := int(binary.BigEndian.Uint16(data[ptr : ptr+2]))
		ptr += 2
		if ptr+subCount*6 > len(data) {
			return 0, false
		}
		ptr += subCount * 6
	}
	return ptr, true
}

func decryptSodaSample(block cipher.Block, sample []byte, senc sodaSencSample) ([]byte, error) {
	iv := make([]byte, aes.BlockSize)
	copy(iv, senc.IV)
	stream := cipher.NewCTR(block, iv)
	if len(senc.Subsamples) == 0 {
		dst := make([]byte, len(sample))
		stream.XORKeyStream(dst, sample)
		return dst, nil
	}
	decrypted := make([]byte, 0, len(sample))
	ptr := 0
	for idx, subsample := range senc.Subsamples {
		clearBytes := int(subsample.ClearBytes)
		encryptedBytes := int(subsample.EncryptedBytes)
		if ptr+clearBytes+encryptedBytes > len(sample) {
			return nil, fmt.Errorf("subsample %d exceeds sample size", idx)
		}
		decrypted = append(decrypted, sample[ptr:ptr+clearBytes]...)
		ptr += clearBytes
		if encryptedBytes == 0 {
			continue
		}
		buf := make([]byte, encryptedBytes)
		stream.XORKeyStream(buf, sample[ptr:ptr+encryptedBytes])
		decrypted = append(decrypted, buf...)
		ptr += encryptedBytes
	}
	if ptr < len(sample) {
		decrypted = append(decrypted, sample[ptr:]...)
	}
	return decrypted, nil
}

func looksLikeLosslessAudioContainer(data []byte) bool {
	if len(data) < 16 {
		return false
	}
	headerEnd := minInt(len(data), 128)
	probeEnd := minInt(len(data), 2048)
	if !bytes.Contains(data[:headerEnd], []byte("ftyp")) {
		return false
	}
	return bytes.Contains(data[:probeEnd], []byte("fLaC")) || bytes.Contains(data[:probeEnd], []byte("dfLa"))
}

func extractSodaLosslessFLAC(ctx context.Context, srcPath, dstPath string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-y", "-i", srcPath, "-vn", "-c:a", "copy", dstPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("extract flac from soda mp4: %w, stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func ensureSodaPlayableLosslessFLAC(ctx context.Context, srcPath, dstPath string) (retErr error) {
	ext := filepath.Ext(dstPath)
	base := strings.TrimSuffix(dstPath, ext)
	if ext == "" {
		ext = ".flac"
	}
	tmpExtractedPath := base + ".extracting" + ext
	tmpRewrittenPath := base + ".rewritten" + ext
	defer func() {
		_ = os.Remove(tmpExtractedPath)
		_ = os.Remove(tmpRewrittenPath)
		if retErr != nil {
			_ = os.Remove(dstPath)
		}
	}()

	if err := sodaExtractLosslessFLAC(ctx, srcPath, tmpExtractedPath); err != nil {
		return err
	}
	if err := sodaValidateAudioFile(ctx, tmpExtractedPath, "flac"); err != nil {
		return fmt.Errorf("validate extracted soda flac: %w", err)
	}
	if err := sodaRewriteLosslessFLAC(ctx, tmpExtractedPath, tmpRewrittenPath); err != nil {
		return fmt.Errorf("rewrite extracted soda flac: %w", err)
	}
	if err := sodaValidateAudioFile(ctx, tmpRewrittenPath, "flac"); err != nil {
		return fmt.Errorf("validate rewritten soda flac: %w", err)
	}
	if err := os.Rename(tmpRewrittenPath, dstPath); err != nil {
		return err
	}
	return nil
}

func rewriteSodaLosslessFLAC(ctx context.Context, srcPath, dstPath string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y",
		"-i", srcPath,
		"-map", "0:a:0",
		"-map_metadata", "-1",
		"-vn",
		"-c:a", "flac",
		"-compression_level", "12",
		dstPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rewrite soda flac: %w, stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func validateSodaAudioFile(ctx context.Context, filePath, expectedCodec string) error {
	codecName, err := sodaProbeAudioCodec(filePath)
	if err != nil {
		return fmt.Errorf("probe audio codec: %w", err)
	}
	codecName = strings.ToLower(strings.TrimSpace(codecName))
	expectedCodec = strings.ToLower(strings.TrimSpace(expectedCodec))
	if expectedCodec != "" && codecName != expectedCodec {
		return fmt.Errorf("unexpected codec %q", codecName)
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-v", "error", "-i", filePath, "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("decode audio file: %w, stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func repackSodaM4AIfNeeded(ctx context.Context, srcPath, dstPath string) ([]byte, bool, error) {
	codecName, err := probeAudioCodec(srcPath)
	if err != nil {
		return nil, false, err
	}
	codecName = strings.ToLower(strings.TrimSpace(codecName))
	if codecName != "aac" && codecName != "alac" {
		return nil, false, nil
	}
	if err := remuxAudioContainer(ctx, srcPath, dstPath); err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(dstPath)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func probeAudioCodec(filePath string) (string, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(ffprobePath, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name", "-of", "default=noprint_wrappers=1:nokey=1", filePath)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func remuxAudioContainer(ctx context.Context, srcPath, dstPath string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-y", "-i", srcPath, "-vn", "-c:a", "copy", dstPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remux soda audio container: %w, stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func extractSodaKey(playAuth string) (string, error) {
	binaryStr, err := base64.StdEncoding.DecodeString(playAuth)
	if err != nil {
		return "", err
	}
	bytesData := []byte(binaryStr)
	if len(bytesData) < 3 {
		return "", errors.New("auth data too short")
	}
	paddingLen := int((bytesData[0] ^ bytesData[1] ^ bytesData[2]) - 48)
	if len(bytesData) < paddingLen+2 {
		return "", errors.New("invalid padding length")
	}
	innerInput := bytesData[1 : len(bytesData)-paddingLen]
	tmpBuff := decryptSodaInner(innerInput)
	if len(tmpBuff) == 0 {
		return "", errors.New("decryption failed")
	}
	skipBytes := decodeSodaBase36(tmpBuff[0])
	endIndex := 1 + (len(bytesData) - paddingLen - 2) - skipBytes
	if endIndex > len(tmpBuff) || endIndex < 1 {
		return "", errors.New("index out of bounds")
	}
	return string(tmpBuff[1:endIndex]), nil
}

func decryptSodaInner(keyBytes []byte) []byte {
	result := make([]byte, len(keyBytes))
	buff := append([]byte{0xFA, 0x55}, keyBytes...)
	for i := 0; i < len(result); i++ {
		v := int(keyBytes[i]^buff[i]) - bitcountSoda(i) - 21
		for v < 0 {
			v += 255
		}
		result[i] = byte(v)
	}
	return result
}

func bitcountSoda(n int) int {
	u := uint32(n)
	u = u - ((u >> 1) & 0x55555555)
	u = (u & 0x33333333) + ((u >> 2) & 0x33333333)
	return int((((u + (u >> 4)) & 0x0F0F0F0F) * 0x01010101) >> 24)
}

func decodeSodaBase36(c byte) int {
	if c >= '0' && c <= '9' {
		return int(c - '0')
	}
	if c >= 'a' && c <= 'z' {
		return int(c-'a') + 10
	}
	return 0xFF
}
