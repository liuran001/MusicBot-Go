package handler

import (
	"sync"
	"time"
)

// Action categories throttled by ResourceRateLimiter. Each names a distinct
// class of user-initiated work that hits an external platform API (or another
// expensive resource) and is therefore abusable when tapped repeatedly.
const (
	ActionSearch    = "search"
	ActionLyric     = "lyric"
	ActionDownload  = "download"
	ActionRecognize = "recognize"
	ActionPlaylist  = "playlist"
	ActionEpisode   = "episode"
	ActionArtist    = "artist"
)

// ResourceLimit defines per-window quotas for one action across three
// dimensions. A non-positive value disables that dimension.
type ResourceLimit struct {
	Window      time.Duration
	PerUser     int
	PerPlatform int
	Global      int
}

// ResourceRateLimiter throttles user-initiated platform operations using fixed
// sliding windows across three dimensions, keyed per action category:
//
//   - per user:     how many of this action a single user may issue per window
//   - per platform: how many (from all users) may hit one platform per window
//   - global:       how many (from all users, all platforms) total per window
//
// A request is admitted only when it passes all three limits for its action; a
// rejected request consumes no quota in any dimension. Unlike the telegram
// send-side RateLimiter (a token bucket that waits), this limiter rejects
// immediately so the caller can surface a "too many requests" message.
//
// An action with no registered rule is unlimited, so unknown/unconfigured
// actions fail open rather than blocking the bot.
type ResourceRateLimiter struct {
	mu       sync.Mutex
	rules    map[string]ResourceLimit
	users    map[string][]time.Time // key: action + "\x00" + userID
	plats    map[string][]time.Time // key: action + "\x00" + platform
	global   map[string][]time.Time // key: action
	lastGC   time.Time
	gcPeriod time.Duration
}

// NewResourceRateLimiter builds a limiter with the given per-action rules. A
// nil/empty rule map makes every action unlimited.
func NewResourceRateLimiter(rules map[string]ResourceLimit) *ResourceRateLimiter {
	copied := make(map[string]ResourceLimit, len(rules))
	for action, rule := range rules {
		if rule.Window <= 0 {
			rule.Window = time.Minute
		}
		copied[action] = rule
	}
	return &ResourceRateLimiter{
		rules:    copied,
		users:    make(map[string][]time.Time),
		plats:    make(map[string][]time.Time),
		global:   make(map[string][]time.Time),
		gcPeriod: 5 * time.Minute,
		lastGC:   time.Now(),
	}
}

// pruneTimes drops timestamps older than the cutoff and reports how many
// remain, reusing the backing array.
func pruneTimes(times []time.Time, cutoff time.Time) []time.Time {
	idx := 0
	for idx < len(times) && times[idx].Before(cutoff) {
		idx++
	}
	if idx == 0 {
		return times
	}
	remaining := times[idx:]
	out := times[:len(remaining)]
	copy(out, remaining)
	return out
}

// Allow reports whether the given action by userID against platformName may
// proceed right now. When it returns true the action is recorded against every
// dimension; when false nothing is recorded. A nil limiter, an unregistered
// action, or a zeroed rule all allow the action.
func (l *ResourceRateLimiter) Allow(action string, userID int64, platformName string) bool {
	if l == nil {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	rule, ok := l.rules[action]
	if !ok {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-rule.Window)

	userKey := actionUserKey(action, userID)
	platKey := actionPlatKey(action, platformName)

	userTimes := pruneTimes(l.users[userKey], cutoff)
	platTimes := pruneTimes(l.plats[platKey], cutoff)
	globalTimes := pruneTimes(l.global[action], cutoff)

	reject := func() bool {
		// Persist the pruned slices even on rejection so stale entries don't
		// accumulate, but do not append the new timestamp.
		l.users[userKey] = userTimes
		l.plats[platKey] = platTimes
		l.global[action] = globalTimes
		return false
	}

	if rule.PerUser > 0 && len(userTimes) >= rule.PerUser {
		return reject()
	}
	if rule.PerPlatform > 0 && len(platTimes) >= rule.PerPlatform {
		return reject()
	}
	if rule.Global > 0 && len(globalTimes) >= rule.Global {
		return reject()
	}

	l.users[userKey] = append(userTimes, now)
	l.plats[platKey] = append(platTimes, now)
	l.global[action] = append(globalTimes, now)

	l.maybeGCLocked(now)
	return true
}

func actionUserKey(action string, userID int64) string {
	return action + "\x00u\x00" + itoa(userID)
}

func actionPlatKey(action, platformName string) string {
	return action + "\x00p\x00" + platformName
}

// itoa is a tiny int64→string helper to avoid pulling strconv into the hot path
// key construction (keeps allocations predictable).
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// maybeGCLocked periodically drops empty entries so the maps don't grow without
// bound as distinct users/platforms come and go. Caller holds l.mu.
func (l *ResourceRateLimiter) maybeGCLocked(now time.Time) {
	if l.gcPeriod <= 0 || now.Sub(l.lastGC) < l.gcPeriod {
		return
	}
	for key, times := range l.users {
		if len(times) == 0 {
			delete(l.users, key)
		}
	}
	for key, times := range l.plats {
		if len(times) == 0 {
			delete(l.plats, key)
		}
	}
	for key, times := range l.global {
		if len(times) == 0 {
			delete(l.global, key)
		}
	}
	l.lastGC = now
}
