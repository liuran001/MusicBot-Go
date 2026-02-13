package telegram

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	"golang.org/x/time/rate"
)

const (
	defaultLimiterEntryTTL      = 30 * time.Minute
	defaultLimiterCleanupPeriod = 5 * time.Minute
)

type Logger interface {
	Error(msg string, args ...any)
	Warn(msg string, args ...any)
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
}

type RateLimiter struct {
	limiters        map[int64]*chatLimiter
	mu              sync.RWMutex
	rate            rate.Limit
	burst           int
	globalLimiter   *rate.Limiter
	logger          Logger
	entryTTL        time.Duration
	cleanupInterval time.Duration
	lastCleanup     time.Time
}

type chatLimiter struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

func NewRateLimiter(msgPerSec float64, burst int) *RateLimiter {
	return NewRateLimiterWithGlobal(msgPerSec, burst, 0, 0)
}

func NewRateLimiterWithGlobal(msgPerSec float64, burst int, globalPerSec float64, globalBurst int) *RateLimiter {
	var globalLimiter *rate.Limiter
	if globalPerSec > 0 && globalBurst > 0 {
		globalLimiter = rate.NewLimiter(rate.Limit(globalPerSec), globalBurst)
	}

	return &RateLimiter{
		limiters:        make(map[int64]*chatLimiter),
		rate:            rate.Limit(msgPerSec),
		burst:           burst,
		globalLimiter:   globalLimiter,
		entryTTL:        defaultLimiterEntryTTL,
		cleanupInterval: defaultLimiterCleanupPeriod,
		lastCleanup:     time.Now(),
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
	now := time.Now()
	rl.maybeCleanup(now)

	rl.mu.RLock()
	_, exists := rl.limiters[chatID]
	rl.mu.RUnlock()

	if exists {
		rl.mu.Lock()
		if current, ok := rl.limiters[chatID]; ok {
			current.lastUsed = now
			limiter := current.limiter
			rl.mu.Unlock()
			return limiter
		}
		rl.mu.Unlock()
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if current, exists := rl.limiters[chatID]; exists {
		current.lastUsed = now
		return current.limiter
	}

	limiter := rate.NewLimiter(rl.rate, rl.burst)
	rl.limiters[chatID] = &chatLimiter{
		limiter:  limiter,
		lastUsed: now,
	}
	return limiter
}

func (rl *RateLimiter) maybeCleanup(now time.Time) {
	rl.mu.RLock()
	cleanupInterval := rl.cleanupInterval
	entryTTL := rl.entryTTL
	lastCleanup := rl.lastCleanup
	rl.mu.RUnlock()

	if cleanupInterval <= 0 || entryTTL <= 0 {
		return
	}
	if now.Sub(lastCleanup) < cleanupInterval {
		return
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if now.Sub(rl.lastCleanup) < rl.cleanupInterval {
		return
	}

	for chatID, limiter := range rl.limiters {
		if now.Sub(limiter.lastUsed) > rl.entryTTL {
			delete(rl.limiters, chatID)
		}
	}
	rl.lastCleanup = now
}

func (rl *RateLimiter) Wait(ctx context.Context, chatID int64) error {
	if rl.globalLimiter != nil {
		if err := rl.globalLimiter.Wait(ctx); err != nil {
			return err
		}
	}
	limiter := rl.getLimiter(chatID)
	return limiter.Wait(ctx)
}

type APIError struct {
	Code       int
	Message    string
	RetryAfter int
}

var retryAfterPattern = regexp.MustCompile(`(?i)retry\s+after[:\s]+(\d+)`)

func (e *APIError) Error() string {
	return e.Message
}

func parseRetryAfter(err error) (int, bool) {
	if err == nil {
		return 0, false
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter, true
	}

	errMsg := err.Error()
	if len(errMsg) == 0 {
		return 0, false
	}
	if matches := retryAfterPattern.FindStringSubmatch(errMsg); len(matches) == 2 {
		if parsed, parseErr := strconv.Atoi(matches[1]); parseErr == nil {
			return parsed, parsed > 0
		}
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

func IsMessageNotModified(err error) bool {
	return isMessageNotModified(err)
}

func WithRetry(ctx context.Context, rl *RateLimiter, chatID int64, fn func() error) error {
	if fn == nil {
		return nil
	}
	if rl == nil {
		return fn()
	}
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
		if rl != nil {
			rl.logError("telegram request rate limited, will retry", "chat_id", chatID, "attempt", attempt+1, "retry_after_seconds", retryAfter, "error", err)
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
	case telego.ChatID:
		if v.ID != 0 {
			return v.ID
		}
		return 0
	case *telego.ChatID:
		if v == nil {
			return 0
		}
		if v.ID != 0 {
			return v.ID
		}
		return 0
	case string:
		id, _ := strconv.ParseInt(v, 10, 64)
		return id
	default:
		return 0
	}
}

func SendMessageWithRetry(ctx context.Context, rl *RateLimiter, b *telego.Bot, params *telego.SendMessageParams) (*telego.Message, error) {
	var result *telego.Message
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
		retErr := lastErr
		if retErr == nil {
			retErr = err
		}
		if rl != nil {
			rl.logError("SendMessage failed", "chat_id", chatID, "error", retErr)
		}
		return result, retErr
	}
	return result, nil
}

func EditMessageTextWithRetry(ctx context.Context, rl *RateLimiter, b *telego.Bot, params *telego.EditMessageTextParams) (*telego.Message, error) {
	var result *telego.Message
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
		retErr := lastErr
		if retErr == nil {
			retErr = err
		}
		if rl != nil && !isMessageNotModified(retErr) {
			rl.logError("EditMessageText failed", "chat_id", chatID, "message_id", params.MessageID, "error", retErr)
		}
		return result, retErr
	}
	return result, nil
}

// EditMessageTextBestEffort edits message text with rate-limit wait but drops 429 retries.
// Suitable for high-frequency progress updates where stale updates can be skipped.
func EditMessageTextBestEffort(ctx context.Context, rl *RateLimiter, b *telego.Bot, params *telego.EditMessageTextParams) (*telego.Message, error) {
	chatID := extractChatID(params.ChatID)
	if rl != nil {
		if err := rl.Wait(ctx, chatID); err != nil {
			return nil, err
		}
	}

	msg, err := b.EditMessageText(ctx, params)
	if err == nil {
		return msg, nil
	}
	if isMessageNotModified(err) {
		return msg, nil
	}
	if _, shouldDrop := parseRetryAfter(err); shouldDrop {
		return nil, nil
	}
	return msg, err
}

func EditMessageReplyMarkupWithRetry(ctx context.Context, rl *RateLimiter, b *telego.Bot, params *telego.EditMessageReplyMarkupParams) (*telego.Message, error) {
	var result *telego.Message
	var lastErr error

	chatID := extractChatID(params.ChatID)
	err := WithRetry(ctx, rl, chatID, func() error {
		msg, err := b.EditMessageReplyMarkup(ctx, params)
		if err != nil {
			lastErr = err
			return err
		}
		result = msg
		return nil
	})

	if err != nil {
		retErr := lastErr
		if retErr == nil {
			retErr = err
		}
		if rl != nil && !isMessageNotModified(retErr) {
			rl.logError("EditMessageReplyMarkup failed", "chat_id", chatID, "message_id", params.MessageID, "error", retErr)
		}
		return result, retErr
	}
	return result, nil
}

func DeleteMessageWithRetry(ctx context.Context, rl *RateLimiter, b *telego.Bot, params *telego.DeleteMessageParams) error {
	chatID := extractChatID(params.ChatID)
	err := WithRetry(ctx, rl, chatID, func() error {
		return b.DeleteMessage(ctx, params)
	})

	if err != nil && rl != nil {
		rl.logError("DeleteMessage failed", "chat_id", chatID, "message_id", params.MessageID, "error", err)
	}
	return err
}

func SendAudioWithRetry(ctx context.Context, rl *RateLimiter, b *telego.Bot, params *telego.SendAudioParams) (*telego.Message, error) {
	var result *telego.Message
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
		retErr := lastErr
		if retErr == nil {
			retErr = err
		}
		if rl != nil {
			rl.logError("SendAudio failed", "chat_id", chatID, "error", retErr)
		}
		return result, retErr
	}
	return result, nil
}

func EditMessageMediaWithRetry(ctx context.Context, rl *RateLimiter, b *telego.Bot, params *telego.EditMessageMediaParams) (*telego.Message, error) {
	var result *telego.Message
	var lastErr error

	chatID := extractChatID(params.ChatID)
	err := WithRetry(ctx, rl, chatID, func() error {
		msg, err := b.EditMessageMedia(ctx, params)
		if err != nil {
			lastErr = err
			return err
		}
		result = msg
		return nil
	})

	if err != nil {
		retErr := lastErr
		if retErr == nil {
			retErr = err
		}
		if rl != nil && !isMessageNotModified(retErr) {
			rl.logError("EditMessageMedia failed", "chat_id", chatID, "message_id", params.MessageID, "error", retErr)
		}
		return result, retErr
	}
	return result, nil
}
