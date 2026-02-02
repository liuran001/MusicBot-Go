package telegram

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/time/rate"
)

type Logger interface {
	Error(msg string, args ...any)
	Warn(msg string, args ...any)
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
}

type RateLimiter struct {
	limiters map[int64]*rate.Limiter
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
	logger   Logger
}

func NewRateLimiter(msgPerSec float64, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[int64]*rate.Limiter),
		rate:     rate.Limit(msgPerSec),
		burst:    burst,
	}
}

func (rl *RateLimiter) SetLogger(logger Logger) {
	rl.logger = logger
}

func (rl *RateLimiter) logError(msg string, args ...any) {
	if rl.logger != nil {
		rl.logger.Error(msg, args...)
	} else {
		log.Printf("ERROR: "+msg, args...)
	}
}

func (rl *RateLimiter) getLimiter(chatID int64) *rate.Limiter {
	rl.mu.RLock()
	limiter, exists := rl.limiters[chatID]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if limiter, exists := rl.limiters[chatID]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(rl.rate, rl.burst)
	rl.limiters[chatID] = limiter
	return limiter
}

func (rl *RateLimiter) Wait(ctx context.Context, chatID int64) error {
	limiter := rl.getLimiter(chatID)
	return limiter.Wait(ctx)
}

type APIError struct {
	Code       int
	Message    string
	RetryAfter int
}

func (e *APIError) Error() string {
	return e.Message
}

func parseRetryAfter(err error) (int, bool) {
	if err == nil {
		return 0, false
	}

	errMsg := err.Error()
	if len(errMsg) == 0 {
		return 0, false
	}

	var retryAfter int
	if parsed, parseErr := strconv.Atoi(errMsg); parseErr == nil {
		retryAfter = parsed
		return retryAfter, retryAfter > 0
	}

	return retryAfter, retryAfter > 0
}

func isMessageNotModified(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "message is not modified")
}

func WithRetry(ctx context.Context, rl *RateLimiter, chatID int64, fn func() error) error {
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := rl.Wait(ctx, chatID); err != nil {
			return err
		}

		err := fn()
		if err == nil {
			return nil
		}

		retryAfter, shouldRetry := parseRetryAfter(err)
		if !shouldRetry {
			return err
		}

		if attempt < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(retryAfter) * time.Second):
			}
		}
	}

	return &APIError{Code: 429, Message: "max retries exceeded"}
}

func extractChatID(chatID any) int64 {
	switch v := chatID.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		id, _ := strconv.ParseInt(v, 10, 64)
		return id
	default:
		return 0
	}
}

func SendMessageWithRetry(ctx context.Context, rl *RateLimiter, b *bot.Bot, params *bot.SendMessageParams) (*models.Message, error) {
	var result *models.Message
	var lastErr error

	chatID := extractChatID(params.ChatID)
	err := WithRetry(ctx, rl, chatID, func() error {
		msg, err := b.SendMessage(ctx, params)
		if err != nil {
			lastErr = err
			return err
		}
		result = msg
		return nil
	})

	if err != nil {
		if rl != nil {
			rl.logError("SendMessage failed", "chat_id", chatID, "error", lastErr)
		}
		return result, lastErr
	}
	return result, nil
}

func EditMessageTextWithRetry(ctx context.Context, rl *RateLimiter, b *bot.Bot, params *bot.EditMessageTextParams) (*models.Message, error) {
	var result *models.Message
	var lastErr error

	chatID := extractChatID(params.ChatID)
	err := WithRetry(ctx, rl, chatID, func() error {
		msg, err := b.EditMessageText(ctx, params)
		if err != nil {
			lastErr = err
			return err
		}
		result = msg
		return nil
	})

	if err != nil {
		if rl != nil && !isMessageNotModified(lastErr) {
			rl.logError("EditMessageText failed", "chat_id", chatID, "message_id", params.MessageID, "error", lastErr)
		}
		return result, lastErr
	}
	return result, nil
}

func DeleteMessageWithRetry(ctx context.Context, rl *RateLimiter, b *bot.Bot, params *bot.DeleteMessageParams) error {
	chatID := extractChatID(params.ChatID)
	err := WithRetry(ctx, rl, chatID, func() error {
		_, err := b.DeleteMessage(ctx, params)
		return err
	})

	if err != nil && rl != nil {
		rl.logError("DeleteMessage failed", "chat_id", chatID, "message_id", params.MessageID, "error", err)
	}
	return err
}

func SendAudioWithRetry(ctx context.Context, rl *RateLimiter, b *bot.Bot, params *bot.SendAudioParams) (*models.Message, error) {
	var result *models.Message
	var lastErr error

	chatID := extractChatID(params.ChatID)
	err := WithRetry(ctx, rl, chatID, func() error {
		msg, err := b.SendAudio(ctx, params)
		if err != nil {
			lastErr = err
			return err
		}
		result = msg
		return nil
	})

	if err != nil {
		if rl != nil {
			rl.logError("SendAudio failed", "chat_id", chatID, "error", lastErr)
		}
		return result, lastErr
	}
	return result, nil
}
