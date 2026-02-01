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
	b.RegisterHandler(bot.HandlerTypeMessageText, "start", bot.MatchTypeCommand, r.wrapMessage(r.Music))
	b.RegisterHandler(bot.HandlerTypeMessageText, "music", bot.MatchTypeCommand, r.wrapMessage(r.Music))
	b.RegisterHandler(bot.HandlerTypeMessageText, "netease", bot.MatchTypeCommand, r.wrapMessage(r.Music))
	b.RegisterHandler(bot.HandlerTypeMessageText, "program", bot.MatchTypeCommand, r.wrapMessage(r.Music))
	b.RegisterHandler(bot.HandlerTypeMessageText, "search", bot.MatchTypeCommand, r.wrapMessage(r.Search))
	b.RegisterHandler(bot.HandlerTypeMessageText, "lyric", bot.MatchTypeCommand, r.wrapMessage(r.Lyric))
	b.RegisterHandler(bot.HandlerTypeMessageText, "recognize", bot.MatchTypeCommand, r.wrapMessage(r.Recognize))
	b.RegisterHandler(bot.HandlerTypeMessageText, "about", bot.MatchTypeCommand, r.wrapMessage(r.About))
	b.RegisterHandler(bot.HandlerTypeMessageText, "status", bot.MatchTypeCommand, r.wrapMessage(r.Status))
	b.RegisterHandler(bot.HandlerTypeMessageText, "settings", bot.MatchTypeCommand, r.wrapMessage(r.Settings))
	b.RegisterHandler(bot.HandlerTypeMessageText, "rmcache", bot.MatchTypeCommand, r.wrapMessage(r.RmCache))

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		if update.Message == nil || update.Message.Text == "" {
			return false
		}
		// Use PlatformManager for dynamic URL matching if available
		if r.PlatformManager != nil {
			_, _, matched := r.PlatformManager.MatchURL(update.Message.Text)
			if matched {
				return true
			}
		}
		// Fallback to hardcoded patterns for backward compatibility (163cn.tv/link shorteners)
		text := update.Message.Text
		return strings.Contains(text, "163cn.tv") || strings.Contains(text, "163cn.link")
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
			if _, _, matched := r.PlatformManager.MatchURL(text); matched {
				return false
			}
		}
		// Fallback: exclude hardcoded NetEase shortener domains
		if strings.Contains(text, "163cn.tv") || strings.Contains(text, "163cn.link") {
			return false
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
