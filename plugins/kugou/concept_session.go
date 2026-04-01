package kugou

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/guohuiyuan/music-lib/model"
)

type ConceptSessionManager struct {
	mu     sync.RWMutex
	logger interface {
		Info(string, ...interface{})
		Warn(string, ...interface{})
		Error(string, ...interface{})
		Debug(string, ...interface{})
	}
	persistFunc func(map[string]string) error
	client      *ConceptAPIClient
	state       conceptSession
	pollCancel  context.CancelFunc
}

func NewConceptSessionManager(logger interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
	Error(string, ...interface{})
	Debug(string, ...interface{})
}, persist func(map[string]string) error, initial conceptSession) *ConceptSessionManager {
	mgr := &ConceptSessionManager{logger: logger, persistFunc: persist, state: initial}
	mgr.client = NewConceptAPIClient("", mgr)
	return mgr
}

func (m *ConceptSessionManager) API() *ConceptAPIClient {
	if m == nil {
		return nil
	}
	return m.client
}

func (m *ConceptSessionManager) SetBaseURL(baseURL string) {
	_ = baseURL
}

func (m *ConceptSessionManager) SetHTTPClient(client *http.Client) {
	if m == nil || m.client == nil {
		return
	}
	m.client.SetHTTPClient(client)
}

func (m *ConceptSessionManager) Enabled() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.Enabled
}

func (m *ConceptSessionManager) HasUsableSession() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return strings.TrimSpace(m.state.Token) != "" && strings.TrimSpace(m.state.UserID) != ""
}

func (m *ConceptSessionManager) CookieString() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return strings.TrimSpace(m.state.Cookie)
}

