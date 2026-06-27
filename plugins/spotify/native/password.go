package native

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/devgianlu/go-librespot/ap"
	pb "github.com/devgianlu/go-librespot/proto/spotify"
	"google.golang.org/protobuf/proto"

	"github.com/liuran001/MusicBot-Go/bot"
)

// OGGResult carries the outcome of the device-free librespot OGG path.
type OGGResult struct {
	OGG     []byte
	Bitrate int
	Steps   []string
}

// DownloadOGGWithPassword connects to the Spotify access point using
// username/password (USER_PASS auth — NO Widevine device, NO OAuth browser),
// then downloads + decrypts the track as Ogg Vorbis via the legacy audio-key
// path. This sidesteps the Widevine device-revocation wall entirely.
//
// It can still fail with TravelRestriction (account-region != server-IP) at the
// AP login, or AesKeyError (license-gated track / free account) at the audio
// key request — both are reported in the step trace.
func DownloadOGGWithPassword(ctx context.Context, baseLog bot.Logger, username, password, trackID string, preferredBitrate int) (*OGGResult, error) {
	res := &OGGResult{}
	add := func(f string, a ...any) { res.Steps = append(res.Steps, fmt.Sprintf(f, a...)) }

	log := newLogAdapter(baseLog)
	deviceID := newDeviceID()
	add("connecting to AP with username/password (USER_PASS, no device/OAuth)…")
	sess, err := connect(ctx, log, deviceID, nil, func(ctx context.Context, apc *ap.Accesspoint) error {
		return apc.Connect(ctx, &pb.LoginCredentials{
			Typ:      pb.AuthenticationType_AUTHENTICATION_USER_PASS.Enum(),
			Username: proto.String(username),
			AuthData: []byte(password),
		})
	})
	if err != nil {
		return res, fmt.Errorf("AP login: %w", err)
	}
	defer sess.Close()
	add("AP login OK (username=%s)", sess.Username())

	uri := fmt.Sprintf("spotify:track:%s", trackID)
	stream, err := sess.download(ctx, uri, preferredBitrate)
	if err != nil {
		return res, fmt.Errorf("ogg download: %w", err)
	}
	defer stream.Close()
	add("ogg stream resolved: %d bytes, %d kbps", stream.Size(), stream.format.Bitrate)

	ogg, err := io.ReadAll(stream)
	if err != nil {
		return res, fmt.Errorf("read ogg: %w", err)
	}
	res.OGG = ogg
	res.Bitrate = stream.format.Bitrate
	add("ogg read OK: %d bytes decrypted", len(ogg))
	return res, nil
}

// DownloadOGGWithCookie connects to the AP using an sp_dc-derived web token
// (AUTHENTICATION_SPOTIFY_TOKEN — still accepted, unlike USER_PASS), then pulls
// Ogg Vorbis via the legacy audio-key path. Device-free, no OAuth browser; uses
// the sp_dc the user already provides. Can still hit TravelRestriction or
// AesKeyError.
func DownloadOGGWithCookie(ctx context.Context, baseLog bot.Logger, hc *http.Client, spDC, usernameHint, trackID string, preferredBitrate int) (*OGGResult, error) {
	res := &OGGResult{}
	add := func(f string, a ...any) { res.Steps = append(res.Steps, fmt.Sprintf(f, a...)) }

	wt, err := WebTokenFromCookie(ctx, hc, spDC)
	if wt != nil {
		res.Steps = append(res.Steps, wt.Steps...)
	}
	if err != nil {
		return res, fmt.Errorf("web token: %w", err)
	}
	username := strings.TrimSpace(usernameHint)
	if username == "" {
		username, err = fetchWebUsername(ctx, hc, wt.AccessToken)
		if err != nil {
			add("token 分段数=%d, jwtSub 失败", len(strings.Split(wt.AccessToken, ".")))
			return res, fmt.Errorf("get username: %w", err)
		}
	}
	add("username=%s", username)
	// Best-effort account tier — explains an audio-key refusal (free accounts
	// are denied keys on the legacy path).
	if product := fetchProduct(ctx, hc, wt.AccessToken); product != "" {
		add("account product=%s", product)
	}

	log := newLogAdapter(baseLog)
	deviceID := newDeviceID()
	add("connecting to AP with sp_dc-derived token (SPOTIFY_TOKEN, no device)…")
	sess, err := connect(ctx, log, deviceID, hc, func(ctx context.Context, apc *ap.Accesspoint) error {
		return apc.ConnectSpotifyToken(ctx, username, wt.AccessToken)
	})
	if err != nil {
		return res, fmt.Errorf("AP login: %w", err)
	}
	defer sess.Close()
	add("AP login OK (username=%s)", sess.Username())

	uri := fmt.Sprintf("spotify:track:%s", trackID)
	stream, err := sess.download(ctx, uri, preferredBitrate)
	if err != nil {
		return res, fmt.Errorf("ogg download: %w", err)
	}
	defer stream.Close()
	add("ogg stream resolved: %d bytes, %d kbps", stream.Size(), stream.format.Bitrate)

	ogg, err := io.ReadAll(stream)
	if err != nil {
		return res, fmt.Errorf("read ogg: %w", err)
	}
	res.OGG = ogg
	res.Bitrate = stream.format.Bitrate
	add("ogg read OK: %d bytes decrypted", len(ogg))
	return res, nil
}

// fetchWebUsername resolves the canonical Spotify user id (needed as the AP
// login username). The web access token is a JWT whose `sub` claim is the user
// id, so we decode that first (no API call); only if that fails do we fall back
// to the rate-limited Web API /v1/me endpoint.
func fetchWebUsername(ctx context.Context, hc *http.Client, token string) (string, error) {
	if sub := jwtSub(token); sub != "" {
		return sub, nil
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.spotify.com/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("/v1/me HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	var me struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(b, &me) != nil || me.ID == "" {
		return "", fmt.Errorf("/v1/me parse failed: %s", snippet(b))
	}
	return me.ID, nil
}

// fetchProduct returns the account's product tier (free/premium) best-effort.
func fetchProduct(ctx context.Context, hc *http.Client, token string) string {
	if hc == nil {
		hc = http.DefaultClient
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.spotify.com/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != 200 {
		return fmt.Sprintf("(unknown, /v1/me HTTP %d)", resp.StatusCode)
	}
	var me struct {
		Product string `json:"product"`
	}
	if json.Unmarshal(b, &me) != nil {
		return ""
	}
	return me.Product
}

// jwtSub decodes a JWT access token's payload and returns its `sub` claim
// (the Spotify user id), or "" if the token isn't a decodable JWT.
func jwtSub(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Sub
}
