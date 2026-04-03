package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func (n *NeteasePlatform) SupportedLoginMethods() []string {
	return []string{"qr", "cookie", "renew", "auto", "status"}
}

func (n *NeteasePlatform) AccountStatus(ctx context.Context) (platform.AccountStatus, error) {
	_ = ctx
	status := platform.AccountStatus{
		Platform:        n.Name(),
		DisplayName:     n.Metadata().DisplayName,
		AuthMode:        "cookie",
		CanCheckCookie:  true,
		CanRenewCookie:  false,
		SupportedLogins: n.SupportedLoginMethods(),
	}
	if n == nil || n.client == nil {
		status.Summary = "- 状态: 插件未初始化"
		return status, nil
	}
	cookies := n.client.CookieMap()
	_, hasMusicU := cookies["MUSIC_U"]
	_, hasMusicA := cookies["MUSIC_A"]
	status.LoggedIn = hasMusicU
	lines := []string{"- 状态: 未登录"}
	if hasMusicU {
		lines = []string{"- 状态: 已配置 MUSIC_U"}
	} else if hasMusicA {
		lines = []string{"- 状态: 已配置访客 Cookie（MUSIC_A）"}
	}
	keys := make([]string, 0, len(cookies))
	for key := range cookies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		lines = append(lines, "- Cookie 字段: "+strings.Join(keys, ", "))
	}
	status.Summary = strings.Join(lines, "\n")
	return status, nil
}

func (n *NeteasePlatform) ImportCookie(ctx context.Context, raw string) (platform.CookieImportResult, error) {
	_ = ctx
	if n == nil || n.client == nil {
		return platform.CookieImportResult{}, fmt.Errorf("netease client unavailable")
	}
	if err := n.client.SetCookieString(raw); err != nil {
		return platform.CookieImportResult{}, err
	}
	return platform.CookieImportResult{Updated: true, Message: "网易云音乐 Cookie 已更新"}, nil
}

func (n *NeteasePlatform) ManualRenew(ctx context.Context) (string, error) {
	if n == nil || n.client == nil {
		return "", fmt.Errorf("netease client unavailable")
	}
	return n.client.ManualRenew(ctx)
}

func (n *NeteasePlatform) GetAutoRenewStatus(ctx context.Context) (platform.AutoRenewStatus, error) {
	_ = ctx
	if n == nil || n.client == nil {
		return platform.AutoRenewStatus{}, fmt.Errorf("netease client unavailable")
	}
	return n.client.AutoRenewStatus(), nil
}

func (n *NeteasePlatform) SetAutoRenew(ctx context.Context, enabled bool, interval time.Duration) (platform.AutoRenewStatus, error) {
	_ = ctx
	if n == nil || n.client == nil {
		return platform.AutoRenewStatus{}, fmt.Errorf("netease client unavailable")
	}
	return n.client.SetAutoRenew(enabled, interval)
}

type neteaseRefreshResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) ManualRenew(ctx context.Context) (string, error) {
	body, cookies, err := c.weapiRequestDetailed(ctx, "https://music.163.com/weapi/login/token/refresh", map[string]any{}, nil)
	if err != nil {
		return "", err
	}
	var resp neteaseRefreshResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	if resp.Code != 200 {
		if strings.TrimSpace(resp.Message) != "" {
			return "", fmt.Errorf("netease refresh failed: %s", strings.TrimSpace(resp.Message))
		}
		return "", fmt.Errorf("netease refresh failed: code=%d", resp.Code)
	}
	if len(cookies) > 0 {
		current := c.CookieMap()
		for _, cookie := range cookies {
			if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
				continue
			}
			if strings.TrimSpace(cookie.Value) == "" {
				delete(current, strings.TrimSpace(cookie.Name))
				continue
			}
			current[strings.TrimSpace(cookie.Name)] = strings.TrimSpace(cookie.Value)
		}
		if err := c.SetCookieMap(current); err != nil {
			return "", err
		}
	}
	return "网易云音乐登录已刷新", nil
}

func (c *Client) CookieMap() map[string]string {
	result := make(map[string]string, len(c.baseData.Cookies))
	for _, cookie := range c.baseData.Cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		result[strings.TrimSpace(cookie.Name)] = strings.TrimSpace(cookie.Value)
	}
	return result
}

func (c *Client) SetCookieString(raw string) error {
	raw = strings.TrimSpace(strings.Trim(raw, "`\"'"))
	if raw == "" {
		return fmt.Errorf("cookie empty")
	}
	parts := strings.Split(raw, ";")
	cookies := make([]*http.Cookie, 0, len(parts))
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		name := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		if name == "" {
			continue
		}
		cookies = append(cookies, &http.Cookie{Name: name, Value: value})
	}
	if len(cookies) == 0 {
		return fmt.Errorf("no valid cookies found")
	}
	return c.SetCookieObjects(cookies)
}

func (c *Client) SetCookieMap(cookieMap map[string]string) error {
	if len(cookieMap) == 0 {
		return fmt.Errorf("cookie empty")
	}
	cookies := make([]*http.Cookie, 0, len(cookieMap))
	for key, value := range cookieMap {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		cookies = append(cookies, &http.Cookie{Name: key, Value: value})
	}
	if len(cookies) == 0 {
		return fmt.Errorf("no valid cookies found")
	}
	return c.SetCookieObjects(cookies)
}

func (c *Client) SetCookieObjects(cookies []*http.Cookie) error {
	if len(cookies) == 0 {
		return fmt.Errorf("cookie empty")
	}
	c.baseData.Cookies = cookies
	if c.persistFunc != nil {
		musicU := ""
		for _, cookie := range cookies {
			if cookie != nil && strings.EqualFold(strings.TrimSpace(cookie.Name), "MUSIC_U") {
				musicU = strings.TrimSpace(cookie.Value)
				break
			}
		}
		pairs := map[string]string{"cookie": renderNeteaseCookie(cookies)}
		if musicU != "" {
			pairs["music_u"] = musicU
		}
		if err := c.persistFunc(pairs); err != nil {
			return err
		}
	}
	return nil
}

func renderNeteaseCookie(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		parts = append(parts, strings.TrimSpace(cookie.Name)+"="+strings.TrimSpace(cookie.Value))
	}
	return strings.Join(parts, "; ")
}
