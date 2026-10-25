package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestDesktop(t *testing.T, site *url.URL) *desktop {
	t.Helper()
	st := &store{data: state{Local: map[string]string{}, Remote: map[string]string{}}, fileKey: make([]byte, 32), SessionKey: make([]byte, 32)}
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>VRCove</title>")}, "assets/app.js": {Data: []byte("app")}}
	d, err := newDesktop(st, site, assets)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func do(d *desktop, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://wails.localhost"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	// The window's own cookies are ignored; the app keeps its own.
	r.AddCookie(&http.Cookie{Name: "atlas-vrc-local", Value: "from-the-window"})
	w := httptest.NewRecorder()
	d.ServeHTTP(w, r)
	return w
}

func TestDesktopKeepsVRChatOnTheDevice(t *testing.T) {
	var remoteSaw []*http.Request
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteSaw = append(remoteSaw, r)
		http.SetCookie(w, &http.Cookie{Name: "__Host-veil-account", Value: "cove-token", Path: "/", Secure: true, HttpOnly: true})
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"avatars":[],"total":0}`)
	}))
	defer remote.Close()
	site, _ := url.Parse(remote.URL)
	d := newTestDesktop(t, site)
	vrchat := 0
	d.api.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		vrchat++
		if r.URL.Host != "api.vrchat.cloud" {
			t.Errorf("VRChat request went to %s", r.URL.Host)
		}
		h := http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"auth=authcookie_desktop; Path=/"}}
		return &http.Response{StatusCode: 200, Header: h, Request: r,
			Body: io.NopCloser(strings.NewReader(`{"id":"usr_00000000-0000-0000-0000-000000000001","username":"tester","displayName":"Tester","releaseStatus":"public"}`))}, nil
	})

	// Pages fall back to the app shell; files are served as they are.
	if w := do(d, "GET", "/settings", ""); !strings.Contains(w.Body.String(), "<title>VRCove") {
		t.Fatalf("page = %d %q", w.Code, w.Body)
	}
	if w := do(d, "GET", "/assets/app.js", ""); w.Body.String() != "app" {
		t.Fatalf("asset = %q", w.Body)
	}

	// Signing in goes to VRChat only, and the session stays with the app.
	w := do(d, "POST", "/api/account/login", `{"username":"tester","password":"synthetic passphrase"}`)
	if w.Code != 200 || w.Header().Get("Set-Cookie") != "" || len(remoteSaw) != 0 || vrchat != 1 {
		t.Fatalf("login = %d %s, cookies %q, remote %d", w.Code, w.Body, w.Header().Values("Set-Cookie"), len(remoteSaw))
	}
	if len(d.store.cookies(false)) != 1 {
		t.Fatalf("local cookies = %v", d.store.cookies(false))
	}
	if w := do(d, "GET", "/api/account/device", ""); !strings.Contains(w.Body.String(), `"username":"tester"`) {
		t.Fatalf("device = %s", w.Body)
	}
	if !(verifier{d}).Ready() {
		t.Fatal("logger does not see the device sign-in")
	}
	if public, err := (verifier{d}).Check(context.Background(), "usr_00000000-0000-0000-0000-000000000001"); err != nil || public {
		t.Fatalf("check of a non-avatar = %v %v", public, err)
	}

	// VRCove features go to the website, which sees only its own cookies.
	w = do(d, "POST", "/api/catalog?q=robot", `{}`)
	if len(remoteSaw) != 1 || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("remote = %d, cookies %q", len(remoteSaw), w.Header().Values("Set-Cookie"))
	}
	got := remoteSaw[0]
	if got.Header.Get("Origin") != remote.URL || strings.Contains(got.Header.Get("Cookie"), "atlas-vrc") || got.Header.Get("X-Forwarded-For") != "" {
		t.Fatalf("remote request headers = %v", got.Header)
	}
	if d.store.cookies(true)["__Host-veil-account"] != "cove-token" {
		t.Fatalf("remote cookies = %v", d.store.cookies(true))
	}
	do(d, "GET", "/api/cove/me", "")
	if c := remoteSaw[1].Header.Get("Cookie"); c != "__Host-veil-account=cove-token" {
		t.Fatalf("remote cookie sent = %q", c)
	}

	// Signing out clears the device session.
	do(d, "POST", "/api/account/logout", `{}`)
	if len(d.store.cookies(false)) != 0 || (verifier{d}).Ready() {
		t.Fatalf("after sign-out = %v", d.store.cookies(false))
	}
}
