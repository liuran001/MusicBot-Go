package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

func BuildAccountLoginCommand(manager platform.Manager) admincmd.Command {
	return admincmd.Command{
		Name:        "login",
		Description: "统一账号登录（qr/cookie/sign/check/renew/auto/help）",
		RichHandler: func(ctx context.Context, args string) (*admincmd.Response, error) {
			return handleAccountLogin(ctx, manager, args)
		},
		CallbackPrefix: "admin login ",
		CallbackHandler: func(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery) error {
			return handleAccountLoginCallback(ctx, manager, b, query)
		},
	}
}

func handleAccountLogin(ctx context.Context, manager platform.Manager, args string) (*admincmd.Response, error) {
	if manager == nil {
		return &admincmd.Response{Text: "平台管理器未初始化"}, nil
	}
	if text, ok, err := handleGlobalLoginActions(ctx, manager, args); ok {
		if err != nil {
			return nil, err
		}
		return &admincmd.Response{Text: text}, nil
	}
	platformName, action, payload, usage := parseLoginArgs(manager, args)
	if usage != "" {
		return &admincmd.Response{Text: usage}, nil
	}
	if platformName == "" {
		return &admincmd.Response{Text: loginUsage(manager)}, nil
	}
	plat := manager.Get(platformName)
	if plat == nil {
		return &admincmd.Response{Text: fmt.Sprintf("未识别的平台: %s", args)}, nil
	}
	switch action {
	case "", "help":
		return &admincmd.Response{Text: buildPlatformLoginHelp(manager, plat)}, nil
	case "sign":
		signer, ok := plat.(platform.SignInProvider)
		if !ok {
			return &admincmd.Response{Text: fmt.Sprintf("%s 当前不支持签到/VIP 领取", platformDisplayName(manager, platformName))}, nil
		}
		message, err := signer.SignIn(ctx)
		if err != nil {
			return nil, err
		}
		message = strings.TrimSpace(message)
		if message == "" {
			message = fmt.Sprintf("%s 签到/VIP 领取完成", platformDisplayName(manager, platformName))
		}
		return &admincmd.Response{Text: sanitizeSensitiveText(message)}, nil
	case "renew":
		message, err := renewCookieForPlatform(ctx, manager, platformName)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(message) == "" {
			message = fmt.Sprintf("%s 当前不支持续期", platformDisplayName(manager, platformName))
		}
		return &admincmd.Response{Text: message}, nil
	case "check":
		message, err := checkCookieForPlatform(ctx, manager, platformName)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(message) == "" {
			message = fmt.Sprintf("%s 当前不支持账号检查", platformDisplayName(manager, platformName))
		}
		return &admincmd.Response{Text: message}, nil
	case "auto":
		message, err := handlePlatformAutoRenew(ctx, manager, platformName, payload)
		if err != nil {
			return nil, err
		}
		return &admincmd.Response{Text: message}, nil
	case "cookie":
		importer, ok := plat.(platform.CookieImporter)
		if !ok {
			return &admincmd.Response{Text: fmt.Sprintf("%s 当前不支持 Cookie 导入", platformDisplayName(manager, platformName))}, nil
		}
		if strings.TrimSpace(payload) == "" {
			return &admincmd.Response{Text: fmt.Sprintf("用法: /login %s cookie <cookie内容>", platformName)}, nil
		}
		result, err := importer.ImportCookie(ctx, payload)
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(result.Message)
		if text == "" {
			text = "Cookie 导入完成"
		}
		return &admincmd.Response{Text: sanitizeSensitiveText(text)}, nil
	case "qr":
		provider, ok := plat.(platform.QRLoginProvider)
		if !ok {
			return &admincmd.Response{Text: fmt.Sprintf("%s 当前不支持扫码登录", platformDisplayName(manager, platformName))}, nil
		}
		session, err := provider.StartQRLogin(ctx)
		if err != nil {
			return nil, err
		}
		if session == nil {
			return &admincmd.Response{Text: "二维码会话创建失败"}, nil
		}
		resp := &admincmd.Response{Text: sanitizeSensitiveText(strings.TrimSpace(session.Caption))}
		if len(session.Image.PNG) > 0 {
			resp.Photo = session.Image.PNG
			resp.PhotoName = firstNonEmptyText(session.Image.FileName, "login_qr.png")
		} else if strings.HasPrefix(strings.TrimSpace(session.Image.Base64), "data:image/png;base64,") {
			encoded := strings.TrimPrefix(strings.TrimSpace(session.Image.Base64), "data:image/png;base64,")
			if png, decodeErr := base64.StdEncoding.DecodeString(encoded); decodeErr == nil && len(png) > 0 {
				resp.Photo = png
				resp.PhotoName = firstNonEmptyText(session.Image.FileName, "login_qr.png")
			}
		}
		if session.CancelID != "" {
			resp.ReplyMarkup = &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{{{
				Text:         "取消登录",
				CallbackData: "admin login cancel " + session.CancelID,
			}}}}
		}
		resp.AfterSend = func(parent context.Context, b *telego.Bot, sent *telego.Message) {
			_ = parent
			if session.Poll == nil || b == nil || sent == nil {
				return
			}
			timeout := session.Timeout
			if timeout <= 0 {
				timeout = 2 * time.Minute
			}
			pollCtx, pollCancel := context.WithTimeout(context.Background(), timeout)
			defer pollCancel()
			session.Poll(pollCtx, func(update platform.QRLoginUpdate, err error) {
				if err != nil {
					if err == context.DeadlineExceeded {
						editQRMessageCaption(b, sent, "二维码已超时，请重新执行 /login "+platformName+" qr", true)
					}
					return
				}
				caption := strings.TrimSpace(update.Caption)
				if caption == "" {
					caption = strings.TrimSpace(update.Message)
				}
				caption = sanitizeSensitiveText(caption)
				if caption == "" {
					return
				}
				editQRMessageCaption(b, sent, caption, update.Final)
			})
		}
		return resp, nil
	default:
		return &admincmd.Response{Text: buildPlatformLoginHelp(manager, plat)}, nil
	}
}

