package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/argon2"
)

var testAccountKey = []byte("fedcba9876543210fedcba9876543210")

const (
	testVRCUser  = "usr_00000000-0000-0000-0000-00000000000a"
	otherVRCUser = "usr_00000000-0000-0000-0000-00000000000b"
)

// newAccountHarness is newHarness with a VRCove accounts database.
func newAccountHarness(t *testing.T) (*harness, string) {
	t.Helper()
	h := newHarness(t)
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	accounts, err := OpenAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { accounts.Close() })
	srv, err := New(Config{PublicOrigin: origin, SessionKey: testKey, Contact: "security-test@example.test", Accounts: accounts, AccountKey: testAccountKey},
		h.srv.db, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	srv.Client = h.srv.Client
	h.srv, h.handler = srv, srv.Handler()
	return h, path
}

func jsonBody(s string) *bytes.Reader { return bytes.NewReader([]byte(s)) }

// accountCookie returns the VRCove session cookie set by w.
func accountCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == accountCookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("no account cookie (status %d: %s)", w.Code, w.Body)
	return nil
}

func (h *harness) signup(name, password string) *http.Cookie {
	h.t.Helper()
	w := h.do(post("/cove/signup", jsonBody(`{"username":"`+name+`","password":"`+password+`"}`)))
	return accountCookie(h.t, w)
}

func as(r *http.Request, c *http.Cookie) *http.Request {
	r.AddCookie(c)
	return r
}

// linkedSignup creates a real account and ownership proof for authenticated fixtures.
func (h *harness) linkedSignup(name, password string) *http.Cookie {
	c := h.signup(name, password)
	var id int64
	if err := h.srv.accounts.QueryRow("SELECT id FROM accounts WHERE username = ? COLLATE NOCASE", name).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	vrcID := fmt.Sprintf("usr_00000000-0000-0000-0000-%012x", id)
	h.stubVRChatUser(vrcID, "DifferentHandle")
	w := h.do(vrchatSession(h.t, as(post("/cove/link", jsonBody(`{}`)), c)))
	if w.Code != 200 {
		h.t.Fatalf("fixture link: %d %s", w.Code, w.Body)
	}
	h.upstream = nil
	return c
}

// newConnectedHarness signs requests into a VRCove account with a linked VRChat fixture.
func newConnectedHarness(t *testing.T) *harness {
	h, _ := newAccountHarness(t)
	h.accountCookie = h.signup("connected_fixture", "synthetic passphrase")
	h.stubVRChatUser(testVRCUser, "DifferentHandle")
	w := h.do(vrchatSession(t, post("/cove/link", jsonBody(`{}`))))
	if w.Code != 200 {
		t.Fatalf("fixture link: %d %s", w.Code, w.Body)
	}
	h.upstream = nil
	h.fetches.Store(0)
	return h
}

