package handler

import (
	"context"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// Router registers bot handlers and delegates to feature handlers.
type Router struct {
	Music            MessageHandler
	Search           MessageHandler
	Lyric            MessageHandler
	Recognize        MessageHandler
	About            MessageHandler
	Status           MessageHandler
	RmCache          MessageHandler
	Settings         MessageHandler
	Callback         CallbackHandler
	SettingsCallback CallbackHandler
	Inline           InlineHandler
	PlatformManager  platform.Manager
}

// Register registers all handlers to the bot.
func (r *Router) Register(b *bot.Bot, botName string) {
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "start"), r.wrapMessage(r.Music))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "music"), r.wrapMessage(r.Music))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "netease"), r.wrapMessage(r.Music))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "program"), r.wrapMessage(r.Music))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "search"), r.wrapMessage(r.Search))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "lyric"), r.wrapMessage(r.Lyric))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "recognize"), r.wrapMessage(r.Recognize))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "about"), r.wrapMessage(r.About))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "status"), r.wrapMessage(r.Status))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "settings"), r.wrapMessage(r.Settings))
	b.RegisterHandlerMatchFunc(matchCommandFunc(botName, "rmcache"), r.wrapMessage(r.RmCache))

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		if update.Message == nil || update.Message.Text == "" {
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
			if len(seg) > 1 && seg[1] != "" && seg[1] != botName {
				return false
			}
		}

		if isReservedCommand(command) {
			return false
		}
		if r.PlatformManager == nil {
			return false
		}
		return r.PlatformManager.Get(command) != nil
	}, r.wrapMessage(r.Music))

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		if update.Message == nil || update.Message.Text == "" {
			return false
		}
		// Use PlatformManager for dynamic URL matching if available
		if r.PlatformManager != nil {
			if _, _, matched := r.PlatformManager.MatchText(update.Message.Text); matched {
				return true
			}
			if _, _, matched := r.PlatformManager.MatchURL(update.Message.Text); matched {
				return true
			}
		}
		return false
	}, r.wrapMessage(r.Music))

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		if update.Message == nil || update.Message.Text == "" || isCommandMessage(update.Message) {
			return false
		}
		if update.Message.Chat.Type != "private" {
			return false
		}
		text := update.Message.Text
		// Use PlatformManager to exclude platform URLs if available
		if r.PlatformManager != nil {
			if _, _, matched := r.PlatformManager.MatchText(text); matched {
				return false
			}
			if _, _, matched := r.PlatformManager.MatchURL(text); matched {
				return false
			}
		}
		return true
	}, r.wrapMessage(r.Search))

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "music", bot.MatchTypePrefix, r.wrapCallback(r.Callback))
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "settings", bot.MatchTypePrefix, r.wrapCallback(r.SettingsCallback))
	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return update.InlineQuery != nil
	}, r.wrapInline(r.Inline))

	_ = botName
}

func (r *Router) wrapMessage(handler MessageHandler) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if handler == nil {
			return
		}
		handler.Handle(ctx, b, update)
	}
}

func (r *Router) wrapInline(handler InlineHandler) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if handler == nil {
			return
		}
		handler.Handle(ctx, b, update)
	}
}

func (r *Router) wrapCallback(handler CallbackHandler) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if handler == nil {
			return
		}
		handler.Handle(ctx, b, update)
	}
}

func isCommandMessage(message *models.Message) bool {
	if message == nil || message.Text == "" {
		return false
	}
	if !strings.HasPrefix(message.Text, "/") {
		return false
	}
	for _, entity := range message.Entities {
		if entity.Type == "bot_command" && entity.Offset == 0 {
			return true
		}
	}
	return false
}

func matchCommandFunc(botName, cmd string) func(update *models.Update) bool {
	return func(update *models.Update) bool {
		if update.Message == nil || update.Message.Text == "" {
			return false
		}
		text := strings.TrimSpace(update.Message.Text)
		if !strings.HasPrefix(text, "/") {
			return false
		}
		parts := strings.SplitN(text, " ", 2)
		command := strings.TrimPrefix(parts[0], "/")
		if command == cmd {
			return true
		}
		if botName != "" && command == cmd+"@"+botName {
			return true
		}
		return false
	}
}

func isReservedCommand(command string) bool {
	switch command {
	case "start", "music", "netease", "program", "search", "lyric", "recognize", "about", "status", "settings", "rmcache":
		return true
	default:
		return false
	}
}
