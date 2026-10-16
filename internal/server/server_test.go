package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

// Every upstream request in these tests goes to a stub; nothing reaches VRChat.

const origin = "https://vrcove.example.test"

var testKey = []byte("0123456789abcdef0123456789abcdef")

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func respond(status int, contentType string, body []byte, header ...string) *http.Response {
	h := http.Header{"Content-Type": {contentType}}
	for i := 0; i+1 < len(header); i += 2 {
		h.Add(header[i], header[i+1])
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: -1}
}

type harness struct {
	accountCookie *http.Cookie
	t             *testing.T
	srv           *Server
	handler       http.Handler
	fetches       atomic.Int32
	upstream      func(*http.Request) (*http.Response, error)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := OpenCatalog(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv, err := New(Config{PublicOrigin: origin, SessionKey: testKey, Contact: "security-test@example.test"}, db,
		fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>VRCove</title>")}})
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, srv: srv, handler: srv.Handler()}
	srv.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		h.fetches.Add(1)
		if h.upstream == nil {
			return nil, errors.New("unexpected upstream request blocked by the test")
		}
		return h.upstream(r)
	})
	return h
}

func (h *harness) do(r *http.Request) *httptest.ResponseRecorder {
	if r.RemoteAddr == "" || r.RemoteAddr == "192.0.2.1:1234" {
		r.RemoteAddr = "127.0.0.1:50000"
	}
	if h.accountCookie != nil {
		if _, err := r.Cookie(accountCookieName); err != nil {
			r.AddCookie(h.accountCookie)
		}
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w
}

func post(path string, body io.Reader, header ...string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api"+path, body)
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	return r
}

func get(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api"+path, nil)
}

func withSession(t *testing.T, r *http.Request, s *Session) *http.Request {
	if s.AccountID == 0 {
		s.AccountID = 1
		s.UserID = testVRCUser
	}
	value, err := seal(s, testKey)
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(&http.Cookie{Name: cookieName, Value: value})
	return r
}

func errorMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	var body struct{ Error string }
	json.Unmarshal(w.Body.Bytes(), &body)
	return body.Error
}

func TestSessionEncryption(t *testing.T) {
	now := time.Now()
	s := &Session{Auth: "authcookie_synthetic_test", TwoFactorAuth: "synthetic_2fa", Expires: now.Add(time.Minute).UnixMilli()} // gitleaks:allow -- synthetic session-encryption test fixture
	token, err := seal(s, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := unseal(token, testKey, now); got == nil || got.Auth != s.Auth || got.TwoFactorAuth != s.TwoFactorAuth || got.Expires != s.Expires || got.Pending != nil {
		t.Fatalf("round trip = %+v", got)
	}
	iv, data, _ := strings.Cut(token, ".")
	raw := []byte(data)
	raw[0] ^= 1
	for name, bad := range map[string]string{
		"tampered":  iv + "." + string(raw),
		"malformed": "invalid.cookie",
		"no dot":    "invalid",
	} {
		if unseal(bad, testKey, now) != nil {
			t.Errorf("%s token accepted", name)
		}
	}
	if unseal(token, []byte("fedcba9876543210fedcba9876543210"), now) != nil {
		t.Error("wrong key accepted")
	}
	expired, _ := seal(&Session{Auth: "x", Expires: now.Add(-time.Millisecond).UnixMilli()}, testKey)
	if unseal(expired, testKey, now) != nil {
		t.Error("expired session accepted")
	}
}

// Cookies sealed by the former TypeScript/WebCrypto backend must stay valid.
func TestLegacyCookies(t *testing.T) {
	now := time.Now()
	s := unseal("S2r7yv+a7rWacFSy.xqs7kho67VQy+yAPlriQUGySFfTRh4JNR2XJGxCi5lawXvcK8xqxKSeFarFYq16+6YP7RVxOC+F6xH2FcOoUrbfaFOcMKnPklTFlAmF42voUauOaOL7TXFcByw2sEXRWVQ==", testKey, now)
	if s == nil || s.Auth != "authcookie_legacy" || s.TwoFactorAuth != "legacy_2fa" || s.Pending != nil {
		t.Fatalf("legacy session = %+v", s)
	}
	pending := unseal("APiAPC9nAOsBmaqX./d8UjrXepEykxmqP0qyDg13JRPUr+W64M5wZMJ7IcvvK+T63jkFl6tHDfgfnCV7IgxE/gGW2PXJkjnt4vpPfY4UIYE4O1ko7+Ps8NXYEMNdQ36twy+jJ9zNyZ0j6Qg==", testKey, now)
	if pending == nil || len(pending.Pending) != 2 {
		t.Fatalf("legacy pending session = %+v", pending)
	}
}

func TestSessionCookieFlags(t *testing.T) {
	h := newHarness(t)
	s := &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}
	cookie, err := h.srv.sessionCookie(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"__Host-atlas-vrc=", "; Path=/;", "; HttpOnly;", "; SameSite=Strict;", "; Secure;"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("cookie %q lacks %q", cookie, want)
		}
	}
	if strings.Contains(cookie, "Domain=") {
		t.Error("cookie must be host-only")
	}
	r := httptest.NewRequest(http.MethodGet, "/api/account/me", nil)
	r.Header.Set("Cookie", strings.SplitN(cookie, ";", 2)[0])
	if got := h.srv.readSession(r); got == nil || got.Auth != "a" {
		t.Fatalf("cookie did not round trip: %+v", got)
	}
	cleared, _ := h.srv.sessionCookie(nil)
	if !strings.HasSuffix(cleared, "Max-Age=0") {
		t.Errorf("cleared cookie = %q", cleared)
	}

	local, _ := New(Config{PublicOrigin: "http://localhost:5173", SessionKey: testKey}, nil, nil)
	if cookie, _ := local.sessionCookie(s); !strings.HasPrefix(cookie, "atlas-vrc-local=") || strings.Contains(cookie, "Secure") {
		t.Errorf("local cookie = %q", cookie)
	}
}

