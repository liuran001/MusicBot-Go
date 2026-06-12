package applemusic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func (a *AppleMusicPlatform) SupportedLoginMethods() []string {
	return []string{"cookie"}
}

// ShowLanguage implements platform.LanguageProvider: report the account
// storefront, current language, and the languages that storefront supports
// (fetched live — the supported set varies per storefront).
func (a *AppleMusicPlatform) ShowLanguage(ctx context.Context) (string, error) {
	if a == nil || a.client == nil {
		return "Apple Music 插件未初始化", nil
	}
	info, err := a.client.fetchStorefrontInfo(ctx)
	if err != nil {
		return fmt.Sprintf("🍎 Apple Music 语言\n- 当前 Storefront: %s\n- 当前语言: %s\n- 获取支持语言失败: %v",
			a.client.CurrentStorefront(), a.client.CurrentLanguage(), err), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🍎 Apple Music 语言\n")
	fmt.Fprintf(&b, "- 账号 Storefront: %s（%s）\n", info.ID, info.Name)
	fmt.Fprintf(&b, "- 当前语言: %s\n", a.client.CurrentLanguage())
	fmt.Fprintf(&b, "- 账号默认语言: %s\n", info.DefaultLang)
	fmt.Fprintf(&b, "- 支持的语言:\n")
	for _, lang := range info.SupportedLangs {
		marker := "  •"
		if strings.EqualFold(lang, a.client.CurrentLanguage()) {
			marker = "  ✅"
		}
		fmt.Fprintf(&b, "%s %s\n", marker, lang)
	}
	fmt.Fprintf(&b, "\n用 /login applemusic lang <语言> 设置，例如 /login applemusic lang %s", info.DefaultLang)
	return b.String(), nil
}

// SetLanguage implements platform.LanguageProvider: validate the language
// against the storefront's supported set, apply it, and persist to config.
func (a *AppleMusicPlatform) SetLanguage(ctx context.Context, lang string) (string, error) {
	if a == nil || a.client == nil {
		return "Apple Music 插件未初始化", nil
	}
	if err := a.client.SetLanguage(ctx, lang); err != nil {
		return fmt.Sprintf("❌ 设置失败：%v", err), nil
	}
	return fmt.Sprintf("✅ Apple Music 语言已设为 %s 并写回配置。", a.client.CurrentLanguage()), nil
}

func (a *AppleMusicPlatform) AccountStatus(ctx context.Context) (platform.AccountStatus, error) {
	status := platform.AccountStatus{
		Platform:        a.Name(),
		DisplayName:     a.Metadata().DisplayName,
		AuthMode:        "cookie",
		CanCheckCookie:  true,
		CanRenewCookie:  false,
		SupportedLogins: a.SupportedLoginMethods(),
	}
	if a == nil || a.client == nil {
		status.Summary = "- 状态: 插件未初始化"
		return status, nil
	}

	token := a.client.MediaUserToken()
	if strings.TrimSpace(token) == "" {
		status.Summary = "- 状态: 未配置 media-user-token\n- Storefront: " + a.client.storefront + "\n- Language: " + a.client.language
		return status, nil
	}

	lines := []string{"- 状态: 已配置 media-user-token"}
	lines = append(lines, "- Token: "+maskTokenValue(token))
	lines = append(lines, "- Storefront: "+a.client.storefront)
	lines = append(lines, "- Language: "+a.client.language)

	// Try a test search to verify the token works
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results, err := a.client.Search(probeCtx, "test", 1)
	if err == nil && len(results) > 0 {
		status.LoggedIn = true
		lines = append(lines, "- 验证: Token 有效（搜索测试通过）")
	} else if err != nil {
		lines = append(lines, "- 验证: Token 可能无效（"+err.Error()+"）")
	} else {
		status.LoggedIn = true
		lines = append(lines, "- 验证: Token 可用（搜索无结果但接口正常）")
	}

	status.Summary = strings.Join(lines, "\n")
	return status, nil
}

func (a *AppleMusicPlatform) ImportCookie(ctx context.Context, raw string) (platform.CookieImportResult, error) {
	if a == nil || a.client == nil {
		return platform.CookieImportResult{}, fmt.Errorf("applemusic client unavailable")
	}

	raw = normalizeMediaUserToken(raw)
	if raw == "" {
		return platform.CookieImportResult{}, fmt.Errorf("media-user-token empty")
	}

	a.client.SetMediaUserToken(raw)
	if err := a.client.persistToken(raw); err != nil {
		return platform.CookieImportResult{}, err
	}

	// Verify the token works
	verifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results, err := a.client.Search(verifyCtx, "test", 1)

	message := "Apple Music media-user-token 已更新"
	if err != nil {
		message += "\n验证失败: " + err.Error()
	} else if len(results) > 0 {
		message += "\n验证通过: 搜索测试成功"
	} else {
		message += "\n验证通过: 接口可访问"
	}

	return platform.CookieImportResult{Updated: true, Message: message}, nil
}

func (a *AppleMusicPlatform) CheckCookie(ctx context.Context) (platform.CookieCheckResult, error) {
	if a == nil || a.client == nil {
		return platform.CookieCheckResult{OK: false, Message: "Apple Music 插件未初始化"}, nil
	}

	token := a.client.MediaUserToken()
	if strings.TrimSpace(token) == "" {
		return platform.CookieCheckResult{OK: false, Message: "未配置 media-user-token"}, nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results, err := a.client.Search(checkCtx, "test", 1)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("Apple Music 搜索校验失败: %v", err)}, nil
	}
	if len(results) > 0 {
		return platform.CookieCheckResult{OK: true, Message: "Apple Music Token 有效（搜索测试通过）"}, nil
	}
	return platform.CookieCheckResult{OK: true, Message: "Apple Music Token 可用（接口可访问）"}, nil
}

// Client helpers for account management

func (c *Client) MediaUserToken() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.mediaUserToken)
}

func (c *Client) SetMediaUserToken(token string) {
	if c == nil {
		return
	}
	c.mediaUserToken = normalizeMediaUserToken(token)
}

func (c *Client) persistToken(token string) error {
	if c == nil {
		return fmt.Errorf("applemusic client unavailable")
	}
	if c.persistFunc == nil {
		return fmt.Errorf("applemusic persist func unavailable")
	}
	return c.persistFunc(map[string]string{"media_user_token": normalizeMediaUserToken(token)})
}

func normalizeMediaUserToken(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "`\"'")
	raw = strings.TrimSpace(raw)
	return raw
}

func maskTokenValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return strings.Repeat("*", len(value))
	}
	return value[:4] + strings.Repeat("*", len(value)-8) + value[len(value)-4:]
}
