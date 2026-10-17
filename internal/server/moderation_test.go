package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (h *harness) promote(name, role string) {
	h.t.Helper()
	if err := SetRole(context.Background(), h.srv.accounts, name, role); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) accountID(name string) int64 {
	h.t.Helper()
	var id int64
	if err := h.srv.accounts.QueryRow("SELECT id FROM accounts WHERE username_key = ?", strings.ToLower(name)).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *harness) moderate(c *http.Cookie, name, action string, extra ...string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"id":%d,"action":%q%s}`, h.accountID(name), action, strings.Join(extra, ""))
	return h.do(as(post("/cove/admin/user", jsonBody(body)), c))
}

func login(h *harness, name, password string) *httptest.ResponseRecorder {
	return h.do(post("/cove/login", jsonBody(`{"username":"`+name+`","password":"`+password+`"}`)))
}

func TestRolesKeepTheAdminPanelHidden(t *testing.T) {
	h, _ := newAccountHarness(t)
	owner, admin, user := h.signup("frog", "owner passphrase"), h.signup("mod_one", "admin passphrase"), h.signup("plain_user", "user passphrase")
	for name, c := range map[string]*http.Cookie{"anonymous": nil, "user": user} {
		if w := h.do(withCookie(get("/cove/admin/summary"), c)); w.Code != 404 {
			t.Errorf("%s reached the admin panel: %d", name, w.Code)
		}
	}
	if err := SetRole(context.Background(), h.srv.accounts, "frog", "superuser"); err == nil {
		t.Error("invalid role accepted")
	}
	if err := SetRole(context.Background(), h.srv.accounts, "nobody_here", roleOwner); err == nil {
		t.Error("missing account accepted")
	}
	h.promote("FROG", roleOwner)
	if me := decode(t, h.do(as(get("/cove/me"), owner))).(map[string]any); me["role"] != roleOwner {
		t.Fatalf("role = %v", me["role"])
	}
	if w := h.do(as(get("/cove/admin/summary"), owner)); w.Code != 200 {
		t.Fatalf("owner summary: %d %s", w.Code, w.Body)
	}
	// Only owners promote, and nobody acts on an equal or higher role.
	if w := h.moderate(owner, "mod_one", "make-admin"); w.Code != 200 {
		t.Fatalf("promote: %d %s", w.Code, w.Body)
	}
	for _, c := range []struct {
		actor        *http.Cookie
		target, verb string
	}{{admin, "plain_user", "make-admin"}, {admin, "frog", "ban"}, {owner, "frog", "ban"}, {user, "mod_one", "ban"}} {
		if w := h.moderate(c.actor, c.target, c.verb); w.Code != 403 && w.Code != 404 {
			t.Errorf("%s on %s allowed: %d", c.verb, c.target, w.Code)
		}
	}
	if w := h.moderate(admin, "plain_user", "ban"); w.Code != 200 {
		t.Fatalf("admin ban: %d %s", w.Code, w.Body)
	}
	if w := h.do(as(get("/cove/me"), user)); w.Code != 401 {
		t.Errorf("banned session still works: %d", w.Code)
	}
	if w := login(h, "plain_user", "user passphrase"); w.Code != 403 || !strings.Contains(errorMessage(t, w), "banned") {
		t.Errorf("banned login = %d %s", w.Code, w.Body)
	}
	// A wrong password never reveals the ban.
	if w := login(h, "plain_user", "wrong passphrase"); w.Code != 401 {
		t.Errorf("wrong password on banned account = %d", w.Code)
	}
	if w := h.moderate(admin, "plain_user", "unban"); w.Code != 200 {
		t.Fatal("unban failed")
	}
	if w := login(h, "plain_user", "user passphrase"); w.Code != 200 {
		t.Errorf("unbanned login = %d", w.Code)
	}
	users := decode(t, h.do(as(get("/cove/admin/users?q=mod"), owner))).(map[string]any)
	if list := users["users"].([]any); len(list) != 1 || list[0].(map[string]any)["role"] != roleAdmin {
		t.Errorf("user search = %v", users)
	}
	if w := h.moderate(owner, "mod_one", "delete"); w.Code != 200 {
		t.Fatal("delete failed")
	}
	if w := login(h, "mod_one", "admin passphrase"); w.Code != 401 {
		t.Errorf("deleted account signed in: %d", w.Code)
	}
	summary := decode(t, h.do(as(get("/cove/admin/summary"), owner))).(map[string]any)
	var actions []string
	for _, e := range summary["log"].([]any) {
		actions = append(actions, e.(map[string]any)["action"].(string))
	}
	if got := strings.Join(actions, ","); got != "delete,unban,ban,make-admin,set role owner" {
		t.Errorf("log = %s", got)
	}
}

func TestSuspensionsEndSessionsAndExpire(t *testing.T) {
	h, _ := newAccountHarness(t)
	now := time.Now()
	h.srv.now = func() time.Time { return now }
	owner, user := h.signup("frog", "owner passphrase"), h.signup("plain_user", "user passphrase")
	h.promote("frog", roleOwner)
	for _, days := range []string{"0", "366", "1.5"} {
		if w := h.moderate(owner, "plain_user", "suspend", `,"days":`+days); w.Code != 400 {
			t.Errorf("suspend %s days = %d", days, w.Code)
		}
	}
	if w := h.moderate(owner, "plain_user", "suspend", `,"days":2`); w.Code != 200 {
		t.Fatalf("suspend: %d %s", w.Code, w.Body)
	}
	if w := h.do(as(get("/cove/me"), user)); w.Code != 401 {
		t.Error("suspended session still works")
	}
	if w := login(h, "plain_user", "user passphrase"); w.Code != 403 || !strings.Contains(errorMessage(t, w), "suspended until") {
		t.Errorf("suspended login = %d %s", w.Code, w.Body)
	}
	now = now.Add(49 * time.Hour)
	if w := login(h, "plain_user", "user passphrase"); w.Code != 200 {
		t.Errorf("login after suspension = %d %s", w.Code, w.Body)
	}
}

func TestReportsKeepEvidenceAndBannedContentDisappears(t *testing.T) {
	h, _ := newAccountHarness(t)
	author, reporter := h.linkedSignup("review_author", "author passphrase"), h.signup("reporter", "reporter passphrase")
	owner := h.signup("frog", "owner passphrase")
	h.promote("frog", roleOwner)
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":1,"body":"rude words","name":"Cabin"}`)), author))
	reviews := decode(t, h.do(get("/cove/reviews?kind=world&id="+testWorld))).(map[string]any)["reviews"].([]any)
	reviewID := reviews[0].(map[string]any)["id"].(float64)
	report := func(c *http.Cookie, body string) *httptest.ResponseRecorder {
		return h.do(as(post("/cove/report", jsonBody(body)), c))
	}
	reviewReport := fmt.Sprintf(`{"kind":"review","id":%d,"reason":"harassment","details":"Insults the world creator."}`, int64(reviewID))
	if w := report(reporter, reviewReport); w.Code != 200 {
		t.Fatalf("report: %d %s", w.Code, w.Body)
	}
	for name, c := range map[string]struct {
		cookie *http.Cookie
		body   string
		want   int
	}{
		"duplicate":       {reporter, reviewReport, 409},
		"own review":      {author, reviewReport, 400},
		"bad reason":      {reporter, fmt.Sprintf(`{"kind":"review","id":%d,"reason":"boring"}`, int64(reviewID)), 400},
		"private profile": {reporter, `{"kind":"profile","username":"review_author","reason":"spam"}`, 404},
		"missing review":  {reporter, `{"kind":"review","id":999,"reason":"spam"}`, 404},
	} {
		if w := report(c.cookie, c.body); w.Code != c.want {
			t.Errorf("%s = %d, want %d", name, w.Code, c.want)
		}
	}
	if w := h.do(post("/cove/report", jsonBody(reviewReport))); w.Code != 401 {
		t.Errorf("anonymous report = %d", w.Code)
	}
	h.do(as(post("/cove/profile", jsonBody(`{"public":true,"bio":"spam spam"}`)), author))
	if w := report(reporter, `{"kind":"profile","username":"REVIEW_AUTHOR","reason":"spam"}`); w.Code != 200 {
		t.Fatalf("profile report: %d %s", w.Code, w.Body)
	}
	// The author edits the review; the report keeps what was reported.
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":1,"body":"edited","name":"Cabin"}`)), author))
	var open struct {
		Reports []struct {
			ID      int64
			Kind    string
			Content map[string]any
			Account string
		}
	}
	json.Unmarshal(h.do(as(get("/cove/admin/reports"), owner)).Body.Bytes(), &open)
	if len(open.Reports) != 2 || open.Reports[0].Content["body"] != "rude words" || open.Reports[0].Account != "review_author" || open.Reports[1].Content["bio"] != "spam spam" {
		t.Fatalf("backlog = %+v", open.Reports)
	}
	if w := h.do(as(post("/cove/admin/report", jsonBody(fmt.Sprintf(`{"id":%d,"action":"delete-review"}`, open.Reports[1].ID))), owner)); w.Code != 400 {
		t.Errorf("deleted a review from a profile report: %d", w.Code)
	}
	if w := h.do(as(post("/cove/admin/report", jsonBody(fmt.Sprintf(`{"id":%d,"action":"delete-review"}`, open.Reports[0].ID))), owner)); w.Code != 200 {
		t.Fatalf("delete review: %d %s", w.Code, w.Body)
	}
	if n := decode(t, h.do(get("/cove/reviews?kind=world&id="+testWorld))).(map[string]any)["count"]; n != 0.0 {
		t.Errorf("review still listed: %v", n)
	}
	if w := h.do(as(post("/cove/admin/report", jsonBody(fmt.Sprintf(`{"id":%d,"action":"dismiss"}`, open.Reports[0].ID))), owner)); w.Code != 409 {
		t.Errorf("handled report changed again: %d", w.Code)
	}
	h.do(as(post("/cove/admin/report", jsonBody(fmt.Sprintf(`{"id":%d,"action":"dismiss"}`, open.Reports[1].ID))), owner))
	closed := decode(t, h.do(as(get("/cove/admin/reports?status=closed"), owner))).(map[string]any)["reports"].([]any)
	if len(closed) != 2 || closed[0].(map[string]any)["handledBy"] != "frog" {
		t.Errorf("closed = %v", closed)
	}

	// Banning hides the account's public profile, reviews and collections.
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":5,"body":"","name":"Cabin"}`)), author))
	if w := h.moderate(owner, "review_author", "ban"); w.Code != 200 {
		t.Fatal("ban failed")
	}
	if w := h.do(get("/cove/profile?username=review_author")); w.Code != 404 {
		t.Errorf("banned profile visible: %d", w.Code)
	}
	if n := decode(t, h.do(get("/cove/reviews?kind=world&id="+testWorld))).(map[string]any)["count"]; n != 0.0 {
		t.Errorf("banned review counted: %v", n)
	}
}

