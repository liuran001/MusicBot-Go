package handler

import (
	"context"
	"fmt"
	"sort"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// StatusHandler handles /status command.
type StatusHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
	AdminIDs        map[int64]struct{}
}

func (h *StatusHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil || h.Repo == nil {
		return
	}
	message := update.Message
	isPrivate := strings.EqualFold(strings.TrimSpace(string(message.Chat.Type)), "private")
	isAdmin := message.From != nil && isBotAdmin(h.AdminIDs, message.From.ID)
	showDetailedAccount := isPrivate && isAdmin

	fromCount, _ := h.Repo.Count(ctx)
	chatCount, _ := h.Repo.CountByChatID(ctx, message.Chat.ID)
	chatInfo := mdV2Replacer.Replace(message.Chat.Title)
	if message.Chat.Username != "" && message.Chat.Title == "" {
		chatInfo = fmt.Sprintf("[%s](tg://user?id=%d)", mdV2Replacer.Replace(message.Chat.Username), message.Chat.ID)
	} else if message.Chat.Username != "" {
		chatInfo = fmt.Sprintf("[%s](https://t.me/%s)", mdV2Replacer.Replace(message.Chat.Title), message.Chat.Username)
	}

	userID := int64(0)
	userCount := int64(0)
	if message.From != nil {
		userID = message.From.ID
		userCount, _ = h.Repo.CountByUserID(ctx, userID)
	}

	sendCount, _ := h.Repo.GetSendCount(ctx)
	msgText := fmt.Sprintf(statusInfo, fromCount, chatInfo, chatCount, userID, userID, userCount, sendCount)

	if platformCounts, err := h.Repo.CountByPlatform(ctx); err == nil && len(platformCounts) > 0 {
		platformNames := make([]string, 0, len(platformCounts))
		for name := range platformCounts {
			platformNames = append(platformNames, name)
		}
		sort.Strings(platformNames)
		lines := make([]string, 0, len(platformNames))
		for _, name := range platformNames {
			display := mdV2Replacer.Replace(platformDisplayName(h.PlatformManager, name))
			lines = append(lines, fmt.Sprintf("%s: %d", display, platformCounts[name]))
		}
		msgText += "\n缓存平台统计:\n" + strings.Join(lines, "\n")
	}

	if h.PlatformManager != nil {
		platforms := h.PlatformManager.List()
		if len(platforms) > 0 {
			displayNames := make([]string, 0, len(platforms))
			for _, name := range platforms {
				displayNames = append(displayNames, platformDisplayName(h.PlatformManager, name))
			}
			platformsEscaped := mdV2Replacer.Replace(strings.Join(displayNames, ", "))
			msgText += fmt.Sprintf("\n\n📱 可用平台: %s", platformsEscaped)
		}
	}

	if accountText := h.buildAccountStatusSection(ctx, showDetailedAccount); strings.TrimSpace(accountText) != "" {
		msgText += accountText
	}

	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		Text:            msgText,
		ParseMode:       telego.ModeMarkdownV2,
		ReplyParameters: &telego.ReplyParameters{MessageID: message.MessageID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}

func (h *StatusHandler) buildAccountStatusSection(ctx context.Context, detailed bool) string {
	if h == nil || h.PlatformManager == nil {
		return ""
	}
	platforms := h.PlatformManager.List()
	if len(platforms) == 0 {
		return ""
	}
	sort.Strings(platforms)
	statuses := make([]platform.AccountStatus, 0, len(platforms))
	for _, name := range platforms {
		plat := h.PlatformManager.Get(name)
		provider, ok := plat.(platform.AccountStatusProvider)
		if !ok {
			continue
		}
		status, err := provider.AccountStatus(ctx)
		if err != nil {
			statuses = append(statuses, platform.AccountStatus{
				Platform:    name,
				DisplayName: platformDisplayName(h.PlatformManager, name),
				Summary:     "状态检查失败",
			})
			continue
		}
		if strings.TrimSpace(status.DisplayName) == "" {
			status.DisplayName = platformDisplayName(h.PlatformManager, name)
		}
		statuses = append(statuses, status)
	}
	if len(statuses) == 0 {
		return ""
	}
	if detailed {
		return "\n\n🔐 账号状态:\n" + mdV2Replacer.Replace(renderDetailedAccountStatuses(statuses))
	}
	return "\n\n🔐 账号状态:\n" + mdV2Replacer.Replace(renderSafeAccountStatuses(statuses))
}

func renderSafeAccountStatuses(statuses []platform.AccountStatus) string {
	if len(statuses) == 0 {
		return "未发现可查询的平台账号状态"
	}
	available := 0
	lines := make([]string, 0, len(statuses)+1)
	for _, status := range statuses {
		state := "未登录"
		if status.LoggedIn {
			state = "可用"
			available++
		} else if strings.TrimSpace(status.Summary) != "" {
			state = classifySafeStatus(status)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", status.DisplayName, state))
	}
	return fmt.Sprintf("已登录平台: %d/%d\n%s", available, len(statuses), strings.Join(lines, "\n"))
}

func renderDetailedAccountStatuses(statuses []platform.AccountStatus) string {
	blocks := make([]string, 0, len(statuses))
	for _, status := range statuses {
		lines := []string{status.DisplayName}
		if status.LoggedIn {
			lines = append(lines, "- 状态: 已登录")
		} else {
			lines = append(lines, "- 状态: 未登录")
		}
		if strings.TrimSpace(status.Nickname) != "" {
			lines = append(lines, "- 昵称: "+strings.TrimSpace(status.Nickname))
		}
		if strings.TrimSpace(status.UserID) != "" {
			lines = append(lines, "- 用户ID: "+maskStatusUserID(status.UserID))
		}
		if strings.TrimSpace(status.AuthMode) != "" {
			lines = append(lines, "- 登录方式: "+strings.TrimSpace(status.AuthMode))
		}
		if len(status.SupportedLogins) > 0 {
			lines = append(lines, "- 支持: "+strings.Join(status.SupportedLogins, ", "))
		}
		if strings.TrimSpace(status.SessionSource) != "" {
			lines = append(lines, "- 来源: "+strings.TrimSpace(status.SessionSource))
		}
		if strings.TrimSpace(status.Summary) != "" {
			for _, line := range strings.Split(strings.TrimSpace(status.Summary), "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" || isRedundantStatusLine(lines, trimmed) {
					continue
				}
				lines = append(lines, trimmed)
			}
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func classifySafeStatus(status platform.AccountStatus) string {
	summary := strings.ToLower(strings.TrimSpace(status.Summary))
	if summary == "" {
		return "未登录"
	}
	switch {
	case strings.Contains(summary, "失败"):
		return "异常"
	case strings.Contains(summary, "未初始化"):
		return "未初始化"
	case strings.Contains(summary, "访客"):
		return "访客"
	default:
		return "未登录"
	}
}

func maskStatusUserID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:2]) + strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-2:])
}

func isRedundantStatusLine(lines []string, candidate string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == candidate {
			return true
		}
	}
	return false
}
