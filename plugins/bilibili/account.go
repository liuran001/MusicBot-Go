package bilibili

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/skip2/go-qrcode"
)

type bilibiliQRSessionStore struct {
	mu      sync.Mutex
	nextID  uint64
	cancels map[string]context.CancelFunc
}

var bilibiliQRStore = &bilibiliQRSessionStore{cancels: make(map[string]context.CancelFunc)}

func (s *bilibiliQRSessionStore) newSession() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("bilibili-%d", s.nextID)
	return id
}

func (s *bilibiliQRSessionStore) set(id string, cancel context.CancelFunc) {
	if strings.TrimSpace(id) == "" || cancel == nil {
		return
	}
	s.mu.Lock()
	s.cancels[id] = cancel
	s.mu.Unlock()
}

func (s *bilibiliQRSessionStore) cancel(id string) bool {
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	if ok {
		delete(s.cancels, id)
	}
	s.mu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
	return ok
}

func (s *bilibiliQRSessionStore) clear(id string) {
	s.mu.Lock()
	delete(s.cancels, id)
	s.mu.Unlock()
}

type bilibiliNavResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		IsLogin  bool   `json:"isLogin"`
		Mid      int64  `json:"mid"`
		Uname    string `json:"uname"`
		VipLabel struct {
			Text string `json:"text"`
		} `json:"vip_label"`
	} `json:"data"`
}

type bilibiliQRGenerateResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		URL       string `json:"url"`
		QRCodeKey string `json:"qrcode_key"`
	} `json:"data"`
}

type bilibiliQRPollResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Code         int    `json:"code"`
		Message      string `json:"message"`
		RefreshToken string `json:"refresh_token"`
		Timestamp    int64  `json:"timestamp"`
		URL          string `json:"url"`
	} `json:"data"`
}

func (b *BilibiliPlatform) SupportedLoginMethods() []string {
	return []string{"qr", "cookie", "renew", "auto", "status"}
}

func (b *BilibiliPlatform) AccountStatus(ctx context.Context) (platform.AccountStatus, error) {
	status := platform.AccountStatus{
		Platform:        b.Name(),
		DisplayName:     b.Metadata().DisplayName,
		AuthMode:        "cookie",
		CanCheckCookie:  true,
		CanRenewCookie:  true,
		SupportedLogins: b.SupportedLoginMethods(),
	}
	if b == nil || b.client == nil {
		status.Summary = "- 状态: 插件未初始化"
		return status, nil
	}
	nav, err := b.client.fetchNav(ctx)
	if err != nil {
		if strings.TrimSpace(b.client.cookie) != "" {
			status.Summary = "- 状态: 已配置 Cookie，但账号校验失败"
		} else {
			status.Summary = "- 状态: 未登录"
		}
		return status, nil
	}
	status.LoggedIn = nav.Data.IsLogin
	status.UserID = fmt.Sprintf("%d", nav.Data.Mid)
	status.Nickname = strings.TrimSpace(nav.Data.Uname)
	lines := []string{"- 状态: 未登录"}
	if status.LoggedIn {
		lines = []string{"- 状态: 已登录"}
		if status.Nickname != "" {
			lines = append(lines, "- 昵称: "+status.Nickname)
		}
		if status.UserID != "0" && status.UserID != "" {
			lines = append(lines, "- UID: "+maskBilibiliValue(status.UserID))
		}
		if vip := strings.TrimSpace(nav.Data.VipLabel.Text); vip != "" {
			lines = append(lines, "- 会员: "+vip)
		}
	}
	if strings.TrimSpace(b.client.refreshToken) != "" {
		lines = append(lines, "- refresh_token: 已配置")
	}
	status.Summary = strings.Join(lines, "\n")
	return status, nil
}

