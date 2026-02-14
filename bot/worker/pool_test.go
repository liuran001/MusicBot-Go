package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
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

func TestPoolPanicHandlerCalled(t *testing.T) {
	pool := New(1)
	defer func() {
		_ = pool.Shutdown(context.Background())
	}()

	called := make(chan struct{}, 1)
	pool.SetPanicHandler(func(recovered any, stack []byte) {
		if recovered == nil {
			t.Errorf("expected recovered value")
		}
		if len(stack) == 0 || !strings.Contains(string(stack), "TestPoolPanicHandlerCalled") {
			t.Errorf("expected stack trace to include test function")
		}
		select {
		case called <- struct{}{}:
		default:
		}
	})

	if err := pool.Submit(func() { panic("boom") }); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("panic handler was not called")
	}
}

func TestShutdownDrainsQueuedTasks(t *testing.T) {
	pool := New(1)

	var done int32
	task := func() {
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&done, 1)
	}

	for i := 0; i < 3; i++ {
		if err := pool.Submit(task); err != nil {
			t.Fatalf("submit failed: %v", err)
		}
	}

	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	if got := atomic.LoadInt32(&done); got != 3 {
		t.Fatalf("expected all queued tasks drained, got %d", got)
	}
}

func TestStopNowRejectsFurtherSubmissions(t *testing.T) {
	pool := New(1)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	if err := pool.Submit(func() {
		started <- struct{}{}
		<-release
	}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker task did not start")
	}

	pool.StopNow()
	if err := pool.Submit(func() {}); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("expected ErrPoolClosed after StopNow, got %v", err)
	}

	close(release)
}

func TestConcurrentSubmitAfterShutdownNoPanic(t *testing.T) {
	pool := New(2)
	var wg sync.WaitGroup

	shutdownDone := make(chan struct{})
	go func() {
		_ = pool.Shutdown(context.Background())
		close(shutdownDone)
	}()

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pool.Submit(func() {})
		}()
	}

	wg.Wait()
	<-shutdownDone
}
