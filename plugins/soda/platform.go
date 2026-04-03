package soda

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type SodaPlatform struct {
	client *Client
}

func NewPlatform(client *Client) *SodaPlatform {
	return &SodaPlatform{client: client}
}

func (s *SodaPlatform) Name() string { return "soda" }

func (s *SodaPlatform) SupportsDownload() bool { return true }

func (s *SodaPlatform) SupportsSearch() bool { return true }

func (s *SodaPlatform) SupportsLyrics() bool { return true }

func (s *SodaPlatform) SupportsRecognition() bool { return false }

func (s *SodaPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{Download: true, Search: true, Lyrics: true, Recognition: false, HiRes: false}
}

func (s *SodaPlatform) Metadata() platform.Meta {
	return platform.Meta{
		Name:          "soda",
		DisplayName:   "汽水音乐",
		Emoji:         "🥤",
		Aliases:       []string{"soda", "qs", "qishui", "汽水", "汽水音乐"},
		AllowGroupURL: true,
	}
}

func (s *SodaPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if s == nil || s.client == nil {
		return nil, platform.NewUnavailableError("soda", "track", trackID)
	}
	return s.client.FetchDownloadInfo(ctx, trackID, quality)
}

func (s *SodaPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if s == nil || s.client == nil {
		return nil, platform.NewUnavailableError("soda", "search", query)
	}
	return s.client.Search(ctx, query, limit)
}

func (s *SodaPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	if s == nil || s.client == nil {
		return nil, platform.NewUnavailableError("soda", "lyrics", trackID)
	}
	_, lyric, err := s.client.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(lyric) == "" {
		return nil, platform.NewUnavailableError("soda", "lyrics", trackID)
	}
	return &platform.Lyrics{Plain: lyric, Timestamped: parseSodaLyricLines(lyric)}, nil
}

func selectSodaPlayInfo(playInfos []sodaPlayInfo, quality platform.Quality) *sodaPlayInfo {
	if len(playInfos) == 0 {
		return nil
	}
	rankByQuality := map[string]int{
		"lossless": 6000,
		"hi_res":   5000,
		"spatial":  4500,
		"highest":  4000,
		"high":     3000,
		"higher":   2000,
		"medium":   1000,
	}
	rank := func(item sodaPlayInfo) int {
		qualityName := strings.ToLower(strings.TrimSpace(item.Quality))
		base := rankByQuality[qualityName]
		if base == 0 {
			base = item.Bitrate
		}
		switch quality {
		case platform.QualityLossless:
			if qualityName == "lossless" {
				base += 1000000
			}
		case platform.QualityHiRes:
			if qualityName == "lossless" {
				base += 1000000
			} else if qualityName == "hi_res" {
				base += 900000
			} else if qualityName == "spatial" {
				base += 800000
			}
		case platform.QualityHigh:
			if qualityName == "highest" {
				base += 1000000
			} else if qualityName == "high" || qualityName == "hi_res" || qualityName == "spatial" {
				base += 800000
			}
		case platform.QualityStandard:
			if qualityName == "higher" {
				base += 1000000
			} else if qualityName == "medium" {
				base += 900000
			}
		}
		return base
	}
	ordered := append([]sodaPlayInfo(nil), playInfos...)
	sort.SliceStable(ordered, func(i, j int) bool {
		ri := rank(ordered[i])
		rj := rank(ordered[j])
		if ri != rj {
			return ri > rj
		}
		if ordered[i].Size != ordered[j].Size {
			return ordered[i].Size > ordered[j].Size
		}
		return ordered[i].Bitrate > ordered[j].Bitrate
	})
	return &ordered[0]
}

func mapSodaQuality(playInfo *sodaPlayInfo, bitrate int) platform.Quality {
	if playInfo != nil && bitrate <= 0 {
		bitrate = playInfo.Bitrate
	}
	qualityName := ""
	if playInfo != nil {
		qualityName = strings.ToLower(strings.TrimSpace(playInfo.Quality))
	}
	switch {
	case qualityName == "lossless":
		return platform.QualityLossless
	case qualityName == "hi_res" || qualityName == "spatial":
		return platform.QualityHiRes
	case qualityName == "highest" || qualityName == "high":
		return platform.QualityHigh
	case bitrate >= 192:
		return platform.QualityHigh
	default:
		return platform.QualityStandard
	}
}

