package youtubemusic

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const youtubeMusicCookieCheckVideoID = "dQw4w9WgXcQ"

func (p *YouTubeMusicPlatform) SupportedLoginMethods() []string {
	return []string{"status", "check"}
}

func (p *YouTubeMusicPlatform) AccountStatus(ctx context.Context) (platform.AccountStatus, error) {
	status := platform.AccountStatus{
		Platform:        platformName,
		DisplayName:     metadata().DisplayName,
		AuthMode:        "anonymous",
		CanCheckCookie:  true,
		CanRenewCookie:  false,
		SupportedLogins: p.SupportedLoginMethods(),
	}
	if p == nil || p.client == nil {
		status.Summary = "- 状态: 插件未初始化"
		return status, nil
	}

	hasCookie := strings.TrimSpace(p.client.cookie) != ""
	lines := make([]string, 0, 6)
	if hasCookie {
		status.AuthMode = "cookie"
		lines = append(lines, "- 状态: 已配置 Cookie")
		if keys := youtubeMusicCookieKeys(p.client.cookie); len(keys) > 0 {
			lines = append(lines, "- Cookie 字段: "+strings.Join(keys, ", "))
		}
	} else {
		lines = append(lines, "- 状态: 访客模式（未配置 Cookie）")
	}

	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results, err := p.client.Search(probeCtx, "test", 1)
	if err != nil {
		lines = append(lines, "- 验证: InnerTube 搜索失败（"+formatYouTubeMusicAccountError(err)+"）")
		status.Summary = strings.Join(lines, "\n")
		return status, nil
	}
	if hasCookie {
		status.LoggedIn = true
	}
	if len(results) > 0 {
		lines = append(lines, "- 验证: InnerTube 搜索可访问")
	} else {
		lines = append(lines, "- 验证: InnerTube 可访问（搜索无结果）")
	}
	status.Summary = strings.Join(lines, "\n")
	return status, nil
}

func (p *YouTubeMusicPlatform) CheckCookie(ctx context.Context) (platform.CookieCheckResult, error) {
	if p == nil || p.client == nil {
		return platform.CookieCheckResult{OK: false, Message: "YouTube Music 插件未初始化"}, nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	info, err := p.GetDownloadInfo(checkCtx, youtubeMusicCookieCheckVideoID, platform.QualityHigh)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: "YouTube Music 下载链路校验失败: " + formatYouTubeMusicAccountError(err)}, nil
	}
	if info == nil || strings.TrimSpace(info.URL) == "" {
		return platform.CookieCheckResult{OK: false, Message: "YouTube Music 下载链接为空"}, nil
	}

	mode := "匿名 InnerTube"
	if strings.TrimSpace(p.client.cookie) != "" {
		mode = "Cookie InnerTube"
	}
	message := fmt.Sprintf("%s 可用，%s %dk", mode, strings.TrimSpace(info.Format), info.Bitrate)
	if info.Size > 0 {
		message += fmt.Sprintf("，%.2fMB", float64(info.Size)/1024/1024)
	}
	return platform.CookieCheckResult{OK: true, Message: message}, nil
}

func youtubeMusicCookieKeys(raw string) []string {
	parts := strings.Split(raw, ";")
	keys := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatYouTubeMusicAccountError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 220 {
		msg = msg[:220] + "..."
	}
	return msg
}
