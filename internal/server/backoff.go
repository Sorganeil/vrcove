package server

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// defaultRetryAfter applies when a 429 carries no usable Retry-After.
	defaultRetryAfter = time.Minute
	maxRetryAfter     = 10 * time.Minute
	// ipLimitAccounts 429s from distinct accounts within ipLimitWindow suggest
	// VRChat is limiting this server's address rather than one account.
	ipLimitAccounts = 3
	ipLimitWindow   = time.Minute
	// serverKey is the cooldown shared by calls made without an account:
	// sign-ins, public previews and images. It also holds address-wide
	// cooldowns, which pause those calls for everyone.
	serverKey = ""
)

// backoff remembers VRChat 429s so calls are refused locally until VRChat's
// Retry-After passes, rather than extending the limit.
type backoff struct {
	mu     sync.Mutex
	until  map[string]time.Time
	recent map[string]time.Time
}

// backoffKey keys calls made with a signed-in session by VRChat user, and
// every other call under serverKey.
func backoffKey(session *Session) string {
	if session != nil && session.UserID != "" && session.Pending == nil && session.Auth != "" {
		return "u:" + session.UserID
	}
	return serverKey
}

// coolingDown reports whether key is still inside a VRChat cooldown.
func (b *backoff) coolingDown(key string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.until[key].After(now)
}

// limited records a 429 for key. 429s across several accounts in a short
// window also start an address-wide cooldown.
func (b *backoff) limited(key string, wait time.Duration, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.until == nil {
		b.until, b.recent = map[string]time.Time{}, map[string]time.Time{}
	}
	for k, t := range b.until {
		if !t.After(now) {
			delete(b.until, k)
		}
	}
	for k, t := range b.recent {
		if now.Sub(t) >= ipLimitWindow {
			delete(b.recent, k)
		}
	}
	b.extend(key, now.Add(wait))
	if key == serverKey {
		return
	}
	b.recent[key] = now
	if len(b.recent) >= ipLimitAccounts {
		b.extend(serverKey, now.Add(wait))
	}
}

// extend moves key's cooldown later, never earlier. Callers hold mu.
func (b *backoff) extend(key string, until time.Time) {
	if until.After(b.until[key]) {
		b.until[key] = until
	}
}

// retryAfter reads Retry-After as seconds or an HTTP date, clamped to a sane
// range.
func retryAfter(h http.Header, now time.Time) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	wait := defaultRetryAfter
	if n, err := strconv.Atoi(v); err == nil {
		wait = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		wait = t.Sub(now)
	}
	return min(max(wait, time.Second), maxRetryAfter)
}

func rateLimitedError() error {
	return errStatus(http.StatusTooManyRequests, "VRChat is rate limiting requests. Please wait before trying again.")
}
