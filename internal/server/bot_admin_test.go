package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
)

const botAdminPassword = "panel bot password"

// botPanelUpstream is VRChat for the panel: the panel's password needs an
// email code, and any other password is refused.
type botPanelUpstream struct {
	logins, verifies, logouts int
	expired                   bool
	noCode                    bool
}

func (u *botPanelUpstream) serve(t *testing.T, h *harness) {
	t.Helper()
	h.upstream = func(r *http.Request) (*http.Response, error) {
		cookie := r.Header.Get("Cookie")
		switch {
		case r.URL.Path == "/api/1/auth/user" && r.Header.Get("Authorization") != "":
			u.logins++
			if r.Header.Get("Authorization") != vrchat.BasicAuth("panel_bot", botAdminPassword) {
				return respond(401, "application/json", []byte(`{"error":{"message":"Invalid Username/Email or Password"}}`)), nil
			}
			if u.noCode {
				return respond(200, "application/json", []byte(`{"id":"`+testBotUser+`","displayName":"Panel Bot"}`), "Set-Cookie", "auth=panel_auth; Path=/"), nil
			}
			return respond(200, "application/json", []byte(`{"requiresTwoFactorAuth":["emailOtp"]}`), "Set-Cookie", "auth=partial_auth; Path=/"), nil
		case strings.HasSuffix(r.URL.Path, "/twofactorauth/emailotp/verify"):
			u.verifies++
			if !strings.Contains(cookie, "auth=partial_auth") {
				t.Errorf("verify without the pending session: %q", cookie)
			}
			var code struct{ Code string }
			json.NewDecoder(r.Body).Decode(&code)
			if code.Code != "123456" {
				return respond(400, "application/json", []byte(`{"verified":false}`)), nil
			}
			return respond(200, "application/json", []byte(`{"verified":true}`), "Set-Cookie", "twoFactorAuth=panel_2fa; Path=/", "Set-Cookie", "auth=panel_auth; Path=/"), nil
		case r.URL.Path == "/api/1/auth/user":
			if u.expired {
				return respond(401, "application/json", []byte(`{"error":{"message":"Missing Credentials"}}`)), nil
			}
			return respond(200, "application/json", []byte(`{"id":"`+testBotUser+`","displayName":"Panel Bot"}`)), nil
		case r.URL.Path == "/api/1/logout":
			u.logouts++
			return respond(200, "application/json", []byte(`{"success":{"message":"Ok!"}}`)), nil
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		return respond(500, "application/json", []byte(`{}`)), nil
	}
}

func (h *harness) botAction(c *http.Cookie, body string) (int, map[string]any) {
	h.t.Helper()
	w := h.do(as(post("/cove/admin/bot", jsonBody(body)), c))
	var data map[string]any
	json.Unmarshal(w.Body.Bytes(), &data)
	return w.Code, data
}

func TestBackendAccountIsOwnerOnly(t *testing.T) {
	h := newLoggerHarness(t)
	owner, admin, user := h.signup("frog", "owner passphrase"), h.signup("mod_one", "admin passphrase"), h.signup("plain_user", "user passphrase")
	h.promote("frog", roleOwner)
	h.promote("mod_one", roleAdmin)
	if w := h.do(get("/cove/admin/bot")); w.Code != 404 {
		t.Errorf("anonymous reached the status: %d", w.Code)
	}
	for name, c := range map[string]*http.Cookie{"admin": admin, "user": user} {
		if w := h.do(as(get("/cove/admin/bot"), c)); w.Code != 404 {
			t.Errorf("%s reached the status: %d", name, w.Code)
		}
		if code, _ := h.botAction(c, `{"action":"logout"}`); code != 404 {
			t.Errorf("%s signed the bot out: %d", name, code)
		}
	}
	if w := h.do(as(get("/cove/admin/bot"), owner)); w.Code != 200 {
		t.Fatalf("owner status: %d %s", w.Code, w.Body)
	}
}

func TestOwnerSignsInBackendAccountWithCode(t *testing.T) {
	h := newLoggerHarness(t)
	owner := h.signup("frog", "owner passphrase")
	h.promote("frog", roleOwner)
	upstream := &botPanelUpstream{}
	upstream.serve(t, h)

	if code, data := h.botAction(owner, `{"action":"login","username":"panel_bot","password":"wrong"}`); code != 400 || data["error"] != "VRChat did not accept that username or password." {
		t.Fatalf("wrong password = %d %v", code, data)
	}
	code, data := h.botAction(owner, `{"action":"login","username":"panel_bot","password":"`+botAdminPassword+`"}`)
	if code != 200 || data["state"] != "code" || data["username"] != "panel_bot" {
		t.Fatalf("login = %d %v", code, data)
	}
	// The partial session must not look usable to the logger.
	h.srv.bot.mu.Lock()
	partial := h.srv.bot.tokens.Auth
	h.srv.bot.mu.Unlock()
	if partial != "" {
		t.Fatalf("pending sign-in reached the bot's tokens: %q", partial)
	}
	if code, data := h.botAction(owner, `{"action":"verify","method":"emailOtp","code":"000000"}`); code != 400 || data["error"] != "VRChat did not accept that code." {
		t.Fatalf("wrong code = %d %v", code, data)
	}
	if code, _ := h.botAction(owner, `{"action":"verify","method":"totp","code":"123456"}`); code != 400 {
		t.Fatalf("unoffered method = %d", code)
	}
	code, data = h.botAction(owner, `{"action":"verify","method":"emailOtp","code":"123456"}`)
	if code != 200 || data["state"] != "active" || data["displayName"] != "Panel Bot" || data["passwordSaved"] != true {
		t.Fatalf("verify = %d %v", code, data)
	}
	// Nothing secret reaches the browser or the log, and storage is sealed.
	if w := h.do(as(get("/cove/admin/bot"), owner)); strings.Contains(w.Body.String(), botAdminPassword) || strings.Contains(w.Body.String(), "panel_auth") || strings.Contains(w.Body.String(), "panel_2fa") {
		t.Fatalf("status leaks a secret: %s", w.Body)
	}
	var sealed string
	if err := h.srv.accounts.QueryRow("SELECT sealed FROM bot_session").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, botAdminPassword) || strings.Contains(sealed, "panel_auth") {
		t.Fatal("bot sign-in stored in plaintext")
	}
	var logged int
	if err := h.srv.accounts.QueryRow("SELECT count(*) FROM moderation_log WHERE action = 'signed in the backend account' AND actor = 'frog' AND target NOT LIKE '%panel_bot%'").Scan(&logged); err != nil || logged != 1 {
		t.Fatalf("log entries = %d %v", logged, err)
	}
	// A restart restores the session and the saved sign-in.
	h.srv.bot.mu.Lock()
	h.srv.bot.tokens, h.srv.bot.userID, h.srv.bot.saved = vrchat.Tokens{}, "", BotConfig{}
	h.srv.bot.mu.Unlock()
	if code, data := h.botAction(owner, `{"action":"check"}`); code != 200 || data["state"] != "active" || data["username"] != "panel_bot" {
		t.Fatalf("after restart = %d %v", code, data)
	}
}

