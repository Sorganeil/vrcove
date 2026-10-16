package server

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackoffHonorsRetryAfter(t *testing.T) {
	h := newConnectedHarness(t)
	now := time.Now()
	h.srv.now = func() time.Time { return now }
	session := &Session{Auth: "a", Expires: now.Add(time.Hour).UnixMilli()}
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(429, "application/json", []byte(`{}`), "Retry-After", "120"), nil
	}
	for range 3 {
		if w := h.do(withSession(t, get("/account/me"), session)); w.Code != http.StatusTooManyRequests {
			t.Fatalf("limited me = %d", w.Code)
		}
	}
	if n := h.fetches.Load(); n != 1 {
		t.Errorf("%d upstream requests during cooldown, want 1", n)
	}
	// Calls without an account are unaffected by one account's cooldown.
	if h.srv.backoff.coolingDown(serverKey, now) {
		t.Error("one account's 429 paused everyone")
	}

	now = now.Add(121 * time.Second)
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`"}`)), nil
	}
	if w := h.do(withSession(t, get("/account/me"), session)); w.Code != 200 {
		t.Errorf("me after cooldown = %d", w.Code)
	}
}

func TestBackoffAddressWide(t *testing.T) {
	var b backoff
	now := time.Now()
	b.limited("u:a", time.Minute, now)
	b.limited("u:b", time.Minute, now)
	b.limited("u:a", time.Minute, now)
	if b.coolingDown(serverKey, now) {
		t.Fatal("two accounts started an address-wide cooldown")
	}
	b.limited("u:c", 2*time.Minute, now.Add(10*time.Second))
	if !b.coolingDown(serverKey, now.Add(10*time.Second)) || b.coolingDown(serverKey, now.Add(3*time.Minute)) {
		t.Error("three accounts should pause anonymous calls until Retry-After")
	}
	if b.coolingDown("u:d", now.Add(10*time.Second)) {
		t.Error("address-wide cooldown blocked signed-in calls")
	}

	// 429s spread beyond the window do not add up.
	var spread backoff
	for i, key := range []string{"u:a", "u:b", "u:c"} {
		spread.limited(key, time.Second, now.Add(time.Duration(i)*ipLimitWindow))
	}
	if spread.coolingDown(serverKey, now.Add(2*ipLimitWindow)) {
		t.Error("spread-out 429s started an address-wide cooldown")
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for v, want := range map[string]time.Duration{
		"":                              defaultRetryAfter,
		"30":                            30 * time.Second,
		"0":                             time.Second,
		"999999":                        maxRetryAfter,
		"Thu, 01 Jan 2026 00:02:00 GMT": 2 * time.Minute,
		"soon":                          defaultRetryAfter,
	} {
		if got := retryAfter(http.Header{"Retry-After": {v}}, now); got != want {
			t.Errorf("Retry-After %q = %v, want %v", v, got, want)
		}
	}
}

func TestImageCacheShared(t *testing.T) {
	h := newHarness(t)
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	release := make(chan struct{})
	h.upstream = func(r *http.Request) (*http.Response, error) {
		<-release
		return respond(200, "image/png", png), nil
	}
	target := "https://assets.vrchat.com/shared.png"
	var wg sync.WaitGroup
	var ok atomic.Int32
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := h.do(imageRequest(target)); w.Code == 200 && w.Header().Get("Cache-Control") == "private, no-store" {
				ok.Add(1)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if w := h.do(imageRequest(target)); w.Code != 200 {
		t.Errorf("cached image = %d", w.Code)
	}
	if ok.Load() != 5 || h.fetches.Load() != 1 {
		t.Errorf("%d of 5 concurrent requests served after %d upstream fetches, want 1", ok.Load(), h.fetches.Load())
	}

	// Failures are not cached.
	h.upstream = func(*http.Request) (*http.Response, error) { return respond(404, "text/plain", nil), nil }
	h.do(imageRequest("https://assets.vrchat.com/missing.png"))
	h.do(imageRequest("https://assets.vrchat.com/missing.png"))
	if n := h.fetches.Load(); n != 3 {
		t.Errorf("%d upstream requests, want failures refetched", n)
	}
}

func TestImageCachePrivate(t *testing.T) {
	h := newConnectedHarness(t)
	session := &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}
	target := "https://api.vrchat.cloud/api/1/file/file_00000000-0000-0000-0000-000000000001/1/file"
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") == "" {
			return respond(401, "application/json", nil), nil
		}
		return respond(200, "image/png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")), nil
	}
	for range 2 {
		if w := h.do(withSession(t, imageRequest(target), session)); w.Code != 200 {
			t.Fatalf("own image = %d", w.Code)
		}
	}
	if n := h.fetches.Load(); n != 1 {
		t.Errorf("%d upstream requests for the viewer's own image, want 1", n)
	}
	// Another viewer without that cookie never receives the cached copy.
	if w := h.do(imageRequest(target)); w.Code != http.StatusNotFound {
		t.Errorf("image fetched with a cookie served to another viewer: %d", w.Code)
	}
	if n := h.fetches.Load(); n != 2 {
		t.Errorf("%d upstream requests, want the other viewer fetched separately", n)
	}
}

func TestImageBackoff(t *testing.T) {
	h := newHarness(t)
	target := "https://api.vrchat.cloud/api/1/image/file_00000000-0000-0000-0000-000000000001/1/256"
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(429, "text/plain", nil, "Retry-After", "60"), nil
	}
	for range 3 {
		if w := h.do(imageRequest(target)); w.Code != http.StatusTooManyRequests {
			t.Errorf("limited image = %d", w.Code)
		}
	}
	if n := h.fetches.Load(); n != 1 {
		t.Errorf("%d upstream requests during cooldown, want 1", n)
	}
	// Images outside the VRChat API are not paused.
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(200, "image/png", []byte("\x89PNG\r\n\x1a\n")), nil
	}
	if w := h.do(imageRequest("https://assets.vrchat.com/cdn.png")); w.Code != 200 {
		t.Errorf("CDN image during cooldown = %d", w.Code)
	}
}
