package qqmusic

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
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const (
	musicuEndpoint     = "https://u.y.qq.com/cgi-bin/musicu.fcg"
	musicsEndpoint     = "https://u.y.qq.com/cgi-bin/musics.fcg"
	songDetailEndpoint = "https://c.y.qq.com/v8/fcg-bin/fcg_play_single_song.fcg"
	lyricEndpoint      = "https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg"
	searchEndpoint     = "https://c.y.qq.com/soso/fcgi-bin/client_search_cp"
)

type Client struct {
	httpClient *http.Client
	headers    http.Header
	cookie     string
	logger     bot.Logger
	mu         sync.RWMutex
	autoRenew  autoRenewConfig
}

type autoRenewConfig struct {
	enabled  bool
	interval time.Duration
	persist  bool
	path     string
}

func NewClient(cookie string, timeout time.Duration, logger bot.Logger, autoRenewEnabled bool, autoRenewInterval time.Duration, autoRenewPersist bool, autoRenewPersistPath string) *Client {
	headers := http.Header{}
	headers.Set("User-Agent", "QQMusic/14090508 (android 12)")
	headers.Set("Referer", "https://y.qq.com/")
	headers.Set("Origin", "https://y.qq.com")
	headers.Set("Accept", "*/*")
	headers.Set("Content-Type", "application/json")

	client := &Client{
		httpClient: &http.Client{Timeout: timeout},
		headers:    headers,
		cookie:     strings.TrimSpace(cookie),
		logger:     logger,
		autoRenew: autoRenewConfig{
			enabled:  autoRenewEnabled,
			interval: autoRenewInterval,
			persist:  autoRenewPersist,
			path:     strings.TrimSpace(autoRenewPersistPath),
		},
	}
	if client.autoRenew.enabled {
		client.startAutoRenew()
	}
	return client
}

func (c *Client) Search(ctx context.Context, keyword string, limit int) ([]qqSearchSong, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, platform.NewNotFoundError("qqmusic", "search", "")
	}
	if limit <= 0 {
		limit = 10
	}
	songs, err := c.searchByMetingDesktop(ctx, keyword, limit)
	if err == nil && len(songs) > 0 {
		return songs, nil
	}
	songs, err = c.searchByMusicu(ctx, keyword, limit)
	if err == nil && len(songs) > 0 {
		return songs, nil
	}
	legacySongs, legacyErr := c.searchByLegacy(ctx, keyword, limit)
	if legacyErr == nil && len(legacySongs) > 0 {
		return legacySongs, nil
	}
	if err == nil {
		return songs, nil
	}
	if legacyErr == nil {
		return legacySongs, nil
	}
	return nil, err
}

