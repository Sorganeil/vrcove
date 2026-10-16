// Package retry handles rate-limit deadlines without discarding queued work.
package retry

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Fallback is used when a rate-limited response has no usable Retry-After.
const Fallback = time.Minute

// Limited reports a rate limit from Source that lasts until Until.
type Limited struct {
	Source string
	Until  time.Time
}

func (e *Limited) Error() string {
	return fmt.Sprintf("%s rate limit · retry at %s · collecting locally", e.Source, e.Until.Local().Format("15:04:05"))
}

// After reads a Retry-After value in seconds or as an HTTP date. Missing or
// unusable values wait Fallback. Deadlines are kept between one second and one
// day, so a broken server cannot park the queue indefinitely.
func After(header string, now time.Time) time.Time {
	header = strings.TrimSpace(header)
	wait := Fallback
	if n, err := strconv.ParseInt(header, 10, 64); err == nil {
		wait = time.Duration(min(max(n, 1), 86400)) * time.Second
	} else if date, err := http.ParseTime(header); err == nil {
		wait = date.Sub(now)
	}
	return now.Add(min(max(wait, time.Second), 24*time.Hour))
}
