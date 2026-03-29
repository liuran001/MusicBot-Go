package handler

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

type AdminCommandHandler struct {
	BotName     string
	AdminIDs    map[int64]struct{}
	RateLimiter *telegram.RateLimiter
	Commands    []admincmd.Command
}

var sensitiveKVPattern = regexp.MustCompile(`(?i)\b(cookie|sessdata|music_u|refresh_token|ac_time_value|psrf_qqaccess_token|qqmusic_key|auth_token|access_token)\b\s*[:=]\s*([^\s;,\n]+)`)

func (h *AdminCommandHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil {
		return
	}
	if update.CallbackQuery != nil {
		h.handleCallback(ctx, b, update.CallbackQuery)
		return
	}
	if update.Message == nil || update.Message.From == nil {
		return
	}
	message := update.Message
	cmd := commandName(message.Text, h.BotName)
	if cmd == "" {
		return
	}
	command, ok := h.commandByName(cmd)
	if !ok {
		return
	}
	if !isBotAdmin(h.AdminIDs, message.From.ID) {
		return
	}
	if command.Handler == nil {
		if command.RichHandler == nil {
			h.sendText(ctx, b, message.Chat.ID, message.MessageID, "命令不可用")
			return
		}
	}
	args := commandArguments(message.Text)
	if command.RichHandler != nil {
		resp, err := command.RichHandler(ctx, args)
		if err != nil {
			h.sendText(ctx, b, message.Chat.ID, message.MessageID, fmt.Sprintf("执行失败: %v", err))
			return
		}
		h.sendResponse(ctx, b, message.Chat.ID, message.MessageID, resp)
		return
	}
	result, err := command.Handler(ctx, args)
	if err != nil {
		h.sendText(ctx, b, message.Chat.ID, message.MessageID, fmt.Sprintf("执行失败: %v", err))
		return
	}
	result = strings.TrimSpace(result)
	if result == "" {
		result = "执行完成"
	}
	result = sanitizeSensitiveText(result)
	h.sendText(ctx, b, message.Chat.ID, message.MessageID, result)
}

func (h *AdminCommandHandler) handleCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery) {
	if h == nil || query == nil {
		return
	}
	if !isBotAdmin(h.AdminIDs, query.From.ID) {
		return
	}
	for _, cmd := range h.Commands {
		prefix := strings.TrimSpace(cmd.CallbackPrefix)
		if prefix == "" || cmd.CallbackHandler == nil {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(query.Data), prefix) {
			continue
		}
		if err := cmd.CallbackHandler(ctx, b, query); err != nil {
			_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: query.ID, Text: sanitizeSensitiveText(err.Error()), ShowAlert: true})
		}
		return
	}
}

func (h *AdminCommandHandler) commandByName(name string) (admincmd.Command, bool) {
	for _, cmd := range h.Commands {
		if strings.TrimSpace(cmd.Name) == name {
			return cmd, true
		}
	}
	return admincmd.Command{}, false
}

func (h *AdminCommandHandler) sendText(ctx context.Context, b *telego.Bot, chatID int64, replyID int, text string) {
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: chatID},
		Text:            text,
		ReplyParameters: &telego.ReplyParameters{MessageID: replyID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}

func (h *AdminCommandHandler) sendResponse(ctx context.Context, b *telego.Bot, chatID int64, replyID int, resp *admincmd.Response) {
	if resp == nil {
		h.sendText(ctx, b, chatID, replyID, "执行完成")
		return
	}
	text := sanitizeSensitiveText(strings.TrimSpace(resp.Text))
	if len(resp.Photo) > 0 {
		name := strings.TrimSpace(resp.PhotoName)
		if name == "" {
			name = "qrcode.png"
		}
		params := &telego.SendPhotoParams{
			ChatID:          telego.ChatID{ID: chatID},
			Photo:           telego.InputFile{File: telegoutil.NameReader(bytes.NewReader(resp.Photo), name)},
			ReplyParameters: &telego.ReplyParameters{MessageID: replyID},
			ReplyMarkup:     resp.ReplyMarkup,
		}
		if text != "" {
			params.Caption = text
		}
		var sent *telego.Message
		if h.RateLimiter != nil {
			sent, _ = telegram.SendPhotoWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			sent, _ = b.SendPhoto(ctx, params)
		}
		if resp.AfterSend != nil && sent != nil {
			resp.AfterSend(ctx, b, sent)
		}
		return
	}
	if text == "" {
		text = "执行完成"
	}
	if h.RateLimiter != nil {
		params := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: chatID},
			Text:            text,
			ReplyParameters: &telego.ReplyParameters{MessageID: replyID},
			ReplyMarkup:     resp.ReplyMarkup,
		}
		sent, _ := telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		if resp.AfterSend != nil && sent != nil {
			resp.AfterSend(ctx, b, sent)
		}
	} else {
		params := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: chatID},
			Text:            text,
			ReplyParameters: &telego.ReplyParameters{MessageID: replyID},
			ReplyMarkup:     resp.ReplyMarkup,
		}
		sent, _ := b.SendMessage(ctx, params)
		if resp.AfterSend != nil && sent != nil {
			resp.AfterSend(ctx, b, sent)
		}
	}
}

