package netease

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/XiaoMengXinX/Music163Api-Go/types"
	"github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// NeteasePlatform implements the Platform interface for NetEase Cloud Music.
// It wraps the existing NetEase client and provides a unified interface.
type NeteasePlatform struct {
	client *Client
}

// NewPlatform creates a new NeteasePlatform instance.
func NewPlatform(client *Client) *NeteasePlatform {
	return &NeteasePlatform{
		client: client,
	}
}

// Name returns the platform identifier.
func (n *NeteasePlatform) Name() string {
	return "netease"
}

// SupportsDownload indicates whether this platform supports downloading audio files.
func (n *NeteasePlatform) SupportsDownload() bool {
	return true
}

// SupportsSearch indicates whether this platform supports searching for tracks.
func (n *NeteasePlatform) SupportsSearch() bool {
	return true
}

// SupportsLyrics indicates whether this platform supports fetching lyrics.
func (n *NeteasePlatform) SupportsLyrics() bool {
	return true
}

// SupportsRecognition indicates whether this platform supports audio recognition.
func (n *NeteasePlatform) SupportsRecognition() bool {
	return true // NetEase has 听歌识曲 feature
}

func (n *NeteasePlatform) CheckCookie(ctx context.Context) (platform.CookieCheckResult, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := n.GetDownloadInfo(checkCtx, "1463165983", platform.QualityHiRes)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("Hi-Res 校验失败: %v", err)}, nil
	}
	if info == nil || strings.TrimSpace(info.URL) == "" || info.Size <= 0 {
		return platform.CookieCheckResult{OK: false, Message: "Hi-Res 下载链接为空或文件大小为 0"}, nil
	}
	return platform.CookieCheckResult{OK: true, Message: "Hi-Res 可用"}, nil
}

func (n *NeteasePlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Download:    true,
		Search:      true,
		Lyrics:      true,
		Recognition: true,
		HiRes:       true,
	}
}

func (n *NeteasePlatform) Metadata() platform.Meta {
	return platform.Meta{
		Name:          "netease",
		DisplayName:   "网易云音乐",
		Emoji:         "🎵",
		Aliases:       []string{"netease", "163", "wy", "网易云", "网易云音乐"},
		AllowGroupURL: true,
	}
}

func (n *NeteasePlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	// Convert trackID string to int
	musicID, err := strconv.Atoi(trackID)
	if err != nil {
		return nil, platform.NewNotFoundError("netease", "track", trackID)
	}

	// Map quality to NetEase quality level
	qualityLevel := n.qualityToBitrateLevel(quality)

	// Get song URL
	songURL, err := n.client.GetSongURL(ctx, musicID, qualityLevel)
	if err != nil {
		return nil, fmt.Errorf("netease: failed to get song URL: %w", err)
	}

	if len(songURL.Data) == 0 || songURL.Data[0].Url == "" {
		return nil, platform.NewUnavailableError("netease", "track", trackID)
	}

	urlData := songURL.Data[0]

	format := "mp3"
	if urlData.Type != "" {
		format = urlData.Type
	}

	expiresAt := time.Now().Add(time.Duration(urlData.Expi) * time.Second)
	info := &platform.DownloadInfo{
		URL:       urlData.Url,
		Size:      int64(urlData.Size),
		Format:    format,
		Bitrate:   urlData.Br / 1000,
		MD5:       urlData.Md5,
		Quality:   n.bitrateToQuality(urlData.Br),
		ExpiresAt: &expiresAt,
	}

	return info, nil
}

// Search searches for tracks matching the given query string.
func (n *NeteasePlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	result, err := n.client.Search(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("netease: search failed: %w", err)
	}

	tracks := make([]platform.Track, 0, len(result.Result.Songs))
	for _, song := range result.Result.Songs {
		track := n.convertSearchSongToTrack(song)
		tracks = append(tracks, track)
	}

	return tracks, nil
}

