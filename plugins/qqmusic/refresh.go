package qqmusic

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const (
	defaultAutoRenewInterval = 20 * time.Hour
	defaultRetryInterval     = time.Hour
	cookieMaxAge             = 24 * time.Hour
)

type refreshData struct {
	RefreshToken       string `json:"refresh_token"`
	AccessToken        string `json:"access_token"`
	MusicKey           string `json:"musickey"`
	MusicKeyCreateTime int64  `json:"musickeyCreateTime"`
	UnionID            string `json:"unionid"`
	LoginType          string `json:"login_type"`
}

type refreshResponse struct {
	Code int `json:"code"`
	Req1 struct {
		Code int         `json:"code"`
		Data refreshData `json:"data"`
	} `json:"req1"`
}

func (c *Client) Cookie() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cookie
}

func (c *Client) setCookie(value string) {
	c.mu.Lock()
	c.cookie = strings.TrimSpace(value)
	c.mu.Unlock()
}

func (c *Client) persistCookie(cookie string) {
	if c.persistFunc == nil {
		return
	}
	if strings.TrimSpace(cookie) == "" {
		return
	}
	if err := c.persistFunc(map[string]string{"cookie": cookie}); err != nil {
		c.logWarn(fmt.Sprintf("qqmusic: persist cookie failed: %v", err))
	}
}

func (c *Client) startAutoRenew() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.autoRenew.started {
		c.mu.Unlock()
		return
	}
	c.autoRenew.started = true
	c.mu.Unlock()
	go c.autoRenewLoop()
}

func (c *Client) AutoRenewStatus() platform.AutoRenewStatus {
	if c == nil {
		return platform.AutoRenewStatus{}
	}
	interval := c.autoRenew.interval
	if interval <= 0 {
		interval = defaultAutoRenewInterval
	}
	return platform.AutoRenewStatus{Enabled: c.autoRenew.enabled, Interval: interval}
}

func (c *Client) SetAutoRenew(enabled bool, interval time.Duration) (platform.AutoRenewStatus, error) {
	if c == nil {
		return platform.AutoRenewStatus{}, fmt.Errorf("qqmusic client unavailable")
	}
	if interval <= 0 {
		interval = defaultAutoRenewInterval
	}
	c.autoRenew.enabled = enabled
	c.autoRenew.interval = interval
	if c.persistFunc != nil {
		if err := c.persistFunc(map[string]string{
			"auto_renew_enabled":      boolStringQQ(enabled),
			"auto_renew_interval_sec": fmt.Sprintf("%d", int(interval/time.Second)),
		}); err != nil {
			return platform.AutoRenewStatus{}, err
		}
	}
	if enabled {
		c.startAutoRenew()
	}
	return c.AutoRenewStatus(), nil
}

func boolStringQQ(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (c *Client) autoRenewLoop() {
	for {
		cookie := c.Cookie()
		if reason, ok := autoRenewSkipReason(cookie); !ok {
			c.logInfo(fmt.Sprintf("qqmusic: auto-renew skipped, %s", reason))
			time.Sleep(defaultRetryInterval)
			continue
		}
		if shouldRenew(cookie, c.autoRenew.interval) {
			newCookie, ok := c.tryRenew(cookie)
			if ok {
				c.setCookie(newCookie)
				c.persistCookie(newCookie)
			}
			sleep := nextCheckDelay(newCookieOr(cookie, newCookie, ok), c.autoRenew.interval)
			time.Sleep(sleep)
			continue
		}
		sleep := nextCheckDelay(cookie, c.autoRenew.interval)
		time.Sleep(sleep)
	}
}

func (c *Client) tryRenew(cookie string) (string, bool) {
	updated, err := c.renewCookie(context.Background(), cookie)
	if err != nil {
		c.logWarn(fmt.Sprintf("qqmusic: auto-renew failed: %v", err))
		return "", false
	}
	if updated == "" {
		c.logWarn("qqmusic: auto-renew returned empty cookie")
		return "", false
	}
	c.logInfo("qqmusic: auto-renew succeeded")
	return updated, true
}

func (c *Client) renewCookie(ctx context.Context, cookie string) (string, error) {
	cookieMap := parseCookie(cookie)
	if len(cookieMap) == 0 {
		return "", fmt.Errorf("cookie empty")
	}
	payload := map[string]interface{}{
		"code": 0,
		"req1": map[string]interface{}{
			"code":   0,
			"module": "QQConnectLogin.LoginServer",
			"method": "QQLogin",
			"param": map[string]interface{}{
				"onlyNeedAccessToken": 0,
				"forceRefreshToken":   0,
				"psrf_qqopenid":       cookieMap["psrf_qqopenid"],
				"refresh_token":       cookieMap["psrf_qqrefresh_token"],
				"access_token":        cookieMap["psrf_qqaccess_token"],
				"expired_at":          cookieMap["psrf_access_token_expiresAt"],
				"musicid":             parseInt(cookieMap["uin"]),
				"musickey":            cookieMap["qqmusic_key"],
				"musickeyCreateTime":  parseInt(cookieMap["psrf_musickey_createtime"]),
				"unionid":             cookieMap["psrf_qqunionid"],
				"str_musicid":         cookieMap["uin"],
				"encryptUin":          cookieMap["euin"],
			},
		},
	}
	body, err := c.postJSON(ctx, "https://u6.y.qq.com/cgi-bin/musicu.fcg?format=json&inCharset=utf8&outCharset=utf8", payload)
	if err != nil {
		return "", err
	}
	var resp refreshResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decode refresh: %w", err)
	}
	if resp.Code != 0 || resp.Req1.Code != 0 {
		return "", fmt.Errorf("refresh error code")
	}
	return updateCookieWithData(cookie, resp.Req1.Data), nil
}