func TestRequestBodies(t *testing.T) {
	h := newConnectedHarness(t)
	// A streamed body has no Content-Length, so the cap must apply while reading.
	streamed := post("/account/login", io.MultiReader(bytes.NewReader(make([]byte, 8193))))
	streamed.ContentLength = -1
	if w := h.do(streamed); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized stream = %d", w.Code)
	}
	for _, body := range []string{"{", "null", "[]", `{"a":1} {}`, "\xff"} {
		if w := h.do(post("/account/login", strings.NewReader(body))); w.Code != http.StatusBadRequest {
			t.Errorf("body %q = %d", body, w.Code)
		}
	}
	if w := h.do(post("/account/login", strings.NewReader("{}"), "Origin", "https://other.example.test")); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin write = %d", w.Code)
	}
	r := post("/account/login", strings.NewReader("{}"))
	r.Header.Del("Origin")
	if w := h.do(r); w.Code != http.StatusForbidden {
		t.Errorf("write without Origin = %d", w.Code)
	}
	if n := h.fetches.Load(); n != 0 {
		t.Errorf("%d upstream requests", n)
	}
}

func TestSignInRateLimit(t *testing.T) {
	h := newConnectedHarness(t)
	attempt := func(ip, remote string) *httptest.ResponseRecorder {
		r := post("/account/login", strings.NewReader(`{"username":"","password":""}`), "CF-Connecting-IP", ip)
		r.RemoteAddr = remote
		return h.do(r)
	}
	var w *httptest.ResponseRecorder
	for range 13 {
		w = attempt("192.0.2.3", "127.0.0.1:40000")
	}
	if w.Code != http.StatusTooManyRequests || !strings.Contains(errorMessage(t, w), "Too many sign-in attempts") {
		t.Fatalf("13th attempt = %d %s", w.Code, w.Body)
	}
	if w := attempt("192.0.2.4", "127.0.0.1:40000"); w.Code != http.StatusBadRequest {
		t.Errorf("other tunnel client = %d", w.Code)
	}
	// A direct, non-loopback peer cannot pick its bucket with the header.
	for range 12 {
		attempt("198.51.100.1", "203.0.113.9:1234")
	}
	if w := attempt("198.51.100.2", "203.0.113.9:1234"); w.Code != http.StatusTooManyRequests {
		t.Errorf("spoofed header bypassed the limit: %d", w.Code)
	}
	if n := h.fetches.Load(); n != 0 {
		t.Errorf("%d upstream requests", n)
	}
}

func imageRequest(target string) *http.Request {
	return get("/image?url=" + url.QueryEscape(target))
}

