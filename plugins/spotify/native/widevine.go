package native

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"

	widevine "github.com/iyear/gowidevine"
	"github.com/iyear/gowidevine/widevinepb"
	"google.golang.org/protobuf/proto"
)

// (spclientHost is declared in verify.go)

// WebAuth carries the two credentials every spclient call needs: the Bearer
// access token and the client-token. Both come from the web-player token flow.
type WebAuth struct {
	Bearer      string
	ClientToken string
}

// getRawAuth performs a GET with Bearer + client-token + web-player headers,
// returning the raw body and status without treating non-200 as an error.
func getRawAuth(ctx context.Context, hc *http.Client, url string, auth WebAuth) ([]byte, int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	ApplyWebAuthHeaders(req, auth)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return b, resp.StatusCode, nil
}

// wvFile is a resolved MP4 (Widevine CENC / AAC) audio file for a track.
type wvFile struct {
	FileID  string // 40-hex file id
	Format  string // storage-resolve format id: "11"=MP4_256, "10"=MP4_128
	Bitrate int    // bits per second, derived from Format for selection/labeling
}

// mp4FormatBitrate maps Spotify's MP4 storage-resolve format id to its nominal
// AAC bitrate. "11"=MP4_256, "10"=MP4_128 (verified against votify constants).
func mp4FormatBitrate(format string) int {
	switch format {
	case "11":
		return 256000
	case "10":
		return 128000
	default:
		return 0
	}
}

// trackPlaybackResp models the track-playback media manifest. Spotify returns
// the MP4 (Widevine) file ids here in 2026; the old JSON metadata `file[]`
// array no longer carries them for web-token clients. `media` is an OBJECT
// keyed by an opaque id (not an array); each entry carries the storage-resolve
// format id ("10"/"11") in its `format` field.
type trackPlaybackResp struct {
	Media map[string]struct {
		Item struct {
			Manifest struct {
				FileIDsMP4 []struct {
					FileID string `json:"file_id"`
					Format string `json:"format"`
				} `json:"file_ids_mp4"`
			} `json:"manifest"`
		} `json:"item"`
	} `json:"media"`
}

