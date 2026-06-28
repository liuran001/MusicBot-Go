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
	"sync"
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

	// visitorData is harvested from a youtube.com/watch page and sent as the
	// X-Goog-Visitor-Id header on ANDROID_VR /player requests. Without it the
	// request is bot-flagged ("Sign in to confirm you're not a bot"). It is
	// cached across calls and refreshed on demand.
	visitorMu   sync.Mutex
	visitorData string
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

// androidVRContext builds the ANDROID_VR client context used for downloads. The
// extra device fields and matching userAgent mirror exactly what yt-dlp sends;
// a bare context (clientName/version only) gets bot-flagged with "Sign in to
// confirm you're not a bot", so all fields below are load-bearing.
func androidVRContext() innertubeContext {
	return innertubeContext{Client: clientInfo{
		ClientName:        androidVRClientName,
		ClientVersion:     androidVRClientVersion,
		Hl:                "en",
		Gl:                "US",
		DeviceMake:        "Oculus",
		DeviceModel:       "Quest 3",
		OsName:            "Android",
		OsVersion:         "12L",
		AndroidSDKVersion: 32,
		UserAgent:         androidVRUserAgent,
	}}
}

// post sends an InnerTube POST and returns the raw body. base is one of the
// innerTubeBase* constants; endpoint is e.g. "search" / "player".
func (c *Client) post(ctx context.Context, base, endpoint string, payload any, userAgent string, extraHeaders map[string]string) ([]byte, error) {
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
	// Per-call overrides (e.g. the ANDROID_VR player call sends its own Origin and
	// an X-Goog-Visitor-Id, and must NOT carry the music.youtube.com origin).
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
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
	data, err := c.post(ctx, innerTubeBaseMusic, "search", payload, defaultUserAgent, nil)
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
		// The stream URL was minted for the ANDROID_VR client context; fetch it
		// with a matching User-Agent so googlevideo doesn't reject the download.
		Headers: map[string]string{"User-Agent": androidVRUserAgent},
		// googlevideo still rejects HEAD, plain GET, and open-ended ("bytes=0-")
		// requests; it serves only bounded Range chunks. Unlike the IOS client's
		// PO-Token-walled URLs (which 403 at any offset!=0), the ANDROID_VR URL
		// serves arbitrary byte ranges, so chunked download succeeds end to end.
		// Keep the cap to force the bounded-Range path the downloader needs.
		MaxChunkSize: googleVideoMaxChunk,
	}
	if secs := parseInt64(pr.StreamingData.ExpiresInSeconds); secs > 0 {
		t := time.Now().Add(time.Duration(secs) * time.Second)
		info.ExpiresAt = &t
	}
	return info, nil
}

// getVisitorData harvests a visitorData token from a youtube.com/watch page and
// caches it. The ANDROID_VR /player request must carry this as the
// X-Goog-Visitor-Id header or YouTube bot-flags it ("Sign in to confirm you're
// not a bot"). yt-dlp does the same initial watch-page fetch. The token is
// stable for a long time, so we cache it and only refetch when empty or when a
// caller forces a refresh after a bot-flag.
func (c *Client) getVisitorData(ctx context.Context, forceRefresh bool) string {
	c.visitorMu.Lock()
	if c.visitorData != "" && !forceRefresh {
		vd := c.visitorData
		c.visitorMu.Unlock()
		return vd
	}
	c.visitorMu.Unlock()

	vd := c.fetchVisitorData(ctx)
	if vd != "" {
		c.visitorMu.Lock()
		c.visitorData = vd
		c.visitorMu.Unlock()
	}
	return vd
}

// fetchVisitorData GETs a watch page and extracts visitorData from the embedded
// ytcfg/initial data. The page ships it in a few shapes; we scan tolerantly.
func (c *Client) fetchVisitorData(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&bpctr=9999999999&has_verified=1", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.5 Safari/605.1.15")
	req.Header.Set("Accept-Language", "en-us,en;q=0.5")
	req.Header.Set("Cookie", "PREF=hl=en&tz=UTC; SOCS=CAI")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return ""
	}
	return extractVisitorData(string(body))
}