// GetLyrics retrieves the lyrics for the given track ID.
func (n *NeteasePlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	musicID, err := strconv.Atoi(trackID)
	if err != nil {
		return nil, platform.NewNotFoundError("netease", "track", trackID)
	}

	lyricData, err := n.client.GetLyric(ctx, musicID)
	if err != nil {
		return nil, fmt.Errorf("netease: failed to get lyrics: %w", err)
	}

	return n.convertLyrics(lyricData), nil
}

// RecognizeAudio attempts to identify a track from the provided audio data.
func (n *NeteasePlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	// NetEase API supports audio recognition, but implementation would require
	// additional API integration. Returning unsupported for now.
	return nil, platform.NewUnsupportedError("netease", "audio recognition")
}

// MatchURL implements platform.URLMatcher interface.
// It delegates to URLMatcher for extracting track IDs from NetEase URLs.
func (n *NeteasePlatform) MatchURL(url string) (trackID string, matched bool) {
	matcher := NewURLMatcher()
	return matcher.MatchURL(url)
}

// MatchPlaylistURL implements platform.PlaylistURLMatcher interface.
func (n *NeteasePlatform) MatchPlaylistURL(url string) (playlistID string, matched bool) {
	matcher := NewURLMatcher()
	return matcher.MatchPlaylistURL(url)
}

// ShortLinkHosts implements platform.ShortLinkProvider.
func (n *NeteasePlatform) ShortLinkHosts() []string {
	return []string{"163cn.tv", "163cn.link"}
}

// GetTrack retrieves detailed information about a track by its ID.
func (n *NeteasePlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	musicID, err := strconv.Atoi(trackID)
	if err != nil {
		return nil, platform.NewNotFoundError("netease", "track", trackID)
	}

	detail, err := n.client.GetSongDetail(ctx, musicID)
	if err != nil {
		return nil, fmt.Errorf("netease: failed to get track detail: %w", err)
	}

	if len(detail.Songs) == 0 {
		return nil, platform.NewNotFoundError("netease", "track", trackID)
	}

	track := n.convertSongDetailToTrack(*detail)
	return &track, nil
}

// GetArtist retrieves detailed information about an artist by their ID.
func (n *NeteasePlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	// NetEase has artist APIs, but not exposed in current client
	return nil, platform.NewUnsupportedError("netease", "get artist")
}

// GetAlbum retrieves detailed information about an album by its ID.
func (n *NeteasePlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	// NetEase has album APIs, but not exposed in current client
	return nil, platform.NewUnsupportedError("netease", "get album")
}

