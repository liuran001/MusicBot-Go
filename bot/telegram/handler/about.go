package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/dynplugin"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// AboutHandler handles /about command.
type AboutHandler struct {
	RuntimeVer  string
	BinVersion  string
	CommitSHA   string
	BuildTime   string
	BuildArch   string
	DynPlugins  *dynplugin.Manager
	RateLimiter *telegram.RateLimiter
}

func (h *AboutHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil {
		return
	}
	versionText := formatVersionLink(h.BinVersion, h.CommitSHA)
	runtimeText := mdV2Replacer.Replace(h.RuntimeVer)
	buildTimeText := mdV2Replacer.Replace(h.BuildTime)
	buildArchText := mdV2Replacer.Replace(h.BuildArch)
	pluginText := h.pluginSummary()
	msg := fmt.Sprintf(aboutText, versionText, pluginText, runtimeText, buildTimeText, buildArchText)
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: update.Message.Chat.ID},
		Text:            msg,
		ParseMode:       telego.ModeMarkdownV2,
		ReplyParameters: &telego.ReplyParameters{MessageID: update.Message.MessageID},
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}

func formatVersionLink(binVersion, commitSHA string) string {
	shortCommit := strings.TrimSpace(commitSHA)
	if len(shortCommit) > 7 {
		shortCommit = shortCommit[:7]
	}
	label := strings.TrimSpace(binVersion)
	if label == "" {
		label = shortCommit
	}
	if label == "" {
		return mdV2Replacer.Replace("unknown")
	}
	if strings.TrimSpace(binVersion) != "" && shortCommit != "" && label != shortCommit {
		label = fmt.Sprintf("%s - %s", binVersion, shortCommit)
	}
	escapedLabel := mdV2Replacer.Replace(label)
	if strings.TrimSpace(commitSHA) == "" {
		return escapedLabel
	}
	commitURL := fmt.Sprintf("https://github.com/liuran001/MusicBot-Go/commit/%s", commitSHA)
	escapedURL := mdV2Replacer.Replace(commitURL)
	return fmt.Sprintf("[%s](%s)", escapedLabel, escapedURL)
}

func (h *AboutHandler) pluginSummary() string {
	plugins := []dynplugin.PluginInfo{}
	if h != nil && h.DynPlugins != nil {
		plugins = h.DynPlugins.PluginInfos()
	}
	if len(plugins) == 0 {
		return mdV2Replacer.Replace("无")
	}
	lines := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		name := strings.TrimSpace(plugin.Name)
		if name == "" {
			name = "unknown"
		}
		line := "\\- " + mdV2Replacer.Replace(name)
		if strings.TrimSpace(plugin.Version) != "" {
			line += " " + mdV2Replacer.Replace("("+plugin.Version+")")
		}
		if strings.TrimSpace(plugin.URL) != "" {
			line += " " + mdV2Replacer.Replace(plugin.URL)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
