package netease

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	marker "github.com/XiaoMengXinX/163KeyMarker"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type ID3Provider struct {
	client *Client
}

func NewID3Provider(client *Client) *ID3Provider {
	return &ID3Provider{client: client}
}

func (p *ID3Provider) GetTagData(ctx context.Context, track *platform.Track, info *platform.DownloadInfo) (*id3.TagData, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("netease id3 provider not configured")
	}
	if track == nil {
		return nil, errors.New("track required")
	}

	musicID, err := strconv.Atoi(track.ID)
	if err != nil {
		return nil, err
	}

	level := "standard"
	if info != nil {
		switch info.Quality {
		case platform.QualityHigh:
			level = "higher"
		case platform.QualityLossless:
			level = "lossless"
		case platform.QualityHiRes:
			level = "hires"
		}
	}

	songDetail, err := p.client.GetSongDetail(ctx, musicID)
	if err != nil {
		return nil, err
	}
	songURL, err := p.client.GetSongURL(ctx, musicID, level)
	if err != nil {
		return nil, err
	}
	if len(songDetail.Songs) == 0 || len(songURL.Data) == 0 {
		return nil, errors.New("netease: empty song detail or url")
	}

	lyrics := ""
	lyricData, err := p.client.GetLyric(ctx, musicID)
	if err != nil {
		_ = err
	} else if lyricData != nil {
		lyrics = strings.TrimSpace(lyricData.Lrc.Lyric)
		if lyrics == "" {
			_ = lyrics
		}
	}

	markerData := marker.CreateMarker(songDetail.Songs[0], songURL.Data[0])

	artists := make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		if artist.Name != "" {
			artists = append(artists, artist.Name)
		}
	}

	albumName := ""
	if track.Album != nil {
		albumName = track.Album.Title
	}

	year := ""
	if songDetail.Songs[0].PublishTime > 0 {
		year = fmt.Sprintf("%d", time.Unix(int64(songDetail.Songs[0].PublishTime)/1000, 0).Year())
	}
	trackNumber := songDetail.Songs[0].No
	discNumber := parseNeteaseDiscNumber(songDetail.Songs[0].Cd)

	if track.Year > 0 {
		year = strconv.Itoa(track.Year)
	}
	if track.TrackNumber > 0 {
		trackNumber = track.TrackNumber
	}
	if track.DiscNumber > 0 {
		discNumber = track.DiscNumber
	}

	return &id3.TagData{
		Title:       track.Title,
		Artist:      strings.Join(artists, ", "),
		Album:       albumName,
		CoverURL:    track.CoverURL,
		Lyrics:      lyrics,
		Year:        year,
		TrackNumber: trackNumber,
		DiscNumber:  discNumber,
		Extra: map[string]any{
			"netease_marker": markerData,
		},
	}, nil
}

func parseNeteaseDiscNumber(disc string) int {
	disc = strings.TrimSpace(disc)
	if disc == "" {
		return 0
	}
	if n, err := strconv.Atoi(disc); err == nil && n > 0 {
		return n
	}
	for i := 0; i < len(disc); i++ {
		if disc[i] < '0' || disc[i] > '9' {
			continue
		}
		j := i
		for j < len(disc) && disc[j] >= '0' && disc[j] <= '9' {
			j++
		}
		if n, err := strconv.Atoi(disc[i:j]); err == nil && n > 0 {
			return n
		}
		i = j
	}
	return 0
}
