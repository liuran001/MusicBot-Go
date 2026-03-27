package kugou

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guohuiyuan/music-lib/model"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type KugouPlatform struct {
	client *Client
}

func NewPlatform(client *Client) *KugouPlatform {
	return &KugouPlatform{client: client}
}

func (k *KugouPlatform) Name() string {
	return "kugou"
}

func (k *KugouPlatform) SupportsDownload() bool {
	return true
}

func (k *KugouPlatform) SupportsSearch() bool {
	return true
}

func (k *KugouPlatform) SupportsLyrics() bool {
	return true
}

func (k *KugouPlatform) SupportsRecognition() bool {
	return false
}

func (k *KugouPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Download: true,
		Search:   true,
		Lyrics:   true,
		HiRes:    true,
	}
}

func (k *KugouPlatform) Metadata() platform.Meta {
	return platform.Meta{
		Name:          "kugou",
		DisplayName:   "酷狗音乐",
		Emoji:         "🐶",
		Aliases:       []string{"kugou", "kg", "酷狗", "酷狗音乐"},
		AllowGroupURL: true,
	}
}

func (k *KugouPlatform) CheckCookie(ctx context.Context) (platform.CookieCheckResult, error) {
	if k == nil || k.client == nil {
		return platform.CookieCheckResult{OK: false, Message: "kugou client unavailable"}, nil
	}
	ok, err := k.client.CheckCookie(ctx)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("Cookie 校验失败: %v", err)}, nil
	}
	if !ok {
		return platform.CookieCheckResult{OK: false, Message: "Cookie 未检测到 VIP 能力或未配置"}, nil
	}
	return platform.CookieCheckResult{OK: true, Message: "Cookie 可用，已检测到 VIP 能力"}, nil
}

func (k *KugouPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "track", trackID)
	}
	song, err := k.client.GetDownloadInfo(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if song == nil || strings.TrimSpace(song.URL) == "" {
		return nil, platform.NewUnavailableError("kugou", "track", trackID)
	}
	resolvedQuality := qualityFromSong(song.Bitrate, song.Ext)
	if requestedQualityUnavailable(quality, resolvedQuality) {
		return nil, platform.NewInvalidQualityError("kugou", normalizeHash(trackID), quality)
	}
	return &platform.DownloadInfo{
		URL:     strings.TrimSpace(song.URL),
		Size:    song.Size,
		Format:  firstNonEmpty(song.Ext, detectExtFromURL(song.URL), "mp3"),
		Bitrate: song.Bitrate,
		Quality: resolvedQuality,
	}, nil
}

func (k *KugouPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "search", "")
	}
	songs, err := k.client.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	tracks := make([]platform.Track, 0, len(songs))
	for _, song := range songs {
		track := convertSong(song)
		if strings.TrimSpace(track.ID) == "" {
			continue
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func (k *KugouPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "lyrics", trackID)
	}
	lyric, err := k.client.GetLyrics(ctx, trackID)
	if err != nil {
		return nil, err
	}
	result := &platform.Lyrics{Plain: lyric}
	if parsed := platform.ParseLRCTimestampedLines(lyric); len(parsed) > 0 {
		result.Timestamped = parsed
	}
	return result, nil
}

func (k *KugouPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.NewUnsupportedError("kugou", "audio recognition")
}

func (k *KugouPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "track", trackID)
	}
	song, err := k.client.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	track := convertSong(*song)
	if strings.TrimSpace(track.ID) == "" {
		return nil, platform.NewNotFoundError("kugou", "track", trackID)
	}
	return &track, nil
}

func (k *KugouPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, platform.NewUnsupportedError("kugou", "get artist")
}

func (k *KugouPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, platform.NewUnsupportedError("kugou", "get album")
}

func (k *KugouPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "playlist", playlistID)
	}
	playlistData, songs, err := k.client.GetPlaylist(ctx, playlistID)
	if err != nil {
		return nil, err
	}
	tracks := make([]platform.Track, 0, len(songs))
	for _, song := range songs {
		tracks = append(tracks, convertSong(song))
	}
	offset := platform.PlaylistOffsetFromContext(ctx)
	if offset < 0 {
		offset = 0
	}
	if offset > 0 {
		if offset >= len(tracks) {
			tracks = nil
		} else {
			tracks = tracks[offset:]
		}
	}
	limit := platform.PlaylistLimitFromContext(ctx)
	if limit > 0 && len(tracks) > limit {
		tracks = tracks[:limit]
	}
	trackCount := playlistData.TrackCount
	if trackCount <= 0 {
		trackCount = len(songs)
	}
	return &platform.Playlist{
		ID:          strings.TrimSpace(playlistData.ID),
		Platform:    "kugou",
		Title:       strings.TrimSpace(playlistData.Name),
		Description: strings.TrimSpace(playlistData.Description),
		CoverURL:    strings.TrimSpace(playlistData.Cover),
		Creator:     strings.TrimSpace(playlistData.Creator),
		TrackCount:  trackCount,
		Tracks:      tracks,
		URL:         strings.TrimSpace(playlistData.Link),
	}, nil
}