// resolveMP4Files returns the available MP4/Widevine audio files for a track,
// highest bitrate first. It uses the track-playback media manifest.
func resolveMP4Files(ctx context.Context, hc *http.Client, auth WebAuth, trackID string) ([]wvFile, error) {
	tpURL := fmt.Sprintf("%s/track-playback/v1/media/spotify:track:%s?manifestFileFormat=file_ids_mp4", spclientHost, trackID)
	raw, status, err := getRawAuth(ctx, hc, tpURL, auth)
	if err != nil {
		return nil, fmt.Errorf("track-playback request: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("track-playback HTTP %d: %s", status, snippet(raw))
	}
	var tp trackPlaybackResp
	if err := json.Unmarshal(raw, &tp); err != nil {
		return nil, fmt.Errorf("track-playback decode: %w (raw: %s)", err, snippet(raw))
	}
	var files []wvFile
	seen := map[string]bool{}
	for _, m := range tp.Media {
		for _, f := range m.Item.Manifest.FileIDsMP4 {
			if f.FileID == "" || seen[f.FileID] {
				continue
			}
			seen[f.FileID] = true
			files = append(files, wvFile{
				FileID:  f.FileID,
				Format:  f.Format,
				Bitrate: mp4FormatBitrate(f.Format),
			})
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no MP4 (Widevine) files in manifest (raw: %s)", snippet(raw))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Bitrate > files[j].Bitrate })
	return files, nil
}

// selectMP4 picks the file at-or-below the preferred bitrate (0 = highest).
func selectMP4(files []wvFile, preferredBitrate int) wvFile {
	if preferredBitrate <= 0 {
		return files[0] // already sorted high→low
	}
	target := preferredBitrate * 1000
	best := files[0]
	for _, f := range files {
		if f.Bitrate <= target && f.Bitrate > 0 {
			return f
		}
		best = f
	}
	return best
}

// selectMP4Candidates returns the preferred file followed by lower-bitrate
// fallbacks. Spotify Free accounts can obtain AAC 128k licenses but reject
// AAC 256k, so callers requesting the highest tier must be able to retry 128k.
func selectMP4Candidates(files []wvFile, preferredBitrate int) []wvFile {
	if len(files) == 0 {
		return nil
	}
	selected := selectMP4(files, preferredBitrate)
	candidates := []wvFile{selected}
	for _, file := range files {
		if file.FileID == selected.FileID || file.Bitrate >= selected.Bitrate {
			continue
		}
		candidates = append(candidates, file)
	}
	return candidates
}

// storageResolveMP4 resolves an MP4 file_id to a CDN URL via storage-resolve v2.
// The format id ("11"=MP4_256, "10"=MP4_128) comes straight from the
// track-playback manifest entry, so it matches the file_id exactly (verified
// against votify's FORMAT_ID_MAP). The version/product/platform query params
// mirror the web player's call.
func storageResolveMP4(ctx context.Context, hc *http.Client, auth WebAuth, fileID, format string) (string, error) {
	if format == "" {
		return "", fmt.Errorf("storage-resolve: empty format id for file %s", fileID)
	}
	srURL := fmt.Sprintf("%s/storage-resolve/v2/files/audio/interactive/%s/%s?version=10000000&product=9&platform=39&alt=json",
		spclientHost, format, fileID)
	raw, status, err := getRawAuth(ctx, hc, srURL, auth)
	if err != nil {
		return "", fmt.Errorf("storage-resolve request: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("storage-resolve[%s] HTTP %d: %s", format, status, snippet(raw))
	}
	var sr struct {
		Result string   `json:"result"`
		CDNURL []string `json:"cdnurl"`
	}
	if json.Unmarshal(raw, &sr) != nil || len(sr.CDNURL) == 0 {
		return "", fmt.Errorf("storage-resolve[%s] no cdn urls (result=%s)", format, sr.Result)
	}
	return sr.CDNURL[0], nil
}

// fetchPSSH gets the Widevine PSSH box (base64) for an MP4 file from the public
// seektable CDN (no auth).
func fetchPSSH(ctx context.Context, hc *http.Client, fileID string) ([]byte, error) {
	stURL := fmt.Sprintf("https://seektables.scdn.co/seektable/%s.json", fileID)
	var st struct {
		PSSH string `json:"pssh"`
	}
	if err := getJSONNoAuth(ctx, hc, stURL, &st); err != nil {
		return nil, fmt.Errorf("seektable: %w", err)
	}
	if st.PSSH == "" {
		return nil, fmt.Errorf("seektable returned empty pssh")
	}
	b, err := base64.StdEncoding.DecodeString(st.PSSH)
	if err != nil {
		return nil, fmt.Errorf("decode pssh: %w", err)
	}
	return b, nil
}

// buildSpotifyPSSH constructs a WidevinePsshData protobuf locally from a
// 40-hex Spotify file_id, matching votify's _get_pssh() exactly. This is
// preferred over the seektable CDN because votify (which works) uses this
// same local construction, and the seektable PSSH may have subtle field
// differences (e.g. missing protection_scheme) that cause a 403.
//
// Fields (all five, matching votify):
//   - algorithm = AESCTR
//   - key_ids = [first 16 bytes of file_id]
//   - provider = "spotify"
//   - content_id = full file_id bytes (20 bytes)
//   - protection_scheme = 0x636E6363 ('cenc' little-endian)
func buildSpotifyPSSH(fileID string) ([]byte, error) {
	raw, err := hex.DecodeString(fileID)
	if err != nil || len(raw) < 16 {
		return nil, fmt.Errorf("bad file id for pssh: %q", fileID)
	}
	psshData := &widevinepb.WidevinePsshData{
		Algorithm:        widevinepb.WidevinePsshData_AESCTR.Enum(),
		KeyIds:           [][]byte{raw[:16]},
		Provider:         proto.String("spotify"),
		ContentId:        raw,
		ProtectionScheme: proto.Uint32(1667591779), // 'cenc' in little-endian
	}
	inner, err := proto.Marshal(psshData)
	if err != nil {
		return nil, fmt.Errorf("marshal pssh data: %w", err)
	}
	// Wrap in a full version-0 PSSH box: size|'pssh'|version+flags|systemid|datasize|data
	var buf bytes.Buffer
	boxLen := 32 + len(inner)
	_ = binary.Write(&buf, binary.BigEndian, uint32(boxLen))
	buf.WriteString("pssh")
	_ = binary.Write(&buf, binary.BigEndian, uint32(0)) // version 0 + flags
	buf.Write(widevineSystemID)
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(inner)))
	buf.Write(inner)
	return buf.Bytes(), nil
}

// widevineSystemID is the Widevine DRM system ID.
var widevineSystemID = []byte{
	0xed, 0xef, 0x8b, 0xa9, 0x79, 0xd6, 0x4a, 0xce,
	0xa3, 0xc8, 0x27, 0xdc, 0xd5, 0x1d, 0x21, 0xed,
}

// postLicense sends a raw Widevine message (challenge or service-cert request)
// to Spotify's license endpoint and returns the raw response body + status.
func postLicense(ctx context.Context, hc *http.Client, auth WebAuth, payload []byte) ([]byte, int, http.Header, error) {
	licURL := fmt.Sprintf("%s/widevine-license/v1/audio/license", spclientHost)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, licURL, bytes.NewReader(payload))
	ApplyWebAuthHeaders(req, auth)
	// votify (which works) posts the raw protobuf challenge but REUSES the
	// default web-player client headers — httpx does NOT override a preset
	// content-type when you pass raw bytes, so the binary challenge actually
	// goes out as content-type/accept: application/json. We match that exactly;
	// octet-stream (a "more correct" guess) is NOT what the working client sends.
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return body, resp.StatusCode, resp.Header, nil
}

// fetchSpotifyServiceCert retrieves Spotify's Widevine service certificate so we
// can build a privacy-mode (encrypted-ClientID) challenge. The browser web
// player ALWAYS does this first; a plaintext-ClientID challenge is what Spotify
// rejects with a bare 403. The service cert is obtained by POSTing the
// well-known ServiceCertificateRequest ({0x08,0x04}) to the license endpoint.
func fetchSpotifyServiceCert(ctx context.Context, hc *http.Client, auth WebAuth) (*widevinepb.DrmCertificate, error) {
	body, status, _, err := postLicense(ctx, hc, auth, widevine.ServiceCertificateRequest)
	if err != nil {
		return nil, fmt.Errorf("service-cert post: %w", err)
	}
	if status != 200 || len(body) == 0 {
		return nil, fmt.Errorf("service-cert HTTP %d (len=%d)", status, len(body))
	}
	cert, err := widevine.ParseServiceCert(body)
	if err != nil {
		return nil, fmt.Errorf("parse service-cert: %w", err)
	}
	return cert, nil
}

// fetchProductState queries Spotify's product_state endpoint and returns the
// account tier ("free", "premium", ...). It is used for quality selection and
// to explain license failures.
func fetchProductState(ctx context.Context, hc *http.Client, auth WebAuth) (string, error) {
	url := spclientHost + "/melody/v1/product_state"
	body, status, err := getRawAuth(ctx, hc, url, auth)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("product_state HTTP %d", status)
	}
	var ps struct {
		Product string `json:"product"`
	}
	if err := json.Unmarshal(body, &ps); err != nil {
		return "", fmt.Errorf("product_state decode: %w", err)
	}
	return ps.Product, nil
}

