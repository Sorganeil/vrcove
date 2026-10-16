package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	phoneAgent    = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
	computerAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
)

// sessionHarness answers every VRChat call as testVRCUser and counts logouts.
func sessionHarness(t *testing.T) (*harness, *http.Cookie, *int) {
	h, _ := newAccountHarness(t)
	account := h.signup("session_owner", "synthetic passphrase")
	logouts := 0
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/1/logout" {
			logouts++
			return respond(200, "application/json", []byte(`{}`)), nil
		}
		return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","displayName":"SessionOwner","username":"vrc"}`), "Set-Cookie", "auth=synthetic_token; Path=/"), nil
	}
	return h, account, &logouts
}

func browserLogin(t *testing.T, b *accountBrowser, agent string) {
	t.Helper()
	r := post("/account/login", jsonBody(`{"username":"vrc","password":"synthetic"}`))
	r.Header.Set("User-Agent", agent)
	if w := b.do(r); w.Code != 200 {
		t.Fatalf("login = %d %s", w.Code, w.Body)
	}
}

func browserSessionsList(t *testing.T, b *accountBrowser) []map[string]any {
	t.Helper()
	links := b.links()
	if len(links) != 1 {
		t.Fatalf("links = %v", links)
	}
	var out []map[string]any
	for _, s := range links[0].(map[string]any)["sessions"].([]any) {
		out = append(out, s.(map[string]any))
	}
	return out
}

func endSession(b *accountBrowser, id any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]any{"id": id})
	return b.do(post("/cove/vrchat-sessions/end", strings.NewReader(string(raw))))
}

func TestSessionsListedAcrossDevicesAndEndedRemotely(t *testing.T) {
	h, account, logouts := sessionHarness(t)
	pc, phone := newAccountBrowser(h, account), newAccountBrowser(h, account)
	browserLogin(t, pc, computerAgent)
	browserLogin(t, phone, phoneAgent)

	// Both devices stay connected at once.
	for name, b := range map[string]*accountBrowser{"pc": pc, "phone": phone} {
		if w := b.do(get("/account/me")); w.Code != 200 {
			t.Fatalf("%s request = %d %s", name, w.Code, w.Body)
		}
	}
	sessions := browserSessionsList(t, pc)
	if len(sessions) != 2 {
		t.Fatalf("sessions = %v", sessions)
	}
	var phoneID any
	for _, s := range sessions {
		switch {
		case s["device"] == "computer" && s["browser"] == "Chrome" && s["current"] == true:
		case s["device"] == "phone" && s["browser"] == "Safari" && s["current"] == false:
			phoneID = s["id"]
		default:
			t.Fatalf("unexpected session %v", s)
		}
	}

	// The PC ends the phone's session: the phone is locked out, the PC is not.
	if w := endSession(pc, phoneID); w.Code != 200 {
		t.Fatalf("end = %d %s", w.Code, w.Body)
	}
	fetches := h.fetches.Load()
	if w := phone.do(get("/account/me")); w.Code != 401 || !strings.Contains(w.Body.String(), "vrchat_session_invalid") {
		t.Fatalf("ended phone session = %d %s", w.Code, w.Body)
	}
	if h.fetches.Load() != fetches {
		t.Fatal("ended session reached VRChat")
	}
	if *logouts != 0 {
		t.Fatal("ending another device's session should not reuse its token")
	}
	if w := pc.do(get("/account/me")); w.Code != 200 {
		t.Fatalf("pc after ending phone = %d", w.Code)
	}
	if got := browserSessionsList(t, pc); len(got) != 1 || got[0]["current"] != true {
		t.Fatalf("sessions after end = %v", got)
	}

	// Ending this browser's own session logs it out at VRChat and clears it here.
	if w := endSession(pc, browserSessionsList(t, pc)[0]["id"]); w.Code != 200 {
		t.Fatalf("end own = %d %s", w.Code, w.Body)
	}
	if *logouts != 1 {
		t.Fatalf("logouts = %d", *logouts)
	}
	if w := pc.do(get("/account/me")); w.Code == 200 {
		t.Fatal("own ended session still works")
	}
	if got := browserSessionsList(t, pc); len(got) != 0 {
		t.Fatalf("sessions after ending own = %v", got)
	}
}

func TestSessionsCannotBeEndedByOtherAccounts(t *testing.T) {
	h, account, _ := sessionHarness(t)
	owner := newAccountBrowser(h, account)
	browserLogin(t, owner, computerAgent)
	id := browserSessionsList(t, owner)[0]["id"]
	stranger := newAccountBrowser(h, h.signup("session_stranger", "synthetic passphrase"))
	if w := endSession(stranger, id); w.Code != 404 {
		t.Fatalf("stranger end = %d", w.Code)
	}
	if w := owner.do(get("/account/me")); w.Code != 200 {
		t.Fatalf("owner after stranger attempt = %d", w.Code)
	}
}

func TestOlderSessionsAreRegisteredOnUse(t *testing.T) {
	h := newConnectedHarness(t)
	h.stubVRChatUser(testVRCUser, "DifferentHandle")
	b := newAccountBrowser(h, h.accountCookie)
	var accountID int64
	if err := h.srv.accounts.QueryRow("SELECT id FROM accounts WHERE username = 'connected_fixture'").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.accounts.Exec("DELETE FROM vrchat_sessions"); err != nil {
		t.Fatal(err)
	}
	legacy, err := seal(&Session{AccountID: accountID, UserID: testVRCUser, Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}, testKey)
	if err != nil {
		t.Fatal(err)
	}
	b.cookies[cookieName] = &http.Cookie{Name: cookieName, Value: legacy}
	if w := b.do(get("/account/me")); w.Code != 200 {
		t.Fatalf("legacy session = %d %s", w.Code, w.Body)
	}
	if got := browserSessionsList(t, b); len(got) != 1 || got[0]["current"] != true {
		t.Fatalf("legacy session not registered: %v", got)
	}
}

func TestPasswordChangeEndsOtherDevicesSessions(t *testing.T) {
	h, account, _ := sessionHarness(t)
	pc, phone := newAccountBrowser(h, account), newAccountBrowser(h, account)
	browserLogin(t, pc, computerAgent)
	browserLogin(t, phone, phoneAgent)
	if w := pc.do(post("/cove/password", jsonBody(`{"current":"synthetic passphrase","next":"a new synthetic passphrase"}`))); w.Code != 200 {
		t.Fatalf("password = %d %s", w.Code, w.Body)
	}
	if got := browserSessionsList(t, pc); len(got) != 1 || got[0]["current"] != true {
		t.Fatalf("sessions after password change = %v", got)
	}
}

func TestDeviceKind(t *testing.T) {
	for ua, want := range map[string]string{
		phoneAgent:    "phone Safari",
		computerAgent: "computer Chrome",
		"Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Mobile Safari/537.36":     "phone Chrome",
		"Mozilla/5.0 (Linux; Android 15; SM-X910) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36":            "tablet Chrome",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:144.0) Gecko/20100101 Firefox/144.0":                                      "computer Firefox",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36 Edg/141.0": "computer Edge",
		"": "computer ",
	} {
		if device, browser := deviceKind(ua); device+" "+browser != want {
			t.Errorf("%q = %s %s, want %s", ua, device, browser, want)
		}
	}
}
