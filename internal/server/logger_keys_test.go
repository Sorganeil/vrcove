package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const loggerTestAvatar = "avtr_12345678-1234-1234-1234-123456789abc"

func submitLogger(h *harness, key string) int {
	return h.do(post("/logger/avatars", jsonBody(`{"ids":["`+loggerTestAvatar+`"]}`), "Authorization", "Bearer "+key)).Code
}
func loggerKeyID(t *testing.T, b *accountBrowser) int64 {
	t.Helper()
	w := b.do(get("/cove/logger-keys"))
	if w.Code != 200 {
		t.Fatalf("list keys %d", w.Code)
	}
	var out struct {
		Keys []struct {
			ID int64 `json:"id"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || len(out.Keys) != 1 {
		t.Fatal("expected one API key")
	}
	return out.Keys[0].ID
}
func revokeKey(b *accountBrowser, id int64) int {
	body, _ := json.Marshal(map[string]any{"id": id})
	return b.do(post("/cove/logger-keys/revoke", strings.NewReader(string(body)))).Code
}
func publicLoggerAvatar() []byte {
	return []byte(`{"id":"` + loggerTestAvatar + `","name":"Public avatar","releaseStatus":"public"}`)
}

func TestLoggerKeyStorageScopeAndSessionEnd(t *testing.T) {
	h, b, key := newLoggerHarness(t)
	var hash []byte
	var sealed string
	var keyID int64
	if err := h.srv.accounts.QueryRow("SELECT k.id,k.key_hash,v.sealed FROM logger_keys k JOIN vrchat_sessions v ON v.token_hash=k.session_hash").Scan(&keyID, &hash, &sealed); err != nil {
		t.Fatal(err)
	}
	if len(hash) != 32 || string(hash) == key || strings.Contains(sealed, "synthetic_token") || strings.Contains(sealed, testVRCUser) {
		t.Fatal("secret stored in plaintext")
	}
	listed := b.do(get("/cove/logger-keys"))
	if strings.Contains(listed.Body.String(), key) || strings.Contains(listed.Body.String(), "synthetic_token") {
		t.Fatal("key or VRChat credentials leaked in key list")
	}
	// A submission key cannot authenticate ordinary account APIs.
	if w := h.do(post("/cove/logger-keys/revoke", jsonBody(`{"id":1}`), "Authorization", "Bearer "+key)); w.Code != 401 {
		t.Fatalf("API key granted account access: %d", w.Code)
	}
	other := newAccountBrowser(h, h.signup("key_other", "synthetic passphrase"))
	if code := revokeKey(other, keyID); code != 404 {
		t.Fatalf("another account revoked key: %d", code)
	}
	if w := endSession(b, browserSessionsList(t, b)[0]["id"]); w.Code != 200 {
		t.Fatal("end session failed")
	}
	before := h.fetches.Load()
	if code := submitLogger(h, key); code != 401 || h.fetches.Load() != before {
		t.Fatal("ended session key reached upstream")
	}
	var count int
	if err := h.srv.accounts.QueryRow("SELECT count(*) FROM logger_keys").Scan(&count); err != nil || count != 0 {
		t.Fatal("session end did not cascade to API key")
	}
}
func TestLoggerKeyExpiryAndRegeneration(t *testing.T) {
	h, b, old := newLoggerHarness(t)
	first := loggerKeyID(t, b)
	key := generateLoggerKey(t, b)
	if loggerKeyID(t, b) == first {
		t.Fatal("key regeneration reused its ID")
	}
	before := h.fetches.Load()
	if code := submitLogger(h, old); code != 401 || h.fetches.Load() != before {
		t.Fatal("replaced key still works")
	}
	h.srv.now = func() time.Time { return time.Now().Add(15 * 24 * time.Hour) }
	if code := submitLogger(h, key); code != 401 || h.fetches.Load() != before {
		t.Fatal("expired session key reached upstream")
	}
}
func TestLoggerKeyUsesLinkedSessionAndSharesRotations(t *testing.T) {
	h, b, key := newLoggerHarness(t)
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/1/avatars/"+loggerTestAvatar {
			c, _ := r.Cookie("auth")
			if c == nil || c.Value != "synthetic_token" {
				t.Fatal("wrong metadata session")
			}
			return respond(200, "application/json", publicLoggerAvatar(), "Set-Cookie", "auth=rotated_logger_auth; Path=/"), nil
		}
		c, _ := r.Cookie("auth")
		if c == nil || c.Value != "rotated_logger_auth" {
			t.Fatal("browser did not use rotated session credentials")
		}
		return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","displayName":"SessionOwner","username":"vrc"}`)), nil
	}
	if code := submitLogger(h, key); code != 200 {
		t.Fatalf("submission: %d", code)
	}
	if w := b.do(get("/account/me")); w.Code != 200 {
		t.Fatalf("browser session after logger rotation: %d %s", w.Code, w.Body)
	}
	if code := revokeKey(b, loggerKeyID(t, b)); code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	before := h.fetches.Load()
	if code := submitLogger(h, key); code != 401 || h.fetches.Load() != before {
		t.Fatal("revoked key reached upstream")
	}
	if w := b.do(get("/account/me")); w.Code != 200 {
		t.Fatal("revoking API key ended browser session")
	}
}
func TestLoggerKeyRevokedDuringVerificationCannotInsert(t *testing.T) {
	h, b, key := newLoggerHarness(t)
	id := loggerKeyID(t, b)
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if _, err := h.srv.accounts.Exec("DELETE FROM logger_keys WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
		return respond(200, "application/json", publicLoggerAvatar()), nil
	}
	if code := submitLogger(h, key); code != 401 {
		t.Fatalf("revoked in flight key: %d", code)
	}
	var n int
	if err := h.srv.cfg.Logger.Catalog.QueryRow("SELECT count(*) FROM catalog").Scan(&n); err != nil || n != 0 {
		t.Fatal("revoked in flight key inserted avatar")
	}
}
func TestLoggerInvalidUpstreamSessionRevokesKey(t *testing.T) {
	h, _, key := newLoggerHarness(t)
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(401, "application/json", []byte(`{}`)), nil
	}
	if code := submitLogger(h, key); code != 401 {
		t.Fatalf("invalid upstream session: %d", code)
	}
	before := h.fetches.Load()
	if code := submitLogger(h, key); code != 401 || h.fetches.Load() != before {
		t.Fatal("invalidated session key reached upstream again")
	}
}

