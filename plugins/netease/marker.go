package netease

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type MarkerData struct {
	MusicID       int             `json:"musicId"`
	MusicName     string          `json:"musicName"`
	Artist        [][]interface{} `json:"artist"`
	AlbumID       int             `json:"albumId"`
	Album         string          `json:"album"`
	AlbumPicDocID string          `json:"albumPicDocId"`
	AlbumPic      string          `json:"albumPic"`
	Bitrate       int             `json:"bitrate"`
	Mp3DocID      string          `json:"mp3DocId"`
	Duration      int             `json:"duration"`
	MvID          int             `json:"mvId"`
	Alias         []interface{}   `json:"alias"`
	Format        string          `json:"format"`
}

func CreateMarker(songDetail SongDetailData, songURL SongURLData) MarkerData {
	artists := make([][]interface{}, 0, len(songDetail.Ar))
	for _, artist := range songDetail.Ar {
		artists = append(artists, []interface{}{artist.Name, artist.Id})
	}
	return MarkerData{
		MusicID:       songDetail.Id,
		MusicName:     songDetail.Name,
		Artist:        artists,
		AlbumID:       songDetail.Al.Id,
		Album:         songDetail.Al.Name,
		AlbumPicDocID: songDetail.Al.PicStr,
		AlbumPic:      songDetail.Al.PicUrl,
		Bitrate:       songURL.Br,
		Mp3DocID:      songURL.Md5,
		Duration:      songDetail.Dt,
		MvID:          songDetail.Mv,
		Alias:         songDetail.Alia,
		Format:        songURL.Type,
	}
}

func Create163KeyStr(marker MarkerData) string {
	markerJSON, err := json.Marshal(marker)
	if err != nil {
		return ""
	}
	encrypted := base64.StdEncoding.EncodeToString(MarkerEncrypt(fmt.Sprintf("music:%s", string(markerJSON))))
	return fmt.Sprintf("163 key(Don't modify):%s", encrypted)
}
