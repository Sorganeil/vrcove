// Package usernamecheck runs one cancellable username queue per app instance.
package usernamecheck

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const interval = 2500 * time.Millisecond

type Row struct {
	Username string `json:"username"`
	Status   string `json:"status"`
}

type Snapshot struct {
	Running    bool   `json:"running"`
	Phase      string `json:"phase"`
	Rows       []Row  `json:"rows"`
	NextAt     int64  `json:"nextAt"`
	WaitReason string `json:"waitReason"`
	Error      string `json:"error"`
}

// Reply distinguishes rate limits, temporary failures and terminal errors from
// availability results. A failed request must never become an available name.
type Reply struct {
	Status    string
	Error     string
	RetryAt   time.Time
	Transient bool
	Fatal     bool
}

type Check func(context.Context, string) Reply

type Checker struct {
	mu     sync.Mutex
	snap   Snapshot
	next   time.Time // Preserved across cancellation and new batches.
	cancel context.CancelFunc
	now    func() time.Time
	wait   func(context.Context, time.Duration) error
}

func (c *Checker) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snap
	s.Rows = append([]Row{}, s.Rows...)
	if s.Phase == "" {
		s.Phase = "idle"
	}
	return s
}

// Names trims blank entries and removes exact duplicates before enforcing the
// 100-name limit. It does not guess VRChat's changing username rules.
func Names(input []string) ([]string, error) {
	if len(input) > 1000 {
		return nil, errors.New("Enter up to 100 usernames.")
	}
	seen := map[string]bool{}
	var names []string
	for _, name := range input {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		if !utf8.ValidString(name) || len(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
			return nil, errors.New("Each username must be at most 128 bytes without control characters.")
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 || len(names) > 100 {
		return nil, errors.New("Enter between 1 and 100 usernames.")
	}
	return names, nil
}

func (c *Checker) Start(ctx context.Context, input []string, check Check) error {
	names, err := Names(input)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snap.Running {
		return errors.New("A username check is already running.")
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.wait == nil {
		c.wait = wait
	}
	ctx, c.cancel = context.WithCancel(ctx)
	c.snap = Snapshot{Running: true, Phase: "running", Rows: make([]Row, len(names))}
	for i, name := range names {
		c.snap.Rows[i] = Row{Username: name, Status: "queued"}
	}
	go c.run(ctx, check)
	return nil
}

func (c *Checker) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snap.Running {
		c.snap.Phase = "stopping"
		c.cancel()
	}
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(max(0, delay))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func (c *Checker) run(ctx context.Context, check Check) {
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.snap.Running = false
		c.snap.NextAt = 0
		c.snap.WaitReason = ""
		if c.snap.Phase != "complete" {
			c.snap.Phase = "stopped"
		}
		for i := range c.snap.Rows {
			if c.snap.Rows[i].Status == "checking" {
				c.snap.Rows[i].Status = "queued"
			}
		}
	}()
	for i := 0; ; i++ {
		c.mu.Lock()
		if i == len(c.snap.Rows) {
			c.snap.Phase = "complete"
			c.mu.Unlock()
			return
		}
		name := c.snap.Rows[i].Username
		c.mu.Unlock()
		failures := 0
		for {
			c.mu.Lock()
			deadline := c.next
			if deadline.After(c.now()) {
				c.snap.NextAt = deadline.UnixMilli()
			}
			c.mu.Unlock()
			if c.wait(ctx, deadline.Sub(c.now())) != nil || ctx.Err() != nil {
				return
			}
			c.mu.Lock()
			c.next = c.now().Add(interval)
			c.snap.NextAt = 0
			c.snap.WaitReason = ""
			c.snap.Rows[i].Status = "checking"
			c.mu.Unlock()
			reply := check(ctx, name)
			c.mu.Lock()
			// Keep an upstream cooldown even if Stop arrived during the request.
			if reply.RetryAt.After(c.next) {
				c.next = reply.RetryAt
			}
			if ctx.Err() != nil {
				c.mu.Unlock()
				return
			}
			if !reply.RetryAt.IsZero() {
				c.snap.WaitReason = "rate-limit"
				c.snap.Rows[i].Status = "queued"
				c.snap.NextAt = c.next.UnixMilli()
				c.mu.Unlock()
				continue
			}
			if reply.Transient && failures < 3 {
				if retryAt := c.now().Add(time.Duration(2<<failures) * time.Second); retryAt.After(c.next) {
					c.next = retryAt
				}
				failures++
				c.snap.WaitReason = "retry"
				c.snap.Rows[i].Status = "queued"
				c.snap.NextAt = c.next.UnixMilli()
				c.mu.Unlock()
				continue
			}
			c.snap.Rows[i].Status = reply.Status
			if reply.Error != "" || reply.Fatal || reply.Transient {
				c.snap.Rows[i].Status = "error"
				c.snap.Error = reply.Error
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
			break
		}
	}
}