func (k *KugouPlatform) MatchURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchURL(rawURL)
}

func (k *KugouPlatform) MatchPlaylistURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchPlaylistURL(rawURL)
}

func (k *KugouPlatform) MatchText(text string) (string, bool) {
	return NewTextMatcher().MatchText(text)
}

func convertSongModel(song songModelLike) platform.Track {
	artists := splitArtists(song.Artist)
	var album *platform.Album
	if strings.TrimSpace(song.Album) != "" || strings.TrimSpace(song.AlbumID) != "" {
		album = &platform.Album{
			ID:       strings.TrimSpace(song.AlbumID),
			Platform: "kugou",
			Title:    strings.TrimSpace(song.Album),
			Artists:  artists,
			CoverURL: strings.TrimSpace(song.Cover),
			URL:      buildAlbumURL(song.AlbumID),
		}
	}
	trackURL := strings.TrimSpace(song.Link)
	if trackURL == "" {
		trackURL = buildTrackLink(song.ID)
	}
	return platform.Track{
		ID:       normalizeHash(song.ID),
		Platform: "kugou",
		Title:    strings.TrimSpace(song.Name),
		Artists:  artists,
		Album:    album,
		Duration: time.Duration(song.Duration) * time.Second,
		CoverURL: strings.TrimSpace(song.Cover),
		URL:      trackURL,
	}
}

type songModelLike struct {
	ID       string
	Name     string
	Artist   string
	Album    string
	AlbumID  string
	Duration int
	Cover    string
	Link     string
	Bitrate  int
	Ext      string
	Size     int64
}

func convertSong(song model.Song) platform.Track {
	return convertSongModel(songModelLike{
		ID:       song.ID,
		Name:     song.Name,
		Artist:   song.Artist,
		Album:    song.Album,
		AlbumID:  song.AlbumID,
		Duration: song.Duration,
		Cover:    song.Cover,
		Link:     song.Link,
		Bitrate:  song.Bitrate,
		Ext:      song.Ext,
		Size:     song.Size,
	})
}

func splitArtists(value string) []platform.Artist {
	fields := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		switch r {
		case '/', '&', '、', ',', '，':
			return true
		default:
			return false
		}
	})
	artists := make([]platform.Artist, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field)
		if name == "" {
			continue
		}
		artists = append(artists, platform.Artist{Platform: "kugou", Name: name})
	}
	if len(artists) == 0 && strings.TrimSpace(value) != "" {
		artists = append(artists, platform.Artist{Platform: "kugou", Name: strings.TrimSpace(value)})
	}
	return artists
}

func buildTrackLink(hash string) string {
	hash = normalizeHash(hash)
	if hash == "" {
		return ""
	}
	return "https://www.kugou.com/song/#hash=" + hash
}

func normalizeHash(value string) string {
	value = strings.TrimSpace(value)
	if matches := kugouHashPattern.FindStringSubmatch(value); len(matches) == 2 {
		return strings.ToLower(matches[1])
	}
	if kugouHashOnlyPattern.MatchString(value) {
		return strings.ToLower(value)
	}
	return ""
}

func qualityFromSong(bitrate int, ext string) platform.Quality {
	ext = strings.ToLower(strings.TrimSpace(ext))
	switch {
	case ext == "flac" || ext == "ape" || ext == "wav":
		if bitrate >= 2000 {
			return platform.QualityHiRes
		}
		return platform.QualityLossless
	case bitrate >= 1000:
		return platform.QualityHiRes
	case bitrate >= 700:
		return platform.QualityLossless
	case bitrate >= 320:
		return platform.QualityHigh
	default:
		return platform.QualityStandard
	}
}

func requestedQualityUnavailable(requested, actual platform.Quality) bool {
	return actual < requested
}

func detectExtFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	pathValue := strings.ToLower(parsed.Path)
	switch {
	case strings.HasSuffix(pathValue, ".flac"):
		return "flac"
	case strings.HasSuffix(pathValue, ".ape"):
		return "ape"
	case strings.HasSuffix(pathValue, ".wav"):
		return "wav"
	case strings.HasSuffix(pathValue, ".m4a"):
		return "m4a"
	case strings.HasSuffix(pathValue, ".aac"):
		return "aac"
	case strings.HasSuffix(pathValue, ".mp3"):
		return "mp3"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func buildAlbumURL(albumID string) string {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return ""
	}
	if _, err := strconv.Atoi(albumID); err != nil {
		return ""
	}
	return "https://www.kugou.com/album/" + albumID + ".html"
}
