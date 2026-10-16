package server

import (
	"net"
	"sync"
	"time"
)

const (
	signInAttempts = 12
	signInWindow   = 10 * time.Minute
	// accountFailures is the per-username backstop against password spraying
	// from many addresses. It is far above signInAttempts so a single client
	// cannot lock someone out; that client hits its own limits first.
	accountFailures = 100
	// maxBuckets bounds the limiter's memory whatever the key spread.
	maxBuckets = 50000
	sweepEvery = time.Minute
)

type bucket struct {
	n     int
	until time.Time
}

// limiter counts events per key within a fixed window.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

func newLimiter() *limiter { return &limiter{buckets: map[string]*bucket{}} }

// live returns key's bucket for the current window, or nil. Callers hold mu.
func (l *limiter) live(key string, now time.Time) *bucket {
	if b := l.buckets[key]; b != nil && b.until.After(now) {
		return b
	}
	return nil
}

// sweep drops expired buckets at most once a minute, then evicts arbitrary
// ones if the map is still over its cap. Callers hold mu.
func (l *limiter) sweep(now time.Time) {
	if len(l.buckets) < maxBuckets && now.Sub(l.swept) < sweepEvery {
		return
	}
	l.swept = now
	for k, b := range l.buckets {
		if !b.until.After(now) {
			delete(l.buckets, k)
		}
	}
	for k := range l.buckets {
		if len(l.buckets) < maxBuckets {
			break
		}
		delete(l.buckets, k)
	}
}

// hit records one event for key.
func (l *limiter) hit(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.add(key, now)
}

func (l *limiter) add(key string, now time.Time) {
	if b := l.live(key, now); b != nil {
		b.n++
		return
	}
	l.sweep(now)
	l.buckets[key] = &bucket{n: 1, until: now.Add(signInWindow)}
}

// over reports whether key already has limit events in its window.
func (l *limiter) over(key string, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.live(key, now)
	return b != nil && b.n >= limit
}

// allow records an event for key unless it already has signInAttempts.
func (l *limiter) allow(key string, now time.Time) bool {
	return l.allowN(key, signInAttempts, now) == 0
}

// rateKey is the client's address for rate limiting. IPv6 is grouped by /64,
// the smallest block a customer is usually given, so rotating addresses
// within one allocation does not reset the limits.
func rateKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// allowN records an event for key unless it already has limit events in the
// current window. When refused, it returns how long until the window resets.
func (l *limiter) allowN(key string, limit int, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b := l.live(key, now); b != nil && b.n >= limit {
		return b.until.Sub(now)
	}
	l.add(key, now)
	return 0
}