func handleGlobalLoginActions(ctx context.Context, manager platform.Manager, args string) (string, bool, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return "", false, nil
	}
	action := strings.ToLower(strings.TrimSpace(fields[0]))
	switch action {
	case "sign":
		if len(fields) == 2 {
			platformName := resolveCookiePlatform(manager, fields[1])
			if platformName == "" {
				return "", false, nil
			}
			plat := manager.Get(platformName)
			if plat == nil {
				return "", false, nil
			}
			signer, ok := plat.(platform.SignInProvider)
			if !ok {
				return fmt.Sprintf("%s 当前不支持签到/VIP 领取", platformDisplayName(manager, platformName)), true, nil
			}
			text, err := signer.SignIn(ctx)
			if err != nil {
				return "", true, err
			}
			text = strings.TrimSpace(text)
			if text == "" {
				text = fmt.Sprintf("%s 签到/VIP 领取完成", platformDisplayName(manager, platformName))
			}
			return sanitizeSensitiveText(text), true, nil
		}
		if len(fields) != 1 {
			return "用法: /login sign <platform>", true, nil
		}
	case "check":
		if len(fields) == 1 {
			text, err := checkCookies(ctx, manager, "")
			return text, true, err
		}
	case "renew":
		if len(fields) == 1 {
			text, err := renewCookies(ctx, manager, "")
			return text, true, err
		}
	case "auto":
		text, err := handleAllPlatformAutoRenew(ctx, manager, strings.Join(fields[1:], " "))
		return text, true, err
	}
	return "", false, nil
}

func handleAccountLoginCallback(ctx context.Context, manager platform.Manager, b *telego.Bot, query *telego.CallbackQuery) error {
	if query == nil || manager == nil {
		return nil
	}
	data := strings.TrimSpace(query.Data)
	if !strings.HasPrefix(data, "admin login cancel ") {
		return nil
	}
	cancelID := strings.TrimSpace(strings.TrimPrefix(data, "admin login cancel "))
	if cancelID == "" {
		return nil
	}
	for _, name := range manager.List() {
		plat := manager.Get(name)
		provider, ok := plat.(platform.QRLoginProvider)
		if !ok {
			continue
		}
		if err := provider.CancelQRLogin(ctx, cancelID); err == nil {
			break
		}
	}
	if query.Message != nil {
		msg := query.Message.Message()
		if msg != nil {
			params := &telego.EditMessageReplyMarkupParams{ChatID: telego.ChatID{ID: msg.Chat.ID}, MessageID: msg.MessageID, ReplyMarkup: &telego.InlineKeyboardMarkup{}}
			_, _ = telegram.EditMessageReplyMarkupWithRetry(ctx, nil, b, params)
		}
	}
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: "已取消登录轮询"})
	return nil
}

func parseLoginArgs(manager platform.Manager, args string) (platformName, action, payload, usage string) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return "", "", "", ""
	}
	if len(fields) == 1 {
		single := strings.ToLower(strings.TrimSpace(fields[0]))
		switch single {
		case "help", "-h", "--help":
			return "", "", "", ""
		}
	}
	platformName = resolveCookiePlatform(manager, fields[0])
	if platformName == "" {
		return "", "", "", fmt.Sprintf("未识别的平台: %s\n\n%s", fields[0], loginUsage(manager))
	}
	if len(fields) >= 2 {
		action = strings.ToLower(strings.TrimSpace(fields[1]))
	}
	if len(fields) >= 3 {
		payload = strings.TrimSpace(strings.Join(fields[2:], " "))
	}
	return platformName, action, payload, ""
}

