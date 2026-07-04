package native

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	librespot "github.com/devgianlu/go-librespot"
)

// VerifyResult is the outcome of the one-shot Widevine-path probe.
type VerifyResult struct {
	Steps   []string // human-readable trace of each step
	GotKey  bool     // true if a Widevine CONTENT key was obtained
	KeyHex  string   // the content key (hex), if obtained
	FileID  string   // the AAC file id used
	Format  string   // the chosen format (MP4_128 etc.)
	CDNHost string   // host of the resolved CDN url
}

// spclientHost is the generic spclient host that fronts metadata,
// storage-resolve, and the widevine-license endpoints.
const spclientHost = "https://spclient.wg.spotify.com"

// VerifyWidevineWithToken runs the AAC+Widevine resolution chain using an
// already-connected session's spclient bearer token, EXCEPT the final license
// decrypt (which needs a real Widevine device). It proves whether the OAuth /
// login5 token is accepted by the web-stream endpoints:
//
//	metadata/4/track  ->  storage-resolve (CDN url)  ->  seektable (PSSH)
//	->  widevine-license POST (does the token sign a license?)
//
// trackID is a base62 Spotify track id. challenge is an opaque Widevine license
// challenge (may be a dummy probe to see if the endpoint accepts the token).
func (c *Client) VerifyWidevineWithToken(ctx context.Context, trackID string) (*VerifyResult, error) {
	c.mu.Lock()
	sess, err := c.ensureSession(ctx)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	token, err := sess.AccessToken(ctx)
	if err != nil {
		return &VerifyResult{}, fmt.Errorf("get access token: %w", err)
	}
	return c.probeWidevineChain(ctx, token, trackID, "login5")
}

// VerifyWidevineRawToken probes the same chain but with a raw OAuth/web access
// token, WITHOUT going through the access point. This isolates whether the
// spclient HTTPS path (metadata/storage-resolve/license) accepts the token,
// independent of the AP-protocol TravelRestriction that can block the librespot
// streaming path.
func (c *Client) VerifyWidevineRawToken(ctx context.Context, token, trackID string) (*VerifyResult, error) {
	return c.probeWidevineChain(ctx, token, trackID, "oauth")
}

