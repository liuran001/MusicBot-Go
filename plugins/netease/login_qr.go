package netease

import (
	"context"
	"crypto/aes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/skip2/go-qrcode"
)

const (
	neteaseQRBaseURL = "https://music.163.com/login?codekey="
	neteasePresetKey = "0CoJUm6Qyw8W8jud"
	neteaseAESIV     = "0102030405060708"
)

type neteaseQRSessionStore struct {
	mu      sync.Mutex
	nextID  uint64
	cancels map[string]context.CancelFunc
}

var neteaseQRStore = &neteaseQRSessionStore{cancels: make(map[string]context.CancelFunc)}

func (s *neteaseQRSessionStore) newSession() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return fmt.Sprintf("netease-%d", s.nextID)
}

func (s *neteaseQRSessionStore) set(id string, cancel context.CancelFunc) {
	if strings.TrimSpace(id) == "" || cancel == nil {
		return
	}
	s.mu.Lock()
	s.cancels[id] = cancel
	s.mu.Unlock()
}

func (s *neteaseQRSessionStore) cancel(id string) bool {
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

func (s *neteaseQRSessionStore) clear(id string) {
	s.mu.Lock()
	delete(s.cancels, id)
	s.mu.Unlock()
}

var (
	neteaseRSAModulus = mustParseHexBigInt("00e0b509f6259df8642dbc35662901477df22677ec152b5ff68ace615bb7b725152b3ab17a876aea8a5aa76d2e417629ec4ee341f56135fccf695280104e0312ecbda92557c93870114af6c9d05c4f7f0c3685b7a46bee255932575cce10b424d813cfe4875d3e82047b97ddef52741d546b8e289dc6935b3ece0462db0a22b8e7")
)

type neteaseQRKeyResponse struct {
	Code   int    `json:"code"`
	Unikey string `json:"unikey"`
	Data   struct {
		Unikey string `json:"unikey"`
	} `json:"data"`
}

type neteaseQRCheckResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Cookie    string `json:"cookie"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}

func (n *NeteasePlatform) StartQRLogin(ctx context.Context) (*platform.QRLoginSession, error) {
	if n == nil || n.client == nil {
		return nil, fmt.Errorf("netease client unavailable")
	}
	unikey, err := n.client.CreateQRCodeKey(ctx)
	if err != nil {
		return nil, err
	}
	qrURL := neteaseQRBaseURL + url.QueryEscape(unikey)
	png, err := qrcode.Encode(qrURL, qrcode.Medium, 320)
	if err != nil {
		return nil, err
	}
	session := &platform.QRLoginSession{
		Platform: n.Name(),
		Image: platform.QRLoginImage{
			URL:      qrURL,
			PNG:      png,
			Base64:   "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
			FileName: "netease_qr.png",
		},
		CancelID: neteaseQRStore.newSession(),
		Caption:  "已生成网易云音乐扫码二维码\n链接: " + qrURL,
		Timeout:  3 * time.Minute,
	}
	session.Cancel = func() {
		_ = neteaseQRStore.cancel(session.CancelID)
	}
	session.Poll = func(ctx context.Context, onUpdate func(platform.QRLoginUpdate, error)) {
		if onUpdate == nil {
			return
		}
		pollCtx, cancel := context.WithCancel(ctx)
		neteaseQRStore.set(session.CancelID, cancel)
		defer neteaseQRStore.clear(session.CancelID)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			resp, err := n.client.CheckQRCode(pollCtx, unikey)
			if err != nil {
				if pollCtx.Err() != nil {
					onUpdate(platform.QRLoginUpdate{State: "cancelled", Message: "已取消网易云二维码登录", Final: true, Caption: "已取消网易云二维码登录"}, nil)
					return
				}
				onUpdate(platform.QRLoginUpdate{}, err)
				return
			}
			update := buildNeteaseQRUpdate(resp)
			if resp.Code == 803 && strings.TrimSpace(resp.Cookie) != "" {
				if _, importErr := n.ImportCookie(context.Background(), resp.Cookie); importErr == nil {
					if status, statusErr := n.AccountStatus(context.Background()); statusErr == nil {
						update.Status = &status
						update.Caption = status.Summary
					}
				}
			}
			onUpdate(update, nil)
			if update.Final {
				return
			}
			select {
			case <-pollCtx.Done():
				onUpdate(platform.QRLoginUpdate{State: "cancelled", Message: "已取消网易云二维码登录", Final: true, Caption: "已取消网易云二维码登录"}, nil)
				return
			case <-ticker.C:
			}
		}
	}
	return session, nil
}

func (n *NeteasePlatform) CancelQRLogin(ctx context.Context, cancelID string) error {
	_ = ctx
	if !neteaseQRStore.cancel(strings.TrimSpace(cancelID)) {
		return fmt.Errorf("qr login session not found")
	}
	return nil
}

func (c *Client) CreateQRCodeKey(ctx context.Context) (string, error) {
	body, err := c.weapiRequest(ctx, "https://interface.music.163.com/weapi/login/qrcode/unikey?csrf_token=", map[string]any{"type": 1})
	if err != nil {
		return "", err
	}
	var resp neteaseQRKeyResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	if resp.Code != 200 {
		return "", fmt.Errorf("netease qr key failed: code=%d", resp.Code)
	}
	unikey := strings.TrimSpace(resp.Unikey)
	if unikey == "" {
		unikey = strings.TrimSpace(resp.Data.Unikey)
	}
	if unikey == "" {
		return "", fmt.Errorf("netease qr key missing unikey")
	}
	return unikey, nil
}

func (c *Client) CheckQRCode(ctx context.Context, key string) (*neteaseQRCheckResponse, error) {
	body, err := c.weapiRequest(ctx, "https://interface.music.163.com/weapi/login/qrcode/client/login?csrf_token=", map[string]any{"key": strings.TrimSpace(key), "type": 1})
	if err != nil {
		return nil, err
	}
	var resp neteaseQRCheckResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) weapiRequest(ctx context.Context, endpoint string, payload map[string]any) ([]byte, error) {
	body, _, err := c.weapiRequestDetailed(ctx, endpoint, payload, nil)
	return body, err
}

func (c *Client) weapiRequestDetailed(ctx context.Context, endpoint string, payload map[string]any, extraCookies map[string]string) ([]byte, []*http.Cookie, error) {
	if c == nil {
		return nil, nil, fmt.Errorf("netease client unavailable")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	form, err := buildNeteaseWeapiForm(string(payloadBytes))
	if err != nil {
		return nil, nil, err
	}
	client := c.baseData.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", chooseUserAgent())
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("Origin", "https://music.163.com")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cookie", strings.TrimSpace(c.renderCookieHeader(extraCookies)))
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	return body, resp.Cookies(), nil
}

func (c *Client) renderCookieHeader(extraCookies map[string]string) string {
	cookies := c.CookieMap()
	for key, value := range extraCookies {
		cookies[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if _, ok := cookies["os"]; !ok {
		cookies["os"] = "pc"
	}
	if _, ok := cookies["appver"]; !ok {
		cookies["appver"] = "8.9.70"
	}
	parts := make([]string, 0, len(cookies))
	for key, value := range cookies {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "; ")
}

func buildNeteaseQRUpdate(resp *neteaseQRCheckResponse) platform.QRLoginUpdate {
	if resp == nil {
		return platform.QRLoginUpdate{State: "failed", Message: "二维码轮询失败", Final: true}
	}
	update := platform.QRLoginUpdate{}
	switch resp.Code {
	case 800:
		update.State = "expired"
		update.Message = "网易云二维码已过期"
		update.Final = true
	case 801:
		update.State = "pending"
		update.Message = "网易云二维码等待扫码"
	case 802:
		update.State = "scanned"
		update.Message = "网易云二维码已扫码，等待确认"
		if strings.TrimSpace(resp.Nickname) != "" {
			update.Message += "\n昵称: " + strings.TrimSpace(resp.Nickname)
		}
	case 803:
		update.State = "success"
		update.Message = "网易云扫码登录成功"
		update.Final = true
	default:
		update.State = "pending"
		update.Message = firstNonEmptyNetease(resp.Message, fmt.Sprintf("二维码状态码: %d", resp.Code))
	}
	update.Caption = update.Message
	return update
}

func buildNeteaseWeapiForm(plain string) (url.Values, error) {
	secKey, err := randomNeteaseSecretKey(16)
	if err != nil {
		return nil, err
	}
	first, err := neteaseAESCBCEncrypt([]byte(plain), []byte(neteasePresetKey), []byte(neteaseAESIV))
	if err != nil {
		return nil, err
	}
	second, err := neteaseAESCBCEncrypt([]byte(first), []byte(secKey), []byte(neteaseAESIV))
	if err != nil {
		return nil, err
	}
	encSecKey, err := neteaseRSAEncrypt(secKey)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("params", second)
	values.Set("encSecKey", encSecKey)
	return values, nil
}

func neteaseAESCBCEncrypt(plain, key, iv []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padded := make([]byte, len(plain)+padding)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	encrypted := make([]byte, len(padded))
	mode := newNeteaseCBCEncrypter(block, iv)
	mode.CryptBlocks(encrypted, padded)
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

type neteaseCBCEncrypter struct {
	b         cipherBlock
	blockSize int
	iv        []byte
}

type cipherBlock interface {
	BlockSize() int
	Encrypt(dst, src []byte)
}

func newNeteaseCBCEncrypter(b cipherBlock, iv []byte) *neteaseCBCEncrypter {
	ivCopy := make([]byte, len(iv))
	copy(ivCopy, iv)
	return &neteaseCBCEncrypter{b: b, blockSize: b.BlockSize(), iv: ivCopy}
}

func (x *neteaseCBCEncrypter) CryptBlocks(dst, src []byte) {
	iv := x.iv
	bs := x.blockSize
	for len(src) > 0 {
		for i := 0; i < bs; i++ {
			dst[i] = src[i] ^ iv[i]
		}
		x.b.Encrypt(dst[:bs], dst[:bs])
		iv = dst[:bs]
		src = src[bs:]
		dst = dst[bs:]
	}
}

func randomNeteaseSecretKey(length int) (string, error) {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if length <= 0 {
		length = 16
	}
	buf := make([]byte, length)
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = chars[int(random[i])%len(chars)]
	}
	return string(buf), nil
}

func neteaseRSAEncrypt(secKey string) (string, error) {
	reversed := reverseBytes([]byte(secKey))
	m := new(big.Int).SetBytes(reversed)
	e := big.NewInt(65537)
	c := new(big.Int).Exp(m, e, neteaseRSAModulus)
	result := fmt.Sprintf("%0256x", c)
	return result, nil
}

func reverseBytes(src []byte) []byte {
	dst := make([]byte, len(src))
	for i := range src {
		dst[i] = src[len(src)-1-i]
	}
	return dst
}

func mustParseHexBigInt(value string) *big.Int {
	parsed, ok := new(big.Int).SetString(strings.TrimSpace(value), 16)
	if !ok {
		panic("invalid netease rsa modulus")
	}
	return parsed
}

func firstNonEmptyNetease(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

const (
	defaultNeteaseAutoRenewInterval = 6 * time.Hour
	neteaseAutoRenewRetryInterval   = time.Hour
)

func (c *Client) ConfigureAutoRenew(enabled bool, interval time.Duration) {
	c.autoRenewMu.Lock()
	defer c.autoRenewMu.Unlock()
	c.autoRenew.enabled = enabled
	if interval <= 0 {
		interval = defaultNeteaseAutoRenewInterval
	}
	c.autoRenew.interval = interval
}

func (c *Client) AutoRenewStatus() platform.AutoRenewStatus {
	c.autoRenewMu.RLock()
	defer c.autoRenewMu.RUnlock()
	interval := c.autoRenew.interval
	if interval <= 0 {
		interval = defaultNeteaseAutoRenewInterval
	}
	return platform.AutoRenewStatus{Enabled: c.autoRenew.enabled, Interval: interval}
}

func (c *Client) SetAutoRenew(enabled bool, interval time.Duration) (platform.AutoRenewStatus, error) {
	if interval <= 0 {
		interval = defaultNeteaseAutoRenewInterval
	}
	c.autoRenewMu.Lock()
	c.autoRenew.enabled = enabled
	c.autoRenew.interval = interval
	c.autoRenewMu.Unlock()
	if c.persistFunc != nil {
		if err := c.persistFunc(map[string]string{
			"auto_renew_enabled":      boolString(enabled),
			"auto_renew_interval_sec": fmt.Sprintf("%d", int(interval/time.Second)),
		}); err != nil {
			return platform.AutoRenewStatus{}, err
		}
	}
	if enabled {
		go func() {
			if _, err := c.ManualRenew(context.Background()); err != nil && c.logger != nil {
				c.logger.Debug("netease: immediate auto-renew failed", "err", err)
			}
		}()
	}
	return c.AutoRenewStatus(), nil
}

func (c *Client) StartAutoRenewDaemon(ctx context.Context) {
	c.autoRenewMu.Lock()
	if c.autoRenew.started {
		c.autoRenewMu.Unlock()
		return
	}
	c.autoRenew.started = true
	c.autoRenewMu.Unlock()
	go c.autoRenewLoop(ctx)
}

func (c *Client) autoRenewLoop(ctx context.Context) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		wait := c.runAutoRenewOnce(ctx)
		if wait <= 0 {
			wait = neteaseAutoRenewRetryInterval
		}
		timer.Reset(wait)
	}
}

func (c *Client) runAutoRenewOnce(ctx context.Context) time.Duration {
	status := c.AutoRenewStatus()
	if !status.Enabled {
		return defaultNeteaseAutoRenewInterval
	}
	if strings.TrimSpace(c.renderCookieHeader(nil)) == "" {
		c.logAutoRenewSkip("cookie empty")
		return neteaseAutoRenewRetryInterval
	}
	if _, ok := c.CookieMap()["MUSIC_U"]; !ok {
		c.logAutoRenewSkip("MUSIC_U missing")
		return neteaseAutoRenewRetryInterval
	}
	renewCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := c.ManualRenew(renewCtx); err != nil {
		c.logAutoRenewFailure(err)
		return neteaseAutoRenewRetryInterval
	}
	c.logAutoRenewSuccess()
	return status.Interval
}

func (c *Client) logAutoRenewSkip(reason string) {
	if c.logger != nil {
		c.logger.Debug("netease: auto-renew skipped", "reason", reason)
	}
}

func (c *Client) logAutoRenewFailure(err error) {
	if err == nil {
		return
	}
	errText := strings.TrimSpace(err.Error())
	c.autoRenewMu.Lock()
	c.autoRenew.consecutiveFail++
	count := c.autoRenew.consecutiveFail
	last := c.autoRenew.lastError
	c.autoRenew.lastError = errText
	c.autoRenewMu.Unlock()
	if c.logger == nil {
		return
	}
	if count == 1 || last != errText {
		c.logger.Warn("netease: auto-renew failed", "error", errText)
		return
	}
	c.logger.Debug("netease: auto-renew repeated failure", "error", errText, "count", count)
}

func (c *Client) logAutoRenewSuccess() {
	c.autoRenewMu.Lock()
	hadFailure := c.autoRenew.consecutiveFail > 0
	c.autoRenew.consecutiveFail = 0
	c.autoRenew.lastError = ""
	c.autoRenewMu.Unlock()
	if c.logger != nil && hadFailure {
		c.logger.Info("netease: auto-renew recovered")
	}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
