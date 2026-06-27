package native

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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
	webHeaders(req, auth.Bearer)
	if auth.ClientToken != "" {
		req.Header.Set("Client-Token", auth.ClientToken)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return b, resp.StatusCode, nil
}

// postRawAuth performs a POST with Bearer + client-token, raw body in/out.
func postRawAuth(ctx context.Context, hc *http.Client, url string, auth WebAuth, body []byte) (int, []byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	webHeaders(req, auth.Bearer)
	if auth.ClientToken != "" {
		req.Header.Set("Client-Token", auth.ClientToken)
	}
	// votify sends the session-default application/json even for the raw-protobuf
	// license challenge; match that (the body is still raw challenge bytes).
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
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

// buildPSSHFromFileID constructs a Widevine PSSH box locally from the file id,
// as a fallback when the seektable CDN has no pssh. It uses the same minimal
// WidevinePsshData shape proven by the applemusic plugin (key_id + algorithm),
// which is sufficient for the license challenge — the key id is what matters.
// key_id = first 16 bytes of the file id.
func buildPSSHFromFileID(fileID string) ([]byte, error) {
	raw, err := hex.DecodeString(fileID)
	if err != nil || len(raw) < 16 {
		return nil, fmt.Errorf("bad file id for pssh: %q", fileID)
	}
	inner, err := proto.Marshal(&widevinepb.WidevinePsshData{
		KeyIds:    [][]byte{raw[:16]},
		Algorithm: widevinepb.WidevinePsshData_AESCTR.Enum(),
	})
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

// acquireContentKey runs the Widevine flow against Spotify's license server and
// returns the CONTENT keys for the given PSSH.
func acquireContentKey(ctx context.Context, hc *http.Client, auth WebAuth, device *widevine.Device, psshBytes []byte) ([]*widevine.Key, error) {
	pssh, err := widevine.NewPSSH(psshBytes)
	if err != nil {
		return nil, fmt.Errorf("parse pssh: %w", err)
	}
	cdm := widevine.NewCDM(device)
	challenge, parseLicense, err := cdm.GetLicenseChallenge(pssh, widevinepb.LicenseType_AUTOMATIC, false)
	if err != nil {
		return nil, fmt.Errorf("license challenge: %w", err)
	}
	licURL := fmt.Sprintf("%s/widevine-license/v1/audio/license", spclientHost)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, licURL, bytes.NewReader(challenge))
	webHeaders(req, auth.Bearer)
	if auth.ClientToken != "" {
		req.Header.Set("Client-Token", auth.ClientToken)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("license post: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		// Dump diagnostic headers — for a 403 these usually indicate WHY (device
		// revoked, token scope, etc.) so we can tell a fixable issue from a hard
		// device rejection.
		diag := fmt.Sprintf("HTTP %d", resp.StatusCode)
		for _, h := range []string{"Www-Authenticate", "X-Error-Code", "X-Spotify-Error", "Retry-After", "Content-Type", "Cf-Mitigated"} {
			if v := resp.Header.Get(h); v != "" {
				diag += fmt.Sprintf(" | %s=%s", h, v)
			}
		}
		if len(body) > 0 {
			diag += " | body=" + snippet(body)
		}
		// A bare 403 with no error body is Spotify's signature for a blocklisted
		// Widevine device. The shared/public L3 CDM is revoked by Spotify (Apple
		// Music accepts it, Spotify does not); supplying a non-revoked,
		// privately-extracted .wvd via wvd_path is the only fix.
		if resp.StatusCode == 403 && len(body) == 0 {
			diag += " — likely a blocklisted Widevine device; supply a non-revoked .wvd via [plugins.spotify] wvd_path (extract with KeyDive from a physical Android device)"
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
// preferredBitrate selects the tier in kbps (0 = highest, typically 256). The
// step trace is populated for diagnostics regardless of success.
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

	file := selectMP4(files, preferredBitrate)
	res.FileID, res.Bitrate = file.FileID, file.Bitrate
	add("selected file_id=%s bitrate=%d", file.FileID, file.Bitrate)

	// 2) Resolve the CDN URL for the encrypted MP4.
	cdnURL, err := storageResolveMP4(ctx, hc, auth, file.FileID, file.Format)
	if err != nil {
		return res, err
	}
	res.CDNURL = cdnURL
	add("storage-resolve ok: host=%s", hostOf(cdnURL))

	// 3) Obtain the PSSH (Widevine init data). Prefer the seektable CDN; if it
	// has none for this file, build it locally from the file id (votify's
	// approach), so a missing seektable entry doesn't block the download.
	psshBytes, err := fetchPSSH(ctx, hc, file.FileID)
	if err != nil {
		built, berr := buildPSSHFromFileID(file.FileID)
		if berr != nil {
			return res, fmt.Errorf("seektable failed (%v) and local pssh build failed: %w", err, berr)
		}
		psshBytes = built
		add("seektable unavailable (%v); built pssh locally: %d bytes", err, len(psshBytes))
	} else {
		add("seektable ok: pssh %d bytes", len(psshBytes))
	}

	// 4) Run the Widevine license flow to get the CONTENT key.
	keys, err := acquireContentKey(ctx, hc, auth, device, psshBytes)
	if err != nil {
		return res, err
	}
	res.NumKeys = len(keys)
	add("widevine license ok: %d key(s)", len(keys))

	// 5) Download the encrypted MP4 and decrypt it in pure Go.
	mp4, err := downloadAndDecryptMP4(ctx, hc, cdnURL, keys)
	if err != nil {
		return res, err
	}
	res.MP4 = mp4
	add("decrypt ok: %d bytes of playable MP4/AAC", len(mp4))
	return res, nil
}
