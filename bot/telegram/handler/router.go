package handler

import (
	"context"
	"regexp"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

// Router registers bot handlers and delegates to feature handlers.
type Router struct {
	Music            MessageHandler
	Playlist         MessageHandler
	Search           MessageHandler
	Lyric            MessageHandler
	Recognize        MessageHandler
	About            MessageHandler
	Status           MessageHandler
	RmCache          MessageHandler
	Settings         MessageHandler
	Reload           MessageHandler
	Admin            MessageHandler
	Callback         CallbackHandler
	SettingsCallback CallbackHandler
	SearchCallback   CallbackHandler
	PlaylistCallback CallbackHandler
	Inline           InlineHandler
	PlatformManager  platform.Manager
	AdminCommands    []string
}

// Register registers all handlers to the bot handler.
func (r *Router) Register(bh *th.BotHandler, botName string) {
	if bh == nil {
		return
	}

	bh.Handle(r.wrapMessage(r.Music), matchCommandFunc(botName, "start"))
	bh.Handle(r.wrapMessage(r.Music), matchCommandFunc(botName, "help"))
	bh.Handle(r.wrapMessage(r.Music), matchCommandFunc(botName, "music"))
	bh.Handle(r.wrapMessage(r.Music), matchCommandFunc(botName, "netease"))
	bh.Handle(r.wrapMessage(r.Music), matchCommandFunc(botName, "program"))
	bh.Handle(r.wrapMessage(r.Search), matchCommandFunc(botName, "search"))
	bh.Handle(r.wrapMessage(r.Lyric), matchCommandFunc(botName, "lyric"))
	bh.Handle(r.wrapMessage(r.Recognize), matchCommandFunc(botName, "recognize"))
	bh.Handle(r.wrapMessage(r.About), matchCommandFunc(botName, "about"))
	bh.Handle(r.wrapMessage(r.Status), matchCommandFunc(botName, "status"))
	bh.Handle(r.wrapMessage(r.Settings), matchCommandFunc(botName, "settings"))
	bh.Handle(r.wrapMessage(r.RmCache), matchCommandFunc(botName, "rmcache"))
	bh.Handle(r.wrapMessage(r.Reload), matchCommandFunc(botName, "reload"))
	for _, cmd := range r.AdminCommands {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		bh.Handle(r.wrapMessage(r.Admin), matchCommandFunc(botName, cmd))
	}

	bh.Handle(r.wrapMessage(r.Music), func(ctx context.Context, update telego.Update) bool {
		if update.Message == nil || update.Message.Text == "" {
			return false
		}
		if update.Message.Chat.Type == "private" && update.Message.Voice != nil {
			return false
		}
		text := strings.TrimSpace(update.Message.Text)
		if !strings.HasPrefix(text, "/") {
			return false
		}
		parts := strings.SplitN(text, " ", 2)
		command := strings.TrimPrefix(parts[0], "/")
		if command == "" {
			return false
		}
		if strings.Contains(command, "@") {
			seg := strings.SplitN(command, "@", 2)
			command = seg[0]
			if len(seg) > 1 && seg[1] != "" && botName != "" && seg[1] != botName {
				return false
			}
		}

		if isReservedCommand(command) || isAdminCommand(command, r.AdminCommands) {
			return false
		}
		if r.PlatformManager == nil {
			return false
		}
		if platformName, ok := r.PlatformManager.ResolveAlias(command); ok {
			return r.PlatformManager.Get(platformName) != nil
		}
		return false
	})

	bh.Handle(r.wrapMessage(r.Recognize), func(ctx context.Context, update telego.Update) bool {
		if update.Message == nil || update.Message.Voice == nil {
			return false
		}
		return update.Message.Chat.Type == "private"
	})

	bh.Handle(r.wrapMessage(r.Playlist), func(ctx context.Context, update telego.Update) bool {
		if update.Message == nil || update.Message.Text == "" {
			return false
		}
		if isCommandMessage(update.Message) {
			return false
		}
		if update.Message.Voice != nil {
			return false
		}
		if r.PlatformManager == nil {
			return false
		}
		text := update.Message.Text
		baseText, _, _ := parseTrailingOptions(text, r.PlatformManager)
		if strings.TrimSpace(baseText) == "" {
			return false
		}
		platformName, _, matched := matchPlaylistURL(ctx, r.PlatformManager, baseText)
		if !matched {
			return false
		}
		if update.Message.Chat.Type != "private" {
			return isAllowedGroupURLPlatform(platformName, r.PlatformManager)
		}
		return true
	})

	bh.Handle(r.wrapMessage(r.Music), func(ctx context.Context, update telego.Update) bool {
		if update.Message == nil || update.Message.Text == "" {
			return false
		}
		if hasSearchPlatformSuffix(update.Message.Text, r.PlatformManager) {
			return false
		}
		if update.Message.Chat.Type != "private" {
			if r.PlatformManager == nil {
				return false
			}
			urls := extractURLs(update.Message.Text)
			if len(urls) == 0 {
				return false
			}
			for _, urlStr := range urls {
				resolvedURL := extractResolvedURL(ctx, r.PlatformManager, urlStr)
				if plat, _, matched := r.PlatformManager.MatchURL(resolvedURL); matched {
					return isAllowedGroupURLPlatform(plat, r.PlatformManager)
				}
				if plat, _, matched := r.PlatformManager.MatchText(resolvedURL); matched {
					return isAllowedGroupURLPlatform(plat, r.PlatformManager)
				}
			}
			return false
		}
		baseText, _, _ := parseTrailingOptions(update.Message.Text, r.PlatformManager)
		if strings.TrimSpace(baseText) == "" {
			return false
		}
		if r.PlatformManager != nil {
			resolvedText := resolveShortLinkText(ctx, r.PlatformManager, baseText)
			if _, _, matched := r.PlatformManager.MatchText(resolvedText); matched {
				return true
			}
			if _, _, matched := r.PlatformManager.MatchURL(resolvedText); matched {
				return true
			}
		}
		return false
	})

	bh.Handle(r.wrapMessage(r.Search), func(ctx context.Context, update telego.Update) bool {
		if update.Message == nil || update.Message.Text == "" || isCommandMessage(update.Message) {
			return false
		}
		if update.Message.Chat.Type != "private" {
			return false
		}
		if update.Message.Voice != nil {
			return false
		}
		text := update.Message.Text
		if hasSearchPlatformSuffix(text, r.PlatformManager) {
			return true
		}
		baseText, _, _ := parseTrailingOptions(text, r.PlatformManager)
		if strings.TrimSpace(baseText) == "" {
			return false
		}
		if r.PlatformManager != nil {
			resolvedText := resolveShortLinkText(ctx, r.PlatformManager, baseText)
			if _, _, matched := matchPlaylistURL(ctx, r.PlatformManager, resolvedText); matched {
				return false
			}
			if _, _, matched := r.PlatformManager.MatchText(resolvedText); matched {
				return false
			}
			if _, _, matched := r.PlatformManager.MatchURL(resolvedText); matched {
				return false
			}
		}
		return true
	})

	bh.Handle(r.wrapCallback(r.Callback), callbackPrefix("music"))
	bh.Handle(r.wrapCallback(r.SettingsCallback), callbackPrefix("settings"))
	bh.Handle(r.wrapCallback(r.SearchCallback), callbackPrefix("search"))
	bh.Handle(r.wrapCallback(r.PlaylistCallback), callbackPrefix("playlist"))
	bh.Handle(r.wrapInline(r.Inline), func(ctx context.Context, update telego.Update) bool {
		return update.InlineQuery != nil
	})

	_ = botName
}

func (r *Router) wrapMessage(handler MessageHandler) th.Handler {
	return func(ctx *th.Context, update telego.Update) error {
		if handler == nil {
			return nil
		}
		handler.Handle(ctx, ctx.Bot(), &update)
		return nil
	}
}

func (r *Router) wrapInline(handler InlineHandler) th.Handler {
	return func(ctx *th.Context, update telego.Update) error {
		if handler == nil {
			return nil
		}
		handler.Handle(ctx, ctx.Bot(), &update)
		return nil
	}
}

func (r *Router) wrapCallback(handler CallbackHandler) th.Handler {
	return func(ctx *th.Context, update telego.Update) error {
		if handler == nil {
			return nil
		}
		handler.Handle(ctx, ctx.Bot(), &update)
		return nil
	}
}

func callbackPrefix(prefix string) th.Predicate {
	return func(ctx context.Context, update telego.Update) bool {
		if update.CallbackQuery == nil {
			return false
		}
		return strings.HasPrefix(update.CallbackQuery.Data, prefix)
	}
}

func isCommandMessage(message *telego.Message) bool {
	if message == nil || message.Entities == nil || message.Text == "" {
		return false
	}
	if len(message.Entities) == 0 {
		return false
	}
	entity := message.Entities[0]
	if entity.Type != "bot_command" || entity.Offset != 0 {
		return false
	}
	return true
}

func matchCommandFunc(botName, cmd string) th.Predicate {
	return func(ctx context.Context, update telego.Update) bool {
		if update.Message == nil || update.Message.Text == "" {
			return false
		}
		messageText := update.Message.Text
		if !strings.HasPrefix(messageText, "/") {
			return false
		}
		parts := strings.SplitN(messageText, " ", 2)
		command := strings.TrimPrefix(parts[0], "/")
		if command == "" {
			return false
		}
		if strings.Contains(command, "@") {
			seg := strings.SplitN(command, "@", 2)
			command = seg[0]
			if len(seg) > 1 && seg[1] != "" && botName != "" && seg[1] != botName {
				return false
			}
		}
		return command == cmd
	}
}

func isReservedCommand(command string) bool {
	switch command {
	case "start", "help", "music", "netease", "program", "search", "lyric", "recognize", "about", "status", "settings", "rmcache":
		return true
	case "reload":
		return true
	default:
		return false
	}
}

func isAdminCommand(command string, commands []string) bool {
	if strings.TrimSpace(command) == "" {
		return false
	}
	for _, cmd := range commands {
		if strings.TrimSpace(cmd) == command {
			return true
		}
	}
	return false
}

func hasSearchPlatformSuffix(text string, manager platform.Manager) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	keyword := strings.TrimSpace(text)
	if strings.HasPrefix(keyword, "/") {
		keyword = commandArguments(keyword)
	}
	if strings.TrimSpace(keyword) == "" {
		return false
	}
	baseText, platformName, _ := parseTrailingOptions(keyword, manager)
	if strings.TrimSpace(platformName) == "" {
		return false
	}
	if len(extractURLs(baseText)) > 0 {
		return false
	}
	parts := strings.Fields(strings.TrimSpace(baseText))
	if len(parts) == 1 && isLikelyIDToken(parts[0]) {
		return false
	}
	return true
}

var urlPattern = regexp.MustCompile(`https?://[^\s]+`)

func extractURLs(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	matches := urlPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		cleaned := strings.TrimRight(match, ".,!?)]}>")
		if cleaned != "" {
			urls = append(urls, cleaned)
		}
	}
	return urls
}

func isAllowedGroupURLPlatform(platformName string, manager platform.Manager) bool {
	if manager == nil {
		return false
	}
	meta, _ := manager.Meta(platformName)
	return meta.AllowGroupURL
}
