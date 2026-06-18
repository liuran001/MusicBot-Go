package handler

import (
	"context"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

// Router registers bot handlers and delegates to feature handlers.
type Router struct {
	Music                    MessageHandler
	Playlist                 MessageHandler
	Artist                   MessageHandler
	Search                   MessageHandler
	Lyric                    MessageHandler
	Recognize                MessageHandler
	About                    MessageHandler
	Status                   MessageHandler
	RmCache                  MessageHandler
	Settings                 MessageHandler
	Reload                   MessageHandler
	Admin                    MessageHandler
	GuestMode                MessageHandler
	MentionRouter            *MentionRouter
	GuestSearchCallback      CallbackHandler
	Callback                 CallbackHandler
	SettingsCallback         CallbackHandler
	SearchCallback           CallbackHandler
	PlaylistCallback         CallbackHandler
	InlineCollectionCallback CallbackHandler
	LyricCallback            CallbackHandler
	Inline                   InlineHandler
	ChosenInline             ChosenInlineHandler
	PlatformManager          platform.Manager
	AdminCommands            []string
	Whitelist                *Whitelist
	Logger                   botpkg.Logger
}

// Register registers all handlers to the bot handler.
func (r *Router) Register(bh *th.BotHandler, botName string) {
	if bh == nil {
		return
	}

	bh.Handle(r.wrapMessage(r.Music), matchCommandFunc(botName, "start"))
	bh.Handle(r.wrapMessage(r.Music), matchCommandFunc(botName, "help"))
	bh.Handle(r.wrapMessage(r.Music), matchCommandFunc(botName, "music"))
	bh.Handle(r.wrapMessage(r.Music), matchCommandFunc(botName, "program"))
	bh.Handle(r.wrapMessage(r.Search), matchCommandFunc(botName, "search"))
	bh.Handle(r.wrapMessage(r.Lyric), matchCommandFunc(botName, "lyric"))
	if r.Recognize != nil {
		bh.Handle(r.wrapMessage(r.Recognize), matchCommandFunc(botName, "recognize"))
	}
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
	bh.Handle(r.wrapCallback(r.Admin), func(ctx context.Context, update telego.Update) bool {
		if update.CallbackQuery == nil {
			return false
		}
		return strings.HasPrefix(strings.TrimSpace(update.CallbackQuery.Data), "admin ")
	})

	// "<keyword> @bot" in a chat the bot has joined: route by guest-mode keyword
	// classification (lyric / recognize / song-or-search). Registered before the
	// URL/keyword content routes so a mention is handled uniformly; non-slash
	// commands and plain keywords would otherwise fall through unmatched.
	if r.MentionRouter != nil {
		bh.Handle(r.wrapMessage(r.MentionRouter), func(ctx context.Context, update telego.Update) bool {
			if update.Message == nil || update.Message.Text == "" {
				return false
			}
			if isCommandMessage(update.Message) {
				return false
			}
			_, ok := r.MentionRouter.mentionsBot(update.Message)
			return ok
		})
	}

	if r.Recognize != nil {
		bh.Handle(r.wrapMessage(r.Recognize), func(ctx context.Context, update telego.Update) bool {
			if update.Message == nil || update.Message.Voice == nil {
				return false
			}
			return update.Message.Chat.Type == "private"
		})
	}

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
		resolvedText := resolveShortLinkText(ctx, r.PlatformManager, baseText)
		platformName, _, matched := matchPlaylistURL(ctx, r.PlatformManager, resolvedText)
		if !matched {
			return false
		}
		if update.Message.Chat.Type != "private" {
			return isAllowedGroupURLPlatform(platformName, r.PlatformManager)
		}
		return true
	})

	bh.Handle(r.wrapMessage(r.Artist), func(ctx context.Context, update telego.Update) bool {
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
		platformName, _, matched := matchArtistURL(ctx, r.PlatformManager, baseText)
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
		baseText, _, _ := parseTrailingOptions(text, r.PlatformManager)
		if strings.TrimSpace(baseText) == "" {
			return false
		}
		if r.PlatformManager != nil {
			resolvedText := resolveShortLinkText(ctx, r.PlatformManager, baseText)
			if _, _, matched := matchPlaylistURL(ctx, r.PlatformManager, resolvedText); matched {
				return false
			}
			if _, _, matched := matchArtistURL(ctx, r.PlatformManager, resolvedText); matched {
				return false
			}
			if _, _, matched := r.PlatformManager.MatchText(resolvedText); matched {
				return false
			}
			if _, _, matched := r.PlatformManager.MatchURL(resolvedText); matched {
				return false
			}
		}
		if hasSearchPlatformSuffix(text, r.PlatformManager) {
			return true
		}
		return true
	})

	bh.Handle(r.wrapCallback(r.Callback), callbackPrefix("music"))
	bh.Handle(r.wrapCallback(r.SettingsCallback), callbackPrefix("settings"))
	bh.Handle(r.wrapCallback(r.SearchCallback), callbackPrefix("search"))
	bh.Handle(r.wrapCallback(r.PlaylistCallback), callbackPrefix("playlist"))
	bh.Handle(r.wrapCallback(r.InlineCollectionCallback), callbackPrefix("ipl"))
	if r.LyricCallback != nil {
		bh.Handle(r.wrapCallback(r.LyricCallback), callbackPrefix("lyric "))
	}
	if r.GuestSearchCallback != nil {
		bh.Handle(r.wrapCallback(r.GuestSearchCallback), callbackPrefix("guest "))
	}
	bh.Handle(r.wrapInline(r.Inline), func(ctx context.Context, update telego.Update) bool {
		return update.InlineQuery != nil
	})
	bh.Handle(r.wrapChosenInline(r.ChosenInline), func(ctx context.Context, update telego.Update) bool {
		return update.ChosenInlineResult != nil
	})

	if r.GuestMode != nil {
		bh.HandleGuestMessage(func(ctx *th.Context, message telego.Message) error {
			if r.GuestMode != nil {
				// Skip messages sent via the bot's own inline mode —
				// otherwise the inline song card triggers guest mode
				// again ("请输入歌曲名或链接"), creating a loop.
				if r.isOwnInlineMessage(&message) {
					return nil
				}
				r.GuestMode.Handle(ctx, ctx.Bot(), &telego.Update{GuestMessage: &message})
			}
			return nil
		})
	}

	_ = botName
}

