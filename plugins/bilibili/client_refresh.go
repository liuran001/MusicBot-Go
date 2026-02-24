package bilibili

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"gopkg.in/ini.v1"
)

var correspondPathPublicKey *rsa.PublicKey
var refreshCsrfRegex = regexp.MustCompile(`<div\s+id="1-name"\s*>(.*?)</div>`)

func init() {
	const publicKeyPEM = `
-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDLgd2OAkcGVtoE3ThUREbio0Eg
Uc/prcajMKXvkCKFCWhJYJcLkcM2DKKcSeFpD/j6Boy538YXnR6VhcuUJOhH2x71
nzPjfdTcqMz7djHum0qSZA0AyCBDABUqCrfNgCiJ00Ra7GmRj+YCK1NJEuewlb40
JNrRuoEUXpabUzGB8QIDAQAB
-----END PUBLIC KEY-----
`
	pubKeyBlock, _ := pem.Decode([]byte(strings.TrimSpace(publicKeyPEM)))
	pubInterface, err := x509.ParsePKIXPublicKey(pubKeyBlock.Bytes)
	if err != nil {
		panic(err)
	}

	var ok bool
	correspondPathPublicKey, ok = pubInterface.(*rsa.PublicKey)
	if !ok {
		panic("rsa public key type error")
	}
}

type CookieRefreshInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Refresh   bool  `json:"refresh"`
		Timestamp int64 `json:"timestamp"`
	} `json:"data"`
}

type CookieRefreshConfirm struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		RefreshToken string `json:"refresh_token"`
	} `json:"data"`
}

// StartAutoRefreshDaemon runs a background loop to automatically refresh the bilibili cookie.
func (c *Client) StartAutoRefreshDaemon(ctx context.Context) {
	if c.cookie == "" {
		return
	}
	if !c.autoRenew.enabled {
		if c.logger != nil {
			c.logger.Debug("bilibili: auto refresh disabled by config")
		}
		return
	}

	interval := c.autoRenew.interval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := c.CheckAndRefreshCookie(ctx)
				if err != nil && c.logger != nil {
					c.logger.Error("bilibili: auto refresh cookie failed", "err", err)
				}
			}
		}
	}()

	// Also trigger one check immediately
	go func() {
		err := c.CheckAndRefreshCookie(ctx)
		if err != nil && c.logger != nil {
			c.logger.Error("bilibili: initial check cookie failed", "err", err)
		}
	}()
}

// ManualRenew executes cookie refresh immediately and returns a human readable result.
func (c *Client) ManualRenew(ctx context.Context) (string, error) {
	if c == nil {
		return "", fmt.Errorf("bilibili client unavailable")
	}
	if err := c.CheckAndRefreshCookie(ctx); err != nil {
		return "", err
	}
	return "B站 Cookie 续期完成（如无需刷新会跳过）", nil
}

