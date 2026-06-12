package applemusic

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	widevine "github.com/iyear/gowidevine"
	"github.com/iyear/gowidevine/widevinepb"
)

// decryptTrack performs the full Widevine DRM decryption pipeline:
//  1. Call WebPlayback API to get the encrypted HLS m3u8 URL
//  2. Parse the m3u8 to extract the mp4 URL and Widevine KID
//  3. Download the encrypted mp4 from Apple CDN
//  4. Construct PSSH, generate license challenge
//  5. Acquire license from Apple's Widevine server
//  6. Extract content key and decrypt the mp4
//
// Returns the decrypted m4a bytes.
//
// Requires:
//   - A valid media-user-token (for WebPlayback + license acquisition)
//   - Widevine L3 device credentials (client_id.bin + private_key.pem)
//     embedded at build time or loaded at runtime.
func (c *Client) decryptTrack(ctx context.Context, trackID string, device *widevine.Device) ([]byte, error) {
	if device == nil {
		return nil, fmt.Errorf("widevine device not configured")
	}

	// Step 1: Get WebPlayback assets.
	wpAssets, err := c.callWebPlayback(ctx, trackID)
	if err != nil {
		return nil, fmt.Errorf("webplayback: %w", err)
	}

	// Select ctrp256 (Widevine CENC, AAC 256kbps).
	var selected *webPlaybackAsset
	for i := range wpAssets {
		if wpAssets[i].Flavor == "28:ctrp256" {
			selected = &wpAssets[i]
			break
		}
	}
	if selected == nil && len(wpAssets) > 0 {
		selected = &wpAssets[0]
	}
	if selected == nil {
		return nil, fmt.Errorf("no assets in webplayback response")
	}

	hlsURL := strings.TrimSpace(selected.URL)
	if hlsURL == "" {
		return nil, fmt.Errorf("empty HLS URL")
	}

	// Step 2: Parse m3u8 to get mp4 URL and key info.
	m3u8Body, err := c.downloadURL(ctx, hlsURL)
	if err != nil {
		return nil, fmt.Errorf("fetch m3u8: %w", err)
	}

	mp4URL, kidB64, uriPrefix, err := parseWidevineHLS(hlsURL, string(m3u8Body))
	if err != nil {
		return nil, fmt.Errorf("parse m3u8: %w", err)
	}

	// Step 3: Download encrypted mp4.
	encData, err := c.downloadURL(ctx, mp4URL)
	if err != nil {
		return nil, fmt.Errorf("download encrypted mp4: %w", err)
	}
	if c.logger != nil {
		c.logger.Debug("applemusic: downloaded encrypted mp4", "size", len(encData))
	}

	// Step 4: Build PSSH and get license.
	kidBytes, err := base64.StdEncoding.DecodeString(kidB64)
	if err != nil {
		return nil, fmt.Errorf("decode KID: %w", err)
	}

	pssh, err := widevine.NewPSSH(kidBytes)
	if err != nil {
		return nil, fmt.Errorf("build PSSH: %w", err)
	}

	cdm := widevine.NewCDM(device)
	challenge, parseLicense, err := cdm.GetLicenseChallenge(pssh, widevinepb.LicenseType_AUTOMATIC, false)
	if err != nil {
		return nil, fmt.Errorf("license challenge: %w", err)
	}

	// Step 5: Acquire license from Apple.
	licenseResp, err := c.acquireLicense(ctx, challenge, trackID, uriPrefix, kidB64)
	if err != nil {
		return nil, fmt.Errorf("acquire license: %w", err)
	}

	keys, err := parseLicense(licenseResp)
	if err != nil {
		return nil, fmt.Errorf("parse license: %w", err)
	}
	if c.logger != nil {
		c.logger.Debug("applemusic: got widevine keys", "count", len(keys))
	}

	// Step 6: Decrypt mp4.
	var decBuf bytes.Buffer
	err = widevine.DecryptMP4Auto(bytes.NewReader(encData), keys, &decBuf)
	if err != nil {
		return nil, fmt.Errorf("decrypt mp4: %w", err)
	}

	return decBuf.Bytes(), nil
}

// callWebPlayback calls the Apple Music WebPlayback API and returns the asset list.
func (c *Client) callWebPlayback(ctx context.Context, trackID string) ([]webPlaybackAsset, error) {
	if err := c.ensureDeveloperToken(ctx); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(map[string]string{"salableAdamId": trackID})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webPlaybackURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.getDeveloperToken())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", appleMusicOrigin)
	req.Header.Set("User-Agent", appleMusicUA)
	req.Header.Set("media-user-token", c.mediaUserToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, err
	}

	var wpResp webPlaybackResponse
	if err := json.Unmarshal(body, &wpResp); err != nil {
		return nil, err
	}
	if len(wpResp.SongList) == 0 {
		return nil, fmt.Errorf("empty songList")
	}
	return wpResp.SongList[0].Assets, nil
}