func TestImageProxy(t *testing.T) {
	h := newHarness(t)
	png := []byte{137, 80, 78, 71}
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" {
			t.Errorf("image request to %s carried cookies", r.URL)
		}
		return respond(200, "image/png", png), nil
	}
	w := h.do(imageRequest("https://assets.vrchat.com/test.png"))
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), png) {
		t.Fatalf("image = %d %v", w.Code, w.Body.Bytes())
	}
	for header, want := range map[string]string{
		"Content-Security-Policy": "sandbox; default-src 'none'", "Cache-Control": "private, no-store",
		"Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "Content-Type": "image/png",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	for _, kind := range []string{"image/svg+xml", "text/html", "application/javascript"} {
		h.upstream = func(*http.Request) (*http.Response, error) {
			return respond(200, kind, []byte("<svg><script>alert(1)</script></svg>")), nil
		}
		if w := h.do(imageRequest("https://assets.vrchat.com/test.png")); w.Code != http.StatusNotFound {
			t.Errorf("%s = %d", kind, w.Code)
		}
	}

	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(200, "image/png", make([]byte, maxImageBytes+1)), nil
	}
	if w := h.do(imageRequest("https://assets.vrchat.com/test.png")); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized image = %d", w.Code)
	}
}

func TestImageAllowList(t *testing.T) {
	h := newHarness(t)
	for _, target := range []string{
		"http://assets.vrchat.com/a.png",
		"https://127.0.0.1/",
		"https://user:pass@assets.vrchat.com/a.png",
		"https://assets.vrchat.com:8443/a.png",
		"https://api.vrchat.cloud/api/1/auth/user",
		"https://api.vrchat.cloud/api/1/file/file_abc/../../auth/user",
		"https://api.vrchat.cloud/api/1/file/file_abc/%2e%2e/%2e%2e/auth/user",
		"https://files.vrchat.cloud/secret.json",
		"https://s3-us-west-2.amazonaws.com/other-bucket/a.png",
		"https://evil.example/assets.vrchat.com/a.png",
	} {
		if w := h.do(imageRequest(target)); w.Code != http.StatusNotFound {
			t.Errorf("%s = %d", target, w.Code)
		}
	}
	if n := h.fetches.Load(); n != 0 {
		t.Errorf("%d upstream requests", n)
	}

	// Redirects are re-checked: a hop to an unlisted host is never fetched.
	h.upstream = func(r *http.Request) (*http.Response, error) {
		return respond(302, "text/plain", nil, "Location", "http://169.254.169.254/latest/meta-data"), nil
	}
	if w := h.do(imageRequest("https://assets.vrchat.com/a.png")); w.Code != http.StatusNotFound || h.fetches.Load() != 1 {
		t.Errorf("redirect to metadata host = %d after %d fetches", w.Code, h.fetches.Load())
	}
}

func TestAccountRequiresSession(t *testing.T) {
	h := newHarness(t)
	if w := h.do(get("/account/me")); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", w.Code)
	}
	pending := withSession(t, get("/account/me"), &Session{Auth: "a", Expires: time.Now().Add(time.Minute).UnixMilli(), Pending: []string{}})
	if w := h.do(pending); w.Code != http.StatusUnauthorized {
		t.Errorf("pending 2FA session = %d", w.Code)
	}
	if n := h.fetches.Load(); n != 0 {
		t.Errorf("%d upstream requests", n)
	}
}

func TestVRChatCalls(t *testing.T) {
	h := newConnectedHarness(t)
	session := &Session{Auth: "authcookie_synthetic_test", Expires: time.Now().Add(time.Hour).UnixMilli()}
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.vrchat.cloud/api/1/auth/user" {
			t.Errorf("upstream URL = %s", r.URL)
		}
		if r.Header.Get("Cookie") != "auth=authcookie_synthetic_test" || r.Header.Get("User-Agent") != "VRCove/"+appVersion+" (security-test@example.test)" {
			t.Errorf("upstream headers = %v", r.Header)
		}
		return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","displayName":"Test","email":"private@example.test","bio":null}`),
			"Set-Cookie", "auth=authcookie_rotated; Path=/; HttpOnly", "Set-Cookie", "twoFactorAuth=bad value; Path=/"), nil
	}
	w := h.do(withSession(t, get("/account/me"), session))
	if w.Code != 200 {
		t.Fatalf("me = %d %s", w.Code, w.Body)
	}
	var user map[string]any
	json.Unmarshal(w.Body.Bytes(), &user)
	if user["displayName"] != "Test" || user["email"] != nil {
		t.Errorf("public user = %v", user)
	}
	if _, present := user["bio"]; !present {
		t.Error("null fields should pass through")
	}
	set := w.Header().Get("Set-Cookie")
	_, rotatedValue, _ := strings.Cut(strings.SplitN(set, ";", 2)[0], "=")
	rotated := unseal(rotatedValue, testKey, time.Now())
	if rotated == nil || rotated.Auth != "authcookie_rotated" || rotated.TwoFactorAuth != "" {
		t.Errorf("rotated session = %+v", rotated)
	}

	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(401, "text/plain", []byte("sensitive upstream diagnostic")), nil
	}
	w = h.do(withSession(t, get("/account/me"), session))
	if w.Code != 401 || strings.Contains(w.Body.String(), "sensitive") {
		t.Errorf("upstream error = %d %s", w.Code, w.Body)
	}
	h.upstream = func(*http.Request) (*http.Response, error) { return nil, errors.New("dial tcp: secret detail") }
	w = h.do(withSession(t, get("/account/me"), session))
	if w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
		t.Errorf("network error = %d %s", w.Code, w.Body)
	}
}

