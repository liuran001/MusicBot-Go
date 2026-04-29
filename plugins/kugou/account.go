package kugou

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const kugouQRLoginCancelID = "kugou"

func (k *KugouPlatform) AccountStatus(ctx context.Context) (platform.AccountStatus, error) {
	_ = ctx
	status := platform.AccountStatus{
		Platform:      k.Name(),
		DisplayName:   k.Metadata().DisplayName,
		AuthMode:      "qr",
		SessionSource: "concept",
	}
	if k == nil || k.client == nil || k.client.Concept() == nil {
		status.Summary = "- 状态: 插件未初始化"
		return status, nil
	}
	manager := k.client.Concept()
	state := manager.Snapshot()
	status.LoggedIn = manager.HasUsableSession()
	status.UserID = strings.TrimSpace(state.UserID)
	status.Nickname = strings.TrimSpace(state.Nickname)
	status.CanCheckCookie = true
	status.CanRenewCookie = true
	status.SupportedLogins = k.SupportedLoginMethods()
	status.Summary = manager.StatusSummaryForChat(true)
	return status, nil
}

func (k *KugouPlatform) SupportedLoginMethods() []string {
	return []string{"qr", "sign", "renew", "auto", "status"}
}

func (k *KugouPlatform) SignIn(ctx context.Context) (string, error) {
	if k == nil || k.client == nil || k.client.Concept() == nil {
		return "", fmt.Errorf("kugou concept session unavailable")
	}
	return k.client.Concept().SignIn(ctx)
}

func (k *KugouPlatform) StartQRLogin(ctx context.Context) (*platform.QRLoginSession, error) {
	if k == nil || k.client == nil || k.client.Concept() == nil {
		return nil, fmt.Errorf("kugou concept session unavailable")
	}
	manager := k.client.Concept()
	data, err := manager.CreateQRCode(ctx)
	if err != nil {
		return nil, err
	}
	session := &platform.QRLoginSession{
		Platform: k.Name(),
		Image: platform.QRLoginImage{
			URL:      strings.TrimSpace(data.URL),
			Base64:   strings.TrimSpace(data.Base64),
			FileName: "kugou_concept_qr.png",
		},
		CancelID: kugouQRLoginCancelID,
		Caption:  buildKugouQRStartCaption(data),
		Timeout:  2 * time.Minute,
		Cancel:   manager.StopQRCodePolling,
	}
	if strings.HasPrefix(session.Image.Base64, "data:image/png;base64,") {
		encoded := strings.TrimPrefix(session.Image.Base64, "data:image/png;base64,")
		if png, decodeErr := base64.StdEncoding.DecodeString(encoded); decodeErr == nil && len(png) > 0 {
			session.Image.PNG = png
		}
	}
	session.Poll = func(ctx context.Context, onUpdate func(platform.QRLoginUpdate, error)) {
		lastState := "pending"
		skipInitialPending := true
		manager.StartQRCodePolling(ctx, time.Second, func(status conceptQRCheckData, err error) {
			if onUpdate == nil {
				return
			}
			if err != nil {
				if err == context.Canceled {
					onUpdate(platform.QRLoginUpdate{State: "cancelled", Message: "已取消酷狗二维码登录", Final: true, Caption: "已取消酷狗二维码登录"}, nil)
					return
				}
				onUpdate(platform.QRLoginUpdate{}, err)
				return
			}
			caption := buildQRStatusCaption(status, true)
			update := platform.QRLoginUpdate{
				State:   kugouQRUpdateState(status.Status),
				Message: caption,
				Caption: caption,
				Final:   status.Status == 4 || status.Status == 0,
			}
			if status.Status == 4 {
				if accountStatus, statusErr := k.AccountStatus(context.Background()); statusErr == nil {
					accountStatus.Summary = manager.StatusSummaryForChat(true)
					update.Status = &accountStatus
					update.Caption = accountStatus.Summary
				}
			}
			if skipInitialPending && update.State == "pending" && !update.Final {
				skipInitialPending = false
				lastState = update.State
				return
			}
			skipInitialPending = false
			if update.State == lastState && !update.Final {
				return
			}
			lastState = update.State
			onUpdate(update, nil)
		})
	}
	return session, nil
}

func (k *KugouPlatform) CancelQRLogin(ctx context.Context, cancelID string) error {
	_ = ctx
	if k == nil || k.client == nil || k.client.Concept() == nil {
		return fmt.Errorf("kugou concept session unavailable")
	}
	if strings.TrimSpace(cancelID) != kugouQRLoginCancelID {
		return fmt.Errorf("qr login session not found")
	}
	k.client.Concept().StopQRCodePolling()
	return nil
}

func buildKugouQRStartCaption(data conceptQRCreateData) string {
	parts := []string{"已生成酷狗概念版二维码"}
	if strings.TrimSpace(data.URL) != "" {
		parts = append(parts, "链接: "+strings.TrimSpace(data.URL))
	}
	return strings.Join(parts, "\n")
}

func buildQRStatusCaption(data conceptQRCheckData, maskSensitive bool) string {
	parts := []string{"酷狗概念版二维码轮询中", "二维码状态: " + describeQRStatus(data.Status)}
	if nickname := strings.TrimSpace(string(data.Nickname)); nickname != "" {
		parts = append(parts, "昵称: "+nickname)
	}
	if userID := strings.TrimSpace(string(data.UserID)); userID != "" {
		parts = append(parts, "用户ID: "+maskConceptValue(userID, maskSensitive))
	}
	if data.Status == 2 {
		parts = append(parts, "已扫码，等待确认")
	}
	if data.Status == 4 {
		parts = append(parts, "扫码登录成功")
	}
	if data.Status == 0 {
		parts = append(parts, "二维码已过期")
	}
	return strings.Join(parts, "\n")
}

func kugouQRUpdateState(status int) string {
	switch status {
	case 0:
		return "expired"
	case 1:
		return "pending"
	case 2:
		return "scanned"
	case 4:
		return "success"
	default:
		return "pending"
	}
}