// acquireLicense sends the Widevine license challenge to Apple's license server.
func (c *Client) acquireLicense(ctx context.Context, challenge []byte, adamId, uriPrefix, kidB64 string) ([]byte, error) {
	const licenseURL = "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/acquireWebPlaybackLicense"

	reqBody := map[string]interface{}{
		"challenge":      base64.StdEncoding.EncodeToString(challenge),
		"key-system":     "com.widevine.alpha",
		"uri":            uriPrefix + "," + kidB64,
		"adamId":         adamId,
		"isLibrary":      false,
		"user-initiated": true,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, licenseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.getDeveloperToken())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", appleMusicOrigin)
	req.Header.Set("User-Agent", appleMusicUA)
	req.Header.Set("media-user-token", c.mediaUserToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("license HTTP %d: %s", resp.StatusCode, string(body))
	}

	var licResp struct {
		License string `json:"license"`
		Status  int    `json:"status"`
	}
	if err := json.Unmarshal(body, &licResp); err != nil {
		return nil, fmt.Errorf("parse license response: %w", err)
	}
	if licResp.Status != 0 {
		return nil, fmt.Errorf("license status %d", licResp.Status)
	}
	if licResp.License == "" {
		return nil, fmt.Errorf("empty license in response")
	}

	return base64.StdEncoding.DecodeString(licResp.License)
}

// parseWidevineHLS parses an Apple Music Widevine HLS m3u8 playlist and extracts
// the mp4 segment URL, base64-encoded KID, and URI prefix.
func parseWidevineHLS(m3u8URL, content string) (mp4URL, kidB64, uriPrefix string, err error) {
	baseURL := m3u8URL[:strings.LastIndex(m3u8URL, "/")+1]

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)

		// EXT-X-KEY:METHOD=ISO-23001-7,URI="data:;base64,<psshBoxData>"
		// or URI="<uriPrefix>,<kidBase64>"
		if strings.Contains(line, "EXT-X-KEY") {
			if idx := strings.Index(line, `URI="`); idx >= 0 {
				uriStart := idx + 5
				uriEnd := strings.Index(line[uriStart:], `"`)
				if uriEnd > 0 {
					fullURI := line[uriStart : uriStart+uriEnd]
					// Split on comma — format: "<uriPrefix>,<kidBase64>"
					if parts := strings.SplitN(fullURI, ",", 2); len(parts) == 2 {
						uriPrefix = parts[0]
						kidB64 = parts[1]
					} else if strings.HasPrefix(fullURI, "data:;base64,") {
						// Inline PSSH box — extract KID from it.
						psshData := strings.TrimPrefix(fullURI, "data:;base64,")
						kidB64 = psshData
						uriPrefix = ""
					}
				}
			}
		}

		// EXT-X-MAP:URI="<filename>",BYTERANGE="<range>"
		// The actual mp4 file name (same file for init and segments).
		if strings.Contains(line, "EXT-X-MAP") {
			if idx := strings.Index(line, `URI="`); idx >= 0 {
				uriStart := idx + 5
				uriEnd := strings.Index(line[uriStart:], `"`)
				if uriEnd > 0 {
					mapURI := line[uriStart : uriStart+uriEnd]
					if strings.HasPrefix(mapURI, "http") {
						mp4URL = mapURI
					} else {
						mp4URL = baseURL + mapURI
					}
				}
			}
		}

		// Also check for non-comment lines (segment references).
		if !strings.HasPrefix(line, "#") && line != "" &&
			(strings.HasSuffix(line, ".mp4") || strings.HasSuffix(line, ".m4a") || strings.HasSuffix(line, ".m4s")) {
			if mp4URL == "" {
				if strings.HasPrefix(line, "http") {
					mp4URL = line
				} else {
					mp4URL = baseURL + line
				}
			}
		}
	}

	if mp4URL == "" {
		return "", "", "", fmt.Errorf("no mp4 segment URL found")
	}
	if kidB64 == "" {
		return "", "", "", fmt.Errorf("no Widevine KID found")
	}
	return mp4URL, kidB64, uriPrefix, nil
}

// wvDeviceTimeout is the timeout for Widevine device credential loading.
const wvDeviceTimeout = 5 * time.Second