func TestLoginTwoFactor(t *testing.T) {
	h, _ := newAccountHarness(t)
	h.accountCookie = h.signup("two_factor_owner", "synthetic passphrase")
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Basic dXNlciUyMG5hbWU6cCU0MHNzKCk=" {
			t.Errorf("Authorization = %q", got)
		}
		return respond(200, "application/json", []byte(`{"requiresTwoFactorAuth":["totp","otp"]}`), "Set-Cookie", "auth=pending_token; Path=/"), nil
	}
	w := h.do(post("/account/login", strings.NewReader(`{"username":"user name","password":"p@ss()"}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "requiresTwoFactorAuth") {
		t.Fatalf("login = %d %s", w.Code, w.Body)
	}
	cookie := strings.SplitN(w.Header().Get("Set-Cookie"), ";", 2)[0]
	session := unseal(strings.TrimPrefix(cookie, cookieName+"-pending="), testKey, time.Now())
	if session == nil || session.Auth != "pending_token" || len(session.Pending) != 2 || session.Expires > time.Now().Add(pendingLifetime).UnixMilli() {
		t.Fatalf("pending session = %+v", session)
	}

	ownerCookie := h.accountCookie
	otherCookie := h.signup("another_owner", "synthetic passphrase")
	cross := as(post("/account/2fa", strings.NewReader(`{"code":"123456","type":"totp"}`)), otherCookie)
	cross.Header.Add("Cookie", cookie)
	if got := h.do(cross); got.Code != 401 {
		t.Errorf("cross-account two-factor = %d", got.Code)
	}
	h.accountCookie = ownerCookie
	before := decode(t, h.do(get("/veil/me"))).(map[string]any)
	if len(before["links"].([]any)) != 0 {
		t.Fatal("linked before two-factor verification")
	}
	h.upstream = nil
	bad := post("/account/2fa", strings.NewReader(`{"code":"123456","type":"emailotp"}`))
	bad.Header.Set("Cookie", cookie)
	if w := h.do(bad); w.Code != http.StatusBadRequest {
		t.Errorf("wrong 2FA method = %d", w.Code)
	}

	h.upstream = func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/1/auth/twofactorauth/totp/verify":
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"code":"123456"}` {
				t.Errorf("verify body = %s", body)
			}
			return respond(200, "application/json", []byte(`{"verified":true}`), "Set-Cookie", "twoFactorAuth=tfa_token; Path=/"), nil
		case "/api/1/auth/user":
			return respond(200, "application/json", []byte(`{"id":"usr_00000000-0000-0000-0000-000000000000","displayName":"Test"}`)), nil
		}
		t.Errorf("unexpected upstream %s", r.URL)
		return respond(404, "text/plain", nil), nil
	}
	verify := post("/account/2fa", strings.NewReader(`{"code":"123456","type":"totp"}`))
	verify.Header.Set("Cookie", cookie)
	w = h.do(verify)
	if w.Code != 200 {
		t.Fatalf("2fa = %d %s", w.Code, w.Body)
	}
	done := unseal(strings.TrimPrefix(strings.SplitN(w.Header().Get("Set-Cookie"), ";", 2)[0], cookieName+"="), testKey, time.Now())
	if done == nil || done.AccountID == 0 || done.UserID == "" || done.Pending != nil || done.Auth != "pending_token" || done.TwoFactorAuth != "tfa_token" {
		t.Errorf("verified session = %+v", done)
	}
	after := decode(t, h.do(get("/veil/me"))).(map[string]any)
	if len(after["links"].([]any)) != 1 {
		t.Fatal("verified VRChat account was not linked")
	}
}