func (m *ConceptSessionManager) Snapshot() conceptSession {
	if m == nil {
		return conceptSession{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *ConceptSessionManager) Update(mutator func(*conceptSession)) conceptSession {
	if m == nil {
		return conceptSession{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mutator(&m.state)
	return m.state
}

func (m *ConceptSessionManager) Replace(state conceptSession) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
}

func (m *ConceptSessionManager) StartQRCodePolling(ctx context.Context, interval time.Duration, onUpdate func(conceptQRCheckData, error)) {
	if m == nil || m.client == nil {
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	m.StopQRCodePolling()
	pollCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.pollCancel = cancel
	m.mu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-pollCtx.Done():
				if onUpdate != nil && errors.Is(pollCtx.Err(), context.DeadlineExceeded) {
					onUpdate(conceptQRCheckData{}, pollCtx.Err())
				}
				return
			default:
			}
			data, err := m.client.CheckQRCode(pollCtx)
			if onUpdate != nil {
				onUpdate(data, err)
			}
			if err == nil && (data.Status == 0 || data.Status == 4) {
				m.StopQRCodePolling()
				return
			}
			select {
			case <-pollCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (m *ConceptSessionManager) StopQRCodePolling() {
	if m == nil {
		return
	}
	m.mu.Lock()
	cancel := m.pollCancel
	m.pollCancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *ConceptSessionManager) Persist() error {
	if m == nil || m.persistFunc == nil {
		return nil
	}
	state := m.Snapshot()
	pairs := map[string]string{
		"concept_auto_refresh_enabled":      boolString(state.AutoRefresh),
		"concept_auto_refresh_interval_sec": fmt.Sprintf("%d", int(state.AutoRefreshPeriod/time.Second)),
		"concept_cookie":                    state.Cookie,
		"concept_token":                     state.Token,
		"concept_user_id":                   state.UserID,
		"concept_t1":                        state.T1,
		"concept_vip_type":                  state.VIPType,
		"concept_vip_token":                 state.VIPToken,
		"concept_nickname":                  state.Nickname,
		"concept_qr_key":                    state.QRKey,
		"concept_qr_url":                    state.QRURL,
		"concept_session_source":            state.SessionSource,
		"concept_vip_expire_time":           state.VIPExpireTime,
		"concept_last_check_time":           formatConceptTime(state.LastCheckTime),
		"concept_last_refresh_time":         formatConceptTime(state.LastRefreshTime),
		"concept_last_sign_time":            formatConceptTime(state.LastSignTime),
		"concept_last_vip_claim_time":       formatConceptTime(state.LastVIPClaimTime),
		"concept_login_time":                formatConceptTime(state.LoginTime),
		"concept_dfid":                      state.Device.Dfid,
		"concept_mid":                       state.Device.Mid,
		"concept_guid":                      state.Device.Guid,
		"concept_dev":                       state.Device.Dev,
		"concept_mac":                       state.Device.Mac,
	}
	return m.persistFunc(pairs)
}

func (m *ConceptSessionManager) StatusSummary() string {
	return m.StatusSummaryForChat(false)
}

func (m *ConceptSessionManager) StatusSummaryForChat(maskSensitive bool) string {
	state := m.Snapshot()
	lines := []string{"酷狗概念版状态"}
	if m.HasUsableSession() {
		lines = append(lines, "- 会话: 可用")
	} else {
		lines = append(lines, "- 会话: 未登录")
	}
	if strings.TrimSpace(state.Nickname) != "" {
		lines = append(lines, "- 昵称: "+state.Nickname)
	}
	if strings.TrimSpace(state.UserID) != "" {
		lines = append(lines, "- 用户ID: "+maskConceptValue(state.UserID, maskSensitive))
	}
	if strings.TrimSpace(state.Token) != "" {
		if maskSensitive {
			lines = append(lines, "- Token: 已存在")
		} else {
			lines = append(lines, "- Token: "+state.Token)
		}
	}
	if strings.TrimSpace(state.T1) != "" {
		if maskSensitive {
			lines = append(lines, "- T1: 已存在")
		} else {
			lines = append(lines, "- T1: "+state.T1)
		}
	}
	if strings.TrimSpace(state.VIPType) != "" {
		lines = append(lines, "- VIP类型: "+state.VIPType)
	}
	if strings.TrimSpace(state.VIPExpireTime) != "" {
		lines = append(lines, "- VIP到期: "+state.VIPExpireTime)
	}
	if state.QRStatus > 0 {
		lines = append(lines, "- 二维码状态: "+describeQRStatus(state.QRStatus))
	}
	if strings.TrimSpace(state.SessionSource) != "" {
		lines = append(lines, "- 来源: "+state.SessionSource)
	}
	if strings.TrimSpace(state.Device.Dfid) != "" || strings.TrimSpace(state.Device.Mid) != "" || strings.TrimSpace(state.Device.Dev) != "" {
		lines = append(lines, "- 设备:")
		if strings.TrimSpace(state.Device.Dfid) != "" {
			lines = append(lines, "  • DFID: "+maskConceptValue(state.Device.Dfid, maskSensitive))
		}
		if strings.TrimSpace(state.Device.Mid) != "" {
			lines = append(lines, "  • MID: "+maskConceptValue(state.Device.Mid, maskSensitive))
		}
		if strings.TrimSpace(state.Device.Dev) != "" {
			lines = append(lines, "  • DEV: "+maskConceptValue(state.Device.Dev, maskSensitive))
		}
	}
	if !state.LastCheckTime.IsZero() || !state.LastRefreshTime.IsZero() || !state.LastSignTime.IsZero() {
		lines = append(lines, "- 时间:")
		if !state.LastCheckTime.IsZero() {
			lines = append(lines, "  • 上次检查: "+state.LastCheckTime.Format(time.RFC3339))
		}
		if !state.LastRefreshTime.IsZero() {
			lines = append(lines, "  • 上次续期: "+state.LastRefreshTime.Format(time.RFC3339))
		}
		if !state.LastSignTime.IsZero() {
			lines = append(lines, "  • 上次签到: "+state.LastSignTime.Format(time.RFC3339))
		}
	}
	return strings.Join(lines, "\n")
}

func maskConceptValue(value string, masked bool) string {
	value = strings.TrimSpace(value)
	if !masked || value == "" {
		return value
	}
	runes := []rune(value)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	keep := 3
	if len(runes) > 10 {
		keep = 4
	}
	return string(runes[:keep]) + strings.Repeat("*", len(runes)-keep*2) + string(runes[len(runes)-keep:])
}

func describeQRStatus(status int) string {
	switch status {
	case 0:
		return "已过期"
	case 1:
		return "等待扫码"
	case 2:
		return "已扫码，待确认"
	case 4:
		return "登录成功"
	default:
		return fmt.Sprintf("%d", status)
	}
}

func (m *ConceptSessionManager) FetchSongURLNew(ctx context.Context, song *model.Song, plan kugouDownloadPlan) (*conceptSongURLNewResponse, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("concept session unavailable")
	}
	return m.client.FetchSongURLNew(ctx, song, plan)
}

func (m *ConceptSessionManager) FetchAccountStatus(ctx context.Context) (*conceptUserDetailData, *conceptVIPDetailData, error) {
	if m == nil || m.client == nil {
		return nil, nil, fmt.Errorf("concept session unavailable")
	}
	return m.client.FetchAccountStatus(ctx)
}

func (m *ConceptSessionManager) ManualRenew(ctx context.Context) (string, error) {
	if m == nil || m.client == nil {
		return "", fmt.Errorf("concept session unavailable")
	}
	return m.client.ManualRenew(ctx)
}

func (m *ConceptSessionManager) CreateQRCode(ctx context.Context) (conceptQRCreateData, error) {
	if m == nil || m.client == nil {
		return conceptQRCreateData{}, fmt.Errorf("concept session unavailable")
	}
	return m.client.CreateQRCode(ctx)
}

func (m *ConceptSessionManager) CheckQRCode(ctx context.Context) (conceptQRCheckData, error) {
	if m == nil || m.client == nil {
		return conceptQRCheckData{}, fmt.Errorf("concept session unavailable")
	}
	return m.client.CheckQRCode(ctx)
}

func (m *ConceptSessionManager) SignIn(ctx context.Context) (string, error) {
	if m == nil || m.client == nil {
		return "", fmt.Errorf("concept session unavailable")
	}
	return m.client.SignIn(ctx)
}

func (m *ConceptSessionManager) FetchSongURL(ctx context.Context, song *model.Song, plan kugouDownloadPlan) (*conceptSongURLResponse, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("concept session unavailable")
	}
	return m.client.FetchSongURL(ctx, song, plan)
}

func loadConceptSessionFromConfig(getString func(string, string) string, getBool func(string, string) bool, getInt func(string, string) int) conceptSession {
	state := conceptSession{
		Enabled:           true,
		AutoRefresh:       getBool("kugou", "concept_auto_refresh_enabled"),
		AutoRefreshPeriod: time.Duration(getInt("kugou", "concept_auto_refresh_interval_sec")) * time.Second,
		Token:             strings.TrimSpace(getString("kugou", "concept_token")),
		UserID:            strings.TrimSpace(getString("kugou", "concept_user_id")),
		T1:                strings.TrimSpace(getString("kugou", "concept_t1")),
		VIPType:           strings.TrimSpace(getString("kugou", "concept_vip_type")),
		VIPToken:          strings.TrimSpace(getString("kugou", "concept_vip_token")),
		Nickname:          strings.TrimSpace(getString("kugou", "concept_nickname")),
		Cookie:            strings.TrimSpace(getString("kugou", "concept_cookie")),
		QRKey:             strings.TrimSpace(getString("kugou", "concept_qr_key")),
		QRURL:             strings.TrimSpace(getString("kugou", "concept_qr_url")),
		SessionSource:     strings.TrimSpace(getString("kugou", "concept_session_source")),
		VIPExpireTime:     strings.TrimSpace(getString("kugou", "concept_vip_expire_time")),
		Device: conceptDeviceInfo{
			Dfid: strings.TrimSpace(getString("kugou", "concept_dfid")),
			Mid:  strings.TrimSpace(getString("kugou", "concept_mid")),
			Guid: strings.TrimSpace(getString("kugou", "concept_guid")),
			Dev:  strings.TrimSpace(getString("kugou", "concept_dev")),
			Mac:  strings.TrimSpace(getString("kugou", "concept_mac")),
		},
	}
	state.LastCheckTime = parseConceptTime(getString("kugou", "concept_last_check_time"))
	state.LastRefreshTime = parseConceptTime(getString("kugou", "concept_last_refresh_time"))
	state.LastSignTime = parseConceptTime(getString("kugou", "concept_last_sign_time"))
	state.LastVIPClaimTime = parseConceptTime(getString("kugou", "concept_last_vip_claim_time"))
	state.LoginTime = parseConceptTime(getString("kugou", "concept_login_time"))
	if state.AutoRefreshPeriod <= 0 {
		state.AutoRefreshPeriod = 6 * time.Hour
	}
	return state
}

func formatConceptTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseConceptTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
