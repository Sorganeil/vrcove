package logger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"vrcove/companion/internal/retry"
)

const id = "avtr_12345678-1234-1234-1234-123456789abc"

func TestOutputLogsAreNotAnAvatarSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "output_log_2026-10-16.txt"), []byte(id+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Config.Folder = dir
	if err := s.Scan(); err != nil {
		t.Fatal(err)
	}
	if len(s.Pending) != 0 {
		t.Fatal("collected an ID from an obsolete output-log source")
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Synthetic IDs for the upload pipeline tests.
const (
	known   = "avtr_00000000-0000-0000-0000-00000000000a"
	public  = "avtr_00000000-0000-0000-0000-00000000000b"
	private = "avtr_00000000-0000-0000-0000-00000000000c"
	flaky   = "avtr_00000000-0000-0000-0000-00000000000d"
	public2 = "avtr_00000000-0000-0000-0000-00000000000e"
)

// fakeVerifier stands in for the desktop's VRChat login.
type fakeVerifier struct {
	ready   bool
	checked []string
	errs    map[string]error
}

func (v *fakeVerifier) Ready() bool { return v.ready }
func (v *fakeVerifier) Check(_ context.Context, id string) (bool, error) {
	v.checked = append(v.checked, id)
	if err := v.errs[id]; err != nil {
		return false, err
	}
	return id != private, nil
}

// fakeVRCove records requests and answers the catalog check and ingestion.
type fakeVRCove struct {
	t                  *testing.T
	knownIDs           []string
	ingested           [][]string
	knownResp, ingResp func(ids []string) *http.Response
}

func reply(status int, body string, header ...string) *http.Response {
	h := http.Header{}
	for i := 0; i+1 < len(header); i += 2 {
		h.Set(header[i], header[i+1])
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func (f *fakeVRCove) client() *http.Client {
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer test-key" {
			f.t.Fatalf("VRCove request carried unexpected credentials: %v", r.Header)
		}
		var body struct{ IDs []string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Fatal(err)
		}
		switch r.URL.Path {
		case "/api/logger/known":
			if f.knownResp != nil {
				return f.knownResp(body.IDs), nil
			}
			existing := []string{}
			for _, id := range body.IDs {
				if slices.Contains(f.knownIDs, id) {
					existing = append(existing, id)
				}
			}
			raw, _ := json.Marshal(map[string]any{"existing": existing})
			return reply(200, string(raw)), nil
		case "/api/logger/avatars":
			f.ingested = append(f.ingested, body.IDs)
			if f.ingResp != nil {
				return f.ingResp(body.IDs), nil
			}
			results := []Result{}
			for _, id := range body.IDs {
				results = append(results, Result{id, "added"})
			}
			raw, _ := json.Marshal(map[string]any{"results": results})
			return reply(200, string(raw)), nil
		}
		f.t.Fatalf("unexpected path %s", r.URL.Path)
		return nil, nil
	})}
}

func pipeline(t *testing.T, ids ...string) (*State, *fakeVerifier, *fakeVRCove) {
	s := New()
	s.Config.Token = "test-key"
	s.Pending = append(s.Pending, ids...)
	for _, id := range ids {
		s.Seen[id] = "queued"
	}
	v := &fakeVerifier{ready: true, errs: map[string]error{}}
	s.Verifier = v
	return s, v, &fakeVRCove{t: t, knownIDs: []string{known}}
}

func TestKnownAvatarsSkipVRChatAndIngestion(t *testing.T) {
	s, v, f := pipeline(t, known)
	if err := s.Submit(context.Background(), f.client()); err != nil {
		t.Fatal(err)
	}
	if len(v.checked) != 0 || len(f.ingested) != 0 || s.Existing != 1 || len(s.Pending) != 0 || s.Seen[known] != "existing" {
		t.Fatalf("checked=%v ingested=%v state=%+v", v.checked, f.ingested, s)
	}
}

func TestLocalRejectionsNeverReachIngestion(t *testing.T) {
	s, v, f := pipeline(t, private, "avtr_not-valid")
	if err := s.Submit(context.Background(), f.client()); err != nil {
		t.Fatal(err)
	}
	if len(f.ingested) != 0 || s.Rejected != 2 || len(s.Pending) != 0 || len(v.checked) != 1 {
		t.Fatalf("ingested=%v checked=%v rejected=%d", f.ingested, v.checked, s.Rejected)
	}
	if !s.RetryAt.IsZero() {
		t.Fatal("a batch without ingestion was paced like an upload")
	}
}

func TestOnlyVerifiedIDsAreSentAndServerRejectionsCount(t *testing.T) {
	s, _, f := pipeline(t, known, public, private, public2)
	f.ingResp = func(ids []string) *http.Response {
		return reply(200, `{"results":[{"id":"`+public+`","status":"added"},{"id":"`+public2+`","status":"rejected"}]}`)
	}
	if err := s.Submit(context.Background(), f.client()); err != nil {
		t.Fatal(err)
	}
	if len(f.ingested) != 1 || !slices.Equal(f.ingested[0], []string{public, public2}) {
		t.Fatalf("ingested %v", f.ingested)
	}
	if s.Added != 1 || s.Existing != 1 || s.Rejected != 2 || len(s.Pending) != 0 {
		t.Fatalf("counters %+v", s)
	}
	// Uploads are paced to stay within forty batches per ten minutes.
	if wait := time.Until(s.RetryAt); s.RetryReason != "Pacing uploads" || wait < 15*time.Second || wait > pace {
		t.Fatalf("pacing %s %s", s.RetryReason, wait)
	}
}