func seedCatalog(t *testing.T, h *harness) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "seed.sqlite")
	db, err := OpenCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	rw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	for i, a := range []struct {
		id, name, author, rating string
		pc, quest                int
	}{
		{"avtr_00000000-0000-0000-0000-000000000001", "Manuka Blue", "alice", "Good", 1, 1},
		{"avtr_00000000-0000-0000-0000-000000000002", "Manuka Red", "bob", "Poor", 1, 0},
		{"avtr_00000000-0000-0000-0000-000000000003", "Rusk", "carol", "Good", 0, 1},
	} {
		data, _ := json.Marshal(map[string]any{"id": a.id, "name": a.name, "author": a.author, "pc": a.pc == 1, "quest": a.quest == 1})
		res, err := rw.Exec(`INSERT INTO catalog VALUES (?,?,?,?,?,?,?,?)`, a.id, a.name, a.author, "2026-01-0"+string(rune('1'+i)), a.pc, a.quest, a.rating, string(data))
		if err != nil {
			t.Fatal(err)
		}
		rowid, _ := res.LastInsertId()
		if _, err := rw.Exec(`INSERT INTO catalog_fts(rowid,name,author,description,tags) VALUES (?,?,?,'','')`, rowid, a.name, a.author); err != nil {
			t.Fatal(err)
		}
	}
	readOnly, err := OpenCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { readOnly.Close() })
	h.srv.db = readOnly
}

func TestCatalog(t *testing.T) {
	h := newHarness(t)
	seedCatalog(t, h)
	search := func(query string) (ids []string, total int) {
		w := h.do(get("/catalog?" + query))
		if w.Code != 200 {
			t.Fatalf("catalog %s = %d %s", query, w.Code, w.Body)
		}
		var res struct {
			Avatars []struct{ ID string }
			Total   int
		}
		json.Unmarshal(w.Body.Bytes(), &res)
		for _, a := range res.Avatars {
			ids = append(ids, a.ID[len(a.ID)-1:])
		}
		return ids, res.Total
	}
	cases := map[string]string{
		"":                           "321",
		"sort=oldest":                "123",
		"sort=name":                  "123",
		"q=manu":                     "21",
		"q=manuka&platform=both":     "1",
		"q=bob":                      "2",
		"rating=Good":                "31",
		"platform=quest&sort=oldest": "13",
		"q=avtr_00000000-0000-0000-0000-000000000003": "3",
		"q=%22%29%28*":    "",
		"rating=Bogus%27": "321",
		"page=1":          "",
	}
	for query, want := range cases {
		ids, total := search(query)
		if got := strings.Join(ids, ""); got != want {
			t.Errorf("%q = %q, want %q", query, got, want)
		}
		if query != "page=1" && total != len(want) {
			t.Errorf("%q total = %d", query, total)
		}
	}
	w := h.do(get("/stats"))
	if !strings.Contains(w.Body.String(), `"indexed":3`) {
		t.Errorf("stats = %s", w.Body)
	}
	// The serving pool is read-only.
	if _, err := h.srv.db.Exec("DELETE FROM catalog"); err == nil {
		t.Error("catalog pool accepted a write")
	}
}

func TestStaticFallback(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/", "/friends", "/assets/missing.js"} {
		w := h.do(httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "<title>VRCove</title>") {
			t.Errorf("%s = %d", p, w.Code)
		}
		if w.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s lacks frame protection", p)
		}
	}
}

func TestContactValidation(t *testing.T) {
	for _, c := range []string{"ops@vrcove.example", "https://vrcove.example/contact", "  ops@vrcove.example "} {
		if _, err := New(Config{PublicOrigin: "https://vrcove.example", Contact: c}, nil, nil); err != nil {
			t.Errorf("contact %q rejected: %v", c, err)
		}
	}
	for _, c := range []string{"ops", "ops@vrcove.example)", "a (b) c@d.example", "ftp://vrcove.example", "https://user@vrcove.example", "opś@vrcove.example", "ops@vrcove.example\r\nX: y"} {
		if _, err := New(Config{PublicOrigin: "https://vrcove.example", Contact: c}, nil, nil); err == nil {
			t.Errorf("contact %q accepted", c)
		}
	}
}