func TestFailedSignInKeepsTheWorkingSession(t *testing.T) {
	h := newLoggerHarness(t)
	owner := h.signup("frog", "owner passphrase")
	h.promote("frog", roleOwner)
	upstream := &botPanelUpstream{}
	upstream.serve(t, h)
	h.srv.bot.mu.Lock()
	h.srv.bot.tokens, h.srv.bot.userID = vrchat.Tokens{Auth: "bot_auth", TwoFactorAuth: "bot_2fa"}, testBotUser
	h.srv.bot.mu.Unlock()
	if code, _ := h.botAction(owner, `{"action":"login","username":"panel_bot","password":"wrong"}`); code != 400 {
		t.Fatalf("wrong password = %d", code)
	}
	// A sign-in abandoned at the code step changes nothing either.
	if code, _ := h.botAction(owner, `{"action":"login","username":"panel_bot","password":"`+botAdminPassword+`"}`); code != 200 {
		t.Fatalf("login = %d", code)
	}
	code, data := h.botAction(owner, `{"action":"cancel"}`)
	if code != 200 || data["state"] != "active" {
		t.Fatalf("cancel = %d %v", code, data)
	}
	h.srv.bot.mu.Lock()
	defer h.srv.bot.mu.Unlock()
	if h.srv.bot.tokens.Auth != "bot_auth" {
		t.Fatalf("session replaced: %q", h.srv.bot.tokens.Auth)
	}
}