func TestTemporaryCheckFailureKeepsUncheckedIDsQueued(t *testing.T) {
	s, v, f := pipeline(t, public, flaky, public2)
	v.errs[flaky] = errors.New("VRChat connection unavailable · will retry")
	err := s.Submit(context.Background(), f.client())
	if err == nil || s.Added != 1 || !slices.Equal(s.Pending, []string{flaky, public2}) || s.Failures != 0 && s.RetryReason == "" {
		t.Fatalf("err=%v pending=%v added=%d", err, s.Pending, s.Added)
	}
	if len(f.ingested) != 1 || !slices.Equal(f.ingested[0], []string{public}) {
		t.Fatalf("ingested %v", f.ingested)
	}
	if time.Until(s.RetryAt) < 15*time.Second {
		t.Fatal("check failure shortened upload pacing")
	}
}

func TestRateLimitsKeepQueueAndNameTheirSource(t *testing.T) {
	soon := time.Now().Add(3 * time.Minute).UTC().Format(http.TimeFormat)
	for _, tc := range []struct {
		name, reason string
		wait         time.Duration
		setup        func(*fakeVerifier, *fakeVRCove)
		pending      int
	}{
		{"catalog check", "VRCove rate limit", 30 * time.Second, func(_ *fakeVerifier, f *fakeVRCove) {
			f.knownResp = func([]string) *http.Response {
				return reply(429, `{}`, "Retry-After", "30", "X-RateLimit-Source", "vrcove")
			}
		}, 2},
		{"ingestion", "VRCove rate limit", 10 * time.Minute, func(_ *fakeVerifier, f *fakeVRCove) {
			f.ingResp = func([]string) *http.Response {
				return reply(429, `{}`, "Retry-After", "600", "X-RateLimit-Source", "vrcove")
			}
		}, 2},
		{"server-side VRChat", "VRChat (server) rate limit", 3 * time.Minute, func(_ *fakeVerifier, f *fakeVRCove) {
			f.ingResp = func([]string) *http.Response {
				return reply(429, `{}`, "Retry-After", soon, "X-RateLimit-Source", "vrchat")
			}
		}, 2},
		{"partial server result", "VRChat (server) rate limit", 45 * time.Second, func(_ *fakeVerifier, f *fakeVRCove) {
			f.ingResp = func([]string) *http.Response {
				return reply(200, `{"results":[{"id":"`+public+`","status":"added"}]}`, "Retry-After", "45", "X-RateLimit-Source", "vrchat")
			}
		}, 1},
		{"this PC's VRChat", "VRChat (this PC) rate limit", 2 * time.Minute, func(v *fakeVerifier, _ *fakeVRCove) {
			v.errs[public] = &retry.Limited{Source: "VRChat (this PC)", Until: time.Now().Add(2 * time.Minute)}
		}, 2},
		{"missing Retry-After", "VRCove rate limit", retry.Fallback, func(_ *fakeVerifier, f *fakeVRCove) {
			f.knownResp = func([]string) *http.Response { return reply(429, `{}`) }
		}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, v, f := pipeline(t, public, public2)
			tc.setup(v, f)
			var limited *retry.Limited
			if err := s.Submit(context.Background(), f.client()); !errors.As(err, &limited) {
				t.Fatalf("err %v", err)
			}
			wait := time.Until(s.RetryAt)
			if s.RetryReason != tc.reason || wait < tc.wait-5*time.Second || wait > tc.wait+time.Second {
				t.Fatalf("reason %q wait %s", s.RetryReason, wait)
			}
			if len(s.Pending) != tc.pending || s.Failures != 0 {
				t.Fatalf("pending %v failures %d", s.Pending, s.Failures)
			}
			if status := s.WaitingStatus(); !strings.Contains(status, tc.reason) || !strings.Contains(status, "retry at") {
				t.Fatalf("status %q", status)
			}
			// Nothing is sent again until the wait is over.
			before := len(f.ingested)
			if err := s.Submit(context.Background(), f.client()); err != nil || len(f.ingested) != before {
				t.Fatal("submitted during a rate limit")
			}
		})
	}
}

func TestUploadsNeedBothLogins(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token string
		ready bool
		want  string
	}{
		{"no API key", "", true, "add an API key"},
		{"no VRChat login", "test-key", false, "sign in to VRChat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, v, f := pipeline(t, public)
			s.Config.Token, v.ready = tc.token, tc.ready
			client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
				t.Fatal("network request without both logins")
				return nil, nil
			})}
			if err := s.Submit(context.Background(), client); err != nil || !strings.Contains(s.Status, tc.want) || len(s.Pending) != 1 {
				t.Fatalf("err=%v status=%q", err, s.Status)
			}
			_ = f
		})
	}
}

func TestSubmitOnlyAcknowledgesRequestedIDs(t *testing.T) {
	s, _, f := pipeline(t, public)
	f.ingResp = func([]string) *http.Response {
		return reply(200, `{"results":[{"id":"unrequested","status":"added"}]}`)
	}
	if err := s.Submit(context.Background(), f.client()); err == nil {
		t.Fatal("expected retry for unacknowledged ID")
	}
	if len(s.Pending) != 1 || s.Added != 0 || s.Failures != 1 {
		t.Fatal("invalid result changed queue")
	}
}

func TestServerRequiresSafeOrigin(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://user:password@example.com", "https://example.com/path", "https://example.com?x=1"} {
		if _, err := ValidateServer(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"https://vrcove.st", "http://127.0.0.1:8080"} {
		if _, err := ValidateServer(raw); err != nil {
			t.Fatal(err)
		}
	}
}
