package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVRCoveBioAndStatus(t *testing.T) {
	h, _ := newAccountHarness(t)
	owner := h.signup("own_profile", "synthetic passphrase")
	other := h.signup("other_profile", "synthetic passphrase")
	update := func(patch string) map[string]any {
		return decode(t, h.do(as(post("/cove/profile", jsonBody(patch)), owner))).(map[string]any)
	}
	got := update(`{"bio":"World explorer.\nAvatar collector.","status":"  Building a cozy world ☕  "}`)
	if got["bio"] != "World explorer.\nAvatar collector." || got["status"] != "Building a cozy world ☕" {
		t.Fatalf("profile fields = %v", got)
	}
	path := "/cove/profile?username=own_profile"
	for _, r := range []*http.Request{get(path), as(get(path), other)} {
		if w := h.do(r); w.Code != 404 || strings.Contains(w.Body.String(), "Building a cozy") {
			t.Fatal("private profile fields leaked")
		}
	}
	profile := decode(t, h.do(as(get(path), owner))).(map[string]any)
	if profile["status"] != got["status"] || profile["bio"] != got["bio"] {
		t.Fatal("own profile lost saved fields")
	}
	update(`{"public":true}`)
	public := decode(t, h.do(get(path))).(map[string]any)
	if public["bio"] != got["bio"] || public["status"] != got["status"] {
		t.Fatal("public profile lost saved fields")
	}
	update(`{"status":""}`)
	me := decode(t, h.do(as(get("/cove/me"), owner))).(map[string]any)
	if me["status"] != "" || me["bio"] != got["bio"] {
		t.Fatal("clearing status changed bio")
	}
	if h.fetches.Load() != 0 {
		t.Fatal("VRCove bio or status contacted VRChat")
	}
}

func TestProfilePatchValidationIsAtomic(t *testing.T) {
	h, _ := newAccountHarness(t)
	owner := h.signup("own_profile", "synthetic passphrase")
	h.do(as(post("/cove/profile", jsonBody(`{"bio":"Original bio","status":"Original status"}`)), owner))
	for _, status := range []any{strings.Repeat("x", 161), "two\nlines", "a\tb", "bad\x00status", false, 42, nil, strings.Repeat("😀", 81)} {
		raw, _ := json.Marshal(map[string]any{"public": true, "bio": "Should not save", "status": status})
		if w := h.do(as(post("/cove/profile", strings.NewReader(string(raw))), owner)); w.Code != 400 {
			t.Fatalf("invalid status accepted: %q %d", status, w.Code)
		}
		me := decode(t, h.do(as(get("/cove/me"), owner))).(map[string]any)
		if me["public"] != false || me["bio"] != "Original bio" || me["status"] != "Original status" {
			t.Fatalf("invalid patch partially applied: %v", me)
		}
	}
	raw, _ := json.Marshal(map[string]any{"bio": strings.Repeat("x", 501), "status": "Should not save", "public": true})
	if w := h.do(as(post("/cove/profile", strings.NewReader(string(raw))), owner)); w.Code != 400 {
		t.Fatal("invalid bio accepted")
	}
	raw, _ = json.Marshal(map[string]string{"status": strings.Repeat("😀", 80)})
	if w := h.do(as(post("/cove/profile", strings.NewReader(string(raw))), owner)); w.Code != 200 {
		t.Fatal("valid UTF-16 status boundary rejected")
	}
}

func TestStatusMigrationPreservesExistingBio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(accountSchema); err != nil {
		t.Fatal(err)
	}
	for _, migration := range accountMigrations[:len(accountMigrations)-1] {
		if _, err = db.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("PRAGMA user_version = " + strconv.Itoa(len(accountMigrations)-1)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO accounts(username,username_key,password,created,bio) VALUES ('existing','existing','hash','2026-10-15','Existing bio')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = OpenAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var bio, status string
	if err = db.QueryRow("SELECT bio,status FROM accounts WHERE username_key='existing'").Scan(&bio, &status); err != nil {
		t.Fatal(err)
	}
	if bio != "Existing bio" || status != "" {
		t.Fatal("migration did not preserve existing profile")
	}
}
