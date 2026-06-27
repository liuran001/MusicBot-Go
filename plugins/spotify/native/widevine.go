package native

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"

	widevine "github.com/iyear/gowidevine"
	"github.com/iyear/gowidevine/widevinepb"
)

// (spclientHost is declared in verify.go)

// wvFile is a resolved MP4 (Widevine CENC / AAC) audio file for a track.
type wvFile struct {
	FileID  string // 40-hex file id
	Bitrate int    // bits per second (128000 / 256000)
	CDNURL  string // optional: CDN url straight from the manifest (may be empty)
}

// trackPlaybackResp models the track-playback media manifest. Spotify returns
// the MP4 (Widevine) file ids here in 2026; the old JSON metadata `file[]`
// array no longer carries them for web-token clients.
type trackPlaybackResp struct {
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

// resolveMP4Files returns the available MP4/Widevine audio files for a track,
// highest bitrate first. It uses the track-playback media manifest.
func resolveMP4Files(ctx context.Context, hc *http.Client, token, trackID string) ([]wvFile, error) {
	tpURL := fmt.Sprintf("%s/track-playback/v1/media/spotify:track:%s?manifestFileFormat=file_ids_mp4", spclientHost, trackID)
	raw, status, err := getRaw(ctx, hc, tpURL, token)
	if err != nil {
		return nil, fmt.Errorf("track-playback request: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("track-playback HTTP %d: %s", status, snippet(raw))
	}
	var tp trackPlaybackResp
	if err := json.Unmarshal(raw, &tp); err != nil {
		return nil, fmt.Errorf("track-playback decode: %w", err)
	}
	var files []wvFile
	seen := map[string]bool{}
	for _, m := range tp.Media {
		for _, f := range m.Item.Manifest.FileIDsMP4 {
			if f.FileID == "" || seen[f.FileID] {
				continue
			}
			seen[f.FileID] = true
			files = append(files, wvFile{FileID: f.FileID, Bitrate: f.Bitrate})
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no MP4 (Widevine) files in manifest")
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

// storageResolveMP4 resolves an MP4 file_id to a CDN URL. Spotify's MP4 audio
// uses the same storage-resolve v2 service as OGG; the format number for MP4 is
// not in the public proto enum, so we try the documented descriptors in order.
func storageResolveMP4(ctx context.Context, hc *http.Client, token, fileID string) (string, error) {
	// Format numbers to try for MP4 audio. Spotify's storage-resolve path embeds
	// a numeric format; MP4_256/MP4_128 aren't in the open proto enum, so we try
	// the known-working descriptors. Most web clients use the generic resolver.
	var lastErr error
	for _, fmtNum := range []string{"10", "11", "12", "9"} {
		srURL := fmt.Sprintf("%s/storage-resolve/v2/files/audio/interactive/%s/%s?version=10000000&product=9&platform=39&alt=json",
			spclientHost, fmtNum, fileID)
		raw, status, err := getRaw(ctx, hc, srURL, token)
		if err != nil {
			lastErr = err
			continue
		}
		if status != 200 {
			lastErr = fmt.Errorf("storage-resolve[%s] HTTP %d: %s", fmtNum, status, snippet(raw))
			continue
		}
		var sr struct {
			Result string   `json:"result"`
			CDNURL []string `json:"cdnurl"`
		}
		if json.Unmarshal(raw, &sr) != nil || len(sr.CDNURL) == 0 {
			lastErr = fmt.Errorf("storage-resolve[%s] no cdn urls (result=%s)", fmtNum, sr.Result)
			continue
		}
		return sr.CDNURL[0], nil
	}
	return "", fmt.Errorf("storage-resolve failed for all format numbers: %w", lastErr)
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

// acquireContentKey runs the Widevine flow against Spotify's license server and
// returns the CONTENT keys for the given PSSH.
func acquireContentKey(ctx context.Context, hc *http.Client, token string, device *widevine.Device, psshBytes []byte) ([]*widevine.Key, error) {
	pssh, err := widevine.NewPSSH(psshBytes)
	if err != nil {
		return nil, fmt.Errorf("parse pssh: %w", err)
	}
	cdm := widevine.NewCDM(device)
	challenge, parseLicense, err := cdm.GetLicenseChallenge(pssh, widevinepb.LicenseType_STREAMING, false)
	if err != nil {
		return nil, fmt.Errorf("license challenge: %w", err)
	}
	licURL := fmt.Sprintf("%s/widevine-license/v1/audio/license", spclientHost)
	status, body, err := postRaw(ctx, hc, licURL, token, challenge)
	if err != nil {
		return nil, fmt.Errorf("license post: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("license HTTP %d: %s", status, snippet(body))
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
func DownloadWidevineMP4(ctx context.Context, hc *http.Client, token string, device *widevine.Device, trackID string, preferredBitrate int) (*WidevineResult, error) {
	res := &WidevineResult{}
	add := func(f string, a ...any) { res.Steps = append(res.Steps, fmt.Sprintf(f, a...)) }

	if device == nil {
		return res, fmt.Errorf("widevine device not configured")
	}

	// 1) Resolve MP4 file ids from the track-playback manifest.
	files, err := resolveMP4Files(ctx, hc, token, trackID)
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
	cdnURL, err := storageResolveMP4(ctx, hc, token, file.FileID)
	if err != nil {
		return res, err
	}
	res.CDNURL = cdnURL
	add("storage-resolve ok: host=%s", hostOf(cdnURL))

	// 3) Fetch the PSSH (Widevine init data) from the seektable CDN.
	psshBytes, err := fetchPSSH(ctx, hc, file.FileID)
	if err != nil {
		return res, err
	}
	add("seektable ok: pssh %d bytes", len(psshBytes))

	// 4) Run the Widevine license flow to get the CONTENT key.
	keys, err := acquireContentKey(ctx, hc, token, device, psshBytes)
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
