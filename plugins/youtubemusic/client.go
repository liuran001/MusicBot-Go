package youtubemusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// Client talks to YouTube's InnerTube API. It is safe for concurrent use.
type Client struct {
	httpClient *http.Client
	cookie     string
	logger     bot.Logger
}

// NewClient builds a Client with the given request timeout.
func NewClient(cookie string, timeout time.Duration, logger bot.Logger) *Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		cookie:     strings.TrimSpace(cookie),
		logger:     logger,
	}
}

// SetAPIProxy routes InnerTube requests through the configured platform proxy,
// mirroring the other plugins. The proxy only affects API calls; the final
// googlevideo download is handled by the bot's DownloadService.
func (c *Client) SetAPIProxy(cfg httpproxy.Config) error {
	if c == nil {
		return nil
	}
	timeout := 20 * time.Second
	if c.httpClient != nil && c.httpClient.Timeout > 0 {
		timeout = c.httpClient.Timeout
	}
	proxied, err := httpproxy.NewHTTPClient(cfg, timeout)
	if err != nil {
		return err
	}
	if proxied == nil {
		c.httpClient = &http.Client{Timeout: timeout}
		return nil
	}
	c.httpClient = proxied
	return nil
}

// webContext / iosContext build the per-request client contexts.
func webContext() innertubeContext {
	return innertubeContext{Client: clientInfo{
		ClientName:    webRemixClientName,
		ClientVersion: webRemixClientVersion,
		Hl:            "en",
		Gl:            "US",
	}}
}

func iosContext() innertubeContext {
	return innertubeContext{Client: clientInfo{
		ClientName:    iosClientName,
		ClientVersion: iosClientVersion,
		Hl:            "en",
		Gl:            "US",
		DeviceModel:   "iPhone16,2",
		OsName:        "iOS",
		OsVersion:     "18.1.0.22B83",
	}}
}

// post sends an InnerTube POST and returns the raw body. base is one of the
// innerTubeBase* constants; endpoint is e.g. "search" / "player".
func (c *Client) post(ctx context.Context, base, endpoint string, payload any, userAgent string) ([]byte, error) {
	if c == nil || c.httpClient == nil {
		return nil, platform.ErrUnavailable
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/%s?key=%s&prettyPrint=false", base, endpoint, webRemixKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("X-Goog-Api-Format-Version", "1")
	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, platform.ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtubemusic: innertube %s status %d", endpoint, resp.StatusCode)
	}
	return data, nil
}

// Search queries music.youtube.com and returns up to limit tracks. The response
// shape is deeply nested and changes often, so we walk it tolerantly for
// videoId + title + artist text rather than binding a brittle typed tree.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	// params "EgWKAQIIAWoMEAMQBBAJEAoQBRAV" restricts results to Songs.
	payload := searchRequest{Context: webContext(), Query: query, Params: "EgWKAQIIAWoMEAMQBBAJEAoQBRAV"}
	data, err := c.post(ctx, innerTubeBaseMusic, "search", payload, defaultUserAgent)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	items := collectMusicListItems(root)
	tracks := make([]platform.Track, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, it := range items {
		t, ok := trackFromListItem(it)
		if !ok {
			continue
		}
		if _, dup := seen[t.ID]; dup {
			continue
		}
		seen[t.ID] = struct{}{}
		tracks = append(tracks, t)
		if len(tracks) >= limit {
			break
		}
	}
	return tracks, nil
}

