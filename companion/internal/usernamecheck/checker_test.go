package usernamecheck

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func finished(t *testing.T, c *Checker) Snapshot {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s := c.Snapshot(); !s.Running {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("queue did not finish")
	return Snapshot{}
}

func fakeClock() (*Checker, *time.Time) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	c := &Checker{now: func() time.Time { return now }}
	c.wait = func(ctx context.Context, delay time.Duration) error {
		if delay > 0 {
			now = now.Add(delay)
		}
		return ctx.Err()
	}
	return c, &now
}

func TestQueuePacingAndCooldownSurviveNewBatches(t *testing.T) {
	c, now := fakeClock()
	var times []time.Time
	var checked []string
	checks := 0
	check := func(_ context.Context, name string) Reply {
		times = append(times, *now)
		checked = append(checked, name)
		checks++
		if checks == 1 {
			return Reply{RetryAt: now.Add(37 * time.Second)}
		}
		return Reply{Status: "taken"}
	}
	if err := c.Start(context.Background(), []string{"one", "two"}, check); err != nil {
		t.Fatal(err)
	}
	s := finished(t, c)
	if strings.Join(checked, ",") != "one,one,two" || s.Phase != "complete" {
		t.Fatalf("checked=%v state=%+v", checked, s)
	}
	if times[1].Sub(times[0]) != 37*time.Second || times[2].Sub(times[1]) != interval {
		t.Fatalf("request starts=%v", times)
	}
	if err := c.Start(context.Background(), []string{"three"}, check); err != nil {
		t.Fatal(err)
	}
	finished(t, c)
	if times[3].Sub(times[2]) != interval {
		t.Fatal("new batch reset the request spacing")
	}
}

func TestStopAndRestartCannotBypassAnUpstreamCooldown(t *testing.T) {
	c, now := fakeClock()
	ctx, cancel := context.WithCancel(context.Background())
	deadline := now.Add(2 * time.Hour)
	if err := c.Start(ctx, []string{"one", "two"}, func(context.Context, string) Reply {
		cancel()
		return Reply{RetryAt: deadline}
	}); err != nil {
		t.Fatal(err)
	}
	s := finished(t, c)
	if s.Phase != "stopped" || s.Rows[0].Status != "queued" || s.Rows[1].Status != "queued" {
		t.Fatalf("stop=%+v", s)
	}
	if err := c.Start(context.Background(), []string{"three"}, func(context.Context, string) Reply {
		if now.Before(deadline) {
			t.Error("restart bypassed rate limit")
		}
		return Reply{Status: "available"}
	}); err != nil {
		t.Fatal(err)
	}
	finished(t, c)
}

func TestTemporaryFailuresBackOffThenStop(t *testing.T) {
	c, now := fakeClock()
	var times []time.Time
	if err := c.Start(context.Background(), []string{"one", "two"}, func(context.Context, string) Reply {
		times = append(times, *now)
		return Reply{Transient: true, Error: "offline"}
	}); err != nil {
		t.Fatal(err)
	}
	s := finished(t, c)
	if len(times) != 4 || s.Phase != "stopped" || s.Error != "offline" || s.Rows[0].Status != "error" || s.Rows[1].Status != "queued" {
		t.Fatalf("calls=%v state=%+v", times, s)
	}
	for i, delay := range []time.Duration{interval, 4 * time.Second, 8 * time.Second} {
		if times[i+1].Sub(times[i]) != delay {
			t.Errorf("retry %d too soon", i)
		}
	}
}

func TestOnlyOneBatchAndCancellationInterruptsRequest(t *testing.T) {
	c := &Checker{}
	started := make(chan struct{})
	check := func(ctx context.Context, _ string) Reply { close(started); <-ctx.Done(); return Reply{} }
	if err := c.Start(context.Background(), []string{"one", "two"}, check); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := c.Start(context.Background(), []string{"other"}, check); err == nil {
		t.Fatal("overlapping batch accepted")
	}
	c.Stop()
	s := finished(t, c)
	if s.Rows[0].Status != "queued" || s.Phase != "stopped" {
		t.Fatalf("cancel=%+v", s)
	}
}

func TestNamesAndFullHundred(t *testing.T) {
	input := make([]string, 100)
	for i := range input {
		input[i] = fmt.Sprintf("name%d", i)
	}
	c, _ := fakeClock()
	if err := c.Start(context.Background(), input, func(context.Context, string) Reply { return Reply{Status: "available"} }); err != nil {
		t.Fatal(err)
	}
	if s := finished(t, c); len(s.Rows) != 100 || s.Phase != "complete" {
		t.Fatalf("100=%+v", s)
	}
	for _, bad := range [][]string{nil, {" "}, append(input, "name101"), {"a\x00b"}, {strings.Repeat("a", 129)}} {
		if _, err := Names(bad); err == nil {
			t.Errorf("accepted bad list: %v", bad)
		}
	}
	if names, err := Names([]string{" one ", "one", "", "two"}); err != nil || strings.Join(names, ",") != "one,two" {
		t.Fatalf("normalization=%v %v", names, err)
	}
}
