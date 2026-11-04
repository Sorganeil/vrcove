package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
)

const testBotUser = "usr_00000000-0000-0000-0000-00000000b07a"

// newLoggerHarness enables ingestion with a bot account but does not sign it
// in. Most tests call signInBot so each lookup is one upstream request.
func newLoggerHarness(t *testing.T) *harness {
	t.Helper()
	h, _ := newAccountHarness(t)
	writer, err := OpenCatalogWriter(filepath.Join(t.TempDir(), "logger.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	h.srv.cfg.Logger = &LoggerConfig{Catalog: writer, Bot: BotConfig{Username: "vrcove_bot", Password: "synthetic bot password"}}
	return h
}

// newLinkedLoggerHarness also signs a browser in with a linked VRChat
// account, for tests where that account owns avatars.
func newLinkedLoggerHarness(t *testing.T) (*harness, *accountBrowser) {
	t.Helper()
	h, cookie, _ := sessionHarness(t)
	browser := newAccountBrowser(h, cookie)
	browserLogin(t, browser, computerAgent)
	writer, err := OpenCatalogWriter(filepath.Join(t.TempDir(), "logger.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	h.srv.cfg.Logger = &LoggerConfig{Catalog: writer, Bot: BotConfig{Username: "vrcove_bot", Password: "synthetic bot password"}}
	signInBot(t, h)
	return h, browser
}

func signInBot(t *testing.T, h *harness) {
	t.Helper()
	h.srv.bot.mu.Lock()
	defer h.srv.bot.mu.Unlock()
	h.srv.bot.tokens, h.srv.bot.userID = vrchat.Tokens{Auth: "bot_auth", TwoFactorAuth: "bot_2fa"}, testBotUser
	if err := h.srv.saveBot(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLoggerVerifiesBeforeDeduplicating(t *testing.T) {
	h := newLoggerHarness(t)
	signInBot(t, h)
	writer := h.srv.cfg.Logger.Catalog
	id := "avtr_12345678-1234-1234-1234-123456789abc"
	public := true
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/1/avatars/"+id {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "auth=bot_auth") {
			t.Fatalf("lookup without the bot session: %q", cookie)
		}
		visibility := "private"
		if public {
			visibility = "public"
		}
		return respond(200, "application/json", []byte(`{"id":"`+id+`","name":"Test avatar","authorName":"Creator","description":"Searchable description","tags":["test"],"releaseStatus":"`+visibility+`","unityPackages":[{"platform":"standalonewindows","performanceRating":"Good"}]}`)), nil
	}
	for _, want := range []string{"added", "existing", "rejected"} {
		if want == "rejected" {
			public = false
		}
		before := h.fetches.Load()
		// Submissions need no key or VRCove account.
		w := h.do(post("/logger/avatars", strings.NewReader(`{"ids":["`+id+`"]}`)))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var result struct {
			Results []loggerResult `json:"results"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Results) != 1 || result.Results[0].Status != want {
			t.Fatalf("wanted %s, got %s", want, w.Body.String())
		}
		if h.fetches.Load() != before+1 {
			t.Fatal("duplicate was not verified with VRChat first")
		}
	}
	var count int
	if err := writer.QueryRow("SELECT count(*) FROM catalog WHERE id=?", id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("catalog rows %d, err %v", count, err)
	}
	if err := writer.QueryRow("SELECT count(*) FROM catalog_fts WHERE catalog_fts MATCH 'Searchable'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("search index rows %d, err %v", count, err)
	}
}

func TestLoggerTransientFailureStaysRetryable(t *testing.T) {
	h := newLoggerHarness(t)
	signInBot(t, h)
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(429, "application/json", []byte(`{}`)), nil
	}
	w := h.do(post("/logger/avatars", strings.NewReader(`{"ids":["avtr_12345678-1234-1234-1234-123456789abc"]}`)))
	if w.Code != 429 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// botUpstream answers the bot's sign-in and avatar lookups. A sign-in asks
// for an email code unless it carries the remembered twoFactorAuth cookie.
func botUpstream(t *testing.T, h *harness, logins *int, expired *bool) {
	h.upstream = func(r *http.Request) (*http.Response, error) {
		cookie := r.Header.Get("Cookie")
		switch {
		case r.URL.Path == "/api/1/auth/user" && r.Header.Get("Authorization") != "":
			*logins++
			if !strings.Contains(cookie, "twoFactorAuth=bot_2fa") {
				return respond(200, "application/json", []byte(`{"requiresTwoFactorAuth":["emailOtp"]}`), "Set-Cookie", "auth=partial_auth; Path=/"), nil
			}
			*expired = false
			return respond(200, "application/json", []byte(`{"id":"`+testBotUser+`","displayName":"VRCove Bot"}`), "Set-Cookie", "auth=fresh_auth; Path=/"), nil
		case r.URL.Path == "/api/1/auth/twofactorauth/emailotp/verify":
			if !strings.Contains(cookie, "auth=partial_auth") {
				t.Errorf("verification without the partial session: %q", cookie)
			}
			return respond(200, "application/json", []byte(`{"verified":true}`), "Set-Cookie", "twoFactorAuth=bot_2fa; Path=/"), nil
		case r.URL.Path == "/api/1/auth/user":
			return respond(200, "application/json", []byte(`{"id":"`+testBotUser+`","displayName":"VRCove Bot"}`)), nil
		case strings.HasPrefix(r.URL.Path, "/api/1/avatars/"):
			if *expired || !strings.Contains(cookie, "auth=") {
				return respond(401, "application/json", []byte(`{}`)), nil
			}
			id := strings.TrimPrefix(r.URL.Path, "/api/1/avatars/")
			return respond(200, "application/json", []byte(`{"id":"`+id+`","releaseStatus":"private"}`)), nil
		}
		t.Errorf("unexpected upstream %s", r.URL)
		return respond(404, "application/json", []byte(`{}`)), nil
	}
}

func TestBotWaitsForEmailCodeThenUsesStoredSession(t *testing.T) {
	h := newLoggerHarness(t)
	now := time.Now()
	h.srv.now = func() time.Time { return now }
	logins, expired := 0, false
	botUpstream(t, h, &logins, &expired)
	submit := func() int { return loggerPost(h, "/logger/avatars", missingAvatar).Code }
	// A first sign-in needs an emailed code, which the server cannot read.
	if code := submit(); code != 503 || logins != 1 {
		t.Fatalf("without a stored session: %d after %d sign-ins", code, logins)
	}
	// Requests while it waits must not mail more codes.
	now = now.Add(botLoginGap - time.Minute)
	if code := submit(); code != 503 || logins != 1 {
		t.Fatalf("while waiting: %d after %d sign-ins", code, logins)
	}
	// The operator signs the bot in from the command line.
	name, err := h.srv.BotLogin(context.Background(), "vrcove_bot", "synthetic bot password", func(method string) (string, error) {
		if method != "emailOtp" {
			t.Fatalf("method %q", method)
		}
		return "123456", nil
	})
	if err != nil || name != "VRCove Bot" {
		t.Fatalf("bot-login: %q %v", name, err)
	}
	var sealed string
	if err := h.srv.accounts.QueryRow("SELECT sealed FROM bot_session").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "partial_auth") || strings.Contains(sealed, "bot_2fa") || strings.Contains(sealed, testBotUser) {
		t.Fatal("bot session stored in plaintext")
	}
	// A server process that never saw the sign-in picks the session up.
	h.srv.bot.mu.Lock()
	h.srv.bot.tokens, h.srv.bot.userID = vrchat.Tokens{}, ""
	h.srv.bot.mu.Unlock()
	if code := submit(); code != 200 {
		t.Fatalf("after bot-login: %d", code)
	}
	// When VRChat ends the session, the remembered device skips the code.
	expired = true
	if w := loggerPost(h, "/logger/avatars", missingAvatar); w.Code != 200 || logins != 3 {
		t.Fatalf("after expiry: %d %s, %d sign-ins", w.Code, w.Body, logins)
	}
}

func TestLoggerStoresNoUserSessions(t *testing.T) {
	h := newLoggerHarness(t)
	var tables int
	if err := h.srv.accounts.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'logger_keys'").Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("logger_keys tables %d, err %v", tables, err)
	}
	rows, err := h.srv.accounts.Query("SELECT name FROM pragma_table_info('vrchat_sessions')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name == "sealed" {
			t.Fatal("vrchat_sessions still stores sealed VRChat credentials")
		}
	}
}
