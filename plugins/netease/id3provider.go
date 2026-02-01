package netease

import (
	"context"
	"errors"
	"strconv"
	"strings"

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
	if lyricData, err := p.client.GetLyric(ctx, musicID); err == nil && lyricData != nil {
		lyrics = strings.TrimSpace(lyricData.Lrc.Lyric)
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

	return &id3.TagData{
		Title:    track.Title,
		Artist:   strings.Join(artists, ", "),
		Album:    albumName,
		CoverURL: track.CoverURL,
		Lyrics:   lyrics,
		Extra: map[string]any{
			"netease_marker": markerData,
		},
	}, nil
}