func TestPendingCodeExpires(t *testing.T) {
	now := time.Now()
	h := newLoggerHarness(t)
	h.srv.now = func() time.Time { return now }
	owner := h.signup("frog", "owner passphrase")
	h.promote("frog", roleOwner)
	upstream := &botPanelUpstream{}
	upstream.serve(t, h)
	if code, _ := h.botAction(owner, `{"action":"login","username":"panel_bot","password":"`+botAdminPassword+`"}`); code != 200 {
		t.Fatalf("login = %d", code)
	}
	now = now.Add(botCodeLifetime)
	if code, data := h.botAction(owner, `{"action":"verify","method":"emailOtp","code":"123456"}`); code != 409 {
		t.Fatalf("expired code = %d %v", code, data)
	}
	if upstream.verifies != 0 {
		t.Fatal("expired sign-in reached VRChat")
	}
}

func TestSignOutStopsAutomaticSignIn(t *testing.T) {
	h := newLoggerHarness(t)
	owner := h.signup("frog", "owner passphrase")
	h.promote("frog", roleOwner)
	upstream := &botPanelUpstream{noCode: true}
	upstream.serve(t, h)
	if code, data := h.botAction(owner, `{"action":"login","username":"panel_bot","password":"`+botAdminPassword+`"}`); code != 200 || data["state"] != "active" {
		t.Fatalf("login = %d %v", code, data)
	}
	code, data := h.botAction(owner, `{"action":"logout"}`)
	if code != 200 || data["state"] != "signed-out" || data["passwordSaved"] != false || data["displayName"] != "" {
		t.Fatalf("logout = %d %v", code, data)
	}
	if upstream.logouts != 1 {
		t.Fatalf("VRChat logouts = %d", upstream.logouts)
	}
	logins := upstream.logins
	if _, err := h.srv.botCall(context.Background(), vrchat.GetCurrentUser()); err != errBotUnavailable {
		t.Fatalf("signed-out call = %v", err)
	}
	// Signing out survives a restart, even with a sign-in in the environment.
	h.srv.bot.mu.Lock()
	h.srv.bot.signedOut = false
	h.srv.bot.mu.Unlock()
	if _, err := h.srv.botCall(context.Background(), vrchat.GetCurrentUser()); err != errBotUnavailable {
		t.Fatalf("call after restart = %v", err)
	}
	if upstream.logins != logins {
		t.Fatalf("signed in automatically: %d logins", upstream.logins-logins)
	}
	if code, _ := h.botAction(owner, `{"action":"login"}`); code != 400 {
		t.Fatalf("retry after sign-out = %d", code)
	}
}

func TestRetryUsesTheServersSignIn(t *testing.T) {
	h := newLoggerHarness(t)
	owner := h.signup("frog", "owner passphrase")
	h.promote("frog", roleOwner)
	h.srv.cfg.Logger.Bot = BotConfig{Username: "panel_bot", Password: botAdminPassword}
	upstream := &botPanelUpstream{}
	upstream.serve(t, h)
	if w := h.do(as(get("/cove/admin/bot"), owner)); !strings.Contains(w.Body.String(), `"canRetry":true`) {
		t.Fatalf("status = %s", w.Body)
	}
	code, data := h.botAction(owner, `{"action":"login"}`)
	if code != 200 || data["state"] != "code" {
		t.Fatalf("retry = %d %v", code, data)
	}
	code, data = h.botAction(owner, `{"action":"verify","method":"emailOtp","code":"123456"}`)
	if code != 200 || data["state"] != "active" {
		t.Fatalf("verify = %d %v", code, data)
	}
	// The environment already holds this password, so it isn't stored again.
	if data["passwordSaved"] != false {
		t.Fatal("environment password saved")
	}
}

