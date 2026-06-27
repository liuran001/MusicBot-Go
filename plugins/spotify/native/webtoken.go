package native

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Web-player token path. Unlike an OAuth token (which returns metadata WITHOUT
// the streaming `file` array), the web-player access token obtained from an
// sp_dc cookie is authorized to see file ids — this is what votify and the
// Spotify web player itself use. The token is minted via a TOTP-signed call to
// open.spotify.com/api/token, the same flow the web player runs in-browser.

const (
	totpSecretDictURL = "https://git.gay/thereallo/totp-secrets/raw/branch/main/secrets/secretDict.json"
	webUA             = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
	webAppVersion     = "1.2.87.27.ga2033a72"
)

// builtinTOTPSecret is the version-61 secret, used as a fallback when the live
// secret dictionary can't be fetched. Spotify rotates these every few months,
// so the runtime fetch (fetchTOTPSecret) is preferred; this just keeps the path
// working offline / if the dict host is down.
var (
	builtinTOTPSecretVer = "61"
	builtinTOTPCipher    = []int{44, 55, 47, 42, 70, 40, 34, 114, 76, 74, 50, 111, 120, 97, 75, 76, 94, 102, 43, 69, 49, 120, 118, 80, 64, 78}
)

// deriveSecret turns Spotify's obfuscated TOTP cipher (the byte array from the
// secret dictionary) into the actual HMAC-SHA1 key: XOR each byte with
// ((index % 33) + 9), format each result as a variable-width decimal string,
// concatenate, and ASCII-encode. The official web player uses this exact
// derivation; feeding the raw bytes in as the key (a naive port) fails with a
// generic 400 from /api/token.
func deriveSecret(cipher []int) []byte {
	var sb strings.Builder
	for i, b := range cipher {
		sb.WriteString(strconv.Itoa(b ^ ((i % 33) + 9)))
	}
	return []byte(sb.String())
}

// fetchTOTPSecret returns the highest-versioned TOTP cipher from the public
// dictionary, falling back to the built-in version on any failure.
func fetchTOTPSecret(ctx context.Context, hc *http.Client) (version string, cipher []int) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, totpSecretDictURL, nil)
	if err != nil {
		return builtinTOTPSecretVer, builtinTOTPCipher
	}
	resp, err := hc.Do(req)
	if err != nil {
		return builtinTOTPSecretVer, builtinTOTPCipher
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var dict map[string][]int
	if json.Unmarshal(b, &dict) != nil || len(dict) == 0 {
		return builtinTOTPSecretVer, builtinTOTPCipher
	}
	bestVer, bestNum := "", -1
	for k := range dict {
		if n, err := strconv.Atoi(k); err == nil && n > bestNum {
			bestNum, bestVer = n, k
		}
	}
	arr := dict[bestVer]
	if len(arr) == 0 {
		return builtinTOTPSecretVer, builtinTOTPCipher
	}
	return bestVer, arr
}