// isOwnInlineMessage reports whether message was sent via the bot's own inline
// mode. Such messages must not be processed as new user requests to prevent a
// self-referencing loop: user picks inline result → bot sends song card → bot
// sees its own card in the group → treats the card text as a new search query.
func (r *Router) isOwnInlineMessage(message *telego.Message) bool {
	if message == nil || message.ViaBot == nil {
		return false
	}
	if r.MentionRouter != nil {
		botName := strings.TrimPrefix(strings.TrimSpace(r.MentionRouter.BotName), "@")
		if botName != "" && strings.EqualFold(message.ViaBot.Username, botName) {
			return true
		}
	}
	return false
}

func (r *Router) wrapMessage(handler MessageHandler) th.Handler {
	return func(ctx *th.Context, update telego.Update) error {
		if handler == nil {
			return nil
		}
		if r.isOwnInlineMessage(update.Message) {
			return nil
		}
		if r.Whitelist != nil && update.Message != nil {
			chatID := update.Message.Chat.ID
			var userID int64
			if update.Message.From != nil {
				userID = update.Message.From.ID
			}
			if !r.Whitelist.IsAllowed(chatID, userID) {
				if update.Message.Chat.Type == "group" || update.Message.Chat.Type == "supergroup" {
					if r.Logger != nil {
						r.Logger.Info("leave non-whitelisted chat", "chat_id", chatID, "chat_type", update.Message.Chat.Type)
					}
					if err := ctx.Bot().LeaveChat(ctx, &telego.LeaveChatParams{ChatID: telego.ChatID{ID: chatID}}); err != nil {
						if r.Logger != nil {
							r.Logger.Warn("leave chat failed", "chat_id", chatID, "error", err)
						}
					}
				}
				return nil
			}
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
		if r.Whitelist != nil && update.InlineQuery != nil {
			userID := update.InlineQuery.From.ID
			if !r.Whitelist.IsAllowed(userID, userID) {
				return nil
			}
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

func (r *Router) wrapChosenInline(handler ChosenInlineHandler) th.Handler {
	return func(ctx *th.Context, update telego.Update) error {
		if handler == nil {
			return nil
		}
		if r.Whitelist != nil && update.ChosenInlineResult != nil {
			userID := update.ChosenInlineResult.From.ID
			if !r.Whitelist.IsAllowed(userID, userID) {
				return nil
			}
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

func isAllowedGroupURLPlatform(platformName string, manager platform.Manager) bool {
	if manager == nil {
		return false
	}
	meta, _ := manager.Meta(platformName)
	return meta.AllowGroupURL
}
