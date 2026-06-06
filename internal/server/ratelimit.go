package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// loginLimiter is a small per-key (IP) fixed-window rate limiter for the login
// endpoint. After maxFailures bad tokens within `window`, the key is locked
// for `lockFor`. A successful login resets the key. Pure in-memory (the box is
// a single-process private-LAN service; a restart clearing it is acceptable).
type loginLimiter struct {
	mu          sync.Mutex
	entries     map[string]*loginEntry
	maxFailures int
	window      time.Duration
	lockFor     time.Duration
}

type loginEntry struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
}

func newLoginLimiter(maxFailures int, window, lockFor time.Duration) *loginLimiter {
	return &loginLimiter{
		entries:     map[string]*loginEntry{},
		maxFailures: maxFailures,
		window:      window,
		lockFor:     lockFor,
	}
}

// check reports whether the key may attempt login now; if locked it returns
// the seconds to wait.
func (l *loginLimiter) check(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok || !time.Now().Before(e.lockedUntil) {
		return true, 0
	}
	ra := int(e.lockedUntil.Sub(time.Now()).Seconds())
	if ra < 1 {
		ra = 1
	}
	return false, ra
}

// recordFailure bumps the failure count and locks the key once the threshold is
// reached inside the window.
func (l *loginLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for key, entry := range l.entries {
		if now.Sub(entry.windowStart) > l.window && !now.Before(entry.lockedUntil) {
			delete(l.entries, key)
		}
	}
	e, ok := l.entries[key]
	if !ok {
		e = &loginEntry{windowStart: now}
		l.entries[key] = e
	}
	if now.Sub(e.windowStart) > l.window {
		e.failures = 0
		e.windowStart = now
	}
	e.failures++
	if e.failures >= l.maxFailures {
		e.lockedUntil = now.Add(l.lockFor)
		e.failures = 0
		e.windowStart = now
	}
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// clientIP uses the transport peer. Forwarded headers are attacker-controlled
// unless a trusted proxy overwrites them. A proxy should rate-limit separately.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