// GetTrack returns track metadata from the IOS /player videoDetails (which also
// gives a thumbnail and duration). It does not fetch a stream URL.
func (c *Client) GetTrack(ctx context.Context, videoID string) (*platform.Track, error) {
	pr, err := c.player(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if pr == nil || strings.TrimSpace(pr.VideoDetails.VideoID) == "" {
		return nil, platform.ErrNotFound
	}
	track := &platform.Track{
		ID:       videoID,
		Platform: platformName,
		Title:    strings.TrimSpace(pr.VideoDetails.Title),
		Duration: parseSecondsDuration(pr.VideoDetails.LengthSeconds),
		URL:      "https://music.youtube.com/watch?v=" + videoID,
		CoverURL: bestThumbnail(pr.VideoDetails.Thumbnail.Thumbnails),
	}
	if author := strings.TrimSpace(pr.VideoDetails.Author); author != "" {
		track.Artists = []platform.Artist{{Name: cleanArtistName(author), Platform: platformName}}
	}
	return track, nil
}

// GetDownloadInfo resolves a directly-downloadable audio stream for videoID.
// The iOS client context returns adaptiveFormats whose URLs are NOT cipher-
// protected, so the bot's DownloadService can fetch them directly. When only
// ciphered URLs are present (rare for music), it returns ErrUnavailable.
func (c *Client) GetDownloadInfo(ctx context.Context, videoID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	pr, err := c.player(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, platform.ErrUnavailable
	}
	if status := strings.ToUpper(pr.PlayabilityStatus.Status); status != "" && status != "OK" {
		if strings.Contains(strings.ToLower(pr.PlayabilityStatus.Reason), "not available") {
			return nil, platform.ErrNotFound
		}
		return nil, platform.NewUnavailableError(platformName, "track", videoID)
	}
	best := selectAudioFormat(pr.StreamingData.AdaptiveFormats, quality)
	if best == nil {
		best = selectAudioFormat(pr.StreamingData.Formats, quality)
	}
	if best == nil || strings.TrimSpace(best.URL) == "" {
		// Only ciphered URLs available — we don't implement signature decipher.
		return nil, platform.NewUnavailableError(platformName, "stream", videoID)
	}
	info := &platform.DownloadInfo{
		URL:     best.URL,
		Format:  formatFromMime(best.MimeType),
		Bitrate: bestBitrate(best) / 1000,
		Quality: qualityFromBitrate(bestBitrate(best)),
		Size:    parseInt64(best.ContentLength),
		// The stream URL was minted for the iOS client context; fetch it with a
		// matching User-Agent so googlevideo doesn't reject the download.
		Headers: map[string]string{"User-Agent": iosUserAgent},
	}
	if secs := parseInt64(pr.StreamingData.ExpiresInSeconds); secs > 0 {
		t := time.Now().Add(time.Duration(secs) * time.Second)
		info.ExpiresAt = &t
	}
	return info, nil
}

// player calls /player with the iOS context (direct URLs) and falls back to the
// WEB_REMIX context for metadata when iOS is unavailable.
func (c *Client) player(ctx context.Context, videoID string) (*playerResponse, error) {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return nil, platform.ErrNotFound
	}
	payload := playerRequest{Context: iosContext(), VideoID: videoID, RacyOK: true}
	data, err := c.post(ctx, innerTubeBaseVideo, "player", payload, iosUserAgent)
	if err != nil {
		return nil, err
	}
	var pr playerResponse
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// GetLyrics fetches lyrics via /next (to find the lyrics browseId) then /browse.
func (c *Client) GetLyrics(ctx context.Context, videoID string) (*platform.Lyrics, error) {
	nextPayload := nextRequest{Context: webContext(), VideoID: strings.TrimSpace(videoID)}
	data, err := c.post(ctx, innerTubeBaseMusic, "next", nextPayload, defaultUserAgent)
	if err != nil {
		return nil, err
	}
	var nextRoot map[string]any
	if err := json.Unmarshal(data, &nextRoot); err != nil {
		return nil, err
	}
	browseID := findLyricsBrowseID(nextRoot)
	if browseID == "" {
		return nil, platform.NewUnavailableError(platformName, "lyrics", videoID)
	}
	browseData, err := c.post(ctx, innerTubeBaseMusic, "browse", browseRequest{Context: webContext(), BrowseID: browseID}, defaultUserAgent)
	if err != nil {
		return nil, err
	}
	var browseRoot map[string]any
	if err := json.Unmarshal(browseData, &browseRoot); err != nil {
		return nil, err
	}
	plain := findLyricsText(browseRoot)
	if strings.TrimSpace(plain) == "" {
		return nil, platform.NewUnavailableError(platformName, "lyrics", videoID)
	}
	return &platform.Lyrics{Plain: plain}, nil
}

// --- small parsing helpers ---

func parseSecondsDuration(s string) time.Duration {
	n := parseInt64(s)
	if n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

func parseInt64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func bestBitrate(f *streamFormat) int {
	if f.AverageBitrate > 0 {
		return f.AverageBitrate
	}
	return f.Bitrate
}

func bestThumbnail(thumbs []thumbnail) string {
	best := ""
	bestArea := 0
	for _, t := range thumbs {
		area := t.Width * t.Height
		if area >= bestArea && strings.TrimSpace(t.URL) != "" {
			bestArea = area
			best = t.URL
		}
	}
	return best
}

// cleanArtistName strips the trailing " - Topic" suffix YouTube appends to
// auto-generated artist channels.
func cleanArtistName(name string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(name), "- Topic"))
}

func formatFromMime(mime string) string {
	mime = strings.ToLower(mime)
	switch {
	case strings.Contains(mime, "opus"):
		return "opus"
	case strings.Contains(mime, "mp4a"), strings.Contains(mime, "m4a"), strings.Contains(mime, "audio/mp4"):
		return "m4a"
	case strings.Contains(mime, "webm"):
		return "webm"
	case strings.Contains(mime, "mpeg"), strings.Contains(mime, "mp3"):
		return "mp3"
	default:
		return "m4a"
	}
}

// qualityFromBitrate maps an audio bitrate (bps) to the bot's quality ladder.
// YouTube Music tops out around 256 kbps (opus/AAC); there is no lossless.
func qualityFromBitrate(bps int) platform.Quality {
	switch {
	case bps >= 200000:
		return platform.QualityHigh
	default:
		return platform.QualityStandard
	}
}

// selectAudioFormat picks the best audio-only format at or below the requested
// quality ceiling, preferring higher bitrate. Video formats are skipped.
func selectAudioFormat(formats []streamFormat, quality platform.Quality) *streamFormat {
	var best *streamFormat
	for i := range formats {
		f := &formats[i]
		if !strings.Contains(strings.ToLower(f.MimeType), "audio") {
			continue
		}
		if strings.TrimSpace(f.URL) == "" {
			continue // ciphered; we can't use it
		}
		if best == nil || bestBitrate(f) > bestBitrate(best) {
			best = f
		}
	}
	return best
}