// GetPlaylist retrieves detailed information about a playlist by its ID.
func (n *NeteasePlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	if n.client == nil {
		return nil, platform.NewUnavailableError("netease", "playlist", playlistID)
	}
	pid, err := strconv.Atoi(playlistID)
	if err != nil {
		return nil, platform.NewNotFoundError("netease", "playlist", playlistID)
	}
	detail, err := n.client.GetPlaylistDetail(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("netease: failed to get playlist detail: %w", err)
	}
	if detail == nil || detail.Playlist.Id == 0 {
		return nil, platform.NewNotFoundError("netease", "playlist", playlistID)
	}
	description := ""
	if detail.Playlist.Description != nil {
		switch v := detail.Playlist.Description.(type) {
		case string:
			description = strings.TrimSpace(v)
		default:
			description = strings.TrimSpace(fmt.Sprintf("%v", v))
		}
	}
	tracks := make([]platform.Track, 0, len(detail.Playlist.TrackIds))
	if len(detail.Playlist.TrackIds) > 0 {
		trackIDs := make([]int, 0, len(detail.Playlist.TrackIds))
		for _, item := range detail.Playlist.TrackIds {
			if item.Id > 0 {
				trackIDs = append(trackIDs, item.Id)
			}
		}
		limit := platform.PlaylistLimitFromContext(ctx)
		offset := platform.PlaylistOffsetFromContext(ctx)
		if offset < 0 {
			offset = 0
		}
		if offset > 0 {
			if offset >= len(trackIDs) {
				trackIDs = nil
			} else {
				trackIDs = trackIDs[offset:]
			}
		}
		if limit > 0 && len(trackIDs) > limit {
			trackIDs = trackIDs[:limit]
		}
		const batchSize = 100
		for start := 0; start < len(trackIDs); start += batchSize {
			end := start + batchSize
			if end > len(trackIDs) {
				end = len(trackIDs)
			}
			batch := trackIDs[start:end]
			songs, err := n.client.GetSongDetailBatch(ctx, batch)
			if err != nil {
				return nil, fmt.Errorf("netease: failed to get playlist tracks: %w", err)
			}
			if songs == nil {
				continue
			}
			for _, song := range songs.Songs {
				tracks = append(tracks, n.convertSongDetailDataToTrack(song))
			}
		}
	}
	if len(tracks) == 0 && len(detail.Playlist.Tracks) > 0 {
		trackLimit := platform.PlaylistLimitFromContext(ctx)
		trackOffset := platform.PlaylistOffsetFromContext(ctx)
		trackList := detail.Playlist.Tracks
		if trackOffset < 0 {
			trackOffset = 0
		}
		if trackOffset > 0 {
			if trackOffset >= len(trackList) {
				trackList = nil
			} else {
				trackList = trackList[trackOffset:]
			}
		}
		if trackLimit > 0 && len(trackList) > trackLimit {
			trackList = trackList[:trackLimit]
		}
		tracks = make([]platform.Track, 0, len(trackList))
		for _, song := range trackList {
			artists := make([]platform.Artist, 0, len(song.Ar))
			for _, ar := range song.Ar {
				artists = append(artists, platform.Artist{
					ID:       strconv.Itoa(ar.Id),
					Platform: "netease",
					Name:     ar.Name,
					URL:      fmt.Sprintf("https://music.163.com/artist?id=%d", ar.Id),
				})
			}
			var album *platform.Album
			if song.Al.Id != 0 {
				album = &platform.Album{
					ID:       strconv.Itoa(song.Al.Id),
					Platform: "netease",
					Title:    song.Al.Name,
					CoverURL: song.Al.PicUrl,
					Artists:  artists,
					URL:      fmt.Sprintf("https://music.163.com/album?id=%d", song.Al.Id),
				}
			}
			duration := time.Duration(song.Dt) * time.Millisecond
			tracks = append(tracks, platform.Track{
				ID:       strconv.Itoa(song.Id),
				Platform: "netease",
				Title:    song.Name,
				Artists:  artists,
				Album:    album,
				Duration: duration,
				CoverURL: song.Al.PicUrl,
				URL:      fmt.Sprintf("https://music.163.com/song?id=%d", song.Id),
			})
		}
	}
	trackCount := detail.Playlist.TrackCount
	if trackCount == 0 {
		trackCount = len(tracks)
	}
	return &platform.Playlist{
		ID:          strconv.Itoa(detail.Playlist.Id),
		Platform:    "netease",
		Title:       detail.Playlist.Name,
		Description: description,
		CoverURL:    detail.Playlist.CoverImgUrl,
		Creator:     detail.Playlist.Creator.Nickname,
		TrackCount:  trackCount,
		Tracks:      tracks,
		URL:         fmt.Sprintf("https://music.163.com/playlist?id=%d", detail.Playlist.Id),
	}, nil
}

// qualityToBitrateLevel maps platform Quality enum to NetEase quality level strings.
func (n *NeteasePlatform) qualityToBitrateLevel(quality platform.Quality) string {
	switch quality {
	case platform.QualityStandard:
		return "standard" // 128kbps
	case platform.QualityHigh:
		return "higher" // 320kbps
	case platform.QualityLossless:
		return "lossless" // FLAC
	case platform.QualityHiRes:
		return "hires" // Hi-Res
	default:
		return "standard"
	}
}

// bitrateToQuality maps NetEase bitrate to platform Quality enum.
func (n *NeteasePlatform) bitrateToQuality(bitrate int) platform.Quality {
	// Bitrate is in bps, convert to kbps for comparison
	kbps := bitrate / 1000

	if kbps >= 1500 {
		return platform.QualityHiRes
	} else if kbps >= 1000 {
		return platform.QualityLossless
	} else if kbps >= 320 {
		return platform.QualityHigh
	} else {
		return platform.QualityStandard
	}
}