func (c *Client) searchByMusicu(ctx context.Context, keyword string, limit int) ([]qqSearchSong, error) {
	requestBody := map[string]interface{}{
		"comm": map[string]interface{}{
			"ct":             "11",
			"cv":             "14090508",
			"v":              "14090508",
			"tmeAppID":       "qqmusic",
			"phonetype":      "EBG-AN10",
			"deviceScore":    "553.47",
			"devicelevel":    "50",
			"newdevicelevel": "20",
			"rom":            "HuaWei/EMOTION/EmotionUI_14.2.0",
			"os_ver":         "12",
			"OpenUDID":       "0",
			"uid":            "0",
			"modeSwitch":     "6",
			"teenMode":       "0",
			"ui_mode":        "2",
			"nettype":        "1020",
			"v4ip":           "",
		},
	}
	request := map[string]interface{}{
		"method": "DoSearchForQQMusicMobile",
		"module": "music.search.SearchCgiService",
		"param": map[string]interface{}{
			"search_type":  0,
			"query":        keyword,
			"page_num":     1,
			"num_per_page": limit,
			"highlight":    0,
			"nqc_flag":     0,
			"multi_zhida":  0,
			"cat":          2,
			"grp":          1,
			"sin":          0,
			"sem":          0,
		},
	}
	requestBody["req_0"] = request
	requestBody["req"] = request
	endpoint := musicuEndpoint + "?format=json&inCharset=utf8&outCharset=utf8"
	body, err := c.postJSONNoCookie(ctx, endpoint, requestBody)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int `json:"code"`
		Req  struct {
			Code int `json:"code"`
			Data struct {
				Body struct {
					ItemSong []qqSearchSongMobile `json:"item_song"`
					Song     struct {
						List []qqSearchSong `json:"list"`
					} `json:"song"`
				} `json:"body"`
			} `json:"data"`
		} `json:"req"`
		Req0 struct {
			Code int `json:"code"`
			Data struct {
				Body struct {
					ItemSong []qqSearchSongMobile `json:"item_song"`
					Song     struct {
						List []qqSearchSong `json:"list"`
					} `json:"song"`
				} `json:"body"`
			} `json:"data"`
		} `json:"req_0"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("qqmusic: decode search response: %w", err)
	}
	if resp.Code != 0 || (resp.Req.Code != 0 && resp.Req0.Code != 0) {
		return nil, platform.NewUnavailableError("qqmusic", "search", "")
	}
	if songs := extractSearchSongs(resp.Req0.Data.Body, resp.Req.Data.Body); len(songs) > 0 {
		return songs, nil
	}
	if songs := parseSearchSongsFromRaw(body); len(songs) > 0 {
		return songs, nil
	}
	return nil, nil
}

func (c *Client) searchByMetingDesktop(ctx context.Context, keyword string, limit int) ([]qqSearchSong, error) {
	payload := map[string]interface{}{
		"comm": map[string]interface{}{
			"ct":  "19",
			"cv":  "1859",
			"uin": "0",
		},
		"req": map[string]interface{}{
			"method": "DoSearchForQQMusicDesktop",
			"module": "music.search.SearchCgiService",
			"param": map[string]interface{}{
				"grp":          1,
				"num_per_page": limit,
				"page_num":     1,
				"query":        keyword,
				"search_type":  0,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("qqmusic: encode meting search payload: %w", err)
	}
	headers := http.Header{}
	headers.Set("User-Agent", "QQ%E9%9F%B3%E4%B9%90/54409 CFNetwork/901.1 Darwin/17.6.0 (x86_64)")
	headers.Set("Referer", "http://y.qq.com")
	headers.Set("Accept", "*/*")
	headers.Set("Accept-Language", "zh-CN,zh;q=0.8,gl;q=0.6,zh-TW;q=0.4")
	headers.Set("Connection", "keep-alive")
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	bodyBytes, err := c.postRawWithHeaders(ctx, musicuEndpoint, body, headers, true)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int `json:"code"`
		Req  struct {
			Code int `json:"code"`
			Data struct {
				Body struct {
					Song struct {
						List []qqSearchSongAny `json:"list"`
					} `json:"song"`
				} `json:"body"`
			} `json:"data"`
		} `json:"req"`
	}
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return nil, fmt.Errorf("qqmusic: decode meting search response: %w", err)
	}
	if resp.Code != 0 || resp.Req.Code != 0 {
		return nil, platform.NewUnavailableError("qqmusic", "search", "")
	}
	return convertAnySearchSongs(resp.Req.Data.Body.Song.List), nil
}

func convertMobileSearchSongs(songs []qqSearchSongMobile) []qqSearchSong {
	if len(songs) == 0 {
		return nil
	}
	results := make([]qqSearchSong, 0, len(songs))
	for _, song := range songs {
		results = append(results, qqSearchSong{
			SongID:    song.ID,
			SongMID:   strings.TrimSpace(song.Mid),
			SongName:  song.Name,
			AlbumName: song.Album.Name,
			AlbumMID:  song.Album.Mid,
			Interval:  song.Interval,
			Singer:    song.Singer,
		})
	}
	return results
}

func convertAnySearchSongs(songs []qqSearchSongAny) []qqSearchSong {
	if len(songs) == 0 {
		return nil
	}
	results := make([]qqSearchSong, 0, len(songs))
	for _, song := range songs {
		mid := strings.TrimSpace(song.SongMID)
		if mid == "" {
			mid = strings.TrimSpace(song.Mid)
		}
		name := song.SongName
		if strings.TrimSpace(name) == "" {
			name = song.Name
		}
		if strings.TrimSpace(name) == "" {
			name = song.Title
		}
		albumName := song.AlbumName
		albumMid := song.AlbumMID
		if strings.TrimSpace(song.Album.Name) != "" {
			albumName = song.Album.Name
		}
		if strings.TrimSpace(song.Album.Title) != "" {
			albumName = song.Album.Title
		}
		if strings.TrimSpace(song.Album.Mid) != "" {
			albumMid = song.Album.Mid
		}
		id := song.SongID
		if id == 0 {
			id = song.ID
		}
		results = append(results, qqSearchSong{
			SongID:    id,
			SongMID:   mid,
			SongName:  name,
			AlbumName: albumName,
			AlbumMID:  albumMid,
			Interval:  song.Interval,
			Singer:    song.Singer,
		})
	}
	return results
}

func extractSearchSongs(primary, fallback struct {
	ItemSong []qqSearchSongMobile `json:"item_song"`
	Song     struct {
		List []qqSearchSong `json:"list"`
	} `json:"song"`
}) []qqSearchSong {
	if len(primary.ItemSong) > 0 {
		return convertMobileSearchSongs(primary.ItemSong)
	}
	if len(fallback.ItemSong) > 0 {
		return convertMobileSearchSongs(fallback.ItemSong)
	}
	if len(primary.Song.List) > 0 {
		return primary.Song.List
	}
	return fallback.Song.List
}

func parseSearchSongsFromRaw(body []byte) []qqSearchSong {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	keys := []string{
		"music.search.SearchCgiService.DoSearchForQQMusicMobile",
		"music.search.SearchCgiService.DoSearchForQQMusicDesktop",
		"search",
	}
	for _, key := range keys {
		payload, ok := raw[key]
		if !ok {
			continue
		}
		var resp struct {
			Data struct {
				Body struct {
					ItemSong []qqSearchSongAny `json:"item_song"`
					Song     struct {
						List []qqSearchSongAny `json:"list"`
					} `json:"song"`
				} `json:"body"`
			} `json:"data"`
		}
		if err := json.Unmarshal(payload, &resp); err != nil {
			continue
		}
		if len(resp.Data.Body.ItemSong) > 0 {
			return convertAnySearchSongs(resp.Data.Body.ItemSong)
		}
		if len(resp.Data.Body.Song.List) > 0 {
			return convertAnySearchSongs(resp.Data.Body.Song.List)
		}
	}
	return nil
}

func (c *Client) searchByLegacy(ctx context.Context, keyword string, limit int) ([]qqSearchSong, error) {
	query := url.Values{}
	query.Set("ct", "24")
	query.Set("qqmusic_ver", "1298")
	query.Set("new_json", "1")
	query.Set("remoteplace", "txt.yqq.center")
	query.Set("t", "0")
	query.Set("aggr", "1")
	query.Set("cr", "1")
	query.Set("catZhida", "1")
	query.Set("lossless", "0")
	query.Set("flag_qc", "0")
	query.Set("needNewCode", "0")
	query.Set("g_tk", "5381")
	query.Set("loginUin", "0")
	query.Set("hostUin", "0")
	query.Set("uin", "0")
	query.Set("inCharset", "utf8")
	query.Set("outCharset", "utf-8")
	query.Set("notice", "0")
	query.Set("format", "json")
	query.Set("w", keyword)
	query.Set("p", "1")
	query.Set("n", strconv.Itoa(limit))
	query.Set("platform", "yqq")
	endpoint := searchEndpoint + "?" + query.Encode()
	body, err := c.getNoCookie(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Song struct {
				List []qqSearchSongAny `json:"list"`
			} `json:"song"`
		} `json:"data"`
		Song struct {
			List []qqSearchSongAny `json:"list"`
		} `json:"song"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("qqmusic: decode legacy search response: %w", err)
	}
	if resp.Code != 0 {
		return nil, platform.NewUnavailableError("qqmusic", "search", "")
	}
	if len(resp.Data.Song.List) > 0 {
		return convertAnySearchSongs(resp.Data.Song.List), nil
	}
	return convertAnySearchSongs(resp.Song.List), nil
}