func (b *BilibiliPlatform) ImportCookie(ctx context.Context, raw string) (platform.CookieImportResult, error) {
	_ = ctx
	if b == nil || b.client == nil {
		return platform.CookieImportResult{}, fmt.Errorf("bilibili client unavailable")
	}
	raw = normalizeCookieString(raw)
	if strings.TrimSpace(raw) == "" {
		return platform.CookieImportResult{}, fmt.Errorf("cookie empty")
	}
	refreshToken := parseCookieField(raw, "ac_time_value")
	if refreshToken == "" {
		refreshToken = strings.TrimSpace(b.client.refreshToken)
	}
	b.client.cookieMutex.Lock()
	b.client.cookie = raw
	b.client.refreshToken = refreshToken
	b.client.cookieMutex.Unlock()
	if err := b.client.saveCookieToConfig(raw, refreshToken); err != nil {
		return platform.CookieImportResult{}, err
	}
	return platform.CookieImportResult{Updated: true, Message: "B站 Cookie 已更新"}, nil
}

func (b *BilibiliPlatform) StartQRLogin(ctx context.Context) (*platform.QRLoginSession, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("bilibili client unavailable")
	}
	qr, err := b.client.createQRLogin(ctx)
	if err != nil {
		return nil, err
	}
	png, err := qrcode.Encode(qr.Data.URL, qrcode.Medium, 320)
	if err != nil {
		return nil, err
	}
	session := &platform.QRLoginSession{
		Platform: b.Name(),
		Image: platform.QRLoginImage{
			URL:      strings.TrimSpace(qr.Data.URL),
			PNG:      png,
			Base64:   "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
			FileName: "bilibili_qr.png",
		},
		CancelID: bilibiliQRStore.newSession(),
		Caption:  buildBilibiliQRStartCaption(qr.Data.URL),
		Timeout:  3 * time.Minute,
	}
	key := strings.TrimSpace(qr.Data.QRCodeKey)
	session.Cancel = func() {
		_ = bilibiliQRStore.cancel(session.CancelID)
	}
	session.Poll = func(ctx context.Context, onUpdate func(platform.QRLoginUpdate, error)) {
		if onUpdate == nil {
			return
		}
		pollCtx, cancel := context.WithCancel(ctx)
		bilibiliQRStore.set(session.CancelID, cancel)
		defer bilibiliQRStore.clear(session.CancelID)
		interval := time.NewTicker(2 * time.Second)
		defer interval.Stop()
		lastState := "pending"
		skipInitialPending := true
		for {
			resp, err := b.client.pollQRLogin(pollCtx, key)
			if err != nil {
				if pollCtx.Err() != nil {
					onUpdate(platform.QRLoginUpdate{State: "cancelled", Message: "已取消 B站二维码登录", Final: true, Caption: "已取消 B站二维码登录"}, nil)
					return
				}
				onUpdate(platform.QRLoginUpdate{}, err)
				return
			}
			update := bilibiliQRUpdate(resp)
			if update.Final && resp.Data.Code == 0 {
				if status, statusErr := b.AccountStatus(context.Background()); statusErr == nil {
					update.Status = &status
					update.Caption = status.Summary
				}
			}
			if skipInitialPending && update.State == "pending" && !update.Final {
				skipInitialPending = false
				lastState = update.State
				goto waitNextPoll
			}
			skipInitialPending = false
			if update.State == lastState && !update.Final {
				goto waitNextPoll
			}
			lastState = update.State
			onUpdate(update, nil)
			if update.Final {
				return
			}
		waitNextPoll:
			select {
			case <-pollCtx.Done():
				onUpdate(platform.QRLoginUpdate{State: "cancelled", Message: "已取消 B站二维码登录", Final: true, Caption: "已取消 B站二维码登录"}, nil)
				return
			case <-interval.C:
			}
		}
	}
	return session, nil
}

func (b *BilibiliPlatform) CancelQRLogin(ctx context.Context, cancelID string) error {
	_ = ctx
	if !bilibiliQRStore.cancel(strings.TrimSpace(cancelID)) {
		return fmt.Errorf("qr login session not found")
	}
	return nil
}

