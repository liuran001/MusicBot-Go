package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// serialStubPlatform is a stubPlatform that also implements
// platform.SerialDownloadGate, reporting that every download must be
// serialized. Used to exercise the per-platform size-1 gate.
type serialStubPlatform struct {
	*stubPlatform
}

func (p *serialStubPlatform) NeedsSerialDownload(trackID string, quality platform.Quality) bool {
	return true
}

func newSerialManager(t *testing.T, name string) *stubPlatformManager {
	t.Helper()
	m := newStubManager()
	m.platforms[name] = &serialStubPlatform{stubPlatform: newStubPlatform(name)}
	return m
}

// TestAcquireDownloadSlotGlobalLimit verifies the global concurrency pool caps
// simultaneous downloads at the limiter size, and that release frees a slot.
func TestAcquireDownloadSlotGlobalLimit(t *testing.T) {
	h := &MusicHandler{Limiter: make(chan struct{}, 2)}
	ctx := context.Background()

	r1, err := h.acquireDownloadSlot(ctx, "", "t1", platform.QualityHigh, nil)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	r2, err := h.acquireDownloadSlot(ctx, "", "t2", platform.QualityHigh, nil)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	if _, running, _ := h.DownloadQueueStats(); running != 2 {
		t.Fatalf("running = %d, want 2", running)
	}

	// Third acquire must block until a slot frees; with a cancelable ctx it
	// should return the ctx error rather than starting.
	cctx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if _, err := h.acquireDownloadSlot(cctx, "", "t3", platform.QualityHigh, nil); err == nil {
		t.Fatal("third acquire should have been canceled while pool full")
	}

	r1()
	r2()
	if _, running, _ := h.DownloadQueueStats(); running != 0 {
		t.Fatalf("running after release = %d, want 0", running)
	}
}

// TestAcquireDownloadSlotQueueCap verifies the total waiting-queue cap rejects
// the request that would exceed it with errDownloadQueueOverloaded.
func TestAcquireDownloadSlotQueueCap(t *testing.T) {
	// Pool of 1 (so the 2nd+ wait), waiting cap of 1.
	h := &MusicHandler{Limiter: make(chan struct{}, 1), DownloadQueueWaitLimit: 1}
	ctx := context.Background()

	r1, err := h.acquireDownloadSlot(ctx, "", "t1", platform.QualityHigh, nil)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer r1()

	// One waiter is allowed; block it in a goroutine on a context we control.
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	waiterStarted := make(chan struct{})
	queuedCalled := make(chan struct{}, 1)
	go func() {
		close(waiterStarted)
		_, _ = h.acquireDownloadSlot(wctx, "", "t2", platform.QualityHigh, func() {
			queuedCalled <- struct{}{}
		})
	}()
	<-waiterStarted
	// Wait until the waiter is actually counted as waiting.
	deadline := time.Now().Add(time.Second)
	for {
		if waiting, _, _ := h.DownloadQueueStats(); waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter never entered waiting state")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// onQueued must have fired exactly because the task had to wait.
	select {
	case <-queuedCalled:
	case <-time.After(time.Second):
		t.Fatal("onQueued was not called for a waiting task")
	}

	// The next request exceeds the waiting cap and must be rejected immediately.
	if _, err := h.acquireDownloadSlot(ctx, "", "t3", platform.QualityHigh, nil); err != errDownloadQueueOverloaded {
		t.Fatalf("over-cap acquire err = %v, want errDownloadQueueOverloaded", err)
	}
}

// TestSerialGateSerializesDownloads verifies that for a platform requiring
// serialization, only one download holds the gate at a time even when the
// global pool has spare capacity.
func TestSerialGateSerializesDownloads(t *testing.T) {
	m := newSerialManager(t, "applemusic")
	// Pool of 4 (plenty), so any serialization is purely from the gate.
	h := &MusicHandler{Limiter: make(chan struct{}, 4), PlatformManager: m}
	ctx := context.Background()

	r1, err := h.acquireDownloadSlot(ctx, "applemusic", "t1", platform.QualityLossless, nil)
	if err != nil {
		t.Fatalf("first serial acquire: %v", err)
	}

	// Second serial download must NOT start while the first holds the gate,
	// even though global slots are available. Confirm by cancelation.
	cctx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if _, err := h.acquireDownloadSlot(cctx, "applemusic", "t2", platform.QualityLossless, nil); err == nil {
		t.Fatal("second serial download should block on the gate")
	}

	// After releasing the first, a second serial download proceeds.
	r1()
	r2, err := h.acquireDownloadSlot(ctx, "applemusic", "t2", platform.QualityLossless, nil)
	if err != nil {
		t.Fatalf("second serial acquire after release: %v", err)
	}
	r2()
}

// TestSerialGateConcurrencyStress hammers the gate from many goroutines and
// asserts the in-flight serialized count never exceeds 1.
func TestSerialGateConcurrencyStress(t *testing.T) {
	m := newSerialManager(t, "applemusic")
	h := &MusicHandler{Limiter: make(chan struct{}, 8), PlatformManager: m}
	ctx := context.Background()

	var inFlight int32
	var maxSeen int32
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := h.acquireDownloadSlot(ctx, "applemusic", "t", platform.QualityLossless, nil)
			if err != nil {
				return
			}
			mu.Lock()
			inFlight++
			if inFlight > maxSeen {
				maxSeen = inFlight
			}
			mu.Unlock()

			time.Sleep(time.Millisecond)

			mu.Lock()
			inFlight--
			mu.Unlock()
			release()
		}()
	}
	wg.Wait()
	if maxSeen > 1 {
		t.Fatalf("serial gate allowed %d concurrent downloads, want <= 1", maxSeen)
	}
}
