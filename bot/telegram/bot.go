package telegram

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/mymmrac/telego"
)

var allowedUpdates = []string{
	"message",
	"callback_query",
	"inline_query",
	"chosen_inline_result",
}

const longPollingTimeoutSeconds = 60

// AllowedUpdates returns the update types required by the bot.
func AllowedUpdates() []string {
	return append([]string(nil), allowedUpdates...)
}

// LongPollingParams returns long polling parameters with explicit allowed updates.
func LongPollingParams() *telego.GetUpdatesParams {
	return &telego.GetUpdatesParams{Timeout: longPollingTimeoutSeconds, AllowedUpdates: AllowedUpdates()}
}

// WebhookParams builds webhook params with explicit allowed updates.
func WebhookParams(url string, secret string) *telego.SetWebhookParams {
	params := &telego.SetWebhookParams{URL: url, AllowedUpdates: AllowedUpdates()}
	if secret != "" {
		params.SecretToken = secret
	}
	return params
}

// Bot wraps telego with application configuration.
type Bot struct {
	client   *telego.Bot
	upload   *telego.Bot
	download *telego.Bot
	config   *config.Config
	logger   botpkg.Logger
}

// New creates a new Telegram bot client.
func New(cfg *config.Config, logger botpkg.Logger) (*Bot, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger required")
	}

	pollTransport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	uploadTransport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	pollClient := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: pollTransport,
	}
	uploadClient := &http.Client{
		Timeout:   15 * time.Minute,
		Transport: uploadTransport,
	}

	options := []telego.BotOption{
		telego.WithHTTPClient(pollClient),
		telego.WithLogger(telegoLogger{logger: logger}),
	}

	if cfg.GetString("BotAPI") != "" {
		options = append(options, telego.WithAPIServer(cfg.GetString("BotAPI")))
	}
	if cfg.GetBool("BotDebug") {
		options = append(options, telego.WithDebugMode())
	}

	client, err := telego.NewBot(cfg.GetString("BOT_TOKEN"), options...)
	if err != nil {
		return nil, err
	}
	uploadOptions := []telego.BotOption{
		telego.WithHTTPClient(uploadClient),
		telego.WithLogger(telegoLogger{logger: logger}),
	}
	if cfg.GetString("BotAPI") != "" {
		uploadOptions = append(uploadOptions, telego.WithAPIServer(cfg.GetString("BotAPI")))
	}
	if cfg.GetBool("BotDebug") {
		uploadOptions = append(uploadOptions, telego.WithDebugMode())
	}
	upload, err := telego.NewBot(cfg.GetString("BOT_TOKEN"), uploadOptions...)
	if err != nil {
		return nil, err
	}

	apiServer := strings.TrimRight(cfg.GetString("BotAPI"), "/")
	if apiServer != "" && apiServer != "https://api.telegram.org" {
		downloadOptions := []telego.BotOption{
			telego.WithHTTPClient(pollClient),
			telego.WithLogger(telegoLogger{logger: logger}),
		}
		if cfg.GetBool("BotDebug") {
			downloadOptions = append(downloadOptions, telego.WithDebugMode())
		}
		download, err := telego.NewBot(cfg.GetString("BOT_TOKEN"), downloadOptions...)
		if err != nil {
			return nil, err
		}
		return &Bot{client: client, upload: upload, download: download, config: cfg, logger: logger}, nil
	}

	return &Bot{client: client, upload: upload, config: cfg, logger: logger}, nil
}

// Start begins polling updates and blocks until context is canceled.
func (b *Bot) Start(ctx context.Context) {
	_ = ctx
}

// Client exposes the underlying bot client.
func (b *Bot) Client() *telego.Bot {
	return b.client
}

// UploadClient exposes a dedicated client for uploads.
func (b *Bot) UploadClient() *telego.Bot {
	if b.upload != nil {
		return b.upload
	}
	return b.client
}

// DownloadClient exposes a dedicated client for file downloads.
func (b *Bot) DownloadClient() *telego.Bot {
	if b.download != nil {
		return b.download
	}
	return b.client
}

// GetMe retrieves bot info.
func (b *Bot) GetMe(ctx context.Context) (*telego.User, error) {
	return b.client.GetMe(ctx)
}

// SendMessage is a convenience wrapper for sending a text message.
func (b *Bot) SendMessage(ctx context.Context, chatID int64, text string) (*telego.Message, error) {
	parseMode := ""
	params := &telego.SendMessageParams{ChatID: telego.ChatID{ID: chatID}, Text: text, ParseMode: parseMode}
	maybeApplyAprilFoolsTextPrankToSendMessage(b.client.Username(), params)
	return b.client.SendMessage(ctx, params)
}

// SendChatAction sends a chat action.
func (b *Bot) SendChatAction(ctx context.Context, chatID int64, action string) error {
	return b.client.SendChatAction(ctx, &telego.SendChatActionParams{ChatID: telego.ChatID{ID: chatID}, Action: action})
}

// SetWebhook configures webhook and starts the webhook handler.
func (b *Bot) SetWebhook(ctx context.Context, url string, secret string) error {
	return b.client.SetWebhook(ctx, WebhookParams(url, secret))
}

type telegoLogger struct {
	logger botpkg.Logger
}

func (l telegoLogger) Debugf(format string, args ...any) {
	if l.logger == nil {
		return
	}
	l.logger.Debug(fmt.Sprintf(format, args...))
}

func (l telegoLogger) Errorf(format string, args ...any) {
	if l.logger == nil {
		return
	}
	l.logger.Error(fmt.Sprintf(format, args...))
}

// WithTimeout returns a context with timeout for Telegram requests.
func WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