// CheckAndRefreshCookie checks if the cookie needs refreshing, and does so if necessary.
func (c *Client) CheckAndRefreshCookie(ctx context.Context) error {
	c.cookieMutex.RLock()
	currentCookie := c.cookie
	currentRefreshToken := c.refreshToken
	c.cookieMutex.RUnlock()

	if currentCookie == "" {
		return nil
	}

	// 1. Check refresh info
	infoReq, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, "https://passport.bilibili.com/x/passport-login/web/cookie/info", nil)
	if err != nil {
		return err
	}
	c.setHeaders(infoReq, currentCookie)

	infoResp, err := c.httpClient.Do(infoReq)
	if err != nil {
		return fmt.Errorf("check cookie info request failed: %w", err)
	}
	defer infoResp.Body.Close()

	if infoResp.StatusCode != 200 {
		return fmt.Errorf("check cookie info failed with status %d", infoResp.StatusCode)
	}

	infoBody, _ := io.ReadAll(infoResp.Body)
	var info CookieRefreshInfo
	if err := json.Unmarshal(infoBody, &info); err != nil {
		return fmt.Errorf("check cookie info decode failed: %w", err)
	}

	if info.Code != 0 {
		// Possibly already expired/invalid
		return fmt.Errorf("check cookie info api error: %d %s", info.Code, info.Message)
	}

	if !info.Data.Refresh {
		if c.logger != nil {
			c.logger.Debug("bilibili: cookie does not need refresh right now")
		}
		return nil
	}

	if c.logger != nil {
		c.logger.Debug("bilibili: cookie refresh needed, starting refresh process...")
	}

	// 2. Generate correspondPath & get refresh_csrf
	hash := sha256.New()
	random := rand.Reader
	msg := []byte(fmt.Sprintf("refresh_%d", info.Data.Timestamp))
	encryptedData, err := rsa.EncryptOAEP(hash, random, correspondPathPublicKey, msg, nil)
	if err != nil {
		return fmt.Errorf("encrypt correspondPath failed: %w", err)
	}
	correspondPath := hex.EncodeToString(encryptedData)

	csrfReq, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, "https://www.bilibili.com/correspond/1/"+correspondPath, nil)
	if err != nil {
		return err
	}
	c.setHeaders(csrfReq, currentCookie)

	csrfResp, err := c.httpClient.Do(csrfReq)
	if err != nil {
		return fmt.Errorf("get refresh_csrf failed: %w", err)
	}
	defer csrfResp.Body.Close()

	csrfBody, _ := io.ReadAll(csrfResp.Body)
	matches := refreshCsrfRegex.FindStringSubmatch(string(csrfBody))
	if len(matches) < 2 {
		return fmt.Errorf("failed to extract refresh_csrf from response")
	}
	refreshCsrf := matches[1]

	// 3. Confirm Refresh
	biliJct := c.extractCookieValue(currentCookie, "bili_jct")
	if currentRefreshToken == "" {
		if c.logger != nil {
			c.logger.Error("bilibili: [Cookie Auto-Renewal Failed] 'refresh_token' is empty in your configuration. " +
				"To enable auto-renewal of your High-Res audio access, please press F12 in your browser, " +
				"go to Application -> Local Storage, find 'ac_time_value', and fill it into your config.ini as 'refresh_token'.")
		}
		return fmt.Errorf("refresh_token empty, cannot refresh")
	}

	form := url.Values{}
	form.Add("csrf", biliJct)
	form.Add("refresh_csrf", refreshCsrf)
	form.Add("source", "main_web")
	form.Add("refresh_token", currentRefreshToken)

	refreshReq, err := retryablehttp.NewRequestWithContext(ctx, http.MethodPost, "https://passport.bilibili.com/x/passport-login/web/cookie/refresh", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	c.setHeaders(refreshReq, currentCookie)
	refreshReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	refreshResp, err := c.httpClient.Do(refreshReq)
	if err != nil {
		return fmt.Errorf("refresh cookie confirm failed: %w", err)
	}
	defer refreshResp.Body.Close()

	refreshBody, _ := io.ReadAll(refreshResp.Body)
	var confirm CookieRefreshConfirm
	if err := json.Unmarshal(refreshBody, &confirm); err != nil {
		return fmt.Errorf("refresh confirm decode failed: %w", err)
	}

	if confirm.Code != 0 {
		return fmt.Errorf("refresh confirm api return code %d: %s", confirm.Code, confirm.Message)
	}

	// 4. Update the stored cookie string with the new cookies and the new ac_time_value
	newCookies := refreshResp.Cookies()
	updatedCookieStr := c.mergeCookies(currentCookie, newCookies, confirm.Data.RefreshToken)

	c.cookieMutex.Lock()
	c.cookie = updatedCookieStr
	c.refreshToken = confirm.Data.RefreshToken
	c.cookieMutex.Unlock()

	// 5. Persist to config.ini
	if err := c.saveCookieToConfig(updatedCookieStr, confirm.Data.RefreshToken); err != nil {
		if c.logger != nil {
			c.logger.Error("bilibili: failed to persist refreshed cookie to config", "err", err)
		}
	} else {
		if c.logger != nil {
			c.logger.Info("bilibili: cookie successfully refreshed and saved to config.ini")
		}
	}

	return nil
}

// extractCookieValue parses a raw cookie string and retrieves a specific key's value
func (c *Client) extractCookieValue(rawCookie, key string) string {
	parts := strings.Split(rawCookie, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, key+"=") {
			return strings.TrimPrefix(part, key+"=")
		}
	}
	return ""
}

// mergeCookies updates the existing cookie string with new Set-Cookie values and a custom refresh token.
func (c *Client) mergeCookies(oldCookie string, newCookies []*http.Cookie, newAcTimeValue string) string {
	cookieMap := make(map[string]string)

	// Load old cookies
	parts := strings.Split(oldCookie, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			cookieMap[kv[0]] = kv[1]
		}
	}

	// Apply new cookies from response
	for _, c := range newCookies {
		if c.Value != "" {
			cookieMap[c.Name] = c.Value
		}
	}

	// Update refresh token
	if newAcTimeValue != "" {
		cookieMap["ac_time_value"] = newAcTimeValue
	}

	// Build the new string
	var segments []string
	for k, v := range cookieMap {
		segments = append(segments, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(segments, "; ")
}

// saveCookieToConfig saves the updated cookie and refresh token back to the user's config.ini preserving structure
func (c *Client) saveCookieToConfig(cookieStr string, refreshTokenStr string) error {
	if !c.autoRenew.persist {
		return nil
	}
	path := strings.TrimSpace(c.autoRenew.path)
	if path == "" {
		path = "config.ini"
	}
	cfg, err := ini.Load(path)
	if err != nil {
		return err
	}

	sec := cfg.Section("plugins.bilibili")
	if sec == nil {
		return fmt.Errorf("section [plugins.bilibili] not found")
	}

	if !sec.HasKey("cookie") {
		sec.NewKey("cookie", cookieStr)
	} else {
		sec.Key("cookie").SetValue(cookieStr)
	}

	if !sec.HasKey("refresh_token") {
		sec.NewKey("refresh_token", refreshTokenStr)
	} else {
		sec.Key("refresh_token").SetValue(refreshTokenStr)
	}

	return cfg.SaveTo(path)
}
