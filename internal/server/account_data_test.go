package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExportAccount(t *testing.T) {
	h, _ := newAccountHarness(t)
	alice := h.signup("moonlit", "alice passphrase")
	h.stubVRChatUser(testVRCUser, "SunnyOtter")
	h.do(vrchatSession(t, as(post("/cove/link", jsonBody(`{}`)), alice)))
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":4,"body":"cozy"}`)), alice))
	bob := h.linkedSignup("bob_v", "bob passphrase!")
	h.do(as(post("/cove/review", jsonBody(`{"kind":"world","id":"`+testWorld+`","rating":1,"body":"not mine"}`)), bob))

	if w := h.do(post("/cove/export", jsonBody(`{"password":"alice passphrase"}`))); w.Code != 401 {
		t.Errorf("anonymous export = %d", w.Code)
	}
	if w := h.do(as(post("/cove/export", jsonBody(`{"password":"wrong"}`)), alice)); w.Code != 401 {
		t.Errorf("export with wrong password = %d", w.Code)
	}
	w := h.do(as(post("/cove/export", jsonBody(`{"password":"alice passphrase"}`)), alice))
	if w.Code != 200 {
		t.Fatalf("export = %d %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, `attachment; filename="vrcove-data-moonlit-`) {
		t.Errorf("Content-Disposition = %q", got)
	}
	var data struct {
		Account     map[string]any   `json:"account"`
		VRChatLinks []map[string]any `json:"vrchatLinks"`
		Reviews     []map[string]any `json:"reviews"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.Account["username"] != "moonlit" {
		t.Errorf("account = %v", data.Account)
	}
	if len(data.VRChatLinks) != 1 || data.VRChatLinks[0]["vrchatId"] != testVRCUser || data.VRChatLinks[0]["displayName"] != "SunnyOtter" {
		t.Errorf("links = %v", data.VRChatLinks)
	}
	if len(data.Reviews) != 1 || data.Reviews[0]["body"] != "cozy" {
		t.Errorf("reviews = %v", data.Reviews)
	}
	for _, secret := range []string{"alice passphrase", "$argon2", "not mine", "bob_v"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("export contains %q", secret)
		}
	}
}
