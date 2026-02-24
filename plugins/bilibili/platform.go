package bilibili

import (
	"context"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"regexp"
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
	return &BilibiliPlatform{client: client}
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
	return true
}

// SupportsLyrics indicates whether this platform supports fetching lyrics.
func (b *BilibiliPlatform) SupportsLyrics() bool {
	return true
}

// SupportsRecognition indicates whether this platform supports audio recognition.
func (b *BilibiliPlatform) SupportsRecognition() bool {
	return false
}

func (b *BilibiliPlatform) CheckCookie(ctx context.Context) (platform.CookieCheckResult, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	const checkTrackID = "BV1Rc1gBaERq"
	const targetAudioID = 30251 // FLAC

	videoInfo, err := b.client.GetVideoInfo(checkCtx, checkTrackID)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("测试视频信息获取失败: %v", err)}, nil
	}
	if videoInfo == nil || videoInfo.Cid == 0 {
		return platform.CookieCheckResult{OK: false, Message: "测试视频 CID 为空"}, nil
	}

	audioStreams, err := b.client.GetVideoPlayUrl(checkCtx, videoInfo.Bvid, videoInfo.Cid)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("测试音轨信息获取失败: %v", err)}, nil
	}

	var target *VideoDashAudio
	for i := range audioStreams {
		if audioStreams[i].ID == targetAudioID {
			target = &audioStreams[i]
			break
		}
	}
	if target == nil || strings.TrimSpace(target.BaseURL) == "" {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("目标音轨 id=%d 不可用", targetAudioID)}, nil
	}

	headers := map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Referer":    "https://www.bilibili.com/",
	}
	size, err := probeContentLength(checkCtx, target.BaseURL, headers)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("音轨 id=%d 可用但大小探测失败: %v", targetAudioID, err)}, nil
	}
	if size <= 0 {
		return platform.CookieCheckResult{OK: true, Message: fmt.Sprintf("音轨 id=%d 可用", targetAudioID)}, nil
	}

	return platform.CookieCheckResult{OK: true, Message: fmt.Sprintf("音轨 id=%d 可用: %.2fMB", targetAudioID, float64(size)/1024/1024)}, nil
}

func (b *BilibiliPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Download:    true,
		Search:      true,
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
		AllowGroupURL: true,
	}
}

func (b *BilibiliPlatform) ResolveTrackCategory(ctx context.Context, trackID string) (string, int, error) {
	if strings.HasPrefix(trackID, "b23:") {
		resolvedID, err := b.client.ResolveB23ID(ctx, strings.TrimPrefix(trackID, "b23:"))
		if err != nil {
			return "", 0, err
		}
		trackID = resolvedID
	}

	if !(strings.HasPrefix(trackID, "BV") || strings.HasPrefix(strings.ToLower(trackID), "av")) {
		return "", 0, nil
	}

	info, err := b.client.GetVideoInfo(ctx, trackID)
	if err != nil {
		return "", 0, err
	}
	if info == nil {
		return "", 0, nil
	}

	if info.TidV2 > 0 {
		name := strings.TrimSpace(info.TnameV2)
		if name == "" {
			name = strings.TrimSpace(info.Tname)
		}
		if name == "" {
			name = strings.TrimSpace(info.TypeName)
		}
		return name, info.TidV2, nil
	}

	if strings.TrimSpace(info.Tname) != "" {
		return strings.TrimSpace(info.Tname), info.Tid, nil
	}
	return strings.TrimSpace(info.TypeName), info.Tid, nil
}

func (b *BilibiliPlatform) AutoParseSettingKey() string {
	return ParseModeKey
}

func (b *BilibiliPlatform) ShouldAutoParse(ctx context.Context, trackID string, mode string) (bool, error) {
	switch normalizeParseMode(mode) {
	case ParseModeOff:
		return false, nil
	case ParseModeOn:
		return true, nil
	case ParseModeMusicKichiku:
		category, categoryID, err := b.ResolveTrackCategory(ctx, trackID)
		if err != nil {
			return false, err
		}
		if isMusicOrKichikuCategoryID(categoryID) {
			return true, nil
		}
		return isMusicOrKichikuCategoryName(category), nil
	default:
		return false, nil
	}
}

func isMusicOrKichikuCategoryID(categoryID int) bool {
	allowed := map[int]struct{}{
		// v2 音乐区
		1003: {}, 2016: {}, 2017: {}, 2018: {}, 2019: {}, 2020: {}, 2021: {}, 2022: {},
		2023: {}, 2024: {}, 2025: {}, 2026: {}, 2027: {},
		// v2 鬼畜区
		1007: {}, 2059: {}, 2060: {}, 2061: {}, 2062: {}, 2063: {},

		// v1 音乐区
		3: {}, 28: {}, 29: {}, 30: {}, 31: {}, 59: {}, 130: {},
		193: {}, 243: {}, 244: {}, 265: {}, 266: {}, 267: {},
		// v1 鬼畜区
		119: {}, 22: {}, 26: {}, 126: {}, 127: {}, 216: {},
		// 历史兼容
		54: {},
	}
	_, ok := allowed[categoryID]
	return ok
}

