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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Intro       string `json:"intro"`
	Desc        string `json:"desc"`
	ReleaseDate string `json:"release_date"`
	CountTracks int    `json:"count_tracks"`
	TrackCount  int    `json:"track_count"`
	Artists     []struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"artists"`
	URLCover struct {
		URLs []string `json:"urls"`
		URI  string   `json:"uri"`
	} `json:"url_cover"`
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
	if playAuth != "" {
		if decoded, decodeErr := decryptSodaAudio(encryptedData, playAuth); decodeErr == nil {
			decrypted = decoded
			if c != nil && c.logger != nil {
				c.logger.Debug("soda: decrypt succeeded", "format", info.Format, "input_size", len(encryptedData), "output_size", len(decrypted))
			}
		} else if c.logger != nil {
			c.logger.Debug("soda: decrypt failed, fallback to raw media", "err", decodeErr)
		}
	}
	if strings.EqualFold(strings.TrimSpace(info.Format), "mp4") && looksLikeLosslessAudioContainer(encryptedData) {
		decrypted = encryptedData
		if c != nil && c.logger != nil {
			c.logger.Debug("soda: lossless container detected from raw media", "size", len(encryptedData))
		}
	}
	outputPath := destPath
	outputData := decrypted
	if err := os.WriteFile(outputPath, outputData, 0o644); err != nil {
		return 0, err
	}
	codecName, codecErr := probeAudioCodec(outputPath)
	codecName = strings.ToLower(strings.TrimSpace(codecName))
	if codecErr == nil {
		if c != nil && c.logger != nil {
			c.logger.Debug("soda: probed audio codec", "codec", codecName, "path", outputPath)
		}
		switch codecName {
		case "flac":
			extractedPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".flac"
			if err := extractSodaLosslessFLAC(ctx, outputPath, extractedPath); err == nil {
				if extracted, readErr := os.ReadFile(extractedPath); readErr == nil && len(extracted) > 0 {
					_ = os.Remove(outputPath)
					outputPath = extractedPath
					outputData = extracted
					info.Format = "flac"
					info.Headers["X-Soda-Container"] = "mp4(flac)"
					if c != nil && c.logger != nil {
						c.logger.Debug("soda: extracted flac from audio container", "path", outputPath, "size", len(outputData))
					}
				}
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
	if ts := parseSodaDate(meta.ReleaseDate); !ts.IsZero() {
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
		Year:        yearFromSodaDate(meta.ReleaseDate),
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
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006/01/02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
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
	stbl, err := findSodaBox(fileData, "stbl", moov.offset, moov.offset+moov.size)
	if err != nil {
		trak, _ := findSodaBox(fileData, "trak", moov.offset+8, moov.offset+moov.size)
		if trak != nil {
			mdia, _ := findSodaBox(fileData, "mdia", trak.offset+8, trak.offset+trak.size)
			if mdia != nil {
				minf, _ := findSodaBox(fileData, "minf", mdia.offset+8, mdia.offset+mdia.size)
				if minf != nil {
					stbl, _ = findSodaBox(fileData, "stbl", minf.offset+8, minf.offset+minf.size)
				}
			}
		}
	}
	if stbl == nil {
		return nil, errors.New("stbl box not found")
	}
	stsz, err := findSodaBox(fileData, "stsz", stbl.offset+8, stbl.offset+stbl.size)
	if err != nil {
		return nil, errors.New("stsz box not found")
	}
	sampleSizes := parseSodaStsz(stsz.data)
	senc, err := findSodaBox(fileData, "senc", moov.offset+8, moov.offset+moov.size)
	if err != nil {
		senc, err = findSodaBox(fileData, "senc", stbl.offset+8, stbl.offset+stbl.size)
	}
	if err != nil {
		return nil, errors.New("senc box not found")
	}
	ivs := parseSodaSenc(senc.data)
	mdat, err := findSodaBox(fileData, "mdat", 0, len(fileData))
	if err != nil {
		return nil, errors.New("mdat box not found")
	}
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}
	decryptedData := make([]byte, len(fileData))
	copy(decryptedData, fileData)
	readPtr := mdat.offset + 8
	decryptedMdat := make([]byte, 0, mdat.size-8)
	for i := 0; i < len(sampleSizes); i++ {
		size := int(sampleSizes[i])
		if readPtr+size > len(decryptedData) {
			break
		}
		chunk := decryptedData[readPtr : readPtr+size]
		if i < len(ivs) {
			iv := ivs[i]
			if len(iv) < 16 {
				padded := make([]byte, 16)
				copy(padded, iv)
				iv = padded
			}
			stream := cipher.NewCTR(block, iv)
			dst := make([]byte, size)
			stream.XORKeyStream(dst, chunk)
			decryptedMdat = append(decryptedMdat, dst...)
		} else {
			decryptedMdat = append(decryptedMdat, chunk...)
		}
		readPtr += size
	}
	if len(decryptedMdat) != int(mdat.size)-8 {
		return nil, errors.New("decrypted size mismatch")
	}
	copy(decryptedData[mdat.offset+8:], decryptedMdat)
	stsd, err := findSodaBox(fileData, "stsd", stbl.offset+8, stbl.offset+stbl.size)
	if err == nil {
		stsdOffset := stsd.offset
		stsdData := decryptedData[stsdOffset : stsdOffset+stsd.size]
		if idx := bytes.Index(stsdData, []byte("enca")); idx != -1 {
			copy(stsdData[idx:], []byte("mp4a"))
			copy(decryptedData[stsdOffset:], stsdData)
		}
	}
	return decryptedData, nil
}

type sodaMP4Box struct {
	offset int
	size   int
	data   []byte
}

func findSodaBox(data []byte, boxType string, start, end int) (*sodaMP4Box, error) {
	if end > len(data) {
		end = len(data)
	}
	pos := start
	target := []byte(boxType)
	for pos+8 <= end {
		size := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		if size < 8 {
			break
		}
		if bytes.Equal(data[pos+4:pos+8], target) {
			return &sodaMP4Box{offset: pos, size: size, data: data[pos+8 : pos+size]}, nil
		}
		pos += size
	}
	return nil, fmt.Errorf("box not found")
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

func parseSodaSenc(data []byte) [][]byte {
	if len(data) < 8 {
		return nil
	}
	flags := binary.BigEndian.Uint32(data[0:4]) & 0x00FFFFFF
	sampleCount := int(binary.BigEndian.Uint32(data[4:8]))
	ivs := make([][]byte, 0, sampleCount)
	ptr := 8
	hasSubsamples := (flags & 0x02) != 0
	for i := 0; i < sampleCount; i++ {
		if ptr+8 > len(data) {
			break
		}
		iv := make([]byte, 16)
		copy(iv, data[ptr:ptr+8])
		ivs = append(ivs, iv)
		ptr += 8
		if hasSubsamples {
			if ptr+2 > len(data) {
				break
			}
			subCount := int(binary.BigEndian.Uint16(data[ptr : ptr+2]))
			ptr += 2 + (subCount * 6)
		}
	}
	return ivs
}

func looksLikeLosslessAudioContainer(data []byte) bool {
	if len(data) < 16 {
		return false
	}
	if !bytes.Contains(data[:128], []byte("ftyp")) {
		return false
	}
	return bytes.Contains(data[:2048], []byte("fLaC")) || bytes.Contains(data[:2048], []byte("dfLa"))
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
