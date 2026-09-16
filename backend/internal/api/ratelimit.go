package api

import (
	"sync"
	"time"
)

// loginLimiter throttles repeated failed login attempts to blunt password
// brute-forcing. It is intentionally in-memory: the deployment is a single
// instance, and losing the counters on restart is an acceptable trade-off for
// avoiding a schema change.
//
// Keys combine the client IP and the attempted username. A determined attacker
// spread across many IPs is slowed per-IP rather than fully blocked, and an
// attacker can in principle lock out one username from their own IP; both are
// accepted trade-offs for this lightweight protection.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
	max      int
	window   time.Duration
	lock     time.Duration
}

type loginAttempt struct {
	count    int
	first    time.Time
	lockedAt time.Time
}

func newLoginLimiter(max int, window, lock time.Duration) *loginLimiter {
	l := &loginLimiter{
		attempts: make(map[string]*loginAttempt),
		max:      max,
		window:   window,
		lock:     lock,
	}
	go l.janitor()
	return l
}

// blocked reports whether key is currently locked out and for how much longer.
func (l *loginLimiter) blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[key]
	if !ok || a.lockedAt.IsZero() {
		return false, 0
	}
	if remain := l.lock - time.Since(a.lockedAt); remain > 0 {
		return true, remain
	}
	delete(l.attempts, key)
	return false, 0
}

// fail records a failed attempt for key and returns the lockout duration if
// this failure tripped the threshold (zero otherwise).
func (l *loginLimiter) fail(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a, ok := l.attempts[key]
	if !ok || now.Sub(a.first) > l.window {
		a = &loginAttempt{first: now}
		l.attempts[key] = a
	}
	a.count++
	if a.count >= l.max {
		a.lockedAt = now
		return l.lock
	}
	return 0
}

// success clears any counter for key after a fully successful login.
func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// janitor periodically evicts stale entries so the map does not grow without
// bound under a spray of distinct usernames.
func (l *loginLimiter) janitor() {
	ticker := time.NewTicker(l.window)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for k, a := range l.attempts {
			expired := now.Sub(a.first) > l.window
			unlocked := a.lockedAt.IsZero() || now.Sub(a.lockedAt) > l.lock
			if expired && unlocked {
				delete(l.attempts, k)
			}
		}
		l.mu.Unlock()
	}
}
