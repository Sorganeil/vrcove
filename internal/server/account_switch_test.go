package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type accountBrowser struct {
	h       *harness
	cookies map[string]*http.Cookie
}

func (b *accountBrowser) do(r *http.Request) *httptest.ResponseRecorder {
	for _, c := range b.cookies {
		r.AddCookie(c)
	}
	w := b.h.do(r)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return w
}
func newAccountBrowser(h *harness, c *http.Cookie) *accountBrowser {
	return &accountBrowser{h: h, cookies: map[string]*http.Cookie{c.Name: c}}
}
func (b *accountBrowser) login(name string) *httptest.ResponseRecorder {
	return b.do(post("/account/login", jsonBody(`{"username":"`+name+`","password":"synthetic"}`)))
}
func (b *accountBrowser) links() []any {
	return decode(b.h.t, b.do(get("/cove/me"))).(map[string]any)["links"].([]any)
}
func linkID(links []any, userID string) any {
	for _, l := range links {
		if row := l.(map[string]any); row["vrchatId"] == userID {
			return row["id"]
		}
	}
	return nil
}
func (b *accountBrowser) switchTo(id any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]any{"id": id})
	return b.do(post("/account/switch", strings.NewReader(string(raw))))
}

func TestMultipleVRChatSessions(t *testing.T) {
	h, _ := newAccountHarness(t)
	a := h.signup("multiple_owner", "synthetic passphrase")
	b := newAccountBrowser(h, a)
	var selectedCookie string
	revoked := false
	h.upstream = func(r *http.Request) (*http.Response, error) {
		name, _, basic := r.BasicAuth()
		id, display := testVRCUser, "First VRChat"
		if name == "second" || strings.Contains(r.Header.Get("Cookie"), "auth=second_token") {
			id, display = otherVRCUser, "Second VRChat"
		}
		if strings.Contains(r.URL.Path, "/select") {
			selectedCookie = r.Header.Get("Cookie")
			return respond(200, "application/json", []byte(`{}`)), nil
		}
		if revoked && id == otherVRCUser && !basic {
			return respond(401, "application/json", []byte(`{}`)), nil
		}
		header := []string{}
		if basic {
			token := "first_token"
			if name == "second" {
				token = "second_token"
			}
			header = []string{"Set-Cookie", "auth=" + token + "; Path=/"}
		}
		return respond(200, "application/json", []byte(`{"id":"`+id+`","displayName":"`+display+`"}`), header...), nil
	}
	if w := b.login("first"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := b.login("second"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	links := b.links()
	if len(links) != 2 {
		t.Fatalf("links = %v", links)
	}
	first, second := linkID(links, testVRCUser), linkID(links, otherVRCUser)
	if b.cookies[h.srv.linkedCookieName(testVRCUser)] == nil || b.cookies[h.srv.linkedCookieName(otherVRCUser)] == nil {
		t.Fatal("adding another account discarded the first session")
	}
	w := b.switchTo(first)
	if w.Code != 200 || !strings.Contains(w.Body.String(), testVRCUser) {
		t.Fatalf("switch first = %d %s", w.Code, w.Body)
	}
	act := post("/account/select", jsonBody(`{"avatarId":"avtr_00000000-0000-0000-0000-000000000001"}`))
	act.Header.Set("X-VRChat-Active-User", testVRCUser)
	if w := b.do(act); w.Code != 200 || !strings.Contains(selectedCookie, "first_token") {
		t.Fatalf("action used wrong session: %d %s", w.Code, selectedCookie)
	}
	if w := b.switchTo(second); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	// Stale tabs must not perform an action using the newly active account.
	stale := post("/account/select", jsonBody(`{"avatarId":"avtr_00000000-0000-0000-0000-000000000001"}`))
	stale.Header.Set("X-VRChat-Active-User", testVRCUser)
	selectedCookie = ""
	if w := b.do(stale); w.Code != 409 || selectedCookie != "" {
		t.Fatalf("stale action = %d, upstream cookie %q", w.Code, selectedCookie)
	}
	fetches := h.fetches.Load()
	staleLogout := post("/account/logout", jsonBody(`{}`))
	staleLogout.Header.Set("X-VRChat-Active-User", testVRCUser)
	if w := b.do(staleLogout); w.Code != 409 || h.fetches.Load() != fetches {
		t.Fatal("a stale tab disconnected the newly active account")
	}
	// Rotating tokens for an in-flight request must not overwrite the active selector.
	firstSession := unseal(b.cookies[h.srv.linkedCookieName(testVRCUser)].Value, testKey, time.Now())
	firstSession.Auth = "rotated_first"
	rotated := httptest.NewRecorder()
	h.srv.writeWithSession(rotated, map[string]bool{"ok": true}, firstSession)
	for _, c := range rotated.Result().Cookies() {
		if c.Name == cookieName {
			t.Fatal("background response overwrites active account")
		}
	}
	b.switchTo(first)
	revoked = true
	activeBefore := b.cookies[cookieName].Value
	w = b.switchTo(second)
	if w.Code != 401 || !strings.Contains(w.Body.String(), "vrchat_switch_reconnect") || b.cookies[cookieName].Value != activeBefore {
		t.Fatalf("invalid target changed active: %d %s", w.Code, w.Body)
	}
	if w := b.do(get("/account/me")); w.Code != 200 || !strings.Contains(w.Body.String(), testVRCUser) {
		t.Fatal("failed switch lost current session")
	}
	// A late failure for another account only invalidates that account's cookie.
	oldRequest := get("/account/me")
	oldRequest.AddCookie(&http.Cookie{Name: cookieName, Value: activeBefore})
	failure := httptest.NewRecorder()
	h.srv.invalidateRequestSession(failure, oldRequest)
	for _, c := range failure.Result().Cookies() {
		if c.Name == cookieName || c.Name == h.srv.linkedCookieName(otherVRCUser) {
			t.Fatal("late failure affected another account")
		}
	}
	// Reconnecting a chosen link must not silently activate a different account.
	expectedRequest := post("/account/login", jsonBody(`{"username":"second","password":"synthetic","expectedUserId":"`+testVRCUser+`"}`))
	before := b.cookies[cookieName].Value
	if w := b.do(expectedRequest); w.Code != 409 || b.cookies[cookieName].Value != before {
		t.Fatal("reconnecting the wrong account changed the active selection")
	}
	// A different VRCove owner cannot select this owner's link or session.
	other := newAccountBrowser(h, h.signup("different_owner", "synthetic passphrase"))
	other.cookies[h.srv.linkedCookieName(testVRCUser)] = b.cookies[h.srv.linkedCookieName(testVRCUser)]
	if w := other.switchTo(first); w.Code != 404 {
		t.Errorf("cross-owner switch = %d", w.Code)
	}
	// Expired targets fail locally, preserving the current account.
	expired := *firstSession
	expired.Expires = time.Now().Add(-time.Minute).UnixMilli()
	value, _ := seal(&expired, testKey)
	b.cookies[h.srv.linkedCookieName(testVRCUser)] = &http.Cookie{Name: h.srv.linkedCookieName(testVRCUser), Value: value}
	if w := b.switchTo(first); w.Code != 401 {
		t.Errorf("expired switch = %d", w.Code)
	}
	// VRCove logout clears every saved browser session and pending login.
	if w := b.do(post("/cove/logout", jsonBody(`{}`))); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for name := range b.cookies {
		if strings.HasPrefix(name, cookieName) {
			t.Errorf("logout retained %s", name)
		}
	}
}

func TestAddingTwoFactorAccountPreservesActiveSession(t *testing.T) {
	h := newConnectedHarness(t)
	b := newAccountBrowser(h, h.accountCookie)
	existing := &Session{AccountID: 1, UserID: testVRCUser, Auth: "existing_token", Expires: time.Now().Add(time.Hour).UnixMilli()}
	value, _ := seal(existing, testKey)
	b.cookies[cookieName] = &http.Cookie{Name: cookieName, Value: value}
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			return respond(200, "application/json", []byte(`{"requiresTwoFactorAuth":["totp"]}`), "Set-Cookie", "auth=pending_new; Path=/"), nil
		}
		return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","displayName":"Existing"}`)), nil
	}
	if w := b.login("new"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if b.cookies[cookieName].Value != value || b.cookies[cookieName+"-pending"] == nil {
		t.Fatal("pending login replaced active session")
	}
	if w := b.do(get("/account/me")); w.Code != 200 || !strings.Contains(w.Body.String(), testVRCUser) {
		t.Fatal("active session unavailable while verifying another account")
	}
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/verify") {
			return respond(200, "application/json", []byte(`{"verified":true}`)), nil
		}
		return respond(401, "application/json", []byte(`{}`)), nil
	}
	before := b.cookies[cookieName].Value
	failed := b.do(post("/account/2fa", jsonBody(`{"type":"totp","code":"123456"}`)))
	if failed.Code != 401 || strings.Contains(failed.Body.String(), `"code"`) || b.cookies[cookieName].Value != before {
		t.Fatal("failed verification invalidated a different active account")
	}
	if saved := unseal(b.cookies[h.srv.linkedCookieName(testVRCUser)].Value, testKey, time.Now()); saved == nil || saved.Auth != "existing_token" {
		t.Fatal("failed verification invalidated the saved active session")
	}
	// Retry with a fresh pending session and complete verification.
	pending := &Session{AccountID: 1, Auth: "pending_new", Pending: []string{"totp"}, Expires: time.Now().Add(pendingLifetime).UnixMilli()}
	pendingValue, _ := seal(pending, testKey)
	b.cookies[cookieName+"-pending"] = &http.Cookie{Name: cookieName + "-pending", Value: pendingValue}
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/verify") {
			return respond(200, "application/json", []byte(`{"verified":true}`)), nil
		}
		return respond(200, "application/json", []byte(`{"id":"`+otherVRCUser+`","displayName":"New account"}`)), nil
	}
	if w := b.do(post("/account/2fa", jsonBody(`{"type":"totp","code":"123456"}`))); w.Code != 200 {
		t.Fatalf("complete second account: %d %s", w.Code, w.Body)
	}
	if b.cookies[h.srv.linkedCookieName(testVRCUser)] == nil || b.cookies[h.srv.linkedCookieName(otherVRCUser)] == nil || b.cookies[cookieName+"-pending"] != nil {
		t.Fatal("completed verification did not preserve both accounts and clear pending login")
	}
}

