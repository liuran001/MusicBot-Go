package kugou

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

func BuildAdminCommands(client *Client) []admincmd.Command {
	if client == nil || client.Concept() == nil || !client.Concept().Enabled() {
		return nil
	}
	return []admincmd.Command{
		{
			Name:        "kgqr",
			Description: "生成酷狗概念版扫码二维码",
			RichHandler: func(ctx context.Context, args string) (*admincmd.Response, error) {
				_ = args
				data, err := client.Concept().CreateQRCode(ctx)
				if err != nil {
					return nil, err
				}
				parts := []string{"已生成酷狗概念版二维码"}
				if strings.TrimSpace(data.URL) != "" {
					parts = append(parts, "链接: "+data.URL)
				}
				resp := &admincmd.Response{Text: strings.Join(parts, "\n")}
				resp.ReplyMarkup = &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{{
					Text:         "取消登录",
					CallbackData: "admin kgqr cancel",
				}}}}
				if strings.HasPrefix(strings.TrimSpace(data.Base64), "data:image/png;base64,") {
					encoded := strings.TrimPrefix(strings.TrimSpace(data.Base64), "data:image/png;base64,")
					png, decodeErr := decodeBase64PNG(encoded)
					if decodeErr == nil && len(png) > 0 {
						resp.Photo = png
						resp.PhotoName = "kugou_concept_qr.png"
					}
				}
				resp.AfterSend = func(parent context.Context, b *telego.Bot, sent *telego.Message) {
					_ = parent
					if sent == nil || b == nil {
						return
					}
					manager := client.Concept()
					if manager == nil {
						return
					}
					pollCtx := context.Background()
					manager.StartQRCodePolling(pollCtx, time.Second, func(status conceptQRCheckData, err error) {
						if err != nil {
							return
						}
						text := buildQRStatusCaption(status)
						if status.Status == 4 {
							if _, _, statusErr := manager.FetchAccountStatus(context.Background()); statusErr == nil {
								text = manager.StatusSummary()
							}
						}
						edit := &telego.EditMessageCaptionParams{
							ChatID:    telego.ChatID{ID: sent.Chat.ID},
							MessageID: sent.MessageID,
							Caption:   text,
						}
						if status.Status == 4 || status.Status == 0 {
							edit.ReplyMarkup = &telego.InlineKeyboardMarkup{}
							manager.StopQRCodePolling()
						}
						editCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						_, _ = telegram.EditMessageCaptionWithBestEffort(editCtx, nil, b, edit)
					})
				}
				return resp, nil
			},
			CallbackPrefix: "admin kgqr ",
			CallbackHandler: func(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery) error {
				if strings.TrimSpace(query.Data) != "admin kgqr cancel" {
					return nil
				}
				client.Concept().StopQRCodePolling()
				if query.Message != nil {
					msg := query.Message.Message()
					if msg != nil {
						params := &telego.EditMessageReplyMarkupParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID, ReplyMarkup: &telego.InlineKeyboardMarkup{}}
						_, _ = telegram.EditMessageReplyMarkupWithRetry(ctx, nil, b, params)
					}
				}
				_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "已取消二维码轮询"})
				return nil
			},
		},
		{
			Name:        "kgqrstatus",
			Description: "检查酷狗概念版扫码状态",
			Handler: func(ctx context.Context, args string) (string, error) {
				_ = args
				data, err := client.Concept().CheckQRCode(ctx)
				if err != nil {
					return "", err
				}
				if data.Status == 4 {
					if _, _, statusErr := client.Concept().FetchAccountStatus(ctx); statusErr == nil {
						return client.Concept().StatusSummary(), nil
					}
				}
				return buildQRStatusCaption(data), nil
			},
		},
		{
			Name:        "kgstatus",
			Description: "查看酷狗概念版会话状态",
			Handler: func(ctx context.Context, args string) (string, error) {
				_ = ctx
				_ = args
				return client.Concept().StatusSummary(), nil
			},
		},
		{
			Name:        "kgrenew",
			Description: "手动续期酷狗概念版会话",
			Handler: func(ctx context.Context, args string) (string, error) {
				_ = args
				return client.Concept().ManualRenew(ctx)
			},
		},
		{
			Name:        "kgsign",
			Description: "尝试酷狗概念版签到/领 VIP",
			Handler: func(ctx context.Context, args string) (string, error) {
				_ = args
				return client.Concept().SignIn(ctx)
			},
		},
	}
}

func decodeBase64PNG(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(encoded)
}

func buildQRStatusCaption(data conceptQRCheckData) string {
	parts := []string{"酷狗概念版二维码轮询中", "二维码状态: " + describeQRStatus(data.Status)}
	if nickname := strings.TrimSpace(string(data.Nickname)); nickname != "" {
		parts = append(parts, "昵称: "+nickname)
	}
	if userID := strings.TrimSpace(string(data.UserID)); userID != "" {
		parts = append(parts, "用户ID: "+userID)
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