func (c *Client) fetchNav(ctx context.Context) (*bilibiliNavResponse, error) {
	var result bilibiliNavResponse
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, "https://api.bilibili.com/x/web-interface/nav", nil)
		if err != nil {
			return err
		}
		c.setHeaders(req)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status %d", resp.StatusCode)
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		if result.Code != 0 {
			return fmt.Errorf("api error: %d %s", result.Code, result.Message)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) createQRLogin(ctx context.Context) (*bilibiliQRGenerateResponse, error) {
	var result bilibiliQRGenerateResponse
	err := c.execute(ctx, func() error {
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, "https://passport.bilibili.com/x/passport-login/web/qrcode/generate", nil)
		if err != nil {
			return err
		}
		c.setHeaders(req)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		if result.Code != 0 {
			return fmt.Errorf("api error: %d %s", result.Code, result.Message)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) pollQRLogin(ctx context.Context, key string) (*bilibiliQRPollResponse, error) {
	var result bilibiliQRPollResponse
	err := c.execute(ctx, func() error {
		endpoint := "https://passport.bilibili.com/x/passport-login/web/qrcode/poll?qrcode_key=" + url.QueryEscape(strings.TrimSpace(key))
		req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		c.setHeaders(req)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		if result.Code != 0 {
			return fmt.Errorf("api error: %d %s", result.Code, result.Message)
		}
		if result.Data.Code == 0 {
			updatedCookie := c.cookie
			for _, ck := range resp.Cookies() {
				if ck == nil || strings.TrimSpace(ck.Name) == "" || strings.TrimSpace(ck.Value) == "" {
					continue
				}
				updatedCookie = mergeCookiePair(updatedCookie, ck.Name, ck.Value)
			}
			refreshToken := strings.TrimSpace(result.Data.RefreshToken)
			if refreshToken == "" {
				refreshToken = parseQueryValue(result.Data.URL, "refresh_token")
			}
			c.cookieMutex.Lock()
			c.cookie = updatedCookie
			if refreshToken != "" {
				c.refreshToken = refreshToken
			}
			persistCookie := c.cookie
			persistRefresh := c.refreshToken
			c.cookieMutex.Unlock()
			_ = c.saveCookieToConfig(persistCookie, persistRefresh)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func buildBilibiliQRStartCaption(rawURL string) string {
	parts := []string{"已生成 B站扫码二维码"}
	if rawURL = strings.TrimSpace(rawURL); rawURL != "" {
		parts = append(parts, "链接: "+rawURL)
	}
	return strings.Join(parts, "\n")
}

func bilibiliQRUpdate(resp *bilibiliQRPollResponse) platform.QRLoginUpdate {
	if resp == nil {
		return platform.QRLoginUpdate{State: "failed", Message: "二维码轮询失败", Final: true}
	}
	update := platform.QRLoginUpdate{}
	switch resp.Data.Code {
	case 86101:
		update.State = "pending"
		update.Message = "B站二维码等待扫码"
	case 86090:
		update.State = "scanned"
		update.Message = "B站二维码已扫码，等待确认"
	case 86038:
		update.State = "expired"
		update.Message = "B站二维码已过期"
		update.Final = true
	case 0:
		update.State = "success"
		update.Message = "B站扫码登录成功"
		update.Final = true
	default:
		update.State = "pending"
		update.Message = firstNonEmptyBilibili(resp.Data.Message, resp.Message, fmt.Sprintf("二维码状态码: %d", resp.Data.Code))
	}
	update.Caption = update.Message
	return update
}

func normalizeCookieString(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "`\"'")
	return strings.TrimSpace(raw)
}

func parseCookieField(raw, key string) string {
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, key+"=") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(part, key+"="))
	}
	return ""
}

func mergeCookiePair(raw, key, value string) string {
	parts := strings.Split(strings.TrimSpace(raw), ";")
	cookieMap := make(map[string]string, len(parts)+1)
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		cookieMap[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
		cookieMap[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	items := make([]string, 0, len(cookieMap))
	for k, v := range cookieMap {
		items = append(items, k+"="+v)
	}
	return strings.Join(items, "; ")
}

func parseQueryValue(rawURL, key string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get(key))
}

func firstNonEmptyBilibili(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maskBilibiliValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}