func loginUsage(manager platform.Manager) string {
	platforms := manager.ListMeta()
	aliases := make([]string, 0, len(platforms))
	for _, meta := range platforms {
		if strings.TrimSpace(meta.Name) == "" {
			continue
		}
		aliases = append(aliases, meta.Name)
	}
	sort.Strings(aliases)
	joined := strings.Join(aliases, ", ")
	if joined == "" {
		joined = "kugou, bilibili, netease, qqmusic"
	}
	return "统一账号命令:\n" +
		"/status - 查看统计与账号状态\n" +
		"/login <platform> help - 查看平台支持的登录方式\n" +
		"/login <platform> qr - 扫码登录（如支持）\n" +
		"/login <platform> cookie <cookie> - 导入 Cookie\n" +
		"/login <platform> sign - 签到/VIP 领取（如支持）\n" +
		"/login sign <platform> - 按平台执行签到/VIP 领取\n" +
		"/login <platform> check - 检查平台账号状态\n" +
		"/login check - 检查所有平台账号状态\n" +
		"/login <platform> renew - 手动续期\n" +
		"/login renew - 批量续期所有平台\n" +
		"/login <platform> auto on|off|status [intervalSec] - 自动续期\n" +
		"/login auto on|off|status [intervalSec] - 批量设置所有平台自动续期\n" +
		"支持平台: " + joined
}

func buildPlatformLoginHelp(manager platform.Manager, plat platform.Platform) string {
	if plat == nil {
		return loginUsage(manager)
	}
	name := plat.Name()
	methods := make([]string, 0, 4)
	if provider, ok := plat.(platform.LoginMethodProvider); ok {
		methods = append(methods, provider.SupportedLoginMethods()...)
	}
	if len(methods) == 0 {
		if _, ok := plat.(platform.QRLoginProvider); ok {
			methods = append(methods, "qr")
		}
		if _, ok := plat.(platform.CookieImporter); ok {
			methods = append(methods, "cookie")
		}
		if _, ok := plat.(platform.CookieRenewer); ok {
			methods = append(methods, "renew")
		}
	}
	if len(methods) == 0 {
		methods = append(methods, "status")
	}
	examples := []string{"/status"}
	if containsLoginMethod(methods, "qr") {
		examples = append(examples, fmt.Sprintf("/login %s qr", name))
	}
	if containsLoginMethod(methods, "cookie") {
		examples = append(examples, fmt.Sprintf("/login %s cookie <cookie>", name))
	}
	if containsLoginMethod(methods, "check") || implementsAccountCheck(plat) {
		examples = append(examples, fmt.Sprintf("/login %s check", name), "/login check")
	}
	if containsLoginMethod(methods, "sign") {
		examples = append(examples, fmt.Sprintf("/login %s sign", name))
		examples = append(examples, fmt.Sprintf("/login sign %s", name))
	}
	if containsLoginMethod(methods, "renew") || implementsRenew(plat) {
		examples = append(examples, fmt.Sprintf("/login %s renew", name), "/login renew")
	}
	if containsLoginMethod(methods, "auto") || implementsAutoRenew(plat) {
		examples = append(examples, fmt.Sprintf("/login %s auto on 21600", name), fmt.Sprintf("/login %s auto status", name), "/login auto status")
	}
	return fmt.Sprintf("%s 支持: %s\n\n示例:\n%s",
		platformDisplayName(manager, name), strings.Join(methods, ", "), strings.Join(examples, "\n"))
}

func containsLoginMethod(methods []string, target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	for _, method := range methods {
		if strings.TrimSpace(strings.ToLower(method)) == target {
			return true
		}
	}
	return false
}

func implementsAccountCheck(plat platform.Platform) bool {
	_, ok := plat.(platform.CookieChecker)
	return ok
}

func implementsRenew(plat platform.Platform) bool {
	_, ok := plat.(platform.CookieRenewer)
	return ok
}

func implementsAutoRenew(plat platform.Platform) bool {
	_, ok := plat.(platform.AutoRenewer)
	return ok
}