// convertSongDetailToTrack converts NetEase SongDetailData to platform Track.
func (n *NeteasePlatform) convertSongDetailToTrack(song bot.SongDetail) platform.Track {
	if len(song.Songs) == 0 {
		return platform.Track{}
	}

	songData := song.Songs[0]

	// Convert artists
	artists := make([]platform.Artist, 0, len(songData.Ar))
	for _, ar := range songData.Ar {
		artists = append(artists, platform.Artist{
			ID:       strconv.Itoa(ar.Id),
			Platform: "netease",
			Name:     ar.Name,
			URL:      fmt.Sprintf("https://music.163.com/artist?id=%d", ar.Id),
		})
	}

	// Convert album
	var album *platform.Album
	if songData.Al.Id != 0 {
		album = &platform.Album{
			ID:       strconv.Itoa(songData.Al.Id),
			Platform: "netease",
			Title:    songData.Al.Name,
			CoverURL: songData.Al.PicUrl,
			Artists:  artists,
			URL:      fmt.Sprintf("https://music.163.com/album?id=%d", songData.Al.Id),
		}
	}

	// Convert duration from milliseconds to time.Duration
	duration := time.Duration(songData.Dt) * time.Millisecond

	return platform.Track{
		ID:       strconv.Itoa(songData.Id),
		Platform: "netease",
		Title:    songData.Name,
		Artists:  artists,
		Album:    album,
		Duration: duration,
		CoverURL: songData.Al.PicUrl,
		URL:      fmt.Sprintf("https://music.163.com/song?id=%d", songData.Id),
	}
}

func (n *NeteasePlatform) convertSongDetailDataToTrack(song types.SongDetailData) platform.Track {
	artists := make([]platform.Artist, 0, len(song.Ar))
	for _, ar := range song.Ar {
		artists = append(artists, platform.Artist{
			ID:       strconv.Itoa(ar.Id),
			Platform: "netease",
			Name:     ar.Name,
			URL:      fmt.Sprintf("https://music.163.com/artist?id=%d", ar.Id),
		})
	}
	var album *platform.Album
	if song.Al.Id != 0 {
		album = &platform.Album{
			ID:       strconv.Itoa(song.Al.Id),
			Platform: "netease",
			Title:    song.Al.Name,
			CoverURL: song.Al.PicUrl,
			Artists:  artists,
			URL:      fmt.Sprintf("https://music.163.com/album?id=%d", song.Al.Id),
		}
	}
	duration := time.Duration(song.Dt) * time.Millisecond
	return platform.Track{
		ID:       strconv.Itoa(song.Id),
		Platform: "netease",
		Title:    song.Name,
		Artists:  artists,
		Album:    album,
		Duration: duration,
		CoverURL: song.Al.PicUrl,
		URL:      fmt.Sprintf("https://music.163.com/song?id=%d", song.Id),
	}
}

