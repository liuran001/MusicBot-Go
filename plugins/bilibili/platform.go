package bilibili

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"sort"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// BilibiliPlatform implements the Platform interface for Bilibili Audio & Video.
type BilibiliPlatform struct {
	client *Client
}

// NewPlatform creates a new BilibiliPlatform instance.
func NewPlatform(client *Client) *BilibiliPlatform {
	return &BilibiliPlatform{
		client: client,
	}
}

// Name returns the platform identifier.
func (b *BilibiliPlatform) Name() string {
	return "bilibili"
}

// SupportsDownload indicates whether this platform supports downloading audio files.
func (b *BilibiliPlatform) SupportsDownload() bool {
	return true
}

// SupportsSearch indicates whether this platform supports searching for tracks.
func (b *BilibiliPlatform) SupportsSearch() bool {
	return false // Search requires complex WBI signing, omitted for now
}

// SupportsLyrics indicates whether this platform supports fetching lyrics.
func (b *BilibiliPlatform) SupportsLyrics() bool {
	return true
}

// SupportsRecognition indicates whether this platform supports audio recognition.
func (b *BilibiliPlatform) SupportsRecognition() bool {
	return false
}

func (b *BilibiliPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Download:    true,
		Search:      false,
		Lyrics:      true,
		Recognition: false,
		HiRes:       false,
	}
}

func (b *BilibiliPlatform) Metadata() platform.Meta {
	return platform.Meta{
		Name:          "bilibili",
		DisplayName:   "哔哩哔哩",
		Emoji:         "📺",
		Aliases:       []string{"bilibili", "b站", "bili"},
		AllowGroupURL: false,
	}
}

// GetDownloadInfo fetches stream URL. Routes logic by trackID format.
func (b *BilibiliPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if strings.HasPrefix(trackID, "b23:") {
		resolvedID, err := b.client.ResolveB23ID(ctx, strings.TrimPrefix(trackID, "b23:"))
		if err != nil {
			return nil, platform.NewUnavailableError("bilibili", "shortlink", trackID)
		}
		trackID = resolvedID
	}

	if strings.HasPrefix(trackID, "BV") || strings.HasPrefix(strings.ToLower(trackID), "av") {
		return b.getVideoDownloadInfo(ctx, trackID, quality)
	}
	return b.getAudioDownloadInfo(ctx, trackID, quality)
}

func (b *BilibiliPlatform) getAudioDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	musicID, err := strconv.Atoi(trackID)
	if err != nil {
		return nil, platform.NewNotFoundError("bilibili", "track", trackID)
	}

	qualityCode := 0
	switch quality {
	case platform.QualityLossless, platform.QualityHiRes:
		qualityCode = 3
	case platform.QualityHigh:
		qualityCode = 2
	case platform.QualityStandard:
		qualityCode = 0
	}

	streamData, err := b.client.GetAudioStreamUrl(ctx, musicID, qualityCode)
	if err != nil {
		return nil, fmt.Errorf("bilibili: failed to get stream url: %w", err)
	}

	if streamData == nil || len(streamData.Cdns) == 0 {
		return nil, platform.NewUnavailableError("bilibili", "track", trackID)
	}

	url := streamData.Cdns[0]
	expiresAt := time.Now().Add(time.Duration(streamData.Timeout) * time.Second)
	info := &platform.DownloadInfo{
		URL:       url,
		Size:      int64(streamData.Size),
		Format:    "mp3",
		Quality:   b.resolveQualityCode(streamData.Type),
		ExpiresAt: &expiresAt,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Referer":    "https://www.bilibili.com/",
		},
	}

	if streamData.Type == 3 {
		info.Format = "flac"
	}
	return info, nil
}

