package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/config"
)

// Bot wraps go-telegram/bot with application configuration.
type Bot struct {
	client *bot.Bot
	config *config.Config
	logger botpkg.Logger
}

// New creates a new Telegram bot client.
func New(cfg *config.Config, logger botpkg.Logger) (*Bot, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger required")
	}

	options := []bot.Option{
		bot.WithWorkers(4),
		bot.WithErrorsHandler(func(err error) {
			logger.Error("telegram error", "error", err)
		}),
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
			_ = ctx
			_ = b
			_ = update
		}),
	}

	if cfg.GetString("BotAPI") != "" {
		options = append(options, bot.WithServerURL(cfg.GetString("BotAPI")))
	}
	if cfg.GetBool("BotDebug") {
		options = append(options, bot.WithDebug())
	}

	client, err := bot.New(cfg.GetString("BOT_TOKEN"), options...)
	if err != nil {
		return nil, err
	}

	return &Bot{client: client, config: cfg, logger: logger}, nil
}

// Start begins polling updates and blocks until context is canceled.
func (b *Bot) Start(ctx context.Context) {
	b.client.Start(ctx)
}

// Client exposes the underlying bot client.
func (b *Bot) Client() *bot.Bot {
	return b.client
}

// GetMe retrieves bot info.
func (b *Bot) GetMe(ctx context.Context) (*models.User, error) {
	return b.client.GetMe(ctx)
}

// SendMessage is a convenience wrapper for sending a text message.
func (b *Bot) SendMessage(ctx context.Context, chatID int64, text string) (*models.Message, error) {
	params := &bot.SendMessageParams{ChatID: chatID, Text: text}
	return b.client.SendMessage(ctx, params)
}

// SendChatAction sends a chat action.
func (b *Bot) SendChatAction(ctx context.Context, chatID int64, action string) error {
	_, err := b.client.SendChatAction(ctx, &bot.SendChatActionParams{ChatID: chatID, Action: models.ChatAction(action)})
	return err
}

// SetWebhook configures webhook and starts the webhook handler.
func (b *Bot) SetWebhook(ctx context.Context, url string, secret string) error {
	params := &bot.SetWebhookParams{URL: url}
	if secret != "" {
		params.SecretToken = secret
	}
	_, err := b.client.SetWebhook(ctx, params)
	return err
}

// WithTimeout returns a context with timeout for Telegram requests.
func WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