func TestPasswordHashing(t *testing.T) {
	hash := hashPassword("correct horse battery")
	if !strings.HasPrefix(hash, fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$", argonMemory, argonTime, argonThreads)) {
		t.Fatalf("hash = %s", hash)
	}
	if hashPassword("correct horse battery") == hash {
		t.Error("salt is not random")
	}
	if ok, stale := verifyPassword(hash, "correct horse battery"); !ok || stale {
		t.Errorf("verify = %v, %v", ok, stale)
	}
	if ok, _ := verifyPassword(hash, "correct horse batterY"); ok {
		t.Error("wrong password accepted")
	}
	salt := []byte("0123456789abcdef")
	old := fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", b64.EncodeToString(salt), b64.EncodeToString(argon2.IDKey([]byte("pw"), salt, 1, 8192, 1, 32)))
	if ok, stale := verifyPassword(old, "pw"); !ok || !stale {
		t.Errorf("older parameters: ok=%v stale=%v", ok, stale)
	}
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=8192,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=19$m=99999999,t=1,p=1$c2FsdA$aGFzaGhhc2hoYXNoaGFzaA"} {
		if ok, _ := verifyPassword(bad, "pw"); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAccountLifecycle(t *testing.T) {
	h, _ := newAccountHarness(t)
	if w := h.do(get("/cove/me")); w.Code != 401 {
		t.Fatalf("anonymous me = %d", w.Code)
	}
	for body, want := range map[string]string{
		`{"username":"ab","password":"long enough password"}`:        "Usernames",
		`{"username":"has space","password":"long enough password"}`: "Usernames",
		`{"username":"quietfox","password":"short"}`:                 "at least 10",
		`{"username":"quietfox123","password":"QUIETFOX123"}`:        "can’t be your username",
	} {
		if w := h.do(post("/cove/signup", jsonBody(body))); w.Code != 400 || !strings.Contains(errorMessage(t, w), want) {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	cookie := h.signup("QuietFox", "a long passphrase")
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Errorf("cookie flags = %+v", cookie)
	}
	if w := h.do(post("/cove/signup", jsonBody(`{"username":"quietfox","password":"another passphrase"}`))); w.Code != 409 {
		t.Errorf("case-insensitive duplicate = %d", w.Code)
	}
	me := decode(t, h.do(as(get("/cove/me"), cookie))).(map[string]any)
	if me["username"] != "QuietFox" || me["created"] != time.Now().UTC().Format("2006-01-02") {
		t.Errorf("me = %v", me)
	}

	if w := h.do(post("/cove/login", jsonBody(`{"username":"quietfox","password":"wrong passphrase"}`))); w.Code != 401 {
		t.Errorf("wrong password = %d", w.Code)
	}
	if w := h.do(post("/cove/login", jsonBody(`{"username":"nobody","password":"wrong passphrase"}`))); w.Code != 401 || errorMessage(t, w) != "Wrong username or password." {
		t.Errorf("unknown user = %d %s", w.Code, w.Body)
	}
	second := accountCookie(t, h.do(post("/cove/login", jsonBody(`{"username":"QUIETFOX","password":"a long passphrase"}`))))

	// Changing the password signs out every other session.
	w := h.do(as(post("/cove/password", jsonBody(`{"current":"a long passphrase","next":"a newer passphrase"}`)), cookie))
	third := accountCookie(t, w)
	for name, c := range map[string]*http.Cookie{"first": cookie, "second": second} {
		if w := h.do(as(get("/cove/me"), c)); w.Code != 401 {
			t.Errorf("%s session still valid after password change", name)
		}
	}
	if w := h.do(as(get("/cove/me"), third)); w.Code != 200 {
		t.Errorf("new session = %d", w.Code)
	}

	w = h.do(as(post("/cove/logout", jsonBody(`{}`)), third))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("logout = %d %v", w.Code, w.Header())
	}
	if w := h.do(as(get("/cove/me"), third)); w.Code != 401 {
		t.Error("session valid after logout")
	}
}

func TestAccountsDisabled(t *testing.T) {
	h := newHarness(t)
	if w := h.do(post("/cove/signup", jsonBody(`{"username":"quietfox","password":"a long passphrase"}`))); w.Code != 503 {
		t.Errorf("signup without ACCOUNT_KEY = %d", w.Code)
	}
	if _, err := New(Config{PublicOrigin: origin, Accounts: h.srv.db, AccountKey: []byte("short")}, nil, nil); err == nil {
		t.Error("short ACCOUNT_KEY accepted")
	}
}

func TestReviews(t *testing.T) {
	h, _ := newAccountHarness(t)
	target := "/cove/reviews?kind=world&id=" + testWorld
	if w := h.do(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":5}`))); w.Code != 401 {
		t.Errorf("anonymous review = %d", w.Code)
	}
	alice, bob := h.linkedSignup("alice_v", "alice passphrase"), h.linkedSignup("bob_v", "bob passphrase!")
	for body, want := range map[string]int{
		`{"kind":"world","id":"wrld_bad","rating":5}`:                                                     400,
		`{"kind":"instance","id":"` + testWorld + `","rating":5}`:                                         400,
		`{"kind":"world","id":"` + testWorld + `","rating":6}`:                                            400,
		`{"kind":"world","id":"` + testWorld + `","rating":2.5}`:                                          400,
		`{"kind":"world","id":"` + testWorld + `","rating":"5"}`:                                          400,
		`{"kind":"world","id":"` + testWorld + `","rating":4,"body":"a\u0007"}`:                           400,
		`{"kind":"world","id":"` + testWorld + `","rating":4,"body":"` + strings.Repeat("x", 2001) + `"}`: 400,
	} {
		if w := h.do(as(post("/cove/review", jsonBody(body)), alice)); w.Code != want {
			t.Errorf("%.80s = %d %s", body, w.Code, w.Body)
		}
	}
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":2,"body":"first take"}`)), alice))
	// A second post replaces the first rather than adding another review.
	if w := h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":4,"body":"  <b>Cozy</b>\r\nworld  "}`)), alice)); w.Code != 200 {
		t.Fatalf("review = %d %s", w.Code, w.Body)
	}
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":1}`)), bob))

	list := decode(t, h.do(get(target))).(map[string]any)
	if list["count"].(float64) != 2 || list["average"].(float64) != 2.5 || list["mine"] != nil || list["signedIn"] != false {
		t.Errorf("summary = %v", list)
	}
	if got := fmt.Sprint(list["distribution"]); got != "[1 0 0 1 0]" {
		t.Errorf("distribution = %s", got)
	}
	mine := decode(t, h.do(as(get(target), alice))).(map[string]any)["mine"].(map[string]any)
	if mine["author"] != "alice_v" || mine["body"] != "<b>Cozy</b>\nworld" || mine["rating"].(float64) != 4 || mine["date"] != time.Now().UTC().Format("2006-01-02") {
		t.Errorf("mine = %v", mine)
	}
	if strings.Contains(h.do(get(target)).Body.String(), "account_id") {
		t.Error("internal IDs exposed")
	}

	h.do(as(post("/cove/review/delete", jsonBody(`{"kind":"world","id":"`+testWorld+`"}`)), bob))
	if n := decode(t, h.do(get(target))).(map[string]any)["count"].(float64); n != 1 {
		t.Errorf("count after delete = %v", n)
	}
}