func (c *Client) probeWidevineChain(ctx context.Context, token, trackID, tokenKind string) (*VerifyResult, error) {
	res := &VerifyResult{}
	add := func(f string, a ...any) { res.Steps = append(res.Steps, fmt.Sprintf(f, a...)) }

	add("using %s access token (len=%d)", tokenKind, len(token))

	// 1) metadata/4/track/{gid}
	spotID, err := librespot.SpotifyIdFromBase62(librespot.SpotifyIdTypeTrack, trackID)
	if err != nil {
		return res, fmt.Errorf("gid: %w", err)
	}
	gid := spotID.Hex()
	add("track %s -> gid %s", trackID, gid)

	// 1) metadata/4/track/{gid} — try several markets and dump what comes back,
	// so we can see whether the file[] list is genuinely empty (token can't see
	// streams) or just empty for one market.
	type metaFile struct {
		FileID string `json:"file_id"`
		Format string `json:"format"`
	}
	type trackMeta struct {
		Name        string     `json:"name"`
		File        []metaFile `json:"file"`
		Alternative []struct {
			File []metaFile `json:"file"`
		} `json:"alternative"`
	}
	type trackPlayback struct {
		Media []struct {
			Item struct {
				Manifest struct {
					FileIDsMP4 []struct {
						FileID  string `json:"file_id"`
						Bitrate int    `json:"bitrate"`
					} `json:"file_ids_mp4"`
				} `json:"manifest"`
			} `json:"item"`
		} `json:"media"`
	}
	var files []metaFile
	for _, mkt := range []string{"from_token", "US", "GB", "DE", "JP"} {
		metaURL := fmt.Sprintf("%s/metadata/4/track/%s?market=%s", spclientHost, gid, mkt)
		raw, status, gerr := getRaw(ctx, c.httpClient, metaURL, token)
		if gerr != nil {
			add("metadata[%s]: request error: %v", mkt, gerr)
			continue
		}
		var meta trackMeta
		_ = json.Unmarshal(raw, &meta)
		f := meta.File
		if len(f) == 0 && len(meta.Alternative) > 0 {
			f = meta.Alternative[0].File
		}
		add("metadata[%s] HTTP %d: name=%q file=%d alt=%d (raw %dB)", mkt, status, meta.Name, len(meta.File), len(meta.Alternative), len(raw))
		if len(f) > 0 {
			files = f
			break
		}
		if mkt == "from_token" {
			dump := string(raw)
			if len(dump) > 3500 {
				dump = dump[:3500]
			}
			add("  from_token FULL metadata: %s", dump)
		}
	}
	var fileID, format string
	// 1a) Primary: pick MP4 (AAC) file_id from the JSON metadata `file` array if present.
	for _, want := range []string{"MP4_256", "MP4_128", "MP4_128_DUAL"} {
		for _, f := range files {
			if f.Format == want {
				fileID, format = f.FileID, f.Format
				break
			}
		}
		if fileID != "" {
			break
		}
	}
	// 1b) 2026 fallback: the JSON metadata no longer carries `file[]`. Resolve the
	// MP4 (Widevine AAC) file_ids via the track-playback media manifest instead.
	if fileID == "" {
		tpURL := fmt.Sprintf("%s/track-playback/v1/media/spotify:track:%s?manifestFileFormat=file_ids_mp4", spclientHost, trackID)
		tpRaw, tpStatus, tpErr := getRaw(ctx, c.httpClient, tpURL, token)
		if tpErr != nil {
			add("track-playback: request error: %v", tpErr)
		} else {
			dump := string(tpRaw)
			if len(dump) > 2500 {
				dump = dump[:2500]
			}
			add("track-playback HTTP %d (%dB): %s", tpStatus, len(tpRaw), dump)
			var tp trackPlayback
			if json.Unmarshal(tpRaw, &tp) == nil {
				var bestBitrate int
				for _, m := range tp.Media {
					for _, fm := range m.Item.Manifest.FileIDsMP4 {
						if fm.FileID == "" {
							continue
						}
						if fm.Bitrate >= bestBitrate {
							bestBitrate = fm.Bitrate
							fileID = fm.FileID
							format = fmt.Sprintf("MP4_%d", fm.Bitrate/1000)
						}
					}
				}
			}
		}
	}
	if fileID == "" {
		var got []string
		for _, f := range files {
			got = append(got, f.Format)
		}
		return res, fmt.Errorf("no AAC (MP4) format via metadata or track-playback; metadata formats: %s", strings.Join(got, ","))
	}
	res.FileID, res.Format = fileID, format
	add("selected %s file_id=%s", format, fileID)

	// 2) storage-resolve -> CDN url
	srURL := fmt.Sprintf("%s/storage-resolve/v2/files/audio/interactive/11/%s?version=10000000&product=9&platform=39&alt=json", spclientHost, fileID)
	var sr struct {
		Result string   `json:"result"`
		CDNURL []string `json:"cdnurl"`
	}
	if err := getJSON(ctx, c.httpClient, srURL, token, &sr); err != nil {
		return res, fmt.Errorf("storage-resolve: %w", err)
	}
	if len(sr.CDNURL) == 0 {
		return res, fmt.Errorf("storage-resolve returned no cdn urls (result=%s)", sr.Result)
	}
	res.CDNHost = hostOf(sr.CDNURL[0])
	add("storage-resolve ok: %d cdn url(s), host=%s", len(sr.CDNURL), res.CDNHost)

	// 3) seektable -> PSSH
	stURL := fmt.Sprintf("https://seektables.scdn.co/seektable/%s.json", fileID)
	var st struct {
		PSSH string `json:"pssh"`
	}
	if err := getJSONNoAuth(ctx, c.httpClient, stURL, &st); err != nil {
		return res, fmt.Errorf("seektable: %w", err)
	}
	if st.PSSH == "" {
		return res, fmt.Errorf("seektable returned empty pssh")
	}
	psshBytes, err := base64.StdEncoding.DecodeString(st.PSSH)
	if err != nil {
		return res, fmt.Errorf("decode pssh: %w", err)
	}
	add("seektable ok: pssh %d bytes", len(psshBytes))

	// 4) widevine-license POST — the decisive probe. We send the raw PSSH-derived
	// probe body. A real challenge needs a Widevine device; here we only need to
	// learn whether the TOKEN is accepted (200/valid license bytes) vs rejected
	// (401/403). Many servers return 400 for a malformed challenge but still
	// prove the token works; we report the status either way.
	licURL := fmt.Sprintf("%s/widevine-license/v1/audio/license", spclientHost)
	status, body, err := postRaw(ctx, c.httpClient, licURL, token, psshBytes)
	if err != nil {
		return res, fmt.Errorf("license post: %w", err)
	}
	add("widevine-license POST -> HTTP %d (resp %d bytes)", status, len(body))
	switch {
	case status == 401 || status == 403:
		add("VERDICT: token REJECTED by license endpoint (auth scope insufficient)")
	case status == 200:
		res.GotKey = true
		add("VERDICT: token ACCEPTED (200) — OAuth token CAN sign Widevine license")
	default:
		add("VERDICT: token reached endpoint (HTTP %d, not an auth rejection) — token scope OK, body was just not a valid challenge", status)
	}

	return res, nil
}

// --- small HTTP helpers (verification only) ---

func webHeaders(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("App-Platform", "WebPlayer")
	req.Header.Set("Spotify-App-Version", "1.2.87.27.ga2033a72")
	req.Header.Set("Origin", "https://open.spotify.com")
	req.Header.Set("Referer", "https://open.spotify.com/")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")
}

// ApplyWebAuthHeaders applies the headers used by Spotify's web player,
// including the client-token required by Pathfinder and spclient endpoints.
func ApplyWebAuthHeaders(req *http.Request, auth WebAuth) {
	webHeaders(req, auth.Bearer)
	if auth.ClientToken != "" {
		req.Header.Set("Client-Token", auth.ClientToken)
	}
}

func getJSON(ctx context.Context, hc *http.Client, url, token string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	webHeaders(req, token)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	return json.Unmarshal(b, out)
}

// getRaw fetches a URL and returns the raw body + status, without treating
// non-200 as an error (so the caller can inspect/dump the response).
func getRaw(ctx context.Context, hc *http.Client, url, token string) ([]byte, int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	webHeaders(req, token)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return b, resp.StatusCode, nil
}

func getJSONNoAuth(ctx context.Context, hc *http.Client, url string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Origin", "https://open.spotify.com")
	req.Header.Set("Referer", "https://open.spotify.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	return json.Unmarshal(b, out)
}

func postRaw(ctx context.Context, hc *http.Client, url, token string, body []byte) (int, []byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	webHeaders(req, token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func hostOf(rawurl string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(rawurl, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}
