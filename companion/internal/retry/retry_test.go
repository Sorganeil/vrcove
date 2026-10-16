package retry

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAfterReadsSecondsDatesAndFallsBack(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"120", 2 * time.Minute},
		{" 7 ", 7 * time.Second},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{"", Fallback},
		{"soon", Fallback},
		{"0", time.Second},
		{"-5", time.Second},
		{now.Add(-time.Hour).Format(http.TimeFormat), time.Second},
		{"999999999", 24 * time.Hour},
	} {
		if got := After(tc.header, now).Sub(now); got != tc.want {
			t.Errorf("After(%q) waits %s, want %s", tc.header, got, tc.want)
		}
	}
}

func TestLimitedNamesSourceAndTime(t *testing.T) {
	err := &Limited{Source: "VRCove", Until: time.Date(2026, 10, 16, 12, 34, 56, 0, time.Local)}
	if msg := err.Error(); !strings.HasPrefix(msg, "VRCove rate limit · retry at 12:34:56") {
		t.Fatalf("message %q", msg)
	}
}
