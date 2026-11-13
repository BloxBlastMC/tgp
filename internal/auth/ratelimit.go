package auth

import (
	"sync"
	"time"
)

// FailureLimiter counts failures per key in a fixed window and blocks keys
// that exceed the limit until the window expires. It is per-process; with
// several server nodes each enforces its own limit, which still bounds
// guessing to limit × nodes per window.
type FailureLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]*failureEntry
	sweepAt time.Time
}

type failureEntry struct {
	count int
	reset time.Time
}

// NewFailureLimiter allows up to limit failures per key per window.
func NewFailureLimiter(limit int, window time.Duration) *FailureLimiter {
	return &FailureLimiter{limit: limit, window: window, now: time.Now, entries: map[string]*failureEntry{}}
}

// Blocked reports whether key has hit the limit, and when it unblocks.
func (l *FailureLimiter) Blocked(key string) (bool, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok || l.now().After(e.reset) {
		return false, time.Time{}
	}
	return e.count >= l.limit, e.reset
}

// Fail records a failure for key.
func (l *FailureLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	e, ok := l.entries[key]
	if !ok || now.After(e.reset) {
		e = &failureEntry{reset: now.Add(l.window)}
		l.entries[key] = e
	}
	e.count++
}

// Reset clears failures for key (after a successful login).
func (l *FailureLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *FailureLimiter) sweep(now time.Time) {
	if now.Before(l.sweepAt) {
		return
	}
	for k, e := range l.entries {
		if now.After(e.reset) {
			delete(l.entries, k)
		}
	}
	l.sweepAt = now.Add(l.window)
}
