package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// lyricCountingPlatform counts GetLyrics calls so a test can assert how often
// the underlying platform API is actually hit. It embeds *stubPlatform to
// inherit the full platform.Platform interface, overriding only GetLyrics.
type lyricCountingPlatform struct {
	*stubPlatform
	calls int64
}

func newLyricCountingPlatform(name string) *lyricCountingPlatform {
	return &lyricCountingPlatform{stubPlatform: newStubPlatform(name)}
}

func (p *lyricCountingPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	atomic.AddInt64(&p.calls, 1)
	return &platform.Lyrics{Plain: "la la la"}, nil
}

// resetLyricCache clears the package-level cache so tests don't bleed into each
// other through the shared store.
func resetLyricCache() {
	lyricFetchCache = newTTLStore[*platform.Lyrics](30 * time.Minute)
}

func TestGetLyricsLimitedCachesAcrossFormatSwitches(t *testing.T) {
	resetLyricCache()
	plat := newLyricCountingPlatform("netease")
	// A deliberately tiny per-user lyric quota: 1 fetch per window.
	limiter := NewResourceRateLimiter(map[string]ResourceLimit{
		ActionLyric: {Window: time.Minute, PerUser: 1},
	})

	// First fetch: cache miss, hits the API, consumes the single unit of quota.
	if _, err := getLyricsLimited(context.Background(), limiter, 1, plat, "netease", "track-1"); err != nil {
		t.Fatalf("first fetch should succeed, got %v", err)
	}
	if got := atomic.LoadInt64(&plat.calls); got != 1 {
		t.Fatalf("expected 1 API call after first fetch, got %d", got)
	}

	// Simulate 20 format switches/toggles for the SAME track. These must all be
	// served from cache: no extra API calls, and crucially no quota consumed even
	// though the per-user quota is already exhausted.
	for i := 0; i < 20; i++ {
		lyrics, err := getLyricsLimited(context.Background(), limiter, 1, plat, "netease", "track-1")
		if err != nil {
			t.Fatalf("format switch %d should hit cache and succeed, got %v", i+1, err)
		}
		if lyrics == nil || lyrics.Plain != "la la la" {
			t.Fatalf("format switch %d returned unexpected lyrics %+v", i+1, lyrics)
		}
	}
	if got := atomic.LoadInt64(&plat.calls); got != 1 {
		t.Fatalf("format switches must not re-hit the API; expected 1 call, got %d", got)
	}
}

func TestGetLyricsLimitedThrottlesDistinctTracks(t *testing.T) {
	resetLyricCache()
	plat := newLyricCountingPlatform("netease")
	limiter := NewResourceRateLimiter(map[string]ResourceLimit{
		ActionLyric: {Window: time.Minute, PerUser: 2},
	})

	// Two distinct tracks consume the quota (cache misses).
	if _, err := getLyricsLimited(context.Background(), limiter, 1, plat, "netease", "a"); err != nil {
		t.Fatalf("track a should succeed, got %v", err)
	}
	if _, err := getLyricsLimited(context.Background(), limiter, 1, plat, "netease", "b"); err != nil {
		t.Fatalf("track b should succeed, got %v", err)
	}
	// Third distinct track is over quota → rejected before the API call.
	if _, err := getLyricsLimited(context.Background(), limiter, 1, plat, "netease", "c"); err != platform.ErrRateLimited {
		t.Fatalf("track c should be rate limited, got %v", err)
	}
	if got := atomic.LoadInt64(&plat.calls); got != 2 {
		t.Fatalf("rejected fetch must not hit the API; expected 2 calls, got %d", got)
	}
	// But re-viewing an already-cached track (a) is still free despite being over quota.
	if _, err := getLyricsLimited(context.Background(), limiter, 1, plat, "netease", "a"); err != nil {
		t.Fatalf("cached track a should still be served, got %v", err)
	}
}

func TestGetLyricsLimitedNilLimiterFetches(t *testing.T) {
	resetLyricCache()
	plat := newLyricCountingPlatform("netease")
	var limiter *ResourceRateLimiter
	if _, err := getLyricsLimited(context.Background(), limiter, 1, plat, "netease", "x"); err != nil {
		t.Fatalf("nil limiter should fetch, got %v", err)
	}
	if got := atomic.LoadInt64(&plat.calls); got != 1 {
		t.Fatalf("expected 1 API call, got %d", got)
	}
}