func (c *Client) GetSongDetail(ctx context.Context, id string) (*qqSongDetail, error) {
	query := url.Values{}
	query.Set("platform", "yqq")
	query.Set("format", "json")
	if isNumericID(id) {
		query.Set("songid", id)
	} else {
		query.Set("songmid", id)
	}
	endpoint := songDetailEndpoint + "?" + query.Encode()
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int            `json:"code"`
		Data []qqSongDetail `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("qqmusic: decode song detail: %w", err)
	}
	if resp.Code != 0 || len(resp.Data) == 0 {
		return nil, platform.NewNotFoundError("qqmusic", "track", id)
	}
	return &resp.Data[0], nil
}

func (c *Client) GetPlaylist(ctx context.Context, playlistID string) (*qqPlaylistData, error) {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return nil, platform.NewNotFoundError("qqmusic", "playlist", "")
	}
	var disstid interface{} = playlistID
	if numericID, err := strconv.ParseInt(playlistID, 10, 64); err == nil {
		disstid = numericID
	}
	limit := platform.PlaylistLimitFromContext(ctx)
	if limit <= 0 {
		limit = 10000
	}
	offset := platform.PlaylistOffsetFromContext(ctx)
	if offset < 0 {
		offset = 0
	}
	payload := map[string]interface{}{
		"comm": map[string]interface{}{
			"g_tk":        5381,
			"uin":         0,
			"format":      "json",
			"platform":    "h5",
			"needNewCode": 1,
		},
		"req_0": map[string]interface{}{
			"module": "music.srfDissInfo.aiDissInfo",
			"method": "uniform_get_Dissinfo",
			"param": map[string]interface{}{
				"disstid":      disstid,
				"enc_host_uin": "",
				"tag":          1,
				"userinfo":     1,
				"song_begin":   offset,
				"song_num":     limit,
				"onlysonglist": 0,
			},
		},
	}
	endpoint := musicuEndpoint + "?_webcgikey=uniform_get_Dissinfo&format=json&inCharset=utf8&outCharset=utf8"
	body, err := c.postJSON(ctx, endpoint, payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int `json:"code"`
		Req0 struct {
			Code int `json:"code"`
			Data struct {
				Code    int `json:"code"`
				DirInfo struct {
					ID      int64  `json:"id"`
					DirID   int64  `json:"dirid"`
					Title   string `json:"title"`
					Desc    string `json:"desc"`
					PicURL  string `json:"picurl"`
					PicURL2 string `json:"picurl2"`
					SongNum int    `json:"songnum"`
					Creator struct {
						Nick string `json:"nick"`
					} `json:"creator"`
				} `json:"dirinfo"`
				Songlist []qqPlaylistSong `json:"songlist"`
			} `json:"data"`
		} `json:"req_0"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("qqmusic: decode playlist detail: %w", err)
	}
	if resp.Code != 0 || resp.Req0.Code != 0 || resp.Req0.Data.Code != 0 {
		return nil, platform.NewUnavailableError("qqmusic", "playlist", playlistID)
	}
	data := resp.Req0.Data
	playlistData := qqPlaylistData{
		Name:        strings.TrimSpace(data.DirInfo.Title),
		Desc:        strings.TrimSpace(data.DirInfo.Desc),
		Logo:        strings.TrimSpace(data.DirInfo.PicURL2),
		Total:       data.DirInfo.SongNum,
		Songlist:    data.Songlist,
		Creator:     qqPlaylistCreator{Name: strings.TrimSpace(data.DirInfo.Creator.Nick)},
		CreatorName: strings.TrimSpace(data.DirInfo.Creator.Nick),
	}
	if playlistData.Logo == "" {
		playlistData.Logo = strings.TrimSpace(data.DirInfo.PicURL)
	}
	playlistIDValue := data.DirInfo.ID
	if playlistIDValue == 0 {
		playlistIDValue = data.DirInfo.DirID
	}
	if playlistIDValue > 0 {
		playlistData.ID = playlistIDValue
	}
	if playlistData.ID == 0 && playlistData.Name == "" {
		return nil, platform.NewNotFoundError("qqmusic", "playlist", playlistID)
	}
	return &playlistData, nil
}