func BuildCheckCookieCommand(manager platform.Manager) admincmd.Command {
	return admincmd.Command{
		Name:        "checkck",
		Description: "检查插件 Cookie 有效性",
		Handler: func(ctx context.Context, args string) (string, error) {
			return checkCookies(ctx, manager, args)
		},
	}
}

func BuildCookieRenewCommand(manager platform.Manager) admincmd.Command {
	return admincmd.Command{
		Name:        "renewck",
		Description: "手动续期 Cookie（/renewck <platform>，留空续全部）",
		Handler: func(ctx context.Context, args string) (string, error) {
			return renewCookies(ctx, manager, args)
		},
	}
}

func checkCookies(ctx context.Context, manager platform.Manager, args string) (string, error) {
	if manager == nil {
		return "平台管理器未初始化", nil
	}
	args = strings.TrimSpace(args)
	if args != "" {
		platformName := resolveCookiePlatform(manager, args)
		if platformName == "" {
			return fmt.Sprintf("未识别的平台: %s", args), nil
		}
		line, err := checkCookieForPlatform(ctx, manager, platformName)
		if err != nil {
			return "", err
		}
		return line, nil
	}

	names := manager.List()
	if len(names) == 0 {
		return "没有可用的平台", nil
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		line, err := checkCookieForPlatform(ctx, manager, name)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "没有支持 Cookie 检查的平台", nil
	}
	return strings.Join(lines, "\n"), nil
}

func renewCookies(ctx context.Context, manager platform.Manager, args string) (string, error) {
	if manager == nil {
		return "平台管理器未初始化", nil
	}
	args = strings.TrimSpace(args)
	if args != "" {
		platformName := resolveCookiePlatform(manager, args)
		if platformName == "" {
			return fmt.Sprintf("未识别的平台: %s", args), nil
		}
		line, err := renewCookieForPlatform(ctx, manager, platformName)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(line) == "" {
			return fmt.Sprintf("%s 不支持 Cookie 续期", platformName), nil
		}
		return line, nil
	}

	names := manager.List()
	if len(names) == 0 {
		return "没有可用的平台", nil
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	failures := 0
	for _, name := range names {
		line, err := renewCookieForPlatform(ctx, manager, name)
		if err != nil {
			failures++
			lines = append(lines, fmt.Sprintf("❌ %s: %s", name, sanitizeSensitiveText(err.Error())))
			continue
		}
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "没有支持 Cookie 续期的平台", nil
	}
	if failures > 0 {
		lines = append(lines, fmt.Sprintf("\n完成（失败 %d 个平台）", failures))
	}
	return strings.Join(lines, "\n"), nil
}

func resolveCookiePlatform(manager platform.Manager, raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || manager == nil {
		return ""
	}
	if name, ok := manager.ResolveAlias(trimmed); ok {
		return name
	}
	return ""
}

func checkCookieForPlatform(ctx context.Context, manager platform.Manager, platformName string) (string, error) {
	plat := manager.Get(platformName)
	if plat == nil {
		return "", nil
	}
	checker, ok := plat.(platform.CookieChecker)
	if !ok {
		return "", nil
	}
	result, err := checker.CheckCookie(ctx)
	if err != nil {
		return "", err
	}
	status := "❌"
	if result.OK {
		status = "✅"
	}
	message := strings.TrimSpace(result.Message)
	if message == "" {
		message = "未知"
	}
	return fmt.Sprintf("%s %s: %s", status, platformName, message), nil
}

func renewCookieForPlatform(ctx context.Context, manager platform.Manager, platformName string) (string, error) {
	plat := manager.Get(platformName)
	if plat == nil {
		return "", nil
	}
	renewer, ok := plat.(platform.CookieRenewer)
	if !ok {
		return "", nil
	}
	message, err := renewer.ManualRenew(ctx)
	if err != nil {
		return "", err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "续期完成"
	}
	message = sanitizeSensitiveText(message)
	return fmt.Sprintf("✅ %s: %s", platformName, message), nil
}

func sanitizeSensitiveText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}
	return sensitiveKVPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := strings.SplitN(match, "=", 2)
		sep := "="
		if len(parts) < 2 {
			parts = strings.SplitN(match, ":", 2)
			sep = ":"
		}
		if len(parts) < 2 {
			return "[REDACTED]"
		}
		return strings.TrimSpace(parts[0]) + sep + " ***"
	})
}