func handlePlatformAutoRenew(ctx context.Context, manager platform.Manager, platformName, payload string) (string, error) {
	plat := manager.Get(platformName)
	autoRenewer, ok := plat.(platform.AutoRenewer)
	if !ok {
		return fmt.Sprintf("%s 当前不支持自动续期", platformDisplayName(manager, platformName)), nil
	}
	fields := strings.Fields(strings.TrimSpace(payload))
	if len(fields) == 0 || strings.EqualFold(fields[0], "status") {
		status, err := autoRenewer.GetAutoRenewStatus(ctx)
		if err != nil {
			return "", err
		}
		return formatAutoRenewStatus(manager, platformName, status), nil
	}
	sub := strings.ToLower(strings.TrimSpace(fields[0]))
	switch sub {
	case "on":
		interval := time.Duration(0)
		if len(fields) >= 2 {
			sec, err := parsePositiveSeconds(fields[1])
			if err != nil {
				return "", err
			}
			interval = time.Duration(sec) * time.Second
		}
		status, err := autoRenewer.SetAutoRenew(ctx, true, interval)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已开启 %s 自动续期，间隔 %d 秒", platformDisplayName(manager, platformName), int(status.Interval/time.Second)), nil
	case "off":
		status, err := autoRenewer.SetAutoRenew(ctx, false, 0)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已关闭 %s 自动续期（当前间隔 %d 秒）", platformDisplayName(manager, platformName), int(status.Interval/time.Second)), nil
	default:
		return fmt.Sprintf("用法: /login %s auto on [intervalSec]\n/login %s auto off\n/login %s auto status", platformName, platformName, platformName), nil
	}
}

func handleAllPlatformAutoRenew(ctx context.Context, manager platform.Manager, payload string) (string, error) {
	if manager == nil {
		return "平台管理器未初始化", nil
	}
	fields := strings.Fields(strings.TrimSpace(payload))
	if len(fields) == 0 {
		fields = []string{"status"}
	}
	sub := strings.ToLower(strings.TrimSpace(fields[0]))
	platforms := manager.List()
	if len(platforms) == 0 {
		return "没有可用的平台", nil
	}
	sort.Strings(platforms)
	lines := make([]string, 0, len(platforms)+1)
	failures := 0
	for _, name := range platforms {
		plat := manager.Get(name)
		autoRenewer, ok := plat.(platform.AutoRenewer)
		if !ok {
			continue
		}
		switch sub {
		case "status":
			status, err := autoRenewer.GetAutoRenewStatus(ctx)
			if err != nil {
				failures++
				lines = append(lines, fmt.Sprintf("❌ %s: %s", platformDisplayName(manager, name), sanitizeSensitiveText(err.Error())))
				continue
			}
			lines = append(lines, formatAutoRenewStatus(manager, name, status))
		case "on":
			interval := time.Duration(0)
			if len(fields) >= 2 {
				sec, err := parsePositiveSeconds(fields[1])
				if err != nil {
					return "", err
				}
				interval = time.Duration(sec) * time.Second
			}
			status, err := autoRenewer.SetAutoRenew(ctx, true, interval)
			if err != nil {
				failures++
				lines = append(lines, fmt.Sprintf("❌ %s: %s", platformDisplayName(manager, name), sanitizeSensitiveText(err.Error())))
				continue
			}
			lines = append(lines, fmt.Sprintf("✅ %s: 自动续期已开启，间隔 %d 秒", platformDisplayName(manager, name), int(status.Interval/time.Second)))
		case "off":
			status, err := autoRenewer.SetAutoRenew(ctx, false, 0)
			if err != nil {
				failures++
				lines = append(lines, fmt.Sprintf("❌ %s: %s", platformDisplayName(manager, name), sanitizeSensitiveText(err.Error())))
				continue
			}
			lines = append(lines, fmt.Sprintf("✅ %s: 自动续期已关闭（当前间隔 %d 秒）", platformDisplayName(manager, name), int(status.Interval/time.Second)))
		default:
			return "用法: /login auto on [intervalSec]\n/login auto off\n/login auto status", nil
		}
	}
	if len(lines) == 0 {
		return "没有支持自动续期的平台", nil
	}
	if failures > 0 {
		lines = append(lines, fmt.Sprintf("\n完成（失败 %d 个平台）", failures))
	}
	return strings.Join(lines, "\n"), nil
}

func formatAutoRenewStatus(manager platform.Manager, platformName string, status platform.AutoRenewStatus) string {
	state := "关闭"
	if status.Enabled {
		state = "开启"
	}
	return fmt.Sprintf("%s 自动续期: %s，间隔 %d 秒", platformDisplayName(manager, platformName), state, int(status.Interval/time.Second))
}

func parsePositiveSeconds(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("intervalSec 不能为空")
	}
	sec, err := strconv.Atoi(value)
	if err != nil || sec <= 0 {
		return 0, fmt.Errorf("intervalSec 必须为正整数秒")
	}
	return sec, nil
}

func editQRMessageCaption(b *telego.Bot, sent *telego.Message, caption string, final bool) {
	if b == nil || sent == nil || strings.TrimSpace(caption) == "" {
		return
	}
	params := &telego.EditMessageCaptionParams{
		ChatID:    telego.ChatID{ID: sent.Chat.ID},
		MessageID: sent.MessageID,
		Caption:   caption,
	}
	if final {
		params.ReplyMarkup = &telego.InlineKeyboardMarkup{}
	}
	editCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = telegram.EditMessageCaptionWithBestEffort(editCtx, nil, b, params)
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