func (c *Client) GetAlbum(ctx context.Context, albumID string) (*qqAlbumData, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return nil, platform.NewNotFoundError("qqmusic", "album", "")
	}

	limit := platform.PlaylistLimitFromContext(ctx)
	if limit <= 0 {
		limit = 10000
	}
	offset := platform.PlaylistOffsetFromContext(ctx)
	if offset < 0 {
		offset = 0
	}

	var numericAlbumID int64
	if parsedID, err := strconv.ParseInt(albumID, 10, 64); err == nil {
		numericAlbumID = parsedID
	}

	payload := map[string]interface{}{
		"comm": map[string]interface{}{
			"ct": 24,
			"cv": 10000,
		},
		"albumSonglist": map[string]interface{}{
			"module": "music.musichallAlbum.AlbumSongList",
			"method": "GetAlbumSongList",
			"param": map[string]interface{}{
				"albumMid": albumID,
				"albumID":  numericAlbumID,
				"begin":    offset,
				"num":      limit,
				"order":    2,
			},
		},
	}

	endpoint := musicuEndpoint + "?format=json&inCharset=utf8&outCharset=utf8"
	body, err := c.postJSON(ctx, endpoint, payload)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code          int `json:"code"`
		AlbumSonglist struct {
			Code int `json:"code"`
			Data struct {
				AlbumMid string            `json:"albumMid"`
				TotalNum int               `json:"totalNum"`
				SongList []qqAlbumSongItem `json:"songList"`
			} `json:"data"`
		} `json:"albumSonglist"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("qqmusic: decode album detail: %w", err)
	}
	if resp.Code != 0 || resp.AlbumSonglist.Code != 0 {
		return nil, platform.NewUnavailableError("qqmusic", "album", albumID)
	}

	data := resp.AlbumSonglist.Data
	if len(data.SongList) == 0 && data.TotalNum == 0 {
		return nil, platform.NewNotFoundError("qqmusic", "album", albumID)
	}

	album := &qqAlbumData{
		Mid:   strings.TrimSpace(data.AlbumMid),
		Total: data.TotalNum,
	}

	album.Songlist = make([]qqPlaylistSong, 0, len(data.SongList))
	for _, item := range data.SongList {
		album.Songlist = append(album.Songlist, item.SongInfo)
	}

	if len(album.Songlist) > 0 {
		first := album.Songlist[0]
		album.Name = strings.TrimSpace(first.AlbumName)
		if album.Name == "" {
			album.Name = strings.TrimSpace(first.Album.Name)
		}
		if album.Mid == "" {
			album.Mid = strings.TrimSpace(first.AlbumMID)
		}
		if album.Mid == "" {
			album.Mid = strings.TrimSpace(first.Album.Mid)
		}
		if first.Album.ID > 0 {
			album.ID = strconv.FormatInt(first.Album.ID, 10)
		}
		if album.Mid != "" {
			album.CoverURL = buildAlbumCoverURL(album.Mid)
		}
		album.Artists = append(album.Artists, first.Singer...)
	}

	if album.ID == "" {
		album.ID = albumID
	}
	if album.Mid == "" && isTencentSongMID(albumID) {
		album.Mid = albumID
	}

	return album, nil
}

func (c *Client) GetLyrics(ctx context.Context, songMid string) (string, string, error) {
	query := url.Values{}
	query.Set("songmid", songMid)
	query.Set("g_tk", "5381")
	query.Set("format", "json")
	query.Set("inCharset", "utf8")
	query.Set("outCharset", "utf-8")
	query.Set("platform", "yqq")
	endpoint := lyricEndpoint + "?" + query.Encode()
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return "", "", err
	}
	var resp struct {
		RetCode int    `json:"retcode"`
		Lyric   string `json:"lyric"`
		Trans   string `json:"trans"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", fmt.Errorf("qqmusic: decode lyric: %w", err)
	}
	if resp.RetCode != 0 {
		return "", "", platform.NewNotFoundError("qqmusic", "lyrics", songMid)
	}
	lyric := decodeBase64Text(resp.Lyric)
	trans := decodeBase64Text(resp.Trans)
	return lyric, trans, nil
}