func (b *BilibiliPlatform) getVideoDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	videoInfo, err := b.client.GetVideoInfo(ctx, trackID)
	if err != nil {
		return nil, fmt.Errorf("bilibili: failed to fetch video info for stream: %w", err)
	}

	if videoInfo == nil || videoInfo.Cid == 0 {
		return nil, platform.NewUnavailableError("bilibili", "track", trackID)
	}

	audioStreams, err := b.client.GetVideoPlayUrl(ctx, videoInfo.Bvid, videoInfo.Cid)
	if err != nil {
		return nil, fmt.Errorf("bilibili: failed to fetch dash play stream: %w", err)
	}

	if len(audioStreams) == 0 {
		return nil, platform.NewUnavailableError("bilibili", "track", trackID)
	}

	// Sort streams by bandwidth ascending (Lowest first)
	sort.Slice(audioStreams, func(i, j int) bool {
		return audioStreams[i].Bandwidth < audioStreams[j].Bandwidth
	})

	var selectedStream *VideoDashAudio
	switch quality {
	case platform.QualityStandard:
		// Pick the lowest available audio (usually 64kbps / id 30216)
		selectedStream = &audioStreams[0]
	case platform.QualityHigh:
		// Pick middle/higher one (usually 132kbps / id 30232)
		midIdx := len(audioStreams) / 2
		selectedStream = &audioStreams[midIdx]
	case platform.QualityLossless, platform.QualityHiRes:
		// Pick the highest one (usually 192kbps / Dolby / HiRes, id 30280 / 30250+)
		selectedStream = &audioStreams[len(audioStreams)-1]
	default:
		// Default to highest
		selectedStream = &audioStreams[len(audioStreams)-1]
	}

	// Determine resulting quality enum based on Dash audio ID or bandwidth
	var resolvedQuality platform.Quality
	switch selectedStream.ID {
	case 30216:
		resolvedQuality = platform.QualityStandard
	case 30232:
		resolvedQuality = platform.QualityHigh
	case 30280, 30250:
		resolvedQuality = platform.QualityLossless
	case 30251:
		resolvedQuality = platform.QualityHiRes
	default:
		if selectedStream.Bandwidth > 150000 {
			resolvedQuality = platform.QualityLossless
		} else if selectedStream.Bandwidth > 80000 {
			resolvedQuality = platform.QualityHigh
		} else {
			resolvedQuality = platform.QualityStandard
		}
	}

	// Assuming Dash URL timeouts are usually 2 hours, set it to 1h50m
	expiresAt := time.Now().Add(110 * time.Minute)

	// Format is usually derived from codec, we default to m4a instead of mp4 for audio
	info := &platform.DownloadInfo{
		URL:       selectedStream.BaseURL,
		Size:      0, // the API does not always return raw sizes unless accessed with HEAD
		Format:    "m4a",
		Quality:   resolvedQuality,
		ExpiresAt: &expiresAt,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Referer":    "https://www.bilibili.com/",
		},
	}

	return info, nil
}

// GetTrack retrieves song detailing info
func (b *BilibiliPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	if strings.HasPrefix(trackID, "b23:") {
		resolvedID, err := b.client.ResolveB23ID(ctx, strings.TrimPrefix(trackID, "b23:"))
		if err != nil {
			return nil, platform.NewUnavailableError("bilibili", "shortlink", trackID)
		}
		trackID = resolvedID
	}

	if strings.HasPrefix(trackID, "BV") || strings.HasPrefix(strings.ToLower(trackID), "av") {
		return b.getVideoTrack(ctx, trackID)
	}
	return b.getAudioTrack(ctx, trackID)
}

func (b *BilibiliPlatform) getAudioTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	musicID, err := strconv.Atoi(trackID)
	if err != nil {
		return nil, platform.NewNotFoundError("bilibili", "track", trackID)
	}

	songInfo, err := b.client.GetAudioSongInfo(ctx, musicID)
	if err != nil {
		return nil, fmt.Errorf("bilibili: failed to get song detail: %w", err)
	}

	if songInfo == nil || songInfo.ID == 0 {
		return nil, platform.NewNotFoundError("bilibili", "track", trackID)
	}

	artists := []platform.Artist{
		{
			ID:       strconv.Itoa(songInfo.UID),
			Platform: "bilibili",
			Name:     songInfo.UName,
			URL:      fmt.Sprintf("https://space.bilibili.com/%d", songInfo.UID),
		},
	}

	return &platform.Track{
		ID:       strconv.Itoa(songInfo.ID),
		Platform: "bilibili",
		Title:    songInfo.Title,
		Artists:  artists,
		Duration: time.Duration(songInfo.Duration) * time.Second,
		CoverURL: songInfo.Cover,
		URL:      fmt.Sprintf("https://www.bilibili.com/audio/au%d", songInfo.ID),
	}, nil
}

