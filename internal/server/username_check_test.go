package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLocalUsernameCheckResultsAndSharedBackoff(t *testing.T) {
	h, _ := newLocalHarness(t)
	now := time.Now()
	h.srv.now = func() time.Time { return now }
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/1/auth/user" {
			return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","username":"tester","displayName":"Tester"}`), "Set-Cookie", "auth=authcookie_local; Path=/"), nil
		}
		if r.URL.Path != "/api/1/auth/exists" || r.URL.Query().Get("email") != "" || r.URL.Query().Get("displayName") != "" {
			t.Fatalf("wrong query: %s", r.URL)
		}
		if !strings.Contains(r.Header.Get("Cookie"), "auth=authcookie_local") {
			t.Error("session cookie missing")
		}
		body := map[string]string{
			"taken":     `{"userExists":true,"nameOk":false}`,
			"free":      `{"userExists":false,"nameOk":true}`,
			"invalid":   `{"userExists":false,"nameOk":false}`,
			"unknown":   `{"userExists":false}`,
			"malformed": `{}`,
		}[r.URL.Query().Get("username")]
		if body == "" {
			return respond(429, "application/json", []byte(`{}`), "Retry-After", "3600"), nil
		}
		return respond(200, "application/json", []byte(body)), nil
	}
	w := h.do(post("/account/login", jsonBody(`{"username":"tester","password":"synthetic passphrase"}`)))
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == h.srv.cookieName() {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("login=%d %s", w.Code, w.Body)
	}
	for name, status := range map[string]string{"taken": "taken", "free": "available", "invalid": "invalid", "unknown": "unconfirmed"} {
		w := h.do(as(get("/account/username-check?username="+name), cookie))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"`+status+`"`) {
			t.Errorf("%s=%d %s", name, w.Code, w.Body)
		}
	}
	if w := h.do(as(get("/account/username-check?username=malformed"), cookie)); w.Code != 502 {
		t.Errorf("malformed=%d", w.Code)
	}
	w = h.do(as(get("/account/username-check?username=limit"), cookie))
	if w.Code != 429 || w.Header().Get("Retry-After") != "3600" {
		t.Fatalf("cooldown=%d %s", w.Code, w.Header().Get("Retry-After"))
	}
	before := h.fetches.Load()
	if w := h.do(as(get("/account/username-check?username=free"), cookie)); w.Code != 429 || h.fetches.Load() != before {
		t.Fatal("checker bypassed shared cooldown")
	}
	if w := h.do(as(get("/account/me"), cookie)); w.Code != 429 || h.fetches.Load() != before {
		t.Fatal("other API bypassed checker cooldown")
	}
	now = now.Add(time.Hour)
	if w := h.do(as(get("/account/username-check?username=free"), cookie)); w.Code != 200 {
		t.Errorf("after cooldown=%d %s", w.Code, w.Body)
	}
}