func TestCheckReportsAnEndedSession(t *testing.T) {
	h := newLoggerHarness(t)
	owner := h.signup("frog", "owner passphrase")
	h.promote("frog", roleOwner)
	upstream := &botPanelUpstream{expired: true}
	upstream.serve(t, h)
	h.srv.bot.mu.Lock()
	h.srv.bot.tokens, h.srv.bot.userID = vrchat.Tokens{Auth: "bot_auth", TwoFactorAuth: "bot_2fa"}, testBotUser
	h.srv.bot.mu.Unlock()
	code, data := h.botAction(owner, `{"action":"check"}`)
	if code != 200 || data["state"] != "needs-sign-in" {
		t.Fatalf("check = %d %v", code, data)
	}
	if upstream.logins != 0 {
		t.Fatal("check signed in")
	}
	h.srv.bot.mu.Lock()
	defer h.srv.bot.mu.Unlock()
	if h.srv.bot.tokens.TwoFactorAuth != "bot_2fa" {
		t.Fatal("check dropped the twoFactorAuth cookie")
	}
}

func TestBotSignInAttemptsAreLimited(t *testing.T) {
	h := newLoggerHarness(t)
	owner := h.signup("frog", "owner passphrase")
	h.promote("frog", roleOwner)
	upstream := &botPanelUpstream{}
	upstream.serve(t, h)
	for i := 0; i < botSignIns; i++ {
		h.botAction(owner, `{"action":"login","username":"panel_bot","password":"wrong"}`)
	}
	if code, _ := h.botAction(owner, `{"action":"login","username":"panel_bot","password":"wrong"}`); code != 429 {
		t.Fatalf("over the limit = %d", code)
	}
	if upstream.logins != botSignIns {
		t.Fatalf("VRChat saw %d attempts", upstream.logins)
	}
}

func TestStatusReportsOnlyARealRateLimit(t *testing.T) {
	now := time.Now()
	h := newLoggerHarness(t)
	h.srv.now = func() time.Time { return now }
	owner := h.signup("frog", "owner passphrase")
	h.promote("frog", roleOwner)
	signInBot(t, h)
	limitedFor := func() float64 {
		t.Helper()
		_, data := h.botAction(owner, `{"action":"cancel"}`)
		return data["rateLimitedFor"].(float64)
	}
	if got := limitedFor(); got != 0 {
		t.Fatalf("no 429 yet, but limited for %v", got)
	}
	h.srv.backoff.limited("u:"+testBotUser, 30*time.Second, now)
	if got := limitedFor(); got != 30 {
		t.Fatalf("after a 429: %v", got)
	}
	now = now.Add(31 * time.Second)
	if got := limitedFor(); got != 0 {
		t.Fatalf("after the cooldown: %v", got)
	}
	// A limit on the whole server address stops the bot's sign-ins as well.
	h.srv.backoff.limited(serverKey, 20*time.Second, now)
	if got := limitedFor(); got != 20 {
		t.Fatalf("address-wide limit: %v", got)
	}
}

func TestLoggerStartsWithoutABotSignIn(t *testing.T) {
	h := newLoggerHarness(t)
	h.srv.cfg.Logger.Bot = BotConfig{}
	if _, err := h.srv.botCall(context.Background(), vrchat.GetCurrentUser()); err != errBotUnavailable {
		t.Fatalf("call without a sign-in = %v", err)
	}
}