// generateTOTP computes the 6-digit HMAC-SHA1 TOTP Spotify expects, using the
// raw secret bytes as the HMAC key and a 30-second step.
func generateTOTP(secret []byte, serverTimeMs int64) string {
	counter := serverTimeMs / 1000 / 30
	cb := make([]byte, 8)
	binary.BigEndian.PutUint64(cb, uint64(counter))
	h := hmac.New(sha1.New, secret)
	h.Write(cb)
	sum := h.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := uint32(sum[off]&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", bin%1000000)
}

// WebTokenResult carries the web-player token plus a diagnostic trace.
type WebTokenResult struct {
	AccessToken string
	ClientID    string
	ClientToken string
	ExpiresAtMs int64
	Steps       []string
}

// WebTokenFromCookie exchanges an sp_dc cookie for a web-player access token.
func WebTokenFromCookie(ctx context.Context, hc *http.Client, spDC string) (*WebTokenResult, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	spDC = strings.TrimSpace(spDC)
	if spDC == "" {
		return nil, fmt.Errorf("native: empty sp_dc cookie")
	}
	res := &WebTokenResult{}
	add := func(f string, a ...any) { res.Steps = append(res.Steps, fmt.Sprintf(f, a...)) }

	add("sp_dc 收到: 长度=%d (完整的通常 200~270 字符)", len(spDC))

	// 1) server time (cookie-authenticated)
	stReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://open.spotify.com/api/server-time", nil)
	cookieHeaders(stReq, spDC)
	stResp, err := hc.Do(stReq)
	if err != nil {
		return res, fmt.Errorf("server-time: %w", err)
	}
	stBody, _ := io.ReadAll(io.LimitReader(stResp.Body, 1<<16))
	stResp.Body.Close()
	var stj struct {
		ServerTime int64 `json:"serverTime"`
	}
	if json.Unmarshal(stBody, &stj) != nil || stj.ServerTime == 0 {
		return res, fmt.Errorf("server-time parse failed (HTTP %d): %s", stResp.StatusCode, snippet(stBody))
	}
	add("server-time ok: %d", stj.ServerTime)

	// 2) TOTP (derive the real HMAC key from the obfuscated cipher first)
	ver, cipher := fetchTOTPSecret(ctx, hc)
	totp := generateTOTP(deriveSecret(cipher), stj.ServerTime*1000)
	add("totp version %s computed", ver)

	// 3) /api/token (cookie + TOTP)
	tokURL := fmt.Sprintf("https://open.spotify.com/api/token?reason=init&productType=web-player&totp=%s&totpServer=%s&totpVer=%s", totp, totp, ver)
	tReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, tokURL, nil)
	cookieHeaders(tReq, spDC)
	tResp, err := hc.Do(tReq)
	if err != nil {
		return res, fmt.Errorf("token: %w", err)
	}
	tBody, _ := io.ReadAll(io.LimitReader(tResp.Body, 1<<16))
	tResp.Body.Close()
	if tResp.StatusCode != 200 {
		return res, fmt.Errorf("token HTTP %d: %s", tResp.StatusCode, snippet(tBody))
	}
	var tj struct {
		AccessToken                      string `json:"accessToken"`
		ClientID                         string `json:"clientId"`
		IsAnonymous                      bool   `json:"isAnonymous"`
		AccessTokenExpirationTimestampMs int64  `json:"accessTokenExpirationTimestampMs"`
	}
	if json.Unmarshal(tBody, &tj) != nil || tj.AccessToken == "" {
		return res, fmt.Errorf("token parse failed: %s", snippet(tBody))
	}
	if tj.IsAnonymous {
		return res, fmt.Errorf("got anonymous token — sp_dc invalid or expired")
	}
	add("web token ok (len=%d, anonymous=%v)", len(tj.AccessToken), tj.IsAnonymous)
	res.AccessToken = tj.AccessToken
	res.ClientID = tj.ClientID
	res.ExpiresAtMs = tj.AccessTokenExpirationTimestampMs

	// Fetch a client-token: spclient calls (track-playback, storage-resolve,
	// widevine-license) require it in addition to the Bearer token.
	if tj.ClientID != "" {
		if ct, cterr := fetchWebClientToken(ctx, hc, tj.ClientID); cterr == nil {
			res.ClientToken = ct
			add("client-token ok (len=%d)", len(ct))
		} else {
			add("client-token fetch failed (continuing without): %v", cterr)
		}
	}
	return res, nil
}

// fetchWebClientToken obtains a client-token for spclient calls. The web player
// POSTs the client id + version to clienttoken.spotify.com and reads back a
// granted token. Spotify accepts a JSON request/response for the web client.
func fetchWebClientToken(ctx context.Context, hc *http.Client, clientID string) (string, error) {
	body := fmt.Sprintf(`{"client_data":{"client_version":%q,"client_id":%q,"js_sdk_data":{}}}`, webAppVersion, clientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://clienttoken.spotify.com/v1/clienttoken", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", webUA)
	req.Header.Set("Origin", "https://open.spotify.com")
	req.Header.Set("Referer", "https://open.spotify.com/")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("clienttoken HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	var ctr struct {
		GrantedToken struct {
			Token string `json:"token"`
		} `json:"granted_token"`
	}
	if json.Unmarshal(b, &ctr) != nil || ctr.GrantedToken.Token == "" {
		return "", fmt.Errorf("clienttoken parse failed: %s", snippet(b))
	}
	return ctr.GrantedToken.Token, nil
}

func cookieHeaders(req *http.Request, spDC string) {
	req.Header.Set("Cookie", "sp_dc="+spDC)
	req.Header.Set("User-Agent", webUA)
	req.Header.Set("App-Platform", "WebPlayer")
	req.Header.Set("Spotify-App-Version", webAppVersion)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", "https://open.spotify.com")
	req.Header.Set("Referer", "https://open.spotify.com/")
	req.Header.Set("Accept", "application/json")
}
