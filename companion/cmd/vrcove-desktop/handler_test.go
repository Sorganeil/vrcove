package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"vrcove/companion/internal/history"
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
	var remoteBodies []string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		remoteSaw = append(remoteSaw, r)
		remoteBodies = append(remoteBodies, string(body))
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
	w := do(d, "GET", "/settings", "")
	if !strings.Contains(w.Body.String(), "<title>VRCove") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatalf("page = %d %q csp=%q", w.Code, w.Body, w.Header().Get("Content-Security-Policy"))
	}
	if w := do(d, "GET", "/assets/app.js", ""); w.Body.String() != "app" {
		t.Fatalf("asset = %q", w.Body)
	}

	// Signing in goes to VRChat only, and the session stays with the app.
	w = do(d, "POST", "/api/account/login", `{"username":"tester","password":"synthetic passphrase"}`)
	if w.Code != 200 || w.Header().Get("Set-Cookie") != "" || len(remoteSaw) != 0 || vrchat != 1 {
		t.Fatalf("login = %d %s, cookies %q, remote %d", w.Code, w.Body, w.Header().Values("Set-Cookie"), len(remoteSaw))
	}
	if len(d.store.cookies(false)) != 2 {
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
	// WebKitGTK custom-scheme POSTs arrive with ContentLength == 0 on a non-empty stream.
	coveLogin := httptest.NewRequest("POST", "http://wails.localhost/api/cove/login", io.NopCloser(strings.NewReader(`{"username":"frog","password":"secret"}`)))
	coveLogin.ContentLength = 0
	coveLogin.Header.Set("Content-Type", "application/json")
	d.ServeHTTP(httptest.NewRecorder(), coveLogin)
	if len(remoteBodies) < 3 || remoteBodies[2] != `{"username":"frog","password":"secret"}` {
		t.Fatalf("remote POST body = %q", remoteBodies)
	}

	// Signing out clears the device session.
	do(d, "POST", "/api/account/logout", `{}`)
	if len(d.store.cookies(false)) != 0 || (verifier{d}).Ready() {
		t.Fatalf("after sign-out = %v", d.store.cookies(false))
	}
}

func TestDesktopRoutes(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	h, err := history.Open(filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	d.history = h
	var opened []string
	d.openURL = func(u string) error { opened = append(opened, u); return nil }
	var alerts []string
	d.notify = func(id, title, body string) { alerts = append(alerts, title+"|"+body) }

	for _, path := range []string{"/api/desktop/history", "/api/desktop/feed", "/api/desktop/screenshots", "/api/desktop/update"} {
		if w := do(d, "GET", path, ""); w.Code != 200 {
			t.Errorf("%s = %d %s", path, w.Code, w.Body)
		}
	}
	if w := do(d, "POST", "/api/desktop/update", `{"action":"unknown"}`); w.Code != 400 {
		t.Errorf("unknown update action = %d", w.Code)
	}
	if w := do(d, "GET", "/api/desktop/screenshot?file=../../etc/passwd", ""); w.Code != 404 {
		t.Errorf("screenshot outside the folder = %d", w.Code)
	}

	// Only https links leave the app, and they go to the system browser.
	do(d, "POST", "/api/desktop/open", `{"url":"https://vrcove.st/account"}`)
	for _, bad := range []string{"file:///etc/passwd", "http://example.com", "javascript:alert(1)", "https://user@example.com"} {
		if w := do(d, "POST", "/api/desktop/open", `{"url":"`+bad+`"}`); w.Code != 400 {
			t.Errorf("open %s = %d", bad, w.Code)
		}
	}
	if len(opened) != 1 || opened[0] != "https://vrcove.st/account" {
		t.Fatalf("opened = %v", opened)
	}

	// Notifications follow the settings, in the window's wording.
	invite := &history.Note{ID: "not_0123456789abcdef", Type: "invite", SenderUsername: "Mia", WorldName: "Cozy Room"}
	d.alert(invite)
	if len(alerts) != 0 {
		t.Fatal("alert shown while notifications are off")
	}
	do(d, "POST", "/api/desktop/notifications", `{"enabled":true,"kinds":["invite"],"text":{"invite":"{name} te invitó"}}`)
	d.alert(invite)
	d.alert(&history.Note{ID: "not_fedcba9876543210", Type: "friendRequest", SenderUsername: "Zoe"})
	if len(alerts) != 1 || alerts[0] != "Mia te invitó|Cozy Room" {
		t.Fatalf("alerts = %v", alerts)
	}
	do(d, "POST", "/api/desktop/notifications", `{"action":"test"}`)
	if len(alerts) != 2 || alerts[1] != "Notifications are on|New VRChat notifications will show up like this." {
		t.Fatalf("test alert = %v", alerts)
	}

	// Unresolved user IDs in the feed are resolved through the local VRChat session.
	d.api.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		hdr := http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"auth=authcookie_desktop; Path=/"}}
		switch r.URL.Path {
		case "/api/1/users/usr_a9a88016-df1c-4cdb-b1f5-072a41e6b7ce":
			return &http.Response{StatusCode: 200, Header: hdr, Request: r,
				Body: io.NopCloser(strings.NewReader(`{"id":"usr_a9a88016-df1c-4cdb-b1f5-072a41e6b7ce","displayName":"Starlight"}`))}, nil
		case "/api/1/profile/usr_a9a88016-df1c-4cdb-b1f5-072a41e6b7ce":
			return &http.Response{StatusCode: 200, Header: hdr, Request: r,
				Body: io.NopCloser(strings.NewReader(`{"bio":"hi"}`))}, nil
		default:
			return &http.Response{StatusCode: 200, Header: hdr, Request: r,
				Body: io.NopCloser(strings.NewReader(`{"id":"usr_00000000-0000-0000-0000-000000000001","username":"tester","displayName":"Tester"}`))}, nil
		}
	})
	do(d, "POST", "/api/account/login", `{"username":"tester","password":"synthetic passphrase"}`)
	if _, err := h.RecordFor(context.Background(), "usr_00000000-0000-0000-0000-000000000001", []byte(`{"type":"friend-offline","content":{"userId":"usr_a9a88016-df1c-4cdb-b1f5-072a41e6b7ce"}}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	feedResp := do(d, "GET", "/api/desktop/feed", "")
	if feedResp.Code != 200 || !strings.Contains(feedResp.Body.String(), `"displayName":"Starlight"`) {
		t.Fatalf("feed resolution = %d %s", feedResp.Code, feedResp.Body.String())
	}
}
