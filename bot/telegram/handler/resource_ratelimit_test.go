package handler

import (
	"testing"
	"time"
)

func searchRule(window time.Duration, perUser, perPlatform, global int) map[string]ResourceLimit {
	return map[string]ResourceLimit{
		ActionSearch: {Window: window, PerUser: perUser, PerPlatform: perPlatform, Global: global},
	}
}

func TestResourceRateLimiterPerUser(t *testing.T) {
	l := NewResourceRateLimiter(searchRule(time.Minute, 5, 100, 100))
	for i := 0; i < 5; i++ {
		if !l.Allow(ActionSearch, 1, "netease") {
			t.Fatalf("request %d for user 1 should be allowed", i+1)
		}
	}
	if l.Allow(ActionSearch, 1, "netease") {
		t.Fatal("6th request for user 1 should be rejected (per-user limit 5)")
	}
	if !l.Allow(ActionSearch, 2, "netease") {
		t.Fatal("user 2 first request should be allowed")
	}
}

func TestResourceRateLimiterPerPlatform(t *testing.T) {
	l := NewResourceRateLimiter(searchRule(time.Minute, 100, 10, 100))
	for i := 0; i < 10; i++ {
		if !l.Allow(ActionSearch, int64(i), "qq") {
			t.Fatalf("request %d on qq should be allowed", i+1)
		}
	}
	if l.Allow(ActionSearch, 999, "qq") {
		t.Fatal("11th request on qq should be rejected (per-platform limit 10)")
	}
	if !l.Allow(ActionSearch, 999, "netease") {
		t.Fatal("first request on netease should be allowed")
	}
}

func TestResourceRateLimiterGlobal(t *testing.T) {
	l := NewResourceRateLimiter(searchRule(time.Minute, 100, 100, 20))
	allowed := 0
	for i := 0; i < 25; i++ {
		user := int64(i)
		plat := []string{"netease", "qq", "kugou", "bilibili"}[i%4]
		if l.Allow(ActionSearch, user, plat) {
			allowed++
		}
	}
	if allowed != 20 {
		t.Fatalf("global limit should admit exactly 20, got %d", allowed)
	}
}

func TestResourceRateLimiterRejectionConsumesNoQuota(t *testing.T) {
	l := NewResourceRateLimiter(searchRule(time.Minute, 1, 100, 100))
	if !l.Allow(ActionSearch, 1, "netease") {
		t.Fatal("user 1 first request should be allowed")
	}
	for i := 0; i < 50; i++ {
		if l.Allow(ActionSearch, 1, "netease") {
			t.Fatal("user 1 over-limit request should be rejected")
		}
	}
	for i := 2; i <= 100; i++ {
		if !l.Allow(ActionSearch, int64(i), "netease") {
			t.Fatalf("user %d should be allowed; rejections must not consume platform quota", i)
		}
	}
}

func TestResourceRateLimiterWindowExpiry(t *testing.T) {
	l := NewResourceRateLimiter(searchRule(50*time.Millisecond, 2, 100, 100))
	if !l.Allow(ActionSearch, 1, "netease") || !l.Allow(ActionSearch, 1, "netease") {
		t.Fatal("first two requests should be allowed")
	}
	if l.Allow(ActionSearch, 1, "netease") {
		t.Fatal("third request should be rejected within window")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.Allow(ActionSearch, 1, "netease") {
		t.Fatal("request after window expiry should be allowed again")
	}
}

func TestResourceRateLimiterNilSafe(t *testing.T) {
	var l *ResourceRateLimiter
	if !l.Allow(ActionSearch, 1, "netease") {
		t.Fatal("nil limiter should allow all requests")
	}
}

func TestResourceRateLimiterUnregisteredActionUnlimited(t *testing.T) {
	// Only search is registered; lyric has no rule and must fail open.
	l := NewResourceRateLimiter(searchRule(time.Minute, 1, 1, 1))
	for i := 0; i < 100; i++ {
		if !l.Allow(ActionLyric, 1, "netease") {
			t.Fatal("unregistered action should always be allowed")
		}
	}
}

func TestResourceRateLimiterDisabledDimension(t *testing.T) {
	l := NewResourceRateLimiter(searchRule(time.Minute, 0, 0, 3))
	for i := 0; i < 3; i++ {
		if !l.Allow(ActionSearch, 1, "netease") {
			t.Fatalf("request %d should be allowed (per-user disabled)", i+1)
		}
	}
	if l.Allow(ActionSearch, 1, "netease") {
		t.Fatal("4th request should hit global cap of 3")
	}
}

func TestResourceRateLimiterActionsIndependent(t *testing.T) {
	// search and lyric each get perUser=1; exhausting one must not affect the other.
	rules := map[string]ResourceLimit{
		ActionSearch: {Window: time.Minute, PerUser: 1},
		ActionLyric:  {Window: time.Minute, PerUser: 1},
	}
	l := NewResourceRateLimiter(rules)
	if !l.Allow(ActionSearch, 1, "netease") {
		t.Fatal("first search should be allowed")
	}
	if l.Allow(ActionSearch, 1, "netease") {
		t.Fatal("second search should be rejected")
	}
	// Lyric quota is independent and still full.
	if !l.Allow(ActionLyric, 1, "netease") {
		t.Fatal("first lyric should be allowed despite search being exhausted")
	}
}
