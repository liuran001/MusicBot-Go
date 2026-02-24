package netease

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithRetryStopsOnSuccess(t *testing.T) {
	client := New("", true, nil)
	client.maxRetries = 2
	client.minBackoff = time.Millisecond
	client.maxBackoff = time.Millisecond

	attempts := 0
	err := client.withRetry(context.Background(), func() error {
		attempts++
		if attempts < 2 {
			return errors.New("fail")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestWithRetryRespectsContext(t *testing.T) {
	client := New("", true, nil)
	client.maxRetries = 5
	client.minBackoff = time.Millisecond
	client.maxBackoff = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.withRetry(ctx, func() error {
		return errors.New("fail")
	})
	if err == nil {
		t.Fatalf("expected context error")
	}
}