func isMusicOrKichikuCategoryName(category string) bool {
	lower := strings.ToLower(strings.TrimSpace(category))
	if lower == "" {
		return false
	}
	keywords := []string{
		"音乐", "鬼畜", "vocaloid", "utau", "音mad", "人力vocaloid", "演奏", "翻唱", "乐评", "电音", "音乐现场",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
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
		videoInfo, err := b.client.GetVideoInfo(ctx, trackID)
		if err != nil {
			return nil, fmt.Errorf("bilibili: failed to fetch video info for lyric: %w", err)
		}

		subtitleURL, err := b.client.GetVideoSubtitleURL(ctx, videoInfo.Bvid, videoInfo.Cid)
		if err != nil {
			return nil, fmt.Errorf("bilibili: failed to fetch subtitle list: %w", err)
		}

		if strings.TrimSpace(subtitleURL) == "" {
			return nil, platform.NewUnavailableError("bilibili", "lyrics", trackID)
		}

		subtitleLines, err := b.client.GetVideoSubtitleLines(ctx, subtitleURL)
		if err != nil {
			return nil, fmt.Errorf("bilibili: failed to fetch subtitle data: %w", err)
		}

		plain, timestamped := convertSubtitleLinesToLyrics(subtitleLines)
		if strings.TrimSpace(plain) == "" || len(timestamped) == 0 {
			return nil, platform.NewUnavailableError("bilibili", "lyrics", trackID)
		}

		return &platform.Lyrics{
			Plain:       plain,
			Timestamped: timestamped,
		}, nil
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

func convertSubtitleLinesToLyrics(lines []SubtitleBodyLine) (string, []platform.LyricLine) {
	if len(lines) == 0 {
		return "", nil
	}

	sorted := make([]SubtitleBodyLine, len(lines))
	copy(sorted, lines)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].From == sorted[j].From {
			return sorted[i].To < sorted[j].To
		}
		return sorted[i].From < sorted[j].From
	})

	var lrcBuilder strings.Builder
	timestamped := make([]platform.LyricLine, 0, len(sorted))

	for _, line := range sorted {
		text := normalizeSubtitleText(line.Content)
		if shouldSkipSubtitleText(text) {
			continue
		}

		duration := secondsToDuration(line.From)
		timestamped = append(timestamped, platform.LyricLine{Time: duration, Text: text})
		lrcBuilder.WriteString(formatLRCTimestamp(duration))
		lrcBuilder.WriteString(text)
		lrcBuilder.WriteByte('\n')
	}

	if len(timestamped) == 0 {
		return "", nil
	}

	return strings.TrimRight(lrcBuilder.String(), "\n"), timestamped
}

func normalizeSubtitleText(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	for {
		text = strings.TrimSpace(text)
		trimmedNotes := strings.Trim(text, " \t\r\n♪♫♬♩♭♮♯🎵🎶")
		if trimmedNotes == text {
			break
		}
		text = trimmedNotes
	}

	for {
		unwrapped, ok := unwrapOnce(text)
		if !ok {
			break
		}
		text = strings.TrimSpace(unwrapped)
	}

	text = strings.Trim(text, " \t\r\n♪♫♬♩♭♮♯🎵🎶")
	return strings.TrimSpace(text)
}

func unwrapOnce(s string) (string, bool) {
	type pair struct{ left, right string }
	pairs := []pair{
		{"(", ")"}, {"（", "）"}, {"[", "]"}, {"【", "】"},
		{"<", ">"}, {"《", "》"}, {"「", "」"}, {"『", "』"},
	}

	for _, p := range pairs {
		if strings.HasPrefix(s, p.left) && strings.HasSuffix(s, p.right) {
			inner := strings.TrimSuffix(strings.TrimPrefix(s, p.left), p.right)
			return inner, true
		}
	}

	return s, false
}

func shouldSkipSubtitleText(text string) bool {
	if text == "" {
		return true
	}

	normalized := strings.ToLower(strings.TrimSpace(text))
	switch normalized {
	case "音乐", "音樂", "纯音乐", "純音樂", "music", "bgm":
		return true
	default:
		return false
	}
}

func secondsToDuration(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	ms := int64(math.Round(seconds * 1000))
	if ms < 0 {
		ms = 0
	}
	return time.Duration(ms) * time.Millisecond
}