func (b *BilibiliPlatform) getVideoTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	videoInfo, err := b.client.GetVideoInfo(ctx, trackID)
	if err != nil {
		return nil, fmt.Errorf("bilibili: failed to get video detail: %w", err)
	}

	if videoInfo == nil || videoInfo.Bvid == "" {
		return nil, platform.NewNotFoundError("bilibili", "track", trackID)
	}

	artists := []platform.Artist{
		{
			ID:       strconv.Itoa(videoInfo.Owner.Mid),
			Platform: "bilibili",
			Name:     videoInfo.Owner.Name,
			URL:      fmt.Sprintf("https://space.bilibili.com/%d", videoInfo.Owner.Mid),
		},
	}

	return &platform.Track{
		ID:       videoInfo.Bvid,
		Platform: "bilibili",
		Title:    videoInfo.Title,
		Artists:  artists,
		Duration: time.Duration(videoInfo.Duration) * time.Second,
		CoverURL: videoInfo.Pic,
		URL:      fmt.Sprintf("https://www.bilibili.com/video/%s", videoInfo.Bvid),
	}, nil
}

// GetLyrics fetches lyric from the metadata property
func (b *BilibiliPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	if strings.HasPrefix(trackID, "b23:") {
		resolvedID, err := b.client.ResolveB23ID(ctx, strings.TrimPrefix(trackID, "b23:"))
		if err != nil {
			return nil, platform.NewUnavailableError("bilibili", "shortlink", trackID)
		}
		trackID = resolvedID
	}

	if strings.HasPrefix(trackID, "BV") || strings.HasPrefix(strings.ToLower(trackID), "av") {
		// Bilibili video subtitle extracting is omitted for now, so we return unavailable
		return nil, platform.NewUnavailableError("bilibili", "lyrics", trackID)
	}

	musicID, err := strconv.Atoi(trackID)
	if err != nil {
		return nil, platform.NewNotFoundError("bilibili", "track", trackID)
	}

	songInfo, err := b.client.GetAudioSongInfo(ctx, musicID)
	if err != nil {
		return nil, fmt.Errorf("bilibili: failed to fetch song info for lyric: %w", err)
	}

	if songInfo.Lyric == "" {
		return nil, platform.NewUnavailableError("bilibili", "lyrics", trackID)
	}

	lyricStr, err := b.client.GetLyric(ctx, songInfo.Lyric)
	if err != nil {
		return nil, fmt.Errorf("bilibili: failed to fetch lyric data: %w", err)
	}

	return &platform.Lyrics{
		Plain:       lyricStr,
		Timestamped: platform.ParseLRCTimestampedLines(lyricStr),
	}, nil
}

// Other unsupported interfaces

func (b *BilibiliPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	return nil, platform.NewUnsupportedError("bilibili", "search")
}

func (b *BilibiliPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.NewUnsupportedError("bilibili", "audio recognition")
}

func (b *BilibiliPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, platform.NewUnsupportedError("bilibili", "get artist")
}

func (b *BilibiliPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, platform.NewUnsupportedError("bilibili", "get album")
}

func (b *BilibiliPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	return nil, platform.NewUnsupportedError("bilibili", "get playlist")
}

// MatchURL implements platform.URLMatcher
func (b *BilibiliPlatform) MatchURL(url string) (trackID string, matched bool) {
	matcher := NewURLMatcher()
	return matcher.MatchURL(url)
}

// MatchText implements platform.TextMatcher
func (b *BilibiliPlatform) MatchText(text string) (trackID string, matched bool) {
	matcher := NewURLMatcher()
	return matcher.MatchText(text)
}

func (b *BilibiliPlatform) resolveQualityCode(typeID int) platform.Quality {
	switch typeID {
	case 3:
		return platform.QualityLossless // FLAC
	case 2:
		return platform.QualityHigh // 320K
	default:
		return platform.QualityStandard // 192K, 128K
	}
}