func TestReconnectUsernameRecovery(t *testing.T) {
	for _, fromCookie := range []bool{false, true} {
		name := "verified profile"
		if fromCookie {
			name = "expired bound cookie"
		}
		t.Run(name, func(t *testing.T) {
			h, _ := newAccountHarness(t)
			b := newAccountBrowser(h, h.signup("reconnect_owner", "synthetic passphrase"))
			h.upstream = func(*http.Request) (*http.Response, error) {
				return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","displayName":"Different display","username":"login_name"}`), "Set-Cookie", "auth=synthetic; Path=/"), nil
			}
			if w := b.login("login_name"); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			// Simulate a link created before login usernames were retained.
			raw, _ := json.Marshal(linkRecord{VRChatID: testVRCUser, DisplayName: "Different display"})
			sealed, err := sealBytes(h.srv.keys.linkSeal, raw, linkAAD)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.srv.accounts.Exec("UPDATE vrchat_links SET sealed = ?", sealed); err != nil {
				t.Fatal(err)
			}
			if fromCookie {
				for key, cookie := range b.cookies {
					if key != h.srv.cookieName() && key != h.srv.linkedCookieName(testVRCUser) {
						continue
					}
					session := unseal(cookie.Value, testKey, time.Now())
					session.Expires = time.Now().Add(-time.Minute).UnixMilli()
					encoded, err := seal(session, testKey)
					if err != nil {
						t.Fatal(err)
					}
					cookie.Value = encoded
				}
			} else {
				// Old sessions may lack the login name too; /auth/user can restore it.
				for key, cookie := range b.cookies {
					if key != h.srv.cookieName() && key != h.srv.linkedCookieName(testVRCUser) {
						continue
					}
					session := unseal(cookie.Value, testKey, time.Now())
					session.Username = ""
					encoded, err := seal(session, testKey)
					if err != nil {
						t.Fatal(err)
					}
					cookie.Value = encoded
				}
				if w := b.do(get("/account/me")); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
				delete(b.cookies, h.srv.cookieName())
				delete(b.cookies, h.srv.linkedCookieName(testVRCUser))
			}
			links := b.links()
			if got := links[0].(map[string]any)["username"]; got != "login_name" {
				t.Fatalf("reconnect username = %v", got)
			}
			if fromCookie {
				// Names from another VRCove owner's cookie must not be used.
				for key, cookie := range b.cookies {
					if key != h.srv.cookieName() && key != h.srv.linkedCookieName(testVRCUser) {
						continue
					}
					session := unseal(cookie.Value, testKey, time.UnixMilli(0))
					session.AccountID++
					encoded, err := seal(session, testKey)
					if err != nil {
						t.Fatal(err)
					}
					cookie.Value = encoded
				}
				if got := b.links()[0].(map[string]any)["username"]; got != "" {
					t.Fatalf("cross-owner reconnect username = %v", got)
				}
			}
		})
	}
}