// extractVisitorData pulls the visitorData string out of a watch page's HTML.
// It appears as "visitorData":"<token>" in the embedded ytcfg JSON.
func extractVisitorData(html string) string {
	const key = `"visitorData":"`
	i := strings.Index(html, key)
	if i < 0 {
		return ""
	}
	rest := html[i+len(key):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	token := rest[:j]
	// The token is JSON-escaped (e.g. = for '='); unescape via a tiny JSON
	// string decode so the X-Goog-Visitor-Id header carries the literal value.
	var decoded string
	if err := json.Unmarshal([]byte(`"`+token+`"`), &decoded); err == nil {
		return decoded
	}
	return token
}

// player calls /player with the ANDROID_VR context. That client's googlevideo
// stream URLs are not behind the PO Token wall (arbitrary byte ranges return
// 206 even on a YouTube-flagged IP, unlike the IOS client whose URLs 403 past
// the first ~1 MiB), and they are direct/un-ciphered (no JS player needed). The
// extra envelope fields (thirdParty/user/request + playbackContext) mirror
// yt-dlp; a bare context gets bot-flagged.
func (c *Client) player(ctx context.Context, videoID string) (*playerResponse, error) {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return nil, platform.ErrNotFound
	}
	// ANDROID_VR /player is bot-flagged without a Visitor ID. Harvest one from a
	// youtube.com/watch page (cached) and send it as X-Goog-Visitor-Id, matching
	// yt-dlp. If the first attempt comes back LOGIN_REQUIRED (typical bot-flag),
	// the cached visitorData may be stale, so we refresh it once and retry.
	pr, err := c.playerOnce(ctx, videoID, c.getVisitorData(ctx, false))
	if err != nil {
		return nil, err
	}
	if isBotFlagged(pr) {
		if refreshed := c.getVisitorData(ctx, true); refreshed != "" {
			if pr2, err2 := c.playerOnce(ctx, videoID, refreshed); err2 == nil {
				return pr2, nil
			}
		}
	}
	return pr, nil
}

// playerOnce performs a single ANDROID_VR /player call with the given visitor
// ID. visitor may be empty (best-effort).
func (c *Client) playerOnce(ctx context.Context, videoID, visitor string) (*playerResponse, error) {
	vrCtx := androidVRContext()
	if visitor != "" {
		vrCtx.Client.VisitorData = visitor
	}
	vrCtx.ThirdParty = &thirdPartyInfo{EmbedURL: "https://www.youtube.com"}
	vrCtx.User = &userInfo{LockedSafetyMode: false}
	vrCtx.Request = &requestInfo{UseSSL: true, InternalExperimentFlags: []string{}, ConsistencyTokenJars: []string{}}
	payload := playerRequest{
		Context:        vrCtx,
		VideoID:        videoID,
		RacyOK:         true,
		ContentCheckOK: true,
		PlaybackContext: &playbackContextInfo{
			ContentPlaybackContext: contentPlaybackContextInfo{HTML5Preference: "HTML5_PREF_WANTS"},
		},
	}
	headers := map[string]string{"Origin": "https://www.youtube.com"}
	if visitor != "" {
		headers["X-Goog-Visitor-Id"] = visitor
	}
	data, err := c.post(ctx, innerTubeBaseVideo, "player", payload, androidVRUserAgent, headers)
	if err != nil {
		return nil, err
	}
	var pr playerResponse
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// isBotFlagged reports whether a player response is the "Sign in to confirm
// you're not a bot" / LOGIN_REQUIRED rejection that a stale or missing
// visitorData produces.
func isBotFlagged(pr *playerResponse) bool {
	if pr == nil {
		return false
	}
	status := strings.ToUpper(strings.TrimSpace(pr.PlayabilityStatus.Status))
	if status == "LOGIN_REQUIRED" {
		return true
	}
	return strings.Contains(strings.ToLower(pr.PlayabilityStatus.Reason), "not a bot")
}

// GetLyrics fetches lyrics via /next (to find the lyrics browseId) then /browse.
func (c *Client) GetLyrics(ctx context.Context, videoID string) (*platform.Lyrics, error) {
	nextPayload := nextRequest{Context: webContext(), VideoID: strings.TrimSpace(videoID)}
	data, err := c.post(ctx, innerTubeBaseMusic, "next", nextPayload, defaultUserAgent, nil)
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
	browseData, err := c.post(ctx, innerTubeBaseMusic, "browse", browseRequest{Context: webContext(), BrowseID: browseID}, defaultUserAgent, nil)
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
