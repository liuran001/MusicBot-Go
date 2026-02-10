package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolConcurrencyLimit(t *testing.T) {
	pool := New(2)
	defer func() {
		_ = pool.Shutdown(context.Background())
	}()

	var current int32
	var max int32

	work := func() {
		val := atomic.AddInt32(&current, 1)
		for {
			prev := atomic.LoadInt32(&max)
			if val <= prev {
				break
			}
			if atomic.CompareAndSwapInt32(&max, prev, val) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&current, -1)
	}

	for i := 0; i < 4; i++ {
		if err := pool.Submit(work); err != nil {
			t.Fatalf("submit failed: %v", err)
		}
	}

	_ = pool.Shutdown(context.Background())
	if max > 2 {
		t.Fatalf("expected max concurrency <= 2, got %d", max)
	}
}

func TestPoolSubmitAfterShutdown(t *testing.T) {
	pool := New(1)
	_ = pool.Shutdown(context.Background())
	if err := pool.Submit(func() {}); err == nil {
		t.Fatal("expected error after shutdown")
	}
}

func TestPoolSubmitWaitContextTimeout(t *testing.T) {
	pool := New(1)
	defer func() {
		_ = pool.Shutdown(context.Background())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := pool.SubmitWaitContext(ctx, func() error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}
