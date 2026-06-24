package handler

import (
	"context"
	"fmt"
	"html"
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
	AdminIDs        *AdminSet
}

func (h *StatusHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil || h.Repo == nil {
		return
	}
	message := update.Message
	isPrivate := strings.EqualFold(strings.TrimSpace(string(message.Chat.Type)), "private")
	isAdmin := message.From != nil && isBotAdmin(h.AdminIDs, message.From.ID)
	showDetailedAccount := isPrivate && isAdmin
	useHTML := showDetailedAccount

	fromCount, _ := h.Repo.Count(ctx)
	chatCount, _ := h.Repo.CountByChatID(ctx, message.Chat.ID)
	chatInfo := mdV2Replacer.Replace(message.Chat.Title)
	if useHTML {
		chatInfo = html.EscapeString(message.Chat.Title)
	}
	if message.Chat.Username != "" && message.Chat.Title == "" {
		if useHTML {
			chatInfo = fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, message.Chat.ID, html.EscapeString(message.Chat.Username))
		} else {
			chatInfo = fmt.Sprintf("[%s](tg://user?id=%d)", mdV2Replacer.Replace(message.Chat.Username), message.Chat.ID)
		}
	} else if message.Chat.Username != "" {
		if useHTML {
			chatInfo = fmt.Sprintf(`<a href="https://t.me/%s">%s</a>`, html.EscapeString(message.Chat.Username), html.EscapeString(message.Chat.Title))
		} else {
			chatInfo = fmt.Sprintf("[%s](https://t.me/%s)", mdV2Replacer.Replace(message.Chat.Title), message.Chat.Username)
		}
	}

	userID := int64(0)
	userCount := int64(0)
	if message.From != nil {
		userID = message.From.ID
		userCount, _ = h.Repo.CountByUserID(ctx, userID)
	}

	sendCount, _ := h.Repo.GetSendCount(ctx)
	parseMode := telego.ModeMarkdownV2
	var msgText string
	if useHTML {
		msgText = buildStatusInfoHTML(ctx, fromCount, chatInfo, chatCount, userID, userCount, sendCount)
		parseMode = telego.ModeHTML
	} else {
		msgText = buildStatusInfoMarkdown(ctx, fromCount, chatInfo, chatCount, userID, userCount, sendCount)
	}

	if platformCounts, err := h.Repo.CountByPlatform(ctx); err == nil && len(platformCounts) > 0 {
		platformNames := make([]string, 0, len(platformCounts))
		for name := range platformCounts {
			platformNames = append(platformNames, name)
		}
		sort.Strings(platformNames)
		lines := make([]string, 0, len(platformNames))
		for _, name := range platformNames {
			display := mdV2Replacer.Replace(platformDisplayName(h.PlatformManager, name))
			if useHTML {
				display = html.EscapeString(platformDisplayName(h.PlatformManager, name))
			}
			lines = append(lines, fmt.Sprintf("%s: %d", display, platformCounts[name]))
		}
		label := tr(ctx, "status_platform_cache")
		if !useHTML {
			label = mdV2Replacer.Replace(label)
		}
		msgText += "\n📦 " + label + "\n" + strings.Join(lines, "\n")
	}

	if h.PlatformManager != nil {
		platforms := h.PlatformManager.List()
		if len(platforms) > 0 {
			displayNames := make([]string, 0, len(platforms))
			for _, name := range platforms {
				displayNames = append(displayNames, platformDisplayName(h.PlatformManager, name))
			}
			platformsEscaped := mdV2Replacer.Replace(strings.Join(displayNames, " / "))
			if useHTML {
				escaped := make([]string, 0, len(displayNames))
				for _, name := range displayNames {
					escaped = append(escaped, html.EscapeString(name))
				}
				platformsEscaped = strings.Join(escaped, " / ")
			}
			label := tr(ctx, "status_available_platforms")
			if !useHTML {
				label = mdV2Replacer.Replace(label)
			}
			msgText += fmt.Sprintf("\n\n📱 %s\n%s", label, platformsEscaped)
		}
	}

	if accountText := h.buildAccountStatusSection(ctx, showDetailedAccount); strings.TrimSpace(accountText) != "" {
		msgText += accountText
	}

	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: message.Chat.ID},
		Text:            msgText,
		ParseMode:       parseMode,
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
				Summary:     tr(ctx, "status_check_failed"),
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
	accountsLabel := tr(ctx, "status_accounts")
	if detailed {
		return "\n\n🔐 " + accountsLabel + "\n" + renderDetailedAccountStatusesHTML(ctx, statuses)
	}
	return "\n\n🔐 " + mdV2Replacer.Replace(accountsLabel) + "\n" + mdV2Replacer.Replace(renderSafeAccountStatuses(ctx, statuses))
}

func renderSafeAccountStatuses(ctx context.Context, statuses []platform.AccountStatus) string {
	if len(statuses) == 0 {
		return tr(ctx, "status_no_accounts")
	}
	available := 0
	lines := make([]string, 0, len(statuses)+1)
	for _, status := range statuses {
		state := tr(ctx, "status_state_not_logged_in")
		icon := "❌"
		if status.LoggedIn {
			state = tr(ctx, "status_state_available")
			icon = "✅"
			available++
		} else if strings.TrimSpace(status.Summary) != "" {
			state = classifySafeStatus(ctx, status)
		}
		lines = append(lines, fmt.Sprintf("%s %s：%s", icon, status.DisplayName, state))
	}
	return fmt.Sprintf("%s：%d/%d\n%s", tr(ctx, "status_logged_in"), available, len(statuses), strings.Join(lines, "\n"))
}

