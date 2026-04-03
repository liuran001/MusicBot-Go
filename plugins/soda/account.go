package soda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type sodaSelfProfileResponse struct {
	StatusCode int `json:"status_code"`
	User       struct {
		UID      string `json:"uid"`
		Nickname string `json:"nickname"`
		UniqueID string `json:"unique_id"`
	} `json:"user"`
	Data struct {
		UID      string `json:"uid"`
		Nickname string `json:"nickname"`
		UniqueID string `json:"unique_id"`
	} `json:"data"`
}

type sodaAccountInfoResponse struct {
	Data struct {
		UID      string `json:"uid"`
		Nickname string `json:"screen_name"`
		UniqueID string `json:"unique_id"`
	} `json:"data"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (s *SodaPlatform) SupportedLoginMethods() []string {
	return []string{"cookie", "status"}
}

func (s *SodaPlatform) AccountStatus(ctx context.Context) (platform.AccountStatus, error) {
	status := platform.AccountStatus{
		Platform:        s.Name(),
		DisplayName:     s.Metadata().DisplayName,
		AuthMode:        "cookie",
		CanCheckCookie:  true,
		CanRenewCookie:  false,
		SupportedLogins: s.SupportedLoginMethods(),
	}
	if s == nil || s.client == nil {
		status.Summary = "- 状态: 插件未初始化"
		return status, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	fields := s.client.CookieMap()
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	core := s.client.CoreCookieSignals()
	probe, err := s.client.FetchSelfProfile(probeCtx)
	if err == nil && probe != nil {
		status.LoggedIn = true
		status.UserID = strings.TrimSpace(firstNonEmptyString(probe.User.UID, probe.Data.UID))
		status.Nickname = strings.TrimSpace(firstNonEmptyString(probe.User.Nickname, probe.Data.Nickname))
		lines := []string{"- 状态: 已登录"}
		if status.Nickname != "" {
			lines = append(lines, "- 昵称: "+status.Nickname)
		}
		if status.UserID != "" {
			lines = append(lines, "- UID: "+maskSodaValue(status.UserID))
		}
		if unique := strings.TrimSpace(firstNonEmptyString(probe.User.UniqueID, probe.Data.UniqueID)); unique != "" {
			lines = append(lines, "- UniqueID: "+maskSodaValue(unique))
		}
		if len(keys) > 0 {
			lines = append(lines, "- Cookie 字段: "+strings.Join(keys, ", "))
		}
		status.Summary = strings.Join(lines, "\n")
		return status, nil
	}
	if s.client.CanAccessCoreContent(probeCtx) {
		status.LoggedIn = true
		lines := []string{"- 状态: 可用（内容接口正常）", "- 账号资料接口未返回用户信息，但汽水内容接口可访问"}
		if len(core) > 0 {
			lines = append(lines, "- 关键字段: "+strings.Join(core, ", "))
		}
		if len(keys) > 0 {
			lines = append(lines, "- Cookie 字段: "+strings.Join(keys, ", "))
		}
		status.Summary = strings.Join(lines, "\n")
		return status, nil
	}
	lines := []string{"- 状态: 未登录"}
	if len(core) > 0 {
		lines = []string{"- 状态: 已配置 Cookie，但当前不可用"}
		lines = append(lines, "- 关键字段: "+strings.Join(core, ", "))
	} else if len(keys) > 0 {
		lines = []string{"- 状态: 仅配置辅助 Cookie，未检测到核心登录字段"}
	}
	if len(keys) > 0 {
		lines = append(lines, "- Cookie 字段: "+strings.Join(keys, ", "))
	}
	status.Summary = strings.Join(lines, "\n")
	return status, nil
}

func (s *SodaPlatform) ImportCookie(ctx context.Context, raw string) (platform.CookieImportResult, error) {
	_ = ctx
	if s == nil || s.client == nil {
		return platform.CookieImportResult{}, fmt.Errorf("soda client unavailable")
	}
	raw = normalizeSodaCookieString(raw)
	if raw == "" {
		return platform.CookieImportResult{}, fmt.Errorf("cookie empty")
	}
	s.client.SetCookie(raw)
	if err := s.client.persistCookie(raw); err != nil {
		return platform.CookieImportResult{}, err
	}
	core := s.client.CoreCookieSignals()
	message := "汽水音乐 Cookie 已更新"
	if len(core) > 0 {
		message += "\n检测到关键字段: " + strings.Join(core, ", ")
	} else {
		message += "\n未检测到 sessionid/sid_tt/sid_guard/uid_tt 等核心字段，可能只能访问公开内容"
	}
	return platform.CookieImportResult{Updated: true, Message: message}, nil
}

func (c *Client) SetCookie(raw string) {
	if c == nil {
		return
	}
	c.cookie = normalizeSodaCookieString(raw)
}

func (c *Client) persistCookie(raw string) error {
	if c == nil {
		return fmt.Errorf("soda client unavailable")
	}
	if c.persistFunc == nil {
		return fmt.Errorf("soda persist func unavailable")
	}
	return c.persistFunc(map[string]string{"cookie": normalizeSodaCookieString(raw)})
}

func (c *Client) Cookie() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.cookie)
}

func (c *Client) CookieMap() map[string]string {
	result := make(map[string]string)
	raw := c.Cookie()
	raw = strings.TrimSpace(strings.Trim(raw, "`\"'"))
	for _, part := range strings.Split(raw, ";") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 {
			continue
		}
		key := strings.TrimSpace(pair[0])
		value := strings.TrimSpace(pair[1])
		if key == "" || value == "" {
			continue
		}
		result[key] = value
	}
	return result
}

