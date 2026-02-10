package telegram

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		ok   bool
	}{
		{name: "nil", err: nil, want: 0, ok: false},
		{name: "plain int", err: errors.New("3"), want: 3, ok: true},
		{name: "api error", err: &APIError{RetryAfter: 9, Message: "rate"}, want: 9, ok: true},
		{name: "text pattern", err: errors.New("Too Many Requests: retry after 4"), want: 4, ok: true},
		{name: "invalid", err: errors.New("other error"), want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.err)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseRetryAfter() = (%d,%v), want (%d,%v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestWithRetryNilRateLimiter(t *testing.T) {
	calls := 0
	err := WithRetry(context.Background(), nil, 0, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("WithRetry returned err: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestWithRetryContextCancelOnRetry(t *testing.T) {
	rl := NewRateLimiter(1000, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := WithRetry(ctx, rl, 1, func() error {
		return fmt.Errorf("retry after 10")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