func (c *Client) ManualRenew(ctx context.Context) (string, error) {
	cookie := c.Cookie()
	if !isCookieValid(cookie) {
		return "", fmt.Errorf("cookie invalid")
	}
	updated, err := c.renewCookie(ctx, cookie)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(updated) == "" {
		return "", fmt.Errorf("renew returned empty cookie")
	}
	c.setCookie(updated)
	c.persistCookie(updated)
	return updated, nil
}

func parseCookie(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ";")
	result := make(map[string]string, len(parts))
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		if key == "" {
			continue
		}
		result[key] = value
	}
	return result
}

func updateCookieWithData(original string, data refreshData) string {
	cookieMap := parseCookie(original)
	if cookieMap == nil {
		return original
	}
	updateIfNonEmpty(cookieMap, "psrf_qqrefresh_token", data.RefreshToken)
	updateIfNonEmpty(cookieMap, "psrf_qqaccess_token", data.AccessToken)
	updateIfNonEmpty(cookieMap, "qqmusic_key", data.MusicKey)
	updateIfNonEmpty(cookieMap, "qm_keyst", data.MusicKey)
	if data.MusicKeyCreateTime > 0 {
		cookieMap["psrf_musickey_createtime"] = fmt.Sprintf("%d", data.MusicKeyCreateTime)
	}
	updateIfNonEmpty(cookieMap, "psrf_qqunionid", data.UnionID)
	updateIfNonEmpty(cookieMap, "login_type", data.LoginType)
	return renderCookie(cookieMap)
}

func updateIfNonEmpty(cookieMap map[string]string, key, value string) {
	if value == "" {
		return
	}
	if value == "0" {
		return
	}
	cookieMap[key] = value
}

func renderCookie(cookieMap map[string]string) string {
	if len(cookieMap) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cookieMap))
	for key, value := range cookieMap {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "; ")
}

func isCookieValid(cookie string) bool {
	createTime := cookieCreateTime(cookie)
	if createTime <= 0 {
		return false
	}
	now := time.Now().Unix()
	return now < createTime+int64(cookieMaxAge.Seconds())
}

func shouldRenew(cookie string, interval time.Duration) bool {
	createTime := cookieCreateTime(cookie)
	if createTime <= 0 {
		return false
	}
	if interval <= 0 {
		interval = defaultAutoRenewInterval
	}
	now := time.Now().Unix()
	start := createTime + int64(interval.Seconds())
	end := createTime + int64(cookieMaxAge.Seconds())
	return now > start && now < end
}

func nextCheckDelay(cookie string, interval time.Duration) time.Duration {
	createTime := cookieCreateTime(cookie)
	if createTime <= 0 {
		return defaultRetryInterval
	}
	if interval <= 0 {
		interval = defaultAutoRenewInterval
	}
	next := time.Unix(createTime, 0).Add(interval)
	wait := time.Until(next)
	if wait < 5*time.Minute {
		return 5 * time.Minute
	}
	return wait
}

func autoRenewSkipReason(cookie string) (string, bool) {
	createTime := cookieCreateTime(cookie)
	if createTime <= 0 {
		return "missing or invalid musickey create time", false
	}
	if time.Now().Unix() >= createTime+int64(cookieMaxAge.Seconds()) {
		return "musickey create time expired", false
	}
	return "", true
}

func cookieCreateTime(cookie string) int64 {
	cookieMap := parseCookie(cookie)
	if cookieMap == nil {
		return 0
	}
	value := cookieMap["psrf_musickey_createtime"]
	if strings.TrimSpace(value) == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parseInt(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func newCookieOr(oldCookie, newCookie string, ok bool) string {
	if ok && strings.TrimSpace(newCookie) != "" {
		return newCookie
	}
	return oldCookie
}

func (c *Client) logInfo(message string) {
	if c.logger == nil {
		return
	}
	c.logger.Info(message)
}

func (c *Client) logWarn(message string) {
	if c.logger == nil {
		return
	}
	c.logger.Warn(message)
}
