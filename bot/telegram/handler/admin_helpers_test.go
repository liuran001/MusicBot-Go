package handler

import (
	"sync"
	"testing"
)

// TestAdminSetConcurrentReplaceAndContains exercises the exact race that the
// old shared-map implementation suffered from: many handler goroutines calling
// isBotAdmin (read) while /reload swaps the admin set (write). With the previous
// in-place map mutation this triggered "fatal error: concurrent map read and
// map write". Run with -race to verify safety.
func TestAdminSetConcurrentReplaceAndContains(t *testing.T) {
	set := NewAdminSet(map[int64]struct{}{1: {}, 2: {}})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Readers: simulate concurrent handler goroutines checking admin status.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = isBotAdmin(set, 1)
					_ = set.Contains(99)
				}
			}
		}()
	}

	// Writer: simulate /reload swapping the admin set repeatedly.
	for i := 0; i < 1000; i++ {
		set.Replace(map[int64]struct{}{int64(i): {}, 1: {}})
	}
	close(stop)
	wg.Wait()

	if !set.Contains(1) {
		t.Fatal("expected id 1 to remain an admin after reloads")
	}
}

func TestAdminSetReplacePublishesNewMembership(t *testing.T) {
	set := NewAdminSet(map[int64]struct{}{1: {}})
	if !set.Contains(1) || set.Contains(2) {
		t.Fatalf("unexpected initial membership")
	}
	// Hot reload to a disjoint set; all holders of the same *AdminSet must see it.
	set.Replace(map[int64]struct{}{2: {}})
	if set.Contains(1) {
		t.Error("id 1 should no longer be admin after reload")
	}
	if !set.Contains(2) {
		t.Error("id 2 should be admin after reload")
	}
}

func TestAdminSetNilSafe(t *testing.T) {
	var set *AdminSet
	if set.Contains(1) {
		t.Error("nil AdminSet must not report membership")
	}
	if isBotAdmin(nil, 1) {
		t.Error("isBotAdmin(nil, ...) must be false")
	}
	// Replace on nil receiver must not panic.
	set.Replace(map[int64]struct{}{1: {}})
}