func probeContentLength(ctx context.Context, url string, headers map[string]string) (int64, error) {
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	for k, v := range headers {
		headReq.Header.Set(k, v)
	}
	if strings.TrimSpace(headReq.Header.Get("User-Agent")) == "" {
		headReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	}

	headResp, headErr := http.DefaultClient.Do(headReq)
	if headErr == nil {
		defer headResp.Body.Close()
		if headResp.ContentLength > 0 {
			return headResp.ContentLength, nil
		}
	}

	rangeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		if headErr != nil {
			return 0, headErr
		}
		return 0, err
	}
	for k, v := range headers {
		rangeReq.Header.Set(k, v)
	}
	rangeReq.Header.Set("Range", "bytes=0-0")
	if strings.TrimSpace(rangeReq.Header.Get("User-Agent")) == "" {
		rangeReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	}

	rangeResp, err := http.DefaultClient.Do(rangeReq)
	if err != nil {
		if headErr != nil {
			return 0, headErr
		}
		return 0, err
	}
	defer rangeResp.Body.Close()

	if contentRange := strings.TrimSpace(rangeResp.Header.Get("Content-Range")); contentRange != "" {
		parts := strings.Split(contentRange, "/")
		if len(parts) == 2 {
			totalStr := strings.TrimSpace(parts[1])
			if totalStr != "" && totalStr != "*" {
				total, parseErr := strconv.ParseInt(totalStr, 10, 64)
				if parseErr == nil && total > 0 {
					return total, nil
				}
			}
		}
	}

	if rangeResp.ContentLength > 0 {
		return rangeResp.ContentLength, nil
	}

	if headErr != nil {
		return 0, headErr
	}
	return 0, nil
}

func formatLRCTimestamp(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalMillis := int64(d / time.Millisecond)
	minutes := totalMillis / 60000
	seconds := (totalMillis % 60000) / 1000
	centis := (totalMillis % 1000) / 10
	return fmt.Sprintf("[%02d:%02d.%02d]", minutes, seconds, centis)
}

// Other unsupported interfaces

func (b *BilibiliPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	keyword := strings.TrimSpace(query)
	if keyword == "" {
		return []platform.Track{}, nil
	}
	if limit <= 0 {
		limit = 10
	}

	results := make([]platform.Track, 0, limit)
	seen := make(map[string]struct{}, limit)
	collect := func(searchKeyword string) error {
		for page := 1; page <= 5 && len(results) < limit; page++ {
			items, err := b.client.SearchVideo(ctx, searchKeyword, page)
			if err != nil {
				if page == 1 {
					return err
				}
				break
			}
			if len(items) == 0 {
				break
			}

			for _, item := range items {
				track, ok := b.searchItemToTrack(item)
				if !ok {
					continue
				}
				if _, exists := seen[track.ID]; exists {
					continue
				}
				seen[track.ID] = struct{}{}
				results = append(results, track)
				if len(results) >= limit {
					break
				}
			}
		}
		return nil
	}

	if err := collect(keyword); err != nil {
		return nil, err
	}

	if len(results) < 5 && !strings.Contains(strings.ToLower(keyword), "音乐") {
		_ = collect(keyword + " 音乐")
	}

	return results, nil
}

func (b *BilibiliPlatform) searchItemToTrack(item VideoSearchItem) (platform.Track, bool) {
	categoryID, _ := strconv.Atoi(strings.TrimSpace(item.TypeID))
	categoryName := strings.TrimSpace(item.TypeName)
	if !isMusicOrKichikuCategoryID(categoryID) && !isMusicOrKichikuCategoryName(categoryName) {
		return platform.Track{}, false
	}

	id := strings.TrimSpace(item.BVID)
	if id == "" && item.AID > 0 {
		id = fmt.Sprintf("av%d", item.AID)
	}
	if id == "" {
		return platform.Track{}, false
	}

	title := cleanSearchTitle(item.Title)
	if title == "" {
		title = strings.TrimSpace(item.BVID)
	}
	artistName := strings.TrimSpace(item.Author)
	if artistName == "" {
		artistName = "未知UP主"
	}

	trackURL := strings.TrimSpace(item.ArcURL)
	if trackURL == "" {
		if strings.TrimSpace(item.BVID) != "" {
			trackURL = fmt.Sprintf("https://www.bilibili.com/video/%s", strings.TrimSpace(item.BVID))
		} else {
			trackURL = fmt.Sprintf("https://www.bilibili.com/video/av%d", item.AID)
		}
	}

	return platform.Track{
		ID:       id,
		Platform: "bilibili",
		Title:    title,
		Artists: []platform.Artist{{
			ID:       strconv.Itoa(item.Mid),
			Platform: "bilibili",
			Name:     artistName,
			URL:      fmt.Sprintf("https://space.bilibili.com/%d", item.Mid),
		}},
		Duration: parseBilibiliSearchDuration(item.Duration),
		CoverURL: normalizeBilibiliCoverURL(item.Pic),
		URL:      trackURL,
	}, true
}

var searchTagRegexp = regexp.MustCompile(`<[^>]+>`)

func cleanSearchTitle(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	text = searchTagRegexp.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	return strings.TrimSpace(text)
}

func parseBilibiliSearchDuration(raw string) time.Duration {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}

	toInt := func(v string) int {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 {
			return 0
		}
		return n
	}

	if len(parts) == 2 {
		m := toInt(parts[0])
		s := toInt(parts[1])
		return time.Duration(m*60+s) * time.Second
	}

	h := toInt(parts[0])
	m := toInt(parts[1])
	s := toInt(parts[2])
	return time.Duration(h*3600+m*60+s) * time.Second
}

func normalizeBilibiliCoverURL(raw string) string {
	cover := strings.TrimSpace(raw)
	if strings.HasPrefix(cover, "//") {
		return "https:" + cover
	}
	return cover
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