// convertSearchSongToTrack converts search result song to platform Track.
func (n *NeteasePlatform) convertSearchSongToTrack(song struct {
	Id      int    `json:"id"`
	Name    string `json:"name"`
	Artists []struct {
		Id        int           `json:"id"`
		Name      string        `json:"name"`
		PicUrl    interface{}   `json:"picUrl"`
		Alias     []interface{} `json:"alias"`
		AlbumSize int           `json:"albumSize"`
		PicId     int           `json:"picId"`
		Img1V1Url string        `json:"img1v1Url"`
		Img1V1    int           `json:"img1v1"`
		Trans     interface{}   `json:"trans"`
	} `json:"artists"`
	Album struct {
		Id     int    `json:"id"`
		Name   string `json:"name"`
		Artist struct {
			Id        int           `json:"id"`
			Name      string        `json:"name"`
			PicUrl    interface{}   `json:"picUrl"`
			Alias     []interface{} `json:"alias"`
			AlbumSize int           `json:"albumSize"`
			PicId     int           `json:"picId"`
			Img1V1Url string        `json:"img1v1Url"`
			Img1V1    int           `json:"img1v1"`
			Trans     interface{}   `json:"trans"`
		} `json:"artist"`
		PublishTime int64 `json:"publishTime"`
		Size        int   `json:"size"`
		CopyrightId int   `json:"copyrightId"`
		Status      int   `json:"status"`
		PicId       int64 `json:"picId"`
		Mark        int   `json:"mark"`
	} `json:"album"`
	Duration    int           `json:"duration"`
	CopyrightId int           `json:"copyrightId"`
	Status      int           `json:"status"`
	Alias       []interface{} `json:"alias"`
	Rtype       int           `json:"rtype"`
	Ftype       int           `json:"ftype"`
	Mvid        int           `json:"mvid"`
	Fee         int           `json:"fee"`
	RUrl        interface{}   `json:"rUrl"`
	Mark        int           `json:"mark"`
}) platform.Track {
	// Convert artists
	artists := make([]platform.Artist, 0, len(song.Artists))
	for _, ar := range song.Artists {
		artists = append(artists, platform.Artist{
			ID:       strconv.Itoa(ar.Id),
			Platform: "netease",
			Name:     ar.Name,
			URL:      fmt.Sprintf("https://music.163.com/artist?id=%d", ar.Id),
		})
	}

	// Convert album
	var album *platform.Album
	if song.Album.Id != 0 {
		album = &platform.Album{
			ID:       strconv.Itoa(song.Album.Id),
			Platform: "netease",
			Title:    song.Album.Name,
			CoverURL: fmt.Sprintf("https://p4.music.126.net/%d/%d.jpg", song.Album.PicId, song.Album.PicId),
			Artists:  artists,
			URL:      fmt.Sprintf("https://music.163.com/album?id=%d", song.Album.Id),
		}

		// Set release date if available
		if song.Album.PublishTime > 0 {
			releaseDate := time.Unix(song.Album.PublishTime/1000, 0)
			album.ReleaseDate = &releaseDate
		}
	}

	// Convert duration from milliseconds to time.Duration
	duration := time.Duration(song.Duration) * time.Millisecond

	return platform.Track{
		ID:       strconv.Itoa(song.Id),
		Platform: "netease",
		Title:    song.Name,
		Artists:  artists,
		Album:    album,
		Duration: duration,
		URL:      fmt.Sprintf("https://music.163.com/song?id=%d", song.Id),
	}
}

// convertLyrics converts NetEase lyrics to platform Lyrics.
func (n *NeteasePlatform) convertLyrics(lyricData *bot.Lyric) *platform.Lyrics {
	lyrics := &platform.Lyrics{
		Plain: lyricData.Lrc.Lyric,
	}

	// Add translation if available
	if lyricData.Tlyric.Lyric != "" {
		lyrics.Translation = lyricData.Tlyric.Lyric
	}

	// Parse timestamped lyrics
	if lyricData.Lrc.Lyric != "" {
		lyrics.Timestamped = n.parseLyricLines(lyricData.Lrc.Lyric)
	}

	return lyrics
}

// parseLyricLines parses LRC format lyrics into timestamped lines.
func (n *NeteasePlatform) parseLyricLines(lrc string) []platform.LyricLine {
	lines := strings.Split(lrc, "\n")
	result := make([]platform.LyricLine, 0, len(lines))

	// LRC format: [mm:ss.xx]lyric text
	re := regexp.MustCompile(`^\[(\d+):(\d+)\.(\d+)\](.*)$`)

	for _, line := range lines {
		matches := re.FindStringSubmatch(line)
		if len(matches) == 5 {
			minutes, _ := strconv.Atoi(matches[1])
			seconds, _ := strconv.Atoi(matches[2])
			centis, _ := strconv.Atoi(matches[3])
			text := strings.TrimSpace(matches[4])

			if text != "" {
				duration := time.Duration(minutes)*time.Minute +
					time.Duration(seconds)*time.Second +
					time.Duration(centis)*10*time.Millisecond

				result = append(result, platform.LyricLine{
					Time: duration,
					Text: text,
				})
			}
		}
	}

	return result
}
