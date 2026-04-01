package netease

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

const (
	songDetailAPI     = "/api/v3/song/detail"
	songURLAPI        = "/api/song/enhance/player/url/v1"
	searchSongAPI     = "/api/v1/search/song/get"
	songLyricAPI      = "/api/song/lyric"
	playlistDetailAPI = "/api/v6/playlist/detail"
	albumDetailAPI    = "/api/album/v3/detail"
	programDetailAPI  = "/api/dj/program/detail"
)

func GetSongDetail(data RequestData, ids []int) (SongsDetailData, error) {
	type songIDs struct {
		ID int `json:"id"`
	}
	type reqBody struct {
		C string `json:"c"`
	}

	items := make([]songIDs, 0, len(ids))
	for _, id := range ids {
		items = append(items, songIDs{ID: id})
	}
	itemsJSON, _ := json.Marshal(items)
	bodyJSON, _ := json.Marshal(reqBody{C: string(itemsJSON)})
	return doJSONRequest[SongsDetailData](data, EAPIOption{Path: songDetailAPI, Url: "https://music.163.com/eapi/v3/song/detail", Json: string(bodyJSON)})
}

func GetSongURL(data RequestData, config SongURLConfig) (SongsURLData, error) {
	type reqBody struct {
		EncodeType string `json:"encodeType"`
		IDs        string `json:"ids"`
		Level      string `json:"level"`
	}
	ids := make([]string, 0, len(config.IDs))
	for _, id := range config.IDs {
		ids = append(ids, fmt.Sprintf("%d", id))
	}
	idsJSON, _ := json.Marshal(ids)
	if config.Level == "" {
		config.Level = "hires"
	}
	if config.EncodeType == "" {
		config.EncodeType = "mp3"
	}
	bodyJSON, _ := json.Marshal(reqBody{IDs: string(idsJSON), EncodeType: config.EncodeType, Level: config.Level})
	return doJSONRequest[SongsURLData](data, EAPIOption{Path: songURLAPI, Url: "https://music.163.com/eapi/song/enhance/player/url/v1", Json: string(bodyJSON)})
}

func SearchSong(data RequestData, config SearchSongConfig) (SearchSongData, error) {
	type reqBody struct {
		S      string `json:"s"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if config.Limit == 0 {
		config.Limit = 20
	}
	bodyJSON, _ := json.Marshal(reqBody{S: config.Keyword, Offset: config.Offset, Limit: config.Limit})
	return doJSONRequest[SearchSongData](data, EAPIOption{Path: searchSongAPI, Url: "https://music.163.com/eapi/v1/search/song/get", Json: string(bodyJSON)})
}

func GetSongLyric(data RequestData, id int) (SongLyricData, error) {
	type reqBody struct {
		ID int `json:"id"`
		Lv int `json:"lv"`
		Kv int `json:"kv"`
		Tv int `json:"tv"`
		Yv int `json:"yv"`
	}
	bodyJSON, _ := json.Marshal(reqBody{ID: id, Lv: -1, Kv: -1, Tv: -1, Yv: -1})
	return doJSONRequest[SongLyricData](data, EAPIOption{Path: songLyricAPI, Url: "https://music.163.com/eapi/song/lyric", Json: string(bodyJSON)})
}

func GetPlaylistDetail(data RequestData, id int) (PlaylistDetailData, error) {
	type reqBody struct {
		ID string `json:"id"`
		T  string `json:"t"`
		N  string `json:"n"`
		S  string `json:"s"`
	}
	bodyJSON, _ := json.Marshal(reqBody{ID: fmt.Sprintf("%d", id), T: "0", N: "50", S: "5"})
	return doJSONRequest[PlaylistDetailData](data, EAPIOption{Path: playlistDetailAPI, Url: "https://music.163.com/eapi/v6/playlist/detail", Json: string(bodyJSON)})
}

func GetAlbumDetail(data RequestData, albumID int) (AlbumDetailData, error) {
	type reqBody struct {
		ID       int    `json:"id"`
		CacheKey string `json:"cache_key"`
	}
	cacheKey := base64.StdEncoding.EncodeToString(CacheKeyEncrypt(fmt.Sprintf("id=%d", albumID)))
	bodyJSON, _ := json.Marshal(reqBody{ID: albumID, CacheKey: cacheKey})
	return doJSONRequest[AlbumDetailData](data, EAPIOption{Path: albumDetailAPI, Url: "https://music.163.com/eapi/album/v3/detail", Json: string(bodyJSON)})
}

func GetProgramDetail(data RequestData, id int) (ProgramDetailData, error) {
	type reqBody struct {
		ID string `json:"id"`
	}
	bodyJSON, _ := json.Marshal(reqBody{ID: fmt.Sprintf("%d", id)})
	return doJSONRequest[ProgramDetailData](data, EAPIOption{Path: programDetailAPI, Url: "https://music.163.com/eapi/dj/program/detail", Json: string(bodyJSON)})
}

func doJSONRequest[T any](data RequestData, option EAPIOption) (T, error) {
	var result T
	body, _, err := apiRequest(option, data)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return result, err
	}
	assignRawJSON(&result, body)
	return result, nil
}

func assignRawJSON[T any](result *T, raw string) {
	switch v := any(result).(type) {
	case *SongsDetailData:
		v.RawJson = raw
	case *SongsURLData:
		v.RawJson = raw
	case *SongLyricData:
		v.RawJson = raw
	case *SearchSongData:
		v.RawJson = raw
	case *PlaylistDetailData:
		v.RawJson = raw
	case *AlbumDetailData:
		v.RawJson = raw
	case *ProgramDetailData:
		v.RawJson = raw
	}
}
