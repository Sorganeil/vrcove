package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLinkedSessionFailures(t *testing.T) {
	h := newConnectedHarness(t)
	for _, tt := range []struct {
		name, code string
		request    *http.Request
	}{
		{"missing", "vrchat_session_required", get("/account/me")},
		{"expired", "vrchat_session_expired", withSession(t, get("/account/me"), &Session{Auth: "old", Expires: time.Now().Add(-time.Minute).UnixMilli()})},
		{"invalid", "vrchat_session_invalid", as(get("/account/me"), &http.Cookie{Name: cookieName, Value: "invalid.cookie"})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := h.do(tt.request)
			var data map[string]any
			json.Unmarshal(w.Body.Bytes(), &data)
			if w.Code != 401 || data["code"] != tt.code {
				t.Fatalf("session failure = %d %s", w.Code, w.Body)
			}
			if !strings.Contains(data["error"].(string), "still signed in to VRCove") {
				t.Error("missing VRCove reassurance")
			}
			for _, c := range w.Result().Cookies() {
				if c.Name == cookieName {
					t.Error("late session failure can overwrite another active account")
				}
			}
			if got := h.do(get("/cove/me")); got.Code != 200 {
				t.Fatal("session failure signed out VRCove")
			}
		})
	}
	if h.fetches.Load() != 0 {
		t.Fatal("invalid local sessions reached VRChat")
	}
}

func TestUpstreamSessionInvalidation(t *testing.T) {
	h := newConnectedHarness(t)
	session := &Session{Auth: "active", Expires: time.Now().Add(time.Hour).UnixMilli()}
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(401, "application/json", []byte(`{"error":"private upstream diagnostic"}`)), nil
	}
	for _, r := range []*http.Request{get("/account/me"), post("/account/select", jsonBody(`{"avatarId":"avtr_00000000-0000-0000-0000-000000000001"}`))} {
		w := h.do(withSession(t, r, session))
		if w.Code != 401 || !strings.Contains(w.Body.String(), `"code":"vrchat_session_invalid"`) || strings.Contains(w.Body.String(), "private upstream") {
			t.Fatalf("upstream invalidation = %d %s", w.Code, w.Body)
		}
	}
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(403, "application/json", []byte(`{}`)), nil
	}
	w := h.do(withSession(t, get("/account/me"), session))
	if w.Code != 403 || strings.Contains(w.Body.String(), `"code"`) || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("permission denial invalidated session: %d %s", w.Code, w.Body)
	}
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(401, "application/json", []byte(`{}`)), nil
	}
	w = h.do(post("/account/login", jsonBody(`{"username":"wrong","password":"wrong"}`)))
	if w.Code != 401 || strings.Contains(w.Body.String(), `"code"`) {
		t.Fatal("bad credentials marked a linked session invalid")
	}
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(200, "application/json", []byte(`{"requiresTwoFactorAuth":["totp"]}`)), nil
	}
	w = h.do(withSession(t, get("/account/me"), session))
	if w.Code != 401 || !strings.Contains(w.Body.String(), `"code":"vrchat_session_invalid"`) {
		t.Fatal("incomplete authentication treated as connected")
	}
}

func TestConnectedSessionExpiryMetadata(t *testing.T) {
	h := newConnectedHarness(t)
	h.stubVRChatUser(testVRCUser, "DifferentHandle")
	session := &Session{Auth: "active", Expires: time.Now().Add(time.Hour).UnixMilli()}
	w := h.do(withSession(t, get("/account/me"), session))
	if w.Code != 200 || w.Header().Get("X-VRChat-Session-Expires") != strconv.FormatInt(session.Expires, 10) {
		t.Fatalf("expiry metadata = %d %v", w.Code, w.Header())
	}
	pending := &Session{AccountID: 1, Auth: "pending", Pending: []string{"totp"}, Expires: session.Expires}
	w = h.do(withSession(t, get("/account/me"), pending))
	if strings.Contains(w.Body.String(), `"code"`) || w.Header().Get("X-VRChat-Session-Expires") != "" {
		t.Fatal("pending verification marked as an expired active connection")
	}
}