func (c *Client) GetSongFileInfo(ctx context.Context, songMid string) (*qqFileInfo, error) {
	payload := map[string]interface{}{
		"comm": map[string]interface{}{
			"ct":  "19",
			"cv":  "1859",
			"uin": "0",
		},
		"req": map[string]interface{}{
			"module": "music.pf_song_detail_svr",
			"method": "get_song_detail_yqq",
			"param": map[string]interface{}{
				"song_type": 0,
				"song_mid":  songMid,
			},
		},
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("qqmusic: encode file info payload: %w", err)
	}
	sign := tencentSign(string(jsonBody), false)
	endpoint := musicsEndpoint + "?format=json&sign=" + url.QueryEscape(sign)
	body, err := c.postJSONRaw(ctx, endpoint, jsonBody)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Req struct {
			Data struct {
				TrackInfo struct {
					File qqFileInfo `json:"file"`
					VS   []string   `json:"vs"`
				} `json:"track_info"`
			} `json:"data"`
		} `json:"req"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("qqmusic: decode file info: %w", err)
	}
	file := resp.Req.Data.TrackInfo.File
	if len(resp.Req.Data.TrackInfo.VS) > 1 {
		file.CoverMid = strings.TrimSpace(resp.Req.Data.TrackInfo.VS[1])
	}
	if file.MediaMid == "" {
		return nil, platform.NewUnavailableError("qqmusic", "track", songMid)
	}
	return &file, nil
}

func (c *Client) GetVKey(ctx context.Context, songMid, mediaMid, qualityCode, ext, uin, authst string) (string, error) {
	filename := qualityCode + mediaMid + "." + ext
	payload := map[string]interface{}{
		"req": map[string]interface{}{
			"module": "music.vkey.GetVkey",
			"method": "UrlGetVkey",
			"param": map[string]interface{}{
				"filename":  []string{filename},
				"guid":      "114514",
				"songmid":   []string{songMid},
				"songtype":  []int{0},
				"uin":       uin,
				"loginflag": 1,
				"platform":  "20",
			},
		},
		"comm": map[string]interface{}{
			"qq":     uin,
			"authst": authst,
			"ct":     "26",
			"cv":     "2010101",
			"v":      "2010101",
		},
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("qqmusic: encode vkey payload: %w", err)
	}
	sign := tencentSign(string(jsonBody), true)
	endpoint := musicsEndpoint + "?format=json&sign=" + url.QueryEscape(sign)
	body, err := c.postJSONRaw(ctx, endpoint, jsonBody)
	if err != nil {
		return "", err
	}
	var resp struct {
		Req struct {
			Data struct {
				MidURLInfo []struct {
					Purl string `json:"purl"`
					Vkey string `json:"vkey"`
				} `json:"midurlinfo"`
			} `json:"data"`
		} `json:"req"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("qqmusic: decode vkey: %w", err)
	}
	if len(resp.Req.Data.MidURLInfo) == 0 {
		return "", platform.NewUnavailableError("qqmusic", "track", songMid)
	}
	info := resp.Req.Data.MidURLInfo[0]
	if strings.TrimSpace(info.Purl) == "" || strings.TrimSpace(info.Vkey) == "" {
		return "", platform.NewUnavailableError("qqmusic", "track", songMid)
	}
	return info.Purl, nil
}