// stubVRChatUser answers /auth/user as the given VRChat account.
func (h *harness) stubVRChatUser(id, displayName string) {
	h.upstream = func(up *http.Request) (*http.Response, error) {
		if up.URL.Path != "/api/1/auth/user" {
			h.t.Errorf("unexpected upstream %s", up.URL.Path)
		}
		return respond(200, "application/json", []byte(`{"id":"`+id+`","displayName":"`+displayName+`","username":"`+strings.ToLower(displayName)+`"}`)), nil
	}
}

func vrchatSession(t *testing.T, r *http.Request) *http.Request {
	return withSession(t, r, &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()})
}

func TestVRChatLinks(t *testing.T) {
	h, path := newAccountHarness(t)
	alice := h.signup("moonlit", "alice passphrase")
	if w := h.do(as(post("/cove/link", jsonBody(`{}`)), alice)); w.Code != 401 {
		t.Errorf("link without a VRChat session = %d", w.Code)
	}

	h.stubVRChatUser(testVRCUser, "SunnyOtter")
	w := h.do(vrchatSession(t, as(post("/cove/link", jsonBody(`{}`)), alice)))
	if body := decode(t, w).(map[string]any); body["similarName"] != false {
		t.Errorf("link = %v", body)
	}
	links := decode(t, h.do(as(get("/cove/me"), alice))).(map[string]any)["links"].([]any)
	if len(links) != 1 || links[0].(map[string]any)["vrchatId"] != testVRCUser || links[0].(map[string]any)["displayName"] != "SunnyOtter" {
		t.Fatalf("links = %v", links)
	}
	// Linking again is idempotent.
	h.do(vrchatSession(t, as(post("/cove/link", jsonBody(`{}`)), alice)))
	if n := len(decode(t, h.do(as(get("/cove/me"), alice))).(map[string]any)["links"].([]any)); n != 1 {
		t.Errorf("links after relink = %d", n)
	}

	// The link is private: nothing public mentions it, and the database file
	// holds neither the VRChat ID nor the display name in plain text.
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":5}`)), alice))
	if public := h.do(get("/cove/reviews?kind=world&id=" + testWorld)).Body.String(); strings.Contains(public, testVRCUser) || strings.Contains(public, "SunnyOtter") {
		t.Errorf("public reviews leak the link: %s", public)
	}
	h.srv.accounts.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(testVRCUser)) || bytes.Contains(raw, []byte("SunnyOtter")) {
		t.Error("database stores the VRChat account in plain text")
	}

	// Linked accounts can't be self-reviewed; others can.
	if w := h.do(as(post("/cove/review", jsonBody(`{"kind":"user","id":"`+testVRCUser+`","rating":5}`)), alice)); w.Code != 403 {
		t.Errorf("self review = %d", w.Code)
	}
	if w := h.do(as(post("/cove/review", jsonBody(`{"kind":"user","id":"`+otherVRCUser+`","rating":5}`)), alice)); w.Code != 200 {
		t.Errorf("review of another user = %d", w.Code)
	}

	// One VRChat account belongs to at most one VRCove account.
	bob := h.signup("sunnyotter_alt", "bob passphrase!")
	if w := h.do(vrchatSession(t, as(post("/cove/link", jsonBody(`{}`)), bob))); w.Code != 409 {
		t.Errorf("second owner = %d %s", w.Code, w.Body)
	}
	// A VRCove name resembling the VRChat name is flagged.
	h.stubVRChatUser(otherVRCUser, "Sunny.Otter")
	if body := decode(t, h.do(vrchatSession(t, as(post("/cove/link", jsonBody(`{}`)), bob)))).(map[string]any); body["similarName"] != true {
		t.Errorf("similar name not flagged: %v", body)
	}

	id := links[0].(map[string]any)["id"].(float64)
	h.do(as(post("/cove/unlink", jsonBody(fmt.Sprintf(`{"id":%v}`, id))), bob)) // not bob's link
	if n := len(decode(t, h.do(as(get("/cove/me"), alice))).(map[string]any)["links"].([]any)); n != 1 {
		t.Error("another account removed the link")
	}
	h.do(as(post("/cove/unlink", jsonBody(fmt.Sprintf(`{"id":%v}`, id))), alice))
	if n := len(decode(t, h.do(as(get("/cove/me"), alice))).(map[string]any)["links"].([]any)); n != 0 {
		t.Error("unlink failed")
	}
}

