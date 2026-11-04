package vrchat

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryDelay parses Retry-After as seconds or an HTTP date. Unusable values
// default to a minute. The result is clamped to [1 second, limit]; a limit
// below one second uses ten minutes.
func RetryDelay(value string, now time.Time, limit time.Duration) time.Duration {
	if limit < time.Second {
		limit = 10 * time.Minute
	}
	wait := time.Minute
	v := strings.TrimSpace(value)
	if n, err := strconv.Atoi(v); err == nil {
		wait = time.Duration(min(max(n, 1), int(limit/time.Second))) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		wait = t.Sub(now)
	}
	return min(max(wait, time.Second), limit)
}

func (e *APIError) RetryDelay(now time.Time, limit time.Duration) time.Duration {
	return RetryDelay(e.RetryAfter, now, limit)
}