// acquireContentKey runs the Widevine flow against Spotify's license server and
// returns the CONTENT keys for the given PSSH. It matches votify EXACTLY:
// non-privacy mode (plaintext ClientID — votify never sets a service certificate,
// so pywidevine's default privacy_mode=True is a no-op) and a STREAMING license
// type. The decisive variable that was wrong before was the PSSH shape, not the
// privacy mode: the seektable PSSH lacked provider/content_id/protection_scheme.
func acquireContentKey(ctx context.Context, hc *http.Client, auth WebAuth, device *widevine.Device, psshBytes []byte, bitrate int) ([]*widevine.Key, error) {
	pssh, err := widevine.NewPSSH(psshBytes)
	if err != nil {
		return nil, fmt.Errorf("parse pssh: %w", err)
	}
	cdm := widevine.NewCDM(device)

	challenge, parseLicense, err := cdm.GetLicenseChallenge(pssh, widevinepb.LicenseType_STREAMING, false)
	if err != nil {
		return nil, fmt.Errorf("license challenge: %w", err)
	}

	body, status, hdr, err := postLicense(ctx, hc, auth, challenge)
	if err != nil {
		return nil, fmt.Errorf("license post: %w", err)
	}
	if status != 200 {
		diag := fmt.Sprintf("HTTP %d", status)
		for _, h := range []string{"Www-Authenticate", "X-Error-Code", "X-Spotify-Error", "Retry-After", "Content-Type", "Cf-Mitigated"} {
			if v := hdr.Get(h); v != "" {
				diag += fmt.Sprintf(" | %s=%s", h, v)
			}
		}
		if len(body) > 0 {
			diag += " | body=" + snippet(body)
		}
		if status == 403 && len(body) == 0 {
			if prod, perr := fetchProductState(ctx, hc, auth); perr == nil && prod != "" {
				if prod != "premium" {
					if bitrate > 128000 {
						diag += fmt.Sprintf(" — account product=%q; AAC %dk requires Premium. Retrying AAC 128k may succeed.", prod, bitrate/1000)
					} else {
						diag += fmt.Sprintf(" — account product=%q. AAC %dk is normally available to free accounts; the Widevine device may be revoked or the token may be rejected.", prod, bitrate/1000)
					}
				} else {
					diag += " — account is premium, so this is a blocklisted/revoked Widevine device. Supply a non-revoked .wvd (KeyDive from a physical Android 13/14+ device) via [plugins.spotify] wvd_path."
				}
			} else {
				diag += " — bare 403; the requested quality may be unavailable for this account, or the Widevine device/token was rejected."
			}
		}
		return nil, fmt.Errorf("license %s", diag)
	}
	keys, err := parseLicense(body)
	if err != nil {
		return nil, fmt.Errorf("parse license: %w", err)
	}
	return keys, nil
}

