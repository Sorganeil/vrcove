package server

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStrangerCannotLockOutAccount(t *testing.T) {
	h, _ := newAccountHarness(t)
	h.signup("victim", "the real long passphrase")
	login := func(remote, password string) *httptest.ResponseRecorder {
		r := post("/cove/login", jsonBody(`{"username":"victim","password":"`+password+`"}`))
		r.RemoteAddr = remote
		return h.do(r)
	}
	var w *httptest.ResponseRecorder
	for range 13 {
		w = login("203.0.113.66:1234", "wrong guess here")
	}
	if w.Code != 429 {
		t.Fatalf("attacker not limited: %d", w.Code)
	}
	if w := login("198.51.100.7:1234", "the real long passphrase"); w.Code != 200 {
		t.Fatalf("owner locked out: %d %s", w.Code, w.Body)
	}
}

func TestSuccessfulSignInsDoNotCountAsGuesses(t *testing.T) {
	h, _ := newAccountHarness(t)
	h.signup("regular", "the real long passphrase")
	for i := range 11 {
		r := post("/cove/login", jsonBody(`{"username":"regular","password":"the real long passphrase"}`))
		r.RemoteAddr = fmt.Sprintf("198.51.100.%d:1234", i+10)
		if w := h.do(r); w.Code != 200 {
			t.Fatalf("sign-in %d: %d", i, w.Code)
		}
	}
	if h.srv.limiter.over("cove-user:regular", 1, h.srv.now()) {
		t.Fatal("successful sign-ins counted as failures")
	}
}

func TestIPv6ClientsGroupedBySlash64(t *testing.T) {
	if a, b := rateKey("2001:db8:1:2::1"), rateKey("2001:db8:1:2:ffff::9"); a != b {
		t.Fatalf("same /64 split: %s %s", a, b)
	}
	if rateKey("2001:db8:1:2::1") == rateKey("2001:db8:1:3::1") {
		t.Fatal("different /64s merged")
	}
	if rateKey("192.0.2.1") != "192.0.2.1" || rateKey("local") != "local" {
		t.Fatal("non-IPv6 keys changed")
	}
	h := newConnectedHarness(t)
	var w *httptest.ResponseRecorder
	for i := range 13 {
		r := post("/account/login", strings.NewReader(`{"username":"","password":""}`), "CF-Connecting-IP", fmt.Sprintf("2001:db8:5:6::%x", i+1))
		r.RemoteAddr = "127.0.0.1:40000"
		w = h.do(r)
	}
	if w.Code != 429 {
		t.Fatalf("rotating IPv6 addresses bypassed the limit: %d", w.Code)
	}
}

func TestLimiterMemoryIsBounded(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	for i := range maxBuckets + 500 {
		l.allow(fmt.Sprint("k", i), now)
	}
	if len(l.buckets) > maxBuckets {
		t.Fatalf("%d buckets", len(l.buckets))
	}
}

func TestPictureUploadsRateLimited(t *testing.T) {
	h, _ := newAccountHarness(t)
	cookie := h.signup("picture_spammer", "a synthetic long passphrase")
	var w *httptest.ResponseRecorder
	for range 13 {
		w = h.do(as(post("/cove/picture", strings.NewReader("not an image")), cookie))
	}
	if w.Code != 429 {
		t.Fatalf("13th upload = %d", w.Code)
	}
}

func TestSignedInUserLookupsNotPreviewed(t *testing.T) {
	h := newConnectedHarness(t)
	id := "usr_00000000-0000-0000-0000-000000000003"
	if w := h.signedIn(get("/account/user?id="+id), "/users/"+id, `{"id":"`+id+`","displayName":"Looked Up"}`); w.Code != 200 {
		t.Fatalf("lookup: %d %s", w.Code, w.Body)
	}
	if _, cached := h.srv.previews.Entries["user/"+id]; cached {
		t.Fatal("signed-in lookup cached for public previews")
	}
	if w := h.signedIn(get("/account/world?id="+testWorld), "/worlds/"+testWorld, `{"id":"`+testWorld+`","name":"Open","releaseStatus":"public"}`); w.Code != 200 {
		t.Fatalf("world lookup: %d %s", w.Code, w.Body)
	}
	if _, cached := h.srv.previews.Entries["world/"+testWorld]; !cached {
		t.Fatal("public world no longer previewed")
	}
}

func TestStaticSecurityHeaders(t *testing.T) {
	h := previewHarness(t)
	w := h.do(httptest.NewRequest("GET", "http://localhost/", nil))
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self' https://challenges.cloudflare.com", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
	if strings.HasPrefix(origin, "https://") && w.Header().Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS on an https origin")
	}
}

func TestPagesGetFreshScriptNonce(t *testing.T) {
	h := previewHarness(t)
	nonce := func() string {
		csp := h.do(httptest.NewRequest("GET", "http://localhost/", nil)).Header().Get("Content-Security-Policy")
		_, rest, ok := strings.Cut(csp, "'nonce-")
		value, _, closed := strings.Cut(rest, "'")
		if !ok || !closed || len(value) < 22 {
			t.Fatalf("no nonce in %s", csp)
		}
		return value
	}
	if nonce() == nonce() {
		t.Fatal("nonce reused across responses")
	}
	if csp := h.do(httptest.NewRequest("GET", "http://localhost/robots.txt", nil)).Header().Get("Content-Security-Policy"); strings.Contains(csp, "nonce-") {
		t.Fatalf("non-HTML response carries a nonce: %s", csp)
	}
}
