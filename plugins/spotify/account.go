package spotify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const spotifyCookieCheckTrackID = "18gqCQzqYb0zvurQPlRkpo"

type spotifyAudioProbeSource interface {
	directAudioSource
	CookieConfigured() bool
	DeviceConfigured() bool
	AccountProduct(ctx context.Context) (string, error)
	ProbeDownload(ctx context.Context, trackID string, quality platform.Quality) (spotifyAudioProbeResult, error)
}

func (p *SpotifyPlatform) SupportedLoginMethods() []string {
	return []string{"status", "check"}
}

func (p *SpotifyPlatform) AccountStatus(ctx context.Context) (platform.AccountStatus, error) {
	status := platform.AccountStatus{
		Platform:        platformName,
		DisplayName:     metadata().DisplayName,
		AuthMode:        "sp_dc + widevine",
		CanCheckCookie:  true,
		CanRenewCookie:  false,
		SupportedLogins: p.SupportedLoginMethods(),
	}
	if p == nil || p.client == nil {
		status.Summary = "- 状态: 插件未初始化"
		return status, nil
	}

	lines := make([]string, 0, 8)
	if p.client.hasClientCredentials() {
		lines = append(lines, "- Web API: 已配置 client credentials")
	} else {
		lines = append(lines, "- Web API: 未配置 client credentials（直链元数据走 sp_dc fallback）")
	}

	src, ok := p.native.(spotifyAudioProbeSource)
	if !ok || src == nil {
		lines = append(lines, "- 状态: 未启用 Spotify 原生下载源")
		status.Summary = strings.Join(lines, "\n")
		return status, nil
	}
	if !src.CookieConfigured() {
		lines = append(lines, "- 状态: 未配置 sp_dc")
		lines = append(lines, "- Widevine: "+formatSpotifyConfigured(src.DeviceConfigured()))
		status.Summary = strings.Join(lines, "\n")
		return status, nil
	}

	lines = append(lines, "- sp_dc: 已配置")
	lines = append(lines, "- Widevine: "+formatSpotifyConfigured(src.DeviceConfigured()))

	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	product, err := src.AccountProduct(probeCtx)
	if err != nil {
		lines = append(lines, "- 验证: sp_dc 校验失败（"+formatSpotifyAccountError(err)+"）")
		status.Summary = strings.Join(lines, "\n")
		return status, nil
	}

	status.LoggedIn = true
	status.SessionSource = "web-player"
	product = formatSpotifyProduct(product)
	lines = append(lines, "- 账号: "+product)
	lines = append(lines, "- 预计质量: "+spotifyExpectedQuality(product))
	status.Summary = strings.Join(lines, "\n")
	return status, nil
}

func (p *SpotifyPlatform) CheckCookie(ctx context.Context) (platform.CookieCheckResult, error) {
	if p == nil || p.client == nil {
		return platform.CookieCheckResult{OK: false, Message: "Spotify 插件未初始化"}, nil
	}
	src, ok := p.native.(spotifyAudioProbeSource)
	if !ok || src == nil {
		return platform.CookieCheckResult{OK: false, Message: "未启用 Spotify 原生下载源"}, nil
	}
	if !src.CookieConfigured() {
		return platform.CookieCheckResult{OK: false, Message: "未配置 sp_dc"}, nil
	}
	if !src.DeviceConfigured() {
		return platform.CookieCheckResult{OK: false, Message: "未配置 Widevine 设备（wvd_path）"}, nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	product, err := src.AccountProduct(checkCtx)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: "sp_dc 校验失败: " + formatSpotifyAccountError(err)}, nil
	}
	probe, err := src.ProbeDownload(checkCtx, spotifyCookieCheckTrackID, platform.QualityHiRes)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: "Widevine license 校验失败: " + formatSpotifyAccountError(err)}, nil
	}
	if probe.Bitrate <= 0 || probe.NumKeys <= 0 {
		return platform.CookieCheckResult{OK: false, Message: "Widevine license 未返回可用音频密钥"}, nil
	}

	message := fmt.Sprintf("sp_dc 有效，账号=%s，AAC %dk license OK", formatSpotifyProduct(product), probe.Bitrate)
	if strings.TrimSpace(probe.CDNHost) != "" {
		message += "，CDN=" + strings.TrimSpace(probe.CDNHost)
	}
	return platform.CookieCheckResult{OK: true, Message: message}, nil
}

func formatSpotifyConfigured(ok bool) string {
	if ok {
		return "已配置"
	}
	return "未配置"
}

func formatSpotifyProduct(product string) string {
	product = strings.TrimSpace(strings.ToLower(product))
	if product == "" {
		return "unknown"
	}
	return product
}

func spotifyExpectedQuality(product string) string {
	if strings.EqualFold(strings.TrimSpace(product), "premium") {
		return "AAC 256k"
	}
	return "AAC 128k"
}

func formatSpotifyAccountError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 240 {
		msg = msg[:240] + "..."
	}
	return msg
}