// downloadAndDecryptMP4 fetches the encrypted CENC MP4 from the CDN and decrypts
// it in pure Go, returning a playable MP4 (AAC) byte stream.
func downloadAndDecryptMP4(ctx context.Context, hc *http.Client, cdnURL string, keys []*widevine.Key) ([]byte, error) {
	parsed, err := url.Parse(cdnURL)
	if err != nil {
		return nil, fmt.Errorf("bad cdn url: %w", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cdn download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("cdn HTTP %d: %s", resp.StatusCode, string(b))
	}
	enc, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read cdn body: %w", err)
	}
	var out bytes.Buffer
	if err := widevine.DecryptMP4Auto(bytes.NewReader(enc), keys, &out); err != nil {
		return nil, fmt.Errorf("decrypt mp4: %w", err)
	}
	return out.Bytes(), nil
}

// BatchTryDevices resolves a track once (track-playback -> storage-resolve ->
// PSSH), then loops over the given .wvd device files trying ONLY the per-device
// license step until one is accepted (not revoked) by Spotify. On the first
// working device it downloads + decrypts and returns the playable MP4 bytes.
//
// onProgress(idx,total,path,status) is called per device so the caller can show
// progress. Returns the working device path, decrypted MP4, and selected file
// info; err is non-nil only if the one-time resolve failed or NO device worked.
func BatchTryDevices(ctx context.Context, hc *http.Client, auth WebAuth, trackID string, preferredBitrate int, wvdPaths []string, onProgress func(idx, total int, path, status string)) (workingPath string, mp4 []byte, fileID string, bitrate int, err error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	if len(wvdPaths) == 0 {
		return "", nil, "", 0, fmt.Errorf("no Widevine devices provided")
	}
	files, rerr := resolveMP4Files(ctx, hc, auth, trackID)
	if rerr != nil {
		return "", nil, "", 0, fmt.Errorf("resolve: %w", rerr)
	}
	candidates := selectMP4Candidates(files, preferredBitrate)
	totalAttempts := len(candidates) * len(wvdPaths)
	var attemptErrs []error

	for candidateIdx, file := range candidates {
		cdnURL, resolveErr := storageResolveMP4(ctx, hc, auth, file.FileID, file.Format)
		if resolveErr != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%dk storage-resolve: %w", file.Bitrate/1000, resolveErr))
			continue
		}
		psshBytes, psshErr := buildSpotifyPSSH(file.FileID)
		if psshErr != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%dk build pssh: %w", file.Bitrate/1000, psshErr))
			continue
		}

		for deviceIdx, path := range wvdPaths {
			if ctx.Err() != nil {
				return "", nil, "", 0, ctx.Err()
			}
			progressIdx := candidateIdx*len(wvdPaths) + deviceIdx + 1
			device, loadErr := LoadWVDeviceFile(path)
			if loadErr != nil {
				attemptErrs = append(attemptErrs, fmt.Errorf("%s: %w", path, loadErr))
				if onProgress != nil {
					onProgress(progressIdx, totalAttempts, path, fmt.Sprintf("%dk load-error: %v", file.Bitrate/1000, loadErr))
				}
				continue
			}
			keys, keyErr := acquireContentKey(ctx, hc, auth, device, psshBytes, file.Bitrate)
			if keyErr != nil {
				attemptErrs = append(attemptErrs, fmt.Errorf("%s %dk: %w", path, file.Bitrate/1000, keyErr))
				if onProgress != nil {
					onProgress(progressIdx, totalAttempts, path, fmt.Sprintf("%dk rejected: %v", file.Bitrate/1000, keyErr))
				}
				continue
			}
			dec, decryptErr := downloadAndDecryptMP4(ctx, hc, cdnURL, keys)
			if decryptErr != nil {
				attemptErrs = append(attemptErrs, fmt.Errorf("%s %dk: %w", path, file.Bitrate/1000, decryptErr))
				if onProgress != nil {
					onProgress(progressIdx, totalAttempts, path, fmt.Sprintf("%dk license OK but decrypt failed: %v", file.Bitrate/1000, decryptErr))
				}
				continue
			}
			if onProgress != nil {
				onProgress(progressIdx, totalAttempts, path, fmt.Sprintf("WORKS at %dk — %d bytes decrypted", file.Bitrate/1000, len(dec)))
			}
			return path, dec, file.FileID, file.Bitrate, nil
		}
	}

	return "", nil, "", 0, fmt.Errorf("all %d device/quality attempts failed: %w", totalAttempts, errors.Join(attemptErrs...))
}

