package server

import (
	"sync"
	"time"
)

const (
	signInAttempts = 12
	signInWindow   = 10 * time.Minute
)

type bucket struct {
	n     int
	until time.Time
}

// limiter caps sign-in attempts per client IP within a fixed window.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

func newLimiter() *limiter { return &limiter{buckets: map[string]*bucket{}} }

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) > 10000 {
		for k, b := range l.buckets {
			if b.until.Before(now) {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[key]
	if b == nil || !b.until.After(now) {
		l.buckets[key] = &bucket{n: 1, until: now.Add(signInWindow)}
		return true
	}
	if b.n >= signInAttempts {
		return false
	}
	b.n++
	return true
}