func (c *Client) postJSON(ctx context.Context, endpoint string, payload interface{}) ([]byte, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return c.postJSONRawWithCookie(ctx, endpoint, jsonBody, true)
}

func (c *Client) postJSONNoCookie(ctx context.Context, endpoint string, payload interface{}) ([]byte, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return c.postJSONRawWithCookie(ctx, endpoint, jsonBody, false)
}

func (c *Client) postJSONRaw(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	return c.postJSONRawWithCookie(ctx, endpoint, body, true)
}

func (c *Client) postJSONRawWithCookie(ctx context.Context, endpoint string, body []byte, includeCookie bool) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	for key, values := range c.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if includeCookie {
		cookie := c.Cookie()
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := httpError(resp.StatusCode, "qqmusic", bodyBytes); err != nil {
		return nil, err
	}
	return bodyBytes, nil
}

func (c *Client) postRawWithHeaders(ctx context.Context, endpoint string, body []byte, headers http.Header, includeCookie bool) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if includeCookie {
		cookie := c.Cookie()
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := httpError(resp.StatusCode, "qqmusic", bodyBytes); err != nil {
		return nil, err
	}
	return bodyBytes, nil
}

func (c *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	return c.getWithCookie(ctx, endpoint, true)
}

func (c *Client) getNoCookie(ctx context.Context, endpoint string) ([]byte, error) {
	return c.getWithCookie(ctx, endpoint, false)
}

func (c *Client) getWithCookie(ctx context.Context, endpoint string, includeCookie bool) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	for key, values := range c.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if includeCookie {
		cookie := c.Cookie()
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := httpError(resp.StatusCode, "qqmusic", bodyBytes); err != nil {
		return nil, err
	}
	return bodyBytes, nil
}

func httpError(status int, platformName string, body []byte) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return platform.NewAuthRequiredError(platformName)
	case http.StatusTooManyRequests:
		return platform.NewRateLimitedError(platformName)
	case http.StatusNotFound:
		return platform.NewNotFoundError(platformName, "track", "")
	default:
		if status >= 500 {
			return platform.NewUnavailableError(platformName, "api", "")
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("qqmusic: http %d: %s", status, strings.TrimSpace(string(body)))
		}
	}
	return nil
}

func decodeBase64Text(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return trimmed
	}
	return string(decoded)
}

func isNumericID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}
