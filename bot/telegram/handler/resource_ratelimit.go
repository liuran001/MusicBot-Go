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

// ResourceLimit defines per-window quotas for one action across four
// dimensions. A non-positive value disables that dimension.
type ResourceLimit struct {
	Window      time.Duration
	PerUser     int
	PerChat     int
	PerPlatform int
	Global      int
}

// ResourceRateLimiter throttles user-initiated platform operations using fixed
// sliding windows across four dimensions, keyed per action category:
//
//   - per user:     how many of this action a single user may issue per window
//   - per chat:     how many this action one conversation may issue per window
//   - per platform: how many (from all users) may hit one platform per window
//   - global:       how many (from all users, all chats, all platforms) total
//
// A request is admitted only when it passes all enabled limits for its action; a
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
	chats    map[string][]time.Time // key: action + "\x00" + chatID
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
		chats:    make(map[string][]time.Time),
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
	return l.AllowFor(action, userID, 0, platformName)
}

// AllowFor is Allow with an additional chat/conversation dimension. A zero
// userID or chatID means that dimension is unavailable and is skipped. This is
// important for pure inline mode, which has a requester but no originating chat:
// its per-user and global quotas still apply without making all inline users
// share one synthetic conversation bucket.
func (l *ResourceRateLimiter) AllowFor(action string, userID, chatID int64, platformName string) bool {
	if l == nil {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	rule, ok := l.rules[action]
	if !ok {
		return true
	}
	useUser := rule.PerUser > 0 && userID != 0
	useChat := rule.PerChat > 0 && chatID != 0
	usePlatform := rule.PerPlatform > 0 && platformName != ""
	useGlobal := rule.Global > 0
	if !useUser && !useChat && !usePlatform && !useGlobal {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-rule.Window)

	var userKey, chatKey, platKey string
	var userTimes, chatTimes, platTimes, globalTimes []time.Time
	if useUser {
		userKey = actionUserKey(action, userID)
		userTimes = pruneTimes(l.users[userKey], cutoff)
	}
	if useChat {
		chatKey = actionChatKey(action, chatID)
		chatTimes = pruneTimes(l.chats[chatKey], cutoff)
	}
	if usePlatform {
		platKey = actionPlatKey(action, platformName)
		platTimes = pruneTimes(l.plats[platKey], cutoff)
	}
	if useGlobal {
		globalTimes = pruneTimes(l.global[action], cutoff)
	}

	reject := func() bool {
		// Persist the pruned slices even on rejection so stale entries don't
		// accumulate, but do not append the new timestamp.
		if useUser {
			l.users[userKey] = userTimes
		}
		if useChat {
			l.chats[chatKey] = chatTimes
		}
		if usePlatform {
			l.plats[platKey] = platTimes
		}
		if useGlobal {
			l.global[action] = globalTimes
		}
		return false
	}

	if useUser && len(userTimes) >= rule.PerUser {
		return reject()
	}
	if useChat && len(chatTimes) >= rule.PerChat {
		return reject()
	}
	if usePlatform && len(platTimes) >= rule.PerPlatform {
		return reject()
	}
	if useGlobal && len(globalTimes) >= rule.Global {
		return reject()
	}

	if useUser {
		l.users[userKey] = append(userTimes, now)
	}
	if useChat {
		l.chats[chatKey] = append(chatTimes, now)
	}
	if usePlatform {
		l.plats[platKey] = append(platTimes, now)
	}
	if useGlobal {
		l.global[action] = append(globalTimes, now)
	}

	l.maybeGCLocked(now)
	return true
}

func actionUserKey(action string, userID int64) string {
	return action + "\x00u\x00" + itoa(userID)
}

func actionChatKey(action string, chatID int64) string {
	return action + "\x00c\x00" + itoa(chatID)
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
	for key, times := range l.chats {
		if len(times) == 0 {
			delete(l.chats, key)
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
