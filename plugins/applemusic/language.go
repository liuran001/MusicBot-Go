package applemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// storefrontInfo holds the account's storefront and its supported languages,
// fetched live from the Apple Music API (not hardcoded — the supported set
// varies per storefront, e.g. Türkiye only offers [en-GB, tr]).
type storefrontInfo struct {
	ID             string   // storefront id, e.g. "tr"
	Name           string   // human name, e.g. "Türkiye"
	DefaultLang    string   // defaultLanguageTag, e.g. "en-GB"
	SupportedLangs []string // supportedLanguageTags
}

// fetchStorefrontInfo queries /v1/me/storefront for the account's storefront
// and the languages it supports. Requires a media-user-token.
func (c *Client) fetchStorefrontInfo(ctx context.Context) (*storefrontInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("applemusic client unavailable")
	}
	if strings.TrimSpace(c.mediaUserToken) == "" {
		return nil, fmt.Errorf("media-user-token 未配置")
	}
	if err := c.ensureDeveloperToken(ctx); err != nil {
		return nil, err
	}

	body, err := c.doRequestInner(ctx, appleMusicBaseURL+"/v1/me/storefront", false)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				DefaultLanguageTag    string   `json:"defaultLanguageTag"`
				Name                  string   `json:"name"`
				SupportedLanguageTags []string `json:"supportedLanguageTags"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 storefront 响应失败: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("storefront 响应为空")
	}
	d := resp.Data[0]
	return &storefrontInfo{
		ID:             d.ID,
		Name:           d.Attributes.Name,
		DefaultLang:    d.Attributes.DefaultLanguageTag,
		SupportedLangs: d.Attributes.SupportedLanguageTags,
	}, nil
}

// CurrentStorefront / CurrentLanguage expose the in-memory values for display.
func (c *Client) CurrentStorefront() string {
	if c == nil {
		return ""
	}
	return c.storefront
}

func (c *Client) CurrentLanguage() string {
	if c == nil {
		return ""
	}
	return c.language
}

// SetLanguage validates lang against the storefront's supported languages,
// applies it in memory (marking it explicit so storefront auto-detect won't
// override it), and persists it to config.ini.
func (c *Client) SetLanguage(ctx context.Context, lang string) error {
	if c == nil {
		return fmt.Errorf("applemusic client unavailable")
	}
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return fmt.Errorf("语言不能为空")
	}

	info, err := c.fetchStorefrontInfo(ctx)
	if err != nil {
		return err
	}
	if !containsFold(info.SupportedLangs, lang) {
		return fmt.Errorf("storefront %q 不支持语言 %q；支持的语言: %s",
			info.ID, lang, strings.Join(info.SupportedLangs, ", "))
	}

	// Normalize to the exact casing the API advertises.
	for _, sl := range info.SupportedLangs {
		if strings.EqualFold(sl, lang) {
			lang = sl
			break
		}
	}

	c.language = lang
	c.languageExplicit = true
	if c.persistFunc == nil {
		return fmt.Errorf("配置持久化不可用")
	}
	return c.persistFunc(map[string]string{"language": lang})
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}
