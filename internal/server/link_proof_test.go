package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLinkingProvesOwnershipWithProfileCode(t *testing.T) {
	h, _ := newAccountHarness(t)
	h.srv.cfg.Bot = BotConfig{Username: "vrcove_bot", Password: "synthetic bot password"}
	signInBot(t, h)
	now := time.Now()
	h.srv.now = func() time.Time { return now }
	owner := h.signup("proof_owner", "proof passphrase")
	other := h.signup("proof_other", "other passphrase")
	bio := "Hello"
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/1/users/"+testVRCUser || !strings.Contains(r.Header.Get("Cookie"), "auth=bot_auth") {
			t.Errorf("unexpected upstream %s", r.URL)
		}
		data, _ := json.Marshal(map[string]string{"id": testVRCUser, "displayName": "Proof Owner", "bio": bio})
		return respond(200, "application/json", data), nil
	}
	challenge := func(c *http.Cookie) string {
		t.Helper()
		w := h.do(as(post("/cove/link/challenge", jsonBody(`{"vrchatId":"`+testVRCUser+`"}`)), c))
		var out struct{ Code string }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || !strings.HasPrefix(out.Code, "vrcove-") {
			t.Fatalf("challenge: %d %s", w.Code, w.Body)
		}
		return out.Code
	}
	verify := func(c *http.Cookie) int {
		return h.do(as(post("/cove/link/verify", jsonBody(`{"vrchatId":"`+testVRCUser+`","username":"proofowner"}`)), c)).Code
	}
	linked := func(c *http.Cookie) bool {
		return strings.Contains(h.do(as(get("/cove/me"), c)).Body.String(), testVRCUser)
	}

	code := challenge(owner)
	if got := verify(owner); got != http.StatusConflict || linked(owner) {
		t.Fatalf("without the code in the profile: %d", got)
	}
	bio = "Hello\n" + code
	if got := verify(owner); got != 200 || !linked(owner) {
		t.Fatalf("with the code: %d", got)
	}
	if got := verify(owner); got != http.StatusGone {
		t.Fatalf("a used code: %d", got)
	}
	// Someone else cannot take the account over, even with their own code.
	bio = challenge(other)
	if got := verify(other); got != http.StatusConflict || linked(other) {
		t.Fatalf("an account linked elsewhere: %d", got)
	}
	// Codes expire.
	bio = challenge(other)
	now = now.Add(linkCodeLifetime + time.Second)
	if got := verify(other); got != http.StatusGone {
		t.Fatalf("an expired code: %d", got)
	}
	if w := h.do(post("/cove/link/challenge", jsonBody(`{"vrchatId":"`+testVRCUser+`"}`))); w.Code != http.StatusUnauthorized {
		t.Fatalf("without a VRCove account: %d", w.Code)
	}
}
