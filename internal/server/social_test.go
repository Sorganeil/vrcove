package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testAvatar  = "avtr_00000000-0000-0000-0000-00000000000c"
	testImage   = "https://s3-us-west-2.amazonaws.com/vrc-uploads/images/image.png"
	otherWorld  = "wrld_00000000-0000-0000-0000-00000000000d"
	missingUser = "/veil/profile?username=nobody_here"
)

func TestAccountMigrationsUpgradeInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	db, err := OpenAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	// Reopening must not re-run migrations.
	db, err = OpenAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != len(accountMigrations) {
		t.Errorf("user_version = %d", version)
	}
}

func TestPublicProfiles(t *testing.T) {
	h, _ := newAccountHarness(t)
	alice, bob := h.linkedSignup("alice_v", "alice passphrase"), h.linkedSignup("bob_v", "bob passphrase!")
	h.do(as(post("/veil/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":4,"body":"nice","name":"Cozy Cabin"}`)), alice))

	// Private by default: others get the same answer as for a missing user.
	for _, c := range []struct {
		name   string
		cookie *http.Cookie
	}{{"anonymous", nil}, {"other account", bob}} {
		w := h.do(withCookie(get("/veil/profile?username=ALICE_V"), c.cookie))
		if w.Code != 404 || errorMessage(t, w) != errorMessage(t, h.do(get(missingUser))) {
			t.Errorf("%s sees private profile: %d %s", c.name, w.Code, w.Body)
		}
	}
	own := decode(t, h.do(as(get("/veil/profile?username=alice_v"), alice))).(map[string]any)
	if own["mine"] != true || own["public"] != false {
		t.Errorf("own private profile = %v", own)
	}
	reviews := decode(t, h.do(get("/veil/reviews?kind=world&id="+testWorld))).(map[string]any)["reviews"].([]any)
	if reviews[0].(map[string]any)["profile"] != false {
		t.Error("private author linked")
	}

	if w := h.do(as(post("/veil/profile", jsonBody(`{"bio":"`+strings.Repeat("x", 501)+`"}`)), alice)); w.Code != 400 {
		t.Errorf("long bio = %d", w.Code)
	}
	me := decode(t, h.do(as(post("/veil/profile", jsonBody(`{"public":true,"bio":"Worlds with good lighting.\nNo drama."}`)), alice))).(map[string]any)
	if me["public"] != true || me["bio"] != "Worlds with good lighting.\nNo drama." {
		t.Errorf("me = %v", me)
	}
	profile := decode(t, h.do(get("/veil/profile?username=alice_v"))).(map[string]any)
	if profile["username"] != "alice_v" || profile["mine"] != false || profile["reviewCount"].(float64) != 1 {
		t.Errorf("public profile = %v", profile)
	}
	review := profile["reviews"].([]any)[0].(map[string]any)
	if review["kind"] != "world" || review["target"] != testWorld || review["name"] != "Cozy Cabin" || review["body"] != "nice" {
		t.Errorf("profile review = %v", review)
	}
	if body := h.do(get("/veil/profile?username=alice_v")).Body.String(); strings.Contains(body, "links") || strings.Contains(body, "vrchat") {
		t.Errorf("profile exposes links: %s", body)
	}
	reviews = decode(t, h.do(get("/veil/reviews?kind=world&id="+testWorld))).(map[string]any)["reviews"].([]any)
	if reviews[0].(map[string]any)["profile"] != true {
		t.Error("public author not linked")
	}
	// Editing a review without a name keeps the stored one.
	h.do(as(post("/veil/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":5}`)), alice))
	profile = decode(t, h.do(get("/veil/profile?username=alice_v"))).(map[string]any)
	if review := profile["reviews"].([]any)[0].(map[string]any); review["name"] != "Cozy Cabin" || review["rating"].(float64) != 5 {
		t.Errorf("edited review = %v", review)
	}
}

