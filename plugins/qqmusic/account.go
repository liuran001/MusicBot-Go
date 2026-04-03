package qqmusic

import (
	"context"
	"fmt"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func (q *QQMusicPlatform) SupportedLoginMethods() []string {
	return []string{"cookie", "renew", "auto", "status"}
}

func (q *QQMusicPlatform) AccountStatus(ctx context.Context) (platform.AccountStatus, error) {
	_ = ctx
	status := platform.AccountStatus{
		Platform:        q.Name(),
		DisplayName:     q.Metadata().DisplayName,
		AuthMode:        "cookie",
		CanCheckCookie:  true,
		CanRenewCookie:  true,
		SupportedLogins: q.SupportedLoginMethods(),
	}
	if q == nil || q.client == nil {
		status.Summary = "- 状态: 插件未初始化"
		return status, nil
	}
	cookie := q.client.Cookie()
	uin, authst, source := parseQQAuthDetails(cookie)
	status.LoggedIn = strings.TrimSpace(authst) != ""
	status.UserID = strings.TrimSpace(uin)
	lines := []string{"- 状态: 未登录"}
	if status.LoggedIn {
		lines = []string{"- 状态: 已配置登录 Cookie"}
		if uin != "" && uin != "0" {
			lines = append(lines, "- UIN: "+maskQQValue(uin))
		}
		if source != "" {
			lines = append(lines, "- 鉴权来源: "+source)
		}
	}
	if strings.TrimSpace(parseCookieValue(cookie, "psrf_qqrefresh_token")) != "" {
		lines = append(lines, "- refresh_token: 已配置")
	}
	status.Summary = strings.Join(lines, "\n")
	return status, nil
}

func (q *QQMusicPlatform) ImportCookie(ctx context.Context, raw string) (platform.CookieImportResult, error) {
	_ = ctx
	if q == nil || q.client == nil {
		return platform.CookieImportResult{}, fmt.Errorf("qqmusic client unavailable")
	}
	raw = strings.TrimSpace(strings.Trim(raw, "`\"'"))
	if raw == "" {
		return platform.CookieImportResult{}, fmt.Errorf("cookie empty")
	}
	q.client.setCookie(raw)
	q.client.persistCookie(raw)
	return platform.CookieImportResult{Updated: true, Message: "QQ音乐 Cookie 已更新"}, nil
}

func maskQQValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}