func TestLoggerKeysUseTheirOwnSessionsAcrossDevices(t *testing.T) {
	h, firstBrowser, firstKey := newLoggerHarness(t)
	secondBrowser := &accountBrowser{h: h, cookies: map[string]*http.Cookie{}}
	// Reuse only the VRCove login cookie, then establish a separate VRChat session.
	for _, c := range firstBrowser.cookies {
		if c.Name == accountCookieName {
			secondBrowser.cookies[c.Name] = c
		}
	}
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","displayName":"SessionOwner","username":"vrc"}`), "Set-Cookie", "auth=second_device_auth; Path=/"), nil
	}
	browserLogin(t, secondBrowser, phoneAgent)
	secondKey := generateLoggerKey(t, secondBrowser)
	expected := "synthetic_token"
	h.upstream = func(r *http.Request) (*http.Response, error) {
		cookie, _ := r.Cookie("auth")
		if cookie == nil || cookie.Value != expected {
			t.Fatal("API key used another device's VRChat session")
		}
		return respond(200, "application/json", publicLoggerAvatar()), nil
	}
	if code := submitLogger(h, firstKey); code != 200 {
		t.Fatalf("first device: %d", code)
	}
	expected = "second_device_auth"
	if code := submitLogger(h, secondKey); code != 200 {
		t.Fatalf("second device: %d", code)
	}
}

func TestLoggerKeyCreationChecksActiveAccount(t *testing.T) {
	h, b, _ := newLoggerHarness(t)
	before := h.fetches.Load()
	w := b.do(post("/cove/logger-keys", jsonBody(`{"vrchatId":"`+otherVRCUser+`"}`)))
	if w.Code != 409 || h.fetches.Load() != before {
		t.Fatal("stale account selection generated a key")
	}
}