// WidevineProbeResult carries the outcome of a lightweight Widevine health
// check. It resolves the MP4 candidate and obtains a CONTENT key, but does not
// fetch or decrypt the full audio file.
type WidevineProbeResult struct {
	FileID  string
	Format  string
	Bitrate int
	CDNURL  string
	CDNHost string
	NumKeys int
	Steps   []string
}

// ProbeWidevineMP4 runs the Spotify AAC/Widevine chain through the license
// step without downloading the encrypted MP4. It is suitable for account health
// checks where pulling a whole song would be unnecessarily heavy.
func ProbeWidevineMP4(ctx context.Context, hc *http.Client, auth WebAuth, device *widevine.Device, trackID string, preferredBitrate int) (*WidevineProbeResult, error) {
	res := &WidevineProbeResult{}
	add := func(f string, a ...any) { res.Steps = append(res.Steps, fmt.Sprintf(f, a...)) }

	if hc == nil {
		hc = http.DefaultClient
	}
	if device == nil {
		return res, fmt.Errorf("widevine device not configured")
	}

	files, err := resolveMP4Files(ctx, hc, auth, trackID)
	if err != nil {
		return res, err
	}
	var brs []int
	for _, f := range files {
		brs = append(brs, f.Bitrate)
	}
	add("track-playback ok: %d mp4 file(s), bitrates=%v", len(files), brs)

	candidates := selectMP4Candidates(files, preferredBitrate)
	var attemptErrs []error
	for idx, file := range candidates {
		res.FileID, res.Format, res.Bitrate = file.FileID, file.Format, file.Bitrate
		add("trying candidate %d/%d: file_id=%s bitrate=%d", idx+1, len(candidates), file.FileID, file.Bitrate)

		cdnURL, resolveErr := storageResolveMP4(ctx, hc, auth, file.FileID, file.Format)
		if resolveErr != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%dk storage-resolve: %w", file.Bitrate/1000, resolveErr))
			add("candidate %dk failed: %v", file.Bitrate/1000, resolveErr)
			continue
		}
		res.CDNURL = cdnURL
		res.CDNHost = hostOf(cdnURL)
		add("storage-resolve ok: host=%s", res.CDNHost)

		psshBytes, psshErr := buildSpotifyPSSH(file.FileID)
		if psshErr != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%dk build pssh: %w", file.Bitrate/1000, psshErr))
			add("candidate %dk failed: %v", file.Bitrate/1000, psshErr)
			continue
		}
		add("built pssh locally: %d bytes", len(psshBytes))

		keys, keyErr := acquireContentKey(ctx, hc, auth, device, psshBytes, file.Bitrate)
		if keyErr != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%dk license: %w", file.Bitrate/1000, keyErr))
			add("candidate %dk failed: %v", file.Bitrate/1000, keyErr)
			continue
		}
		res.NumKeys = len(keys)
		add("widevine license ok: %d key(s)", len(keys))
		return res, nil
	}

	return res, fmt.Errorf("widevine probe failed for all quality candidates: %w", errors.Join(attemptErrs...))
}