func (c *Client) CoreCookieSignals() []string {
	keys := []string{"sessionid", "sessionid_ss", "sid_tt", "sid_guard", "uid_tt", "uid_tt_ss", "ttwid", "odin_tt", "msToken", "passport_csrf_token"}
	available := make([]string, 0, len(keys))
	fields := c.CookieMap()
	for _, key := range keys {
		if strings.TrimSpace(fields[key]) != "" {
			available = append(available, key)
		}
	}
	return available
}

func (c *Client) FetchSelfProfile(ctx context.Context) (*sodaSelfProfileResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("soda client unavailable")
	}
	body, err := c.getJSONWithHeaders(ctx, "https://www.douyin.com/aweme/v1/web/user/profile/self/", map[string]string{
		"Referer": "https://music.douyin.com/",
	})
	if err == nil {
		var resp sodaSelfProfileResponse
		if json.Unmarshal(body, &resp) == nil {
			uid := strings.TrimSpace(firstNonEmptyString(resp.User.UID, resp.Data.UID))
			if uid != "" {
				return &resp, nil
			}
		}
	}
	body, err = c.getJSONWithHeaders(ctx, "https://www.douyin.com/passport/web/account/info/", map[string]string{
		"Referer": "https://music.douyin.com/",
	})
	if err != nil {
		return nil, err
	}
	var alt sodaAccountInfoResponse
	if err := json.Unmarshal(body, &alt); err != nil {
		return nil, err
	}
	uid := strings.TrimSpace(alt.Data.UID)
	if uid == "" {
		return nil, fmt.Errorf("soda account status unavailable")
	}
	result := &sodaSelfProfileResponse{}
	result.Data.UID = uid
	result.Data.Nickname = strings.TrimSpace(alt.Data.Nickname)
	result.Data.UniqueID = strings.TrimSpace(alt.Data.UniqueID)
	return result, nil
}

func (c *Client) CanAccessCoreContent(ctx context.Context) bool {
	if c == nil {
		return false
	}
	params := url.Values{}
	params.Set("track_id", "7620326800652224539")
	params.Set("media_type", "track")
	params.Set("aid", sodaAid)
	params.Set("device_platform", "web")
	params.Set("channel", sodaPCChannel)
	body, err := c.getJSON(ctx, "https://api.qishui.com/luna/pc/track_v2?"+params.Encode())
	if err != nil {
		return false
	}
	var resp sodaTrackV2Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}
	trackData := resp.TrackInfo
	if strings.TrimSpace(trackData.ID) == "" {
		trackData = resp.Track
	}
	if strings.TrimSpace(trackData.ID) == "" {
		return false
	}
	if strings.TrimSpace(resp.TrackPlayer.URLPlayerInfo) == "" {
		return false
	}
	playInfos, err := c.fetchPlayInfos(ctx, resp.TrackPlayer.URLPlayerInfo)
	if err != nil || len(playInfos) == 0 {
		return false
	}
	best := playInfos[0]
	return strings.TrimSpace(firstNonEmptyString(best.MainPlayURL, best.BackupPlayURL)) != "" && strings.TrimSpace(best.PlayAuth) != ""
}

func (c *Client) getJSONWithHeaders(ctx context.Context, rawURL string, extra map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", sodaUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if strings.TrimSpace(c.cookie) != "" {
		req.Header.Set("Cookie", c.cookie)
	}
	for key, value := range extra {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("soda status request failed: %d", resp.StatusCode)
	}
	return body, nil
}

func normalizeSodaCookieString(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, "`\"'"))
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "cookie="))
	parts := strings.Split(raw, ";")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 {
			continue
		}
		key := strings.TrimSpace(pair[0])
		value := strings.TrimSpace(pair[1])
		if key == "" || value == "" {
			continue
		}
		items = append(items, key+"="+value)
	}
	return strings.Join(items, "; ")
}

func maskSodaValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}
