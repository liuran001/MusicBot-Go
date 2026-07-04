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

func TestRateLimiterSendQueueSeparatesAPIWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rl := NewRateLimiter(1000, 100)
	rl.StartQueue(ctx, 1, 4)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- WithRetry(ctx, rl, 1, func() error {
			started <- struct{}{}
			<-release
			return nil
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first send task did not start")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- WithRetry(ctx, rl, 2, func() error { return nil })
	}()

	deadline := time.Now().Add(time.Second)
	for {
		waiting, running, capacity, workers := rl.QueueStats()
		if waiting == 1 && running == 1 {
			if capacity != 4 || workers != 1 {
				t.Fatalf("queue stats = capacity %d, workers %d; want 4, 1", capacity, workers)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue stats never reached waiting=1 running=1; got waiting=%d running=%d", waiting, running)
		}
		time.Sleep(time.Millisecond)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first send task failed: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second send task failed: %v", err)
	}
}

func TestRateLimiterSendQueueCancellationUnblocksCallers(t *testing.T) {
	queueCtx, cancelQueue := context.WithCancel(context.Background())
	rl := NewRateLimiter(1000, 100)
	rl.StartQueue(queueCtx, 1, 4)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- WithRetry(context.Background(), rl, 1, func() error {
			started <- struct{}{}
			<-release
			return nil
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first send task did not start")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- WithRetry(context.Background(), rl, 2, func() error { return nil })
	}()

	deadline := time.Now().Add(time.Second)
	for {
		waiting, _, _, _ := rl.QueueStats()
		if waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second send task did not enter the queue")
		}
		time.Sleep(time.Millisecond)
	}

	cancelQueue()
	for name, done := range map[string]<-chan error{"first": firstDone, "second": secondDone} {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s caller error = %v, want context.Canceled", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s caller remained blocked after send queue cancellation", name)
		}
	}
	close(release)
}