// Probe resolves the web-player token and configured Widevine device, then runs
// ProbeWidevineMP4.
func (c *WidevineClient) Probe(ctx context.Context, trackID string, preferredBitrate int) (*WidevineProbeResult, error) {
	auth, err := c.WebAuth(ctx)
	if err != nil {
		return nil, err
	}
	device, err := c.ensureDevice()
	if err != nil {
		return nil, err
	}
	return ProbeWidevineMP4(ctx, c.httpClient, auth, device, trackID, preferredBitrate)
}

// WidevineResult carries the outcome of a full Widevine download chain plus a
// diagnostic trace, used by the live probe and the production path.
type WidevineResult struct {
	FileID  string
	Bitrate int
	CDNURL  string
	NumKeys int
	MP4     []byte
	Steps   []string
}

// DownloadWidevineMP4 runs the complete Widevine AAC chain for a track using a
// web-player token, returning decrypted, playable MP4 (AAC) bytes.
//
// preferredBitrate selects the initial tier in kbps (0 = highest). Lower
// available tiers are tried when a higher tier is unavailable for the account.
// The step trace is populated for diagnostics regardless of success.
func DownloadWidevineMP4(ctx context.Context, hc *http.Client, auth WebAuth, device *widevine.Device, trackID string, preferredBitrate int) (*WidevineResult, error) {
	res := &WidevineResult{}
	add := func(f string, a ...any) { res.Steps = append(res.Steps, fmt.Sprintf(f, a...)) }

	if hc == nil {
		hc = http.DefaultClient
	}
	if device == nil {
		return res, fmt.Errorf("widevine device not configured")
	}

	// 1) Resolve MP4 file ids from the track-playback manifest.
	files, err := resolveMP4Files(ctx, hc, auth, trackID)
	if err != nil {
		return res, err
	}
	var brs []int
	for _, f := range files {
		brs = append(brs, f.Bitrate)
	}
	add("track-playback ok: %d mp4 file(s), bitrates=%v", len(files), brs)

	candidates := selectMP4Candidates(files, preferredBitrate)
	var attemptErrs []error
	for idx, file := range candidates {
		res.FileID, res.Bitrate = file.FileID, file.Bitrate
		add("trying candidate %d/%d: file_id=%s bitrate=%d", idx+1, len(candidates), file.FileID, file.Bitrate)

		cdnURL, resolveErr := storageResolveMP4(ctx, hc, auth, file.FileID, file.Format)
		if resolveErr != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%dk storage-resolve: %w", file.Bitrate/1000, resolveErr))
			add("candidate %dk failed: %v", file.Bitrate/1000, resolveErr)
			continue
		}
		res.CDNURL = cdnURL
		add("storage-resolve ok: host=%s", hostOf(cdnURL))

		psshBytes, psshErr := buildSpotifyPSSH(file.FileID)
		if psshErr != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%dk build pssh: %w", file.Bitrate/1000, psshErr))
			add("candidate %dk failed: %v", file.Bitrate/1000, psshErr)
			continue
		}
		add("built pssh locally: %d bytes", len(psshBytes))

		keys, keyErr := acquireContentKey(ctx, hc, auth, device, psshBytes, file.Bitrate)
		if keyErr != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%dk license: %w", file.Bitrate/1000, keyErr))
			add("candidate %dk failed: %v", file.Bitrate/1000, keyErr)
			continue
		}
		res.NumKeys = len(keys)
		add("widevine license ok: %d key(s)", len(keys))

		mp4, decryptErr := downloadAndDecryptMP4(ctx, hc, cdnURL, keys)
		if decryptErr != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%dk decrypt: %w", file.Bitrate/1000, decryptErr))
			add("candidate %dk failed: %v", file.Bitrate/1000, decryptErr)
			continue
		}
		res.MP4 = mp4
		add("decrypt ok: %d bytes of playable MP4/AAC", len(mp4))
		return res, nil
	}

	return res, fmt.Errorf("widevine download failed for all quality candidates: %w", errors.Join(attemptErrs...))
}