func TestDeleteAccount(t *testing.T) {
	h, _ := newAccountHarness(t)
	alice := h.signup("moonlit", "alice passphrase")
	h.stubVRChatUser(testVRCUser, "SunnyOtter")
	h.do(vrchatSession(t, as(post("/cove/link", jsonBody(`{}`)), alice)))
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":5}`)), alice))
	if w := h.do(as(post("/cove/delete", jsonBody(`{"password":"wrong"}`)), alice)); w.Code != 401 {
		t.Errorf("delete with wrong password = %d", w.Code)
	}
	if w := h.do(as(post("/cove/delete", jsonBody(`{"password":"alice passphrase"}`)), alice)); w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	if n := decode(t, h.do(get("/cove/reviews?kind=world&id="+testWorld))).(map[string]any)["count"].(float64); n != 0 {
		t.Error("reviews survived account deletion")
	}
	for _, table := range []string{"accounts", "account_sessions", "vrchat_links", "reviews"} {
		var n int
		h.srv.accounts.QueryRow("SELECT count(*) FROM " + table).Scan(&n)
		if n != 0 {
			t.Errorf("%s has %d rows after deletion", table, n)
		}
	}
	// The VRChat account can be linked afresh.
	fresh := h.signup("newname", "fresh passphrase")
	if w := h.do(vrchatSession(t, as(post("/cove/link", jsonBody(`{}`)), fresh))); w.Code != 200 {
		t.Errorf("relink after deletion = %d", w.Code)
	}
}

func TestSimilarNames(t *testing.T) {
	for _, c := range []struct {
		cove, vrc string
		want      bool
	}{{"SunnyOtter", "sunny_otter", true}, {"otter", "SunnyOtter", true}, {"sunnyotter2", "SunnyOtter", true},
		{"moonlit", "SunnyOtter", false}, {"ott", "SunnyOtter", false}, {"x", "", false}} {
		if got := similarNames(c.cove, c.vrc); got != c.want {
			t.Errorf("similarNames(%q, %q) = %v", c.cove, c.vrc, got)
		}
	}
}

func TestVRCoveRequiredForVRChat(t *testing.T) {
	h, _ := newAccountHarness(t)
	for _, r := range []*http.Request{
		post("/account/login", jsonBody(`{"username":"vrc","password":"synthetic"}`)),
		vrchatSession(t, get("/account/me")),
		vrchatSession(t, post("/account/2fa", jsonBody(`{"type":"totp","code":"123456"}`))),
		vrchatSession(t, post("/account/select", jsonBody(`{"avatarId":"avtr_00000000-0000-0000-0000-000000000001"}`))),
	} {
		if w := h.do(r); w.Code != 401 {
			t.Errorf("%s = %d %s", r.URL.Path, w.Code, w.Body)
		}
	}
	if h.fetches.Load() != 0 {
		t.Fatal("VRChat-only requests reached upstream")
	}
	a := h.signup("unlinked", "synthetic passphrase")
	for _, path := range []string{"/cove/review", "/cove/collections/create"} {
		if w := h.do(as(post(path, jsonBody(`{}`)), a)); w.Code != 403 {
			t.Errorf("unlinked %s = %d", path, w.Code)
		}
	}
	old := &Session{Auth: "old_token", Expires: time.Now().Add(time.Hour).UnixMilli()}
	value, _ := seal(old, testKey)
	r := as(get("/account/me"), a)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: value})
	if w := h.do(r); w.Code != 403 {
		t.Errorf("legacy VRChat-only session = %d", w.Code)
	}
}