func withCookie(r *http.Request, c *http.Cookie) *http.Request {
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

func TestCollections(t *testing.T) {
	h, _ := newAccountHarness(t)
	alice, bob := h.linkedSignup("alice_v", "alice passphrase"), h.linkedSignup("bob_v", "bob passphrase!")
	if w := h.do(post("/veil/collections/create", jsonBody(`{"kind":"world","name":"x"}`))); w.Code != 401 {
		t.Errorf("anonymous create = %d", w.Code)
	}
	for body, want := range map[string]string{
		`{"kind":"group","name":"Groups"}`:                          "avatars or worlds",
		`{"kind":"world","name":""}`:                                "name",
		`{"kind":"world","name":"line\nbreak"}`:                     "name",
		`{"kind":"world","name":"` + strings.Repeat("n", 81) + `"}`: "name",
	} {
		if w := h.do(as(post("/veil/collections/create", jsonBody(body)), alice)); w.Code != 400 || !strings.Contains(errorMessage(t, w), want) {
			t.Errorf("%s = %d %s", body, w.Code, w.Body)
		}
	}
	worlds := decode(t, h.do(as(post("/veil/collections/create", jsonBody(`{"kind":"world","name":"Chill worlds","description":"For late nights."}`)), alice))).(map[string]any)["id"].(string)
	avatars := decode(t, h.do(as(post("/veil/collections/create", jsonBody(`{"kind":"avatar","name":"Quest-ready","public":true}`)), alice))).(map[string]any)["id"].(string)
	if !collectionIDPattern.MatchString(worlds) || worlds == avatars {
		t.Fatalf("ids = %q %q", worlds, avatars)
	}

	add := func(c *http.Cookie, collection, target, extra string) int {
		return h.do(as(post("/veil/collections/add", jsonBody(`{"id":"`+collection+`","target":"`+target+`"`+extra+`}`)), c)).Code
	}
	if code := add(alice, worlds, testWorld, `,"name":"Cozy Cabin","image":"`+testImage+`"`); code != 200 {
		t.Fatalf("add = %d", code)
	}
	add(alice, worlds, otherWorld, `,"name":"Rooftop"`)
	if code := add(alice, worlds, testAvatar, ``); code != 400 {
		t.Errorf("avatar in a world collection = %d", code)
	}
	add(alice, worlds, otherWorld, `,"name":"Rooftop","image":"https://evil.example/x.png"`)
	if code := add(alice, worlds, otherWorld, `,"name":"two\nlines"`); code != 400 {
		t.Errorf("multi-line name = %d", code)
	}
	if code := add(bob, worlds, testWorld, ``); code != 404 {
		t.Errorf("someone else's collection = %d", code)
	}
	add(alice, avatars, testAvatar, `,"name":"Fox"`)

	mine := decode(t, h.do(as(get("/veil/collections?kind=world&contains="+testWorld), alice))).([]any)
	if len(mine) != 1 {
		t.Fatalf("world collections = %v", mine)
	}
	// The unproxied image was dropped, so only one preview remains.
	if c := mine[0].(map[string]any); c["count"].(float64) != 2 || c["contains"] != true || fmt.Sprint(c["preview"]) != "["+testImage+"]" {
		t.Errorf("summary = %v", c)
	}

	// The private collection is hidden from others; the public one opens by
	// link but doesn't name an owner whose profile is private.
	if w := h.do(as(get("/veil/collection?id="+worlds), bob)); w.Code != 404 {
		t.Errorf("private collection = %d", w.Code)
	}
	shared := decode(t, h.do(get("/veil/collection?id="+avatars))).(map[string]any)
	if shared["owner"] != nil || shared["mine"] != false || len(shared["items"].([]any)) != 1 {
		t.Errorf("shared collection = %v", shared)
	}
	h.do(as(post("/veil/profile", jsonBody(`{"public":true}`)), alice))
	if shared := decode(t, h.do(get("/veil/collection?id="+avatars))).(map[string]any); shared["owner"] != "alice_v" {
		t.Errorf("owner of public profile = %v", shared["owner"])
	}
	profile := decode(t, h.do(get("/veil/profile?username=alice_v"))).(map[string]any)
	if list := profile["collections"].([]any); len(list) != 1 || list[0].(map[string]any)["id"] != avatars {
		t.Errorf("profile lists private collections: %v", list)
	}

	h.do(as(post("/veil/collections/update", jsonBody(`{"id":"`+worlds+`","name":"Renamed","public":true}`)), alice))
	if w := h.do(as(post("/veil/collections/update", jsonBody(`{"id":"`+worlds+`","name":"Mine now"}`)), bob)); w.Code != 404 {
		t.Errorf("update someone else's = %d", w.Code)
	}
	view := decode(t, h.do(get("/veil/collection?id="+worlds))).(map[string]any)
	if view["name"] != "Renamed" || view["description"] != "" || len(view["items"].([]any)) != 2 {
		t.Errorf("renamed = %v", view)
	}
	h.do(as(post("/veil/collections/remove", jsonBody(`{"id":"`+worlds+`","target":"`+testWorld+`"}`)), alice))
	if n := len(decode(t, h.do(get("/veil/collection?id="+worlds))).(map[string]any)["items"].([]any)); n != 1 {
		t.Errorf("items after remove = %d", n)
	}
	h.do(as(post("/veil/collections/delete", jsonBody(`{"id":"`+worlds+`"}`)), alice))
	if w := h.do(get("/veil/collection?id=" + worlds)); w.Code != 404 {
		t.Errorf("deleted collection = %d", w.Code)
	}
	var items int
	h.srv.accounts.QueryRow("SELECT count(*) FROM collection_items WHERE collection_id = ?", worlds).Scan(&items)
	if items != 0 {
		t.Error("items survived collection deletion")
	}
}

// Public profiles/collections must not expose ownership proofs, even when
// their owner has opted into sharing their VRCove identity.
func TestPublicSocialViewsKeepVRChatLinksPrivate(t *testing.T) {
	h, _ := newAccountHarness(t)
	owner := h.signup("independent_handle", "synthetic passphrase")
	h.stubVRChatUser(testVRCUser, "PrivateVRChatName")
	if w := h.do(vrchatSession(t, as(post("/veil/link", jsonBody(`{}`)), owner))); w.Code != 200 {
		t.Fatalf("link = %d %s", w.Code, w.Body)
	}
	h.do(as(post("/veil/profile", jsonBody(`{"public":true,"bio":"World reviewer"}`)), owner))
	h.do(as(post("/veil/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":4}`)), owner))
	created := decode(t, h.do(as(post("/veil/collections/create", jsonBody(`{"kind":"world","name":"Shared worlds","public":true}`)), owner))).(map[string]any)
	collectionID := created["id"].(string)
	for _, path := range []string{
		"/veil/profile?username=independent_handle",
		"/veil/collection?id=" + collectionID,
		"/veil/reviews?kind=world&id=" + testWorld,
	} {
		w := h.do(get(path))
		if w.Code != 200 {
			t.Fatalf("%s = %d %s", path, w.Code, w.Body)
		}
		for _, private := range []string{testVRCUser, "PrivateVRChatName", `"links"`, `"sealed"`, `"vrc_key"`} {
			if strings.Contains(w.Body.String(), private) {
				t.Errorf("%s leaks %q", path, private)
			}
		}
	}
	// Account deletion cascades through both collections and their contents.
	h.do(as(post("/veil/collections/add", jsonBody(`{"id":"`+collectionID+`","target":"`+testWorld+`"}`)), owner))
	if w := h.do(as(post("/veil/delete", jsonBody(`{"password":"synthetic passphrase"}`)), owner)); w.Code != 200 {
		t.Fatalf("delete = %d", w.Code)
	}
	for _, table := range []string{"collections", "collection_items"} {
		var n int
		if err := h.srv.accounts.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s after deletion: count %d, error %v", table, n, err)
		}
	}
}