func renderDetailedAccountStatusesHTML(ctx context.Context, statuses []platform.AccountStatus) string {
	blocks := make([]string, 0, len(statuses))
	for _, status := range statuses {
		emoji := "❌"
		stateText := tr(ctx, "status_state_not_logged_in")
		if status.LoggedIn {
			emoji = "✅"
			stateText = tr(ctx, "status_logged_in")
		}
		header := fmt.Sprintf("%s %s（%s）", emoji, html.EscapeString(strings.TrimSpace(status.DisplayName)), html.EscapeString(stateText))
		detailLines := make([]string, 0, 8)
		detailLines = append(detailLines, tr(ctx, "status_field_state")+": "+stateText)
		if strings.TrimSpace(status.Nickname) != "" {
			detailLines = append(detailLines, tr(ctx, "status_field_nickname")+": "+strings.TrimSpace(status.Nickname))
		}
		if strings.TrimSpace(status.UserID) != "" {
			detailLines = append(detailLines, tr(ctx, "status_field_userid")+": "+maskStatusUserID(status.UserID))
		}
		if strings.TrimSpace(status.AuthMode) != "" {
			detailLines = append(detailLines, tr(ctx, "status_field_login_method")+": "+strings.TrimSpace(status.AuthMode))
		}
		if len(status.SupportedLogins) > 0 {
			detailLines = append(detailLines, tr(ctx, "status_field_supports")+": "+strings.Join(status.SupportedLogins, ", "))
		}
		if strings.TrimSpace(status.SessionSource) != "" {
			detailLines = append(detailLines, tr(ctx, "status_field_source")+": "+strings.TrimSpace(status.SessionSource))
		}
		if strings.TrimSpace(status.Summary) != "" {
			for _, line := range strings.Split(strings.TrimSpace(status.Summary), "\n") {
				trimmed := strings.TrimSpace(strings.TrimPrefix(line, "- "))
				if trimmed == "" || isRedundantStatusLine(detailLines, trimmed) {
					continue
				}
				detailLines = append(detailLines, trimmed)
			}
		}
		escapedLines := make([]string, 0, len(detailLines))
		for _, line := range detailLines {
			escapedLines = append(escapedLines, html.EscapeString(strings.TrimSpace(line)))
		}
		blocks = append(blocks, header+"\n<blockquote expandable>"+strings.Join(escapedLines, "\n")+"</blockquote>")
	}
	return strings.Join(blocks, "\n")
}

// buildStatusInfoMarkdown renders the cache summary block as MarkdownV2. Labels
// come from the catalog; the structural markdown lives here.
func buildStatusInfoMarkdown(ctx context.Context, fromCount int64, chatInfo string, chatCount int64, userID int64, userCount int64, sendCount int64) string {
	esc := func(id string) string { return mdV2Replacer.Replace(tr(ctx, id)) }
	tracks := esc("status_unit_tracks")
	return fmt.Sprintf("*📊 %s*\n\n🎧 %s\n%s：%d %s\n%s \\[%s\\]：%d %s\n%s \\[[%d](tg://user?id=%d)\\]：%d %s\n%s：%d %s\n",
		esc("status_title"), esc("status_cache"),
		esc("status_total"), fromCount, tracks,
		esc("status_this_chat"), chatInfo, chatCount, tracks,
		esc("status_your_cache"), userID, userID, userCount, tracks,
		esc("status_sent"), sendCount, esc("status_unit_times"))
}

func buildStatusInfoHTML(ctx context.Context, fromCount int64, chatInfo string, chatCount int64, userID int64, userCount int64, sendCount int64) string {
	esc := func(id string) string { return html.EscapeString(tr(ctx, id)) }
	tracks := esc("status_unit_tracks")
	return fmt.Sprintf("<b>📊 %s</b>\n\n🎧 %s\n%s：%d %s\n%s [%s]：%d %s\n%s [<a href=\"tg://user?id=%d\">%d</a>]：%d %s\n%s：%d %s\n",
		esc("status_title"), esc("status_cache"),
		esc("status_total"), fromCount, tracks,
		esc("status_this_chat"), chatInfo, chatCount, tracks,
		esc("status_your_cache"), userID, userID, userCount, tracks,
		esc("status_sent"), sendCount, esc("status_unit_times"))
}

func classifySafeStatus(ctx context.Context, status platform.AccountStatus) string {
	summary := strings.ToLower(strings.TrimSpace(status.Summary))
	if summary == "" {
		return tr(ctx, "status_state_not_logged_in")
	}
	switch {
	case strings.Contains(summary, "失败") || strings.Contains(summary, "fail"):
		return tr(ctx, "status_state_error")
	case strings.Contains(summary, "未初始化") || strings.Contains(summary, "uninitial"):
		return tr(ctx, "status_state_uninitialized")
	case strings.Contains(summary, "访客") || strings.Contains(summary, "guest"):
		return tr(ctx, "status_state_guest")
	default:
		return tr(ctx, "status_state_not_logged_in")
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