func TestLoginLinksAndBindsVRChat(t *testing.T) {
	h, _ := newAccountHarness(t)
	alice := h.signup("first_account", "synthetic passphrase")
	bob := h.signup("other_account", "synthetic passphrase")
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","displayName":"DifferentHandle"}`), "Set-Cookie", "auth=synthetic_token; Path=/"), nil
	}
	login := func(c *http.Cookie) *httptest.ResponseRecorder {
		return h.do(as(post("/account/login", jsonBody(`{"username":"vrc","password":"synthetic"}`)), c))
	}
	w := login(alice)
	if w.Code != 200 {
		t.Fatalf("login = %d %s", w.Code, w.Body)
	}
	sessionCookie := w.Result().Cookies()[0]
	session := unseal(sessionCookie.Value, testKey, time.Now())
	if session == nil || session.AccountID == 0 || session.UserID != testVRCUser {
		t.Fatalf("unbound login session: %+v", session)
	}
	me := decode(t, h.do(as(get("/cove/me"), alice))).(map[string]any)
	if len(me["links"].([]any)) != 1 {
		t.Fatal("login did not link VRChat")
	}
	request := func(c *http.Cookie) *http.Request { return as(as(get("/account/me"), c), sessionCookie) }
	if got := h.do(request(alice)); got.Code != 200 {
		t.Fatalf("owner request = %d %s", got.Code, got.Body)
	}
	fetches := h.fetches.Load()
	if got := h.do(request(bob)); got.Code != 403 {
		t.Errorf("cross-account session = %d", got.Code)
	}
	if h.fetches.Load() != fetches {
		t.Fatal("cross-account session reached upstream")
	}
	if got := login(bob); got.Code != 409 {
		t.Errorf("link already owned = %d", got.Code)
	}
	link := me["links"].([]any)[0].(map[string]any)
	if link["username"] != "vrc" {
		t.Fatalf("reconnect username = %v", link["username"])
	}
	if got := h.do(as(post("/cove/unlink", jsonBody(fmt.Sprintf(`{"id":%v}`, link["id"]))), alice)); got.Code != 200 {
		t.Fatalf("unlink = %d", got.Code)
	}
	// Unlinking ends the link's sessions, so the old cookie is refused outright.
	fetches = h.fetches.Load()
	if got := h.do(request(alice)); got.Code != 401 {
		t.Errorf("unlinked session = %d", got.Code)
	}
	if h.fetches.Load() != fetches {
		t.Fatal("unlinked session reached upstream")
	}
}

func TestChangeUsername(t *testing.T) {
	h, _ := newAccountHarness(t)
	alice := h.linkedSignup("moonlit", "alice passphrase")
	h.signup("taken_name", "bob passphrase!")
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":5}`)), alice))
	rename := func(body string) *httptest.ResponseRecorder {
		return h.do(as(post("/cove/username", jsonBody(body)), alice))
	}
	for body, want := range map[string]int{
		`{"username":"ab","password":"alice passphrase"}`:         400,
		`{"username":"moonlit","password":"alice passphrase"}`:    400,
		`{"username":"Taken_Name","password":"alice passphrase"}`: 409,
		`{"username":"starlit","password":"wrong"}`:               401,
	} {
		if w := rename(body); w.Code != want {
			t.Errorf("%s = %d %s", body, w.Code, w.Body)
		}
	}
	w := rename(`{"username":"starlit","password":"alice passphrase"}`)
	if w.Code != 200 || decode(t, w).(map[string]any)["username"] != "starlit" {
		t.Fatalf("rename = %d %s", w.Code, w.Body)
	}
	// Changing only the case is allowed.
	if w := rename(`{"username":"StarLit","password":"alice passphrase"}`); w.Code != 200 {
		t.Errorf("case-only rename = %d %s", w.Code, w.Body)
	}
	if mine := decode(t, h.do(as(get("/cove/reviews?kind=world&id="+testWorld), alice))).(map[string]any)["mine"].(map[string]any); mine["author"] != "StarLit" {
		t.Errorf("review author = %v", mine["author"])
	}
	if w := h.do(post("/cove/login", jsonBody(`{"username":"moonlit","password":"alice passphrase"}`))); w.Code != 401 {
		t.Errorf("old name login = %d", w.Code)
	}
	if w := h.do(post("/cove/login", jsonBody(`{"username":"starlit","password":"alice passphrase"}`))); w.Code != 200 {
		t.Errorf("new name login = %d %s", w.Code, w.Body)
	}
	// The old name is free for someone else.
	if w := h.do(post("/cove/signup", jsonBody(`{"username":"moonlit","password":"another passphrase"}`))); w.Code != 200 {
		t.Errorf("reuse old name = %d %s", w.Code, w.Body)
	}
}
