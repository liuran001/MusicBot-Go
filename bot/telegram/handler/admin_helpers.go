package handler

import "sync/atomic"

// AdminSet is a concurrency-safe, hot-reloadable set of admin user IDs.
//
// Multiple handlers hold the same *AdminSet pointer. Reads (Contains) happen
// from many handler goroutines concurrently, while Replace is called from the
// /reload goroutine. Internally it uses copy-on-write over an immutable map
// guarded by atomic.Pointer, so reads never race with a reload and all handlers
// observe the new set after Replace returns.
type AdminSet struct {
	ids atomic.Pointer[map[int64]struct{}]
}

// NewAdminSet builds an AdminSet from an initial id set. The input map is
// copied defensively; the caller may keep mutating its own copy.
func NewAdminSet(ids map[int64]struct{}) *AdminSet {
	s := &AdminSet{}
	s.Replace(ids)
	return s
}

// Replace atomically swaps in a fresh snapshot of ids. Safe to call while other
// goroutines are calling Contains.
func (s *AdminSet) Replace(ids map[int64]struct{}) {
	if s == nil {
		return
	}
	snapshot := make(map[int64]struct{}, len(ids))
	for id := range ids {
		snapshot[id] = struct{}{}
	}
	s.ids.Store(&snapshot)
}

// Contains reports whether userID is an admin. Lock-free and safe for
// concurrent use alongside Replace.
func (s *AdminSet) Contains(userID int64) bool {
	if s == nil {
		return false
	}
	m := s.ids.Load()
	if m == nil {
		return false
	}
	_, ok := (*m)[userID]
	return ok
}

// isBotAdmin reports whether userID belongs to the admin set.
func isBotAdmin(admins *AdminSet, userID int64) bool {
	return admins.Contains(userID)
}