func TestRemovalRequestsBlockAvatarsAfterApproval(t *testing.T) {
	h, browser, key := newLoggerHarness(t)
	writer := h.srv.cfg.Logger.Catalog
	h.srv.db = writer
	mine, other := "avtr_00000000-0000-0000-0000-0000000000a1", "avtr_00000000-0000-0000-0000-0000000000a2"
	for id, author := range map[string]string{mine: testVRCUser, other: otherVRCUser} {
		if _, err := writer.Exec("INSERT INTO catalog(id,name,data) VALUES(?,?,?)", id, "Avatar", `{"name":"Avatar","authorId":"`+author+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	request := func(body string) *httptest.ResponseRecorder {
		return browser.do(post("/cove/removal-request", jsonBody(body)))
	}
	// The connected VRChat account uploaded this avatar, so no written proof is needed.
	w := request(`{"avatarId":"` + mine + `","proof":""}`)
	if w.Code != 200 || decode(t, w).(map[string]any)["verified"] != true {
		t.Fatalf("verified request: %d %s", w.Code, w.Body)
	}
	if w := request(`{"avatarId":"` + mine + `","proof":""}`); w.Code != 409 {
		t.Errorf("second open request = %d", w.Code)
	}
	if w := request(`{"avatarId":"` + other + `","proof":"mine"}`); w.Code != 400 {
		t.Errorf("unverified request without proof = %d", w.Code)
	}
	if w := request(`{"avatarId":"avtr_00000000-0000-0000-0000-0000000000a3","proof":"I made this avatar and sell it on my store page."}`); w.Code != 404 {
		t.Errorf("request for unknown avatar = %d", w.Code)
	}
	if w := request(`{"avatarId":"` + other + `","proof":"I made this avatar and sell it on my store page."}`); w.Code != 200 || decode(t, w).(map[string]any)["verified"] != false {
		t.Fatalf("unverified request: %d %s", w.Code, w.Body)
	}
	admin := h.signup("frog", "owner passphrase")
	h.promote("frog", roleAdmin)
	var backlog struct {
		Requests []struct {
			ID       int64
			AvatarID string
			Verified bool
		}
	}
	json.Unmarshal(h.do(as(get("/cove/admin/requests"), admin)).Body.Bytes(), &backlog)
	if len(backlog.Requests) != 2 || backlog.Requests[0].AvatarID != mine || !backlog.Requests[0].Verified || backlog.Requests[1].Verified {
		t.Fatalf("requests = %+v", backlog.Requests)
	}
	decide := func(id int64, action string) *httptest.ResponseRecorder {
		return h.do(as(post("/cove/admin/request", jsonBody(fmt.Sprintf(`{"id":%d,"action":%q,"note":"checked"}`, id, action))), admin))
	}
	if w := decide(backlog.Requests[0].ID, "approve"); w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if w := decide(backlog.Requests[1].ID, "reject"); w.Code != 200 {
		t.Fatalf("reject: %d %s", w.Code, w.Body)
	}
	if w := decide(backlog.Requests[0].ID, "reject"); w.Code != 409 {
		t.Errorf("handled request changed again: %d", w.Code)
	}
	var count int
	writer.QueryRow("SELECT count(*) FROM catalog WHERE id IN (?, ?)", mine, other).Scan(&count)
	if count != 1 {
		t.Fatalf("catalog rows = %d", count)
	}
	if total := decode(t, h.do(get("/catalog?q="+mine))).(map[string]any)["total"]; total != 0.0 {
		t.Errorf("removed avatar still searchable: %v", total)
	}
	// The companion skips removed avatars, and ingestion never re-adds them.
	privateUpstream(h)
	before := h.fetches.Load()
	var known struct{ Existing []string }
	json.Unmarshal(loggerPost(h, "/logger/known", key, mine).Body.Bytes(), &known)
	if len(known.Existing) != 1 {
		t.Errorf("known = %v", known.Existing)
	}
	w = loggerPost(h, "/logger/avatars", key, mine)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rejected"`) || h.fetches.Load() != before {
		t.Errorf("ingestion of removed avatar: %d %s, fetches %d", w.Code, w.Body, h.fetches.Load()-before)
	}
}