func (s *SodaPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	_ = ctx
	_ = audioData
	return nil, platform.NewUnsupportedError("soda", "audio recognition")
}

func (s *SodaPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	if s == nil || s.client == nil {
		return nil, platform.NewUnavailableError("soda", "track", trackID)
	}
	track, _, err := s.client.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if track == nil {
		return nil, platform.NewNotFoundError("soda", "track", trackID)
	}
	return track, nil
}

func (s *SodaPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	if s == nil || s.client == nil {
		return nil, platform.NewUnavailableError("soda", "artist", artistID)
	}
	artist, _, err := s.client.GetArtist(ctx, artistID)
	if err != nil {
		return nil, err
	}
	if artist == nil {
		return nil, platform.NewNotFoundError("soda", "artist", artistID)
	}
	return artist, nil
}

func (s *SodaPlatform) GetArtistDetails(ctx context.Context, artistID string) (*platform.Artist, int, error) {
	if s == nil || s.client == nil {
		return nil, 0, platform.NewUnavailableError("soda", "artist", artistID)
	}
	artist, trackCount, err := s.client.GetArtist(ctx, artistID)
	if err != nil {
		return nil, 0, err
	}
	if artist == nil {
		return nil, 0, platform.NewNotFoundError("soda", "artist", artistID)
	}
	return artist, trackCount, nil
}

func (s *SodaPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	if s == nil || s.client == nil {
		return nil, platform.NewUnavailableError("soda", "album", albumID)
	}
	album, _, err := s.client.GetAlbum(ctx, albumID)
	if err != nil {
		return nil, err
	}
	if album == nil {
		return nil, platform.NewNotFoundError("soda", "album", albumID)
	}
	return album, nil
}

func (s *SodaPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	isAlbum, rawID := parseCollectionID(playlistID)
	if isAlbum {
		album, tracks, err := s.client.GetAlbum(ctx, rawID)
		if err != nil {
			return nil, err
		}
		if album == nil {
			return nil, platform.NewNotFoundError("soda", "album", rawID)
		}
		return &platform.Playlist{
			ID:          playlistID,
			Platform:    "soda",
			Title:       firstNonEmptyString(album.Title, rawID),
			Description: firstNonEmptyString(album.Description, "专辑"),
			CoverURL:    album.CoverURL,
			Creator:     firstNonEmptyString(joinSodaArtistNames(album.Artists), "汽水音乐"),
			TrackCount:  maxInt(album.TrackCount, len(tracks)),
			Tracks:      tracks,
			URL:         album.URL,
		}, nil
	}
	if s == nil || s.client == nil {
		return nil, platform.NewUnavailableError("soda", "playlist", playlistID)
	}
	return s.client.GetPlaylist(ctx, playlistID)
}

func joinSodaArtistNames(artists []platform.Artist) string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		if strings.TrimSpace(artist.Name) == "" {
			continue
		}
		names = append(names, artist.Name)
	}
	return strings.Join(names, "/")
}

func (s *SodaPlatform) MatchURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchURL(rawURL)
}

func (s *SodaPlatform) MatchPlaylistURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchPlaylistURL(rawURL)
}

func (s *SodaPlatform) MatchArtistURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchArtistURL(rawURL)
}

func (s *SodaPlatform) MatchText(text string) (string, bool) {
	return NewTextMatcher().MatchText(text)
}

func (s *SodaPlatform) ShortLinkHosts() []string {
	return []string{"qishui.douyin.com", "z-qishui.douyin.com", "music.douyin.com", "bubble.qishui.com"}
}

func (s *SodaPlatform) CheckCookie(ctx context.Context) (platform.CookieCheckResult, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := s.GetDownloadInfo(checkCtx, "6696534426169378817", platform.QualityHigh)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("汽水下载链路校验失败: %v", err)}, nil
	}
	if info == nil || strings.TrimSpace(info.URL) == "" {
		return platform.CookieCheckResult{OK: false, Message: "下载链接为空"}, nil
	}
	return platform.CookieCheckResult{OK: true, Message: "汽水音乐下载能力可用"}, nil
}
