package server

import (
	"net/http"
	"strings"
	"testing"
)

func newLocalHarness(t *testing.T) (*harness, *[]string) {
	t.Helper()
	var forwarded []string
	remote := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = append(forwarded, r.URL.Path)
		writeJSON(w, http.StatusOK, map[string]bool{"remote": true})
	})
	srv, err := New(Config{PublicOrigin: origin, SessionKey: testKey, Contact: "security-test@example.test", Local: true, Remote: remote}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, srv: srv, handler: srv.APIHandler()}
	srv.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		h.fetches.Add(1)
		return h.upstream(r)
	})
	return h, &forwarded
}

func TestLocalModeSignsInWithoutVRCoveAccount(t *testing.T) {
	h, forwarded := newLocalHarness(t)
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.vrchat.cloud" {
			t.Errorf("request left for %s", r.URL.Host)
		}
		return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","username":"tester","displayName":"Tester"}`), "Set-Cookie", "auth=authcookie_local; Path=/"), nil
	}
	w := h.do(post("/account/login", jsonBody(`{"username":"tester","password":"synthetic passphrase"}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("local login = %d %s", w.Code, w.Body)
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == h.srv.cookieName() && c.Value != "" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie after local login")
	}
	r := as(get("/account/me"), session)
	if id, name := h.srv.LocalUser(r); id != testVRCUser || name != "tester" {
		t.Fatalf("LocalUser = %q %q", id, name)
	}
	if w := h.do(as(get("/account/device"), session)); !strings.Contains(w.Body.String(), `"vrchatId":"`+testVRCUser+`"`) {
		t.Fatalf("device links = %s", w.Body)
	}
	if w := h.do(get("/account/device")); w.Body.String() != "{\"links\":[]}\n" {
		t.Fatalf("signed-out device links = %s", w.Body)
	}
	if w := h.do(r); w.Code != 200 || !strings.Contains(w.Body.String(), testVRCUser) {
		t.Fatalf("local /account/me = %d %s", w.Code, w.Body)
	}

	// VRCove's own features go to the website; nothing about them runs here.
	for _, path := range []string{"/catalog?q=robot", "/cove/me", "/logger/known", "/stats"} {
		if w := h.do(get(path)); !strings.Contains(w.Body.String(), `"remote":true`) {
			t.Errorf("%s was not forwarded: %s", path, w.Body)
		}
	}
	if len(*forwarded) != 4 {
		t.Fatalf("forwarded = %v", *forwarded)
	}

	// A session VRChat rejects is signed out on the device.
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(401, "application/json", []byte(`{}`)), nil
	}
	w = h.do(as(get("/account/me"), session))
	cleared := false
	for _, c := range w.Result().Cookies() {
		cleared = cleared || (c.Name == h.srv.cookieName() && c.MaxAge < 0)
	}
	if w.Code != 401 || !cleared {
		t.Fatalf("rejected local session = %d, cleared %v", w.Code, cleared)
	}
}

func TestLocalModeNeedsRemote(t *testing.T) {
	if _, err := New(Config{PublicOrigin: origin, Local: true}, nil, nil); err == nil {
		t.Fatal("local mode without a remote handler was accepted")
	}
}
