package handler

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

type AdminCommandHandler struct {
	BotName     string
	AdminIDs    map[int64]struct{}
	RateLimiter *telegram.RateLimiter
	Commands    []admincmd.Command
}

func (h *AdminCommandHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
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
		h.sendText(ctx, b, message.Chat.ID, message.MessageID, "命令不可用")
		return
	}
	args := commandArguments(message.Text)
	result, err := command.Handler(ctx, args)
	if err != nil {
		h.sendText(ctx, b, message.Chat.ID, message.MessageID, fmt.Sprintf("执行失败: %v", err))
		return
	}
	result = strings.TrimSpace(result)
	if result == "" {
		result = "执行完成"
	}
	h.sendText(ctx, b, message.Chat.ID, message.MessageID, result)
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
		Name:        "ckrenew",
		Description: "手动续期 Cookie（/ckrenew <platform>，留空续全部）",
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
	for _, name := range names {
		line, err := renewCookieForPlatform(ctx, manager, name)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "没有支持 Cookie 续期的平台", nil
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
	return fmt.Sprintf("✅ %s: %s", platformName, message), nil
}
