package server

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSignupTurnstile(t *testing.T) {
	h, _ := newAccountHarness(t)
	h.srv.cfg.TurnstileSiteKey, h.srv.cfg.TurnstileSecret = "site-key", "synthetic-secret"
	reply := `{"success":true,"action":"signup","hostname":"vrcove.example.test"}`
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != turnstileVerifyURL || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		if form.Get("secret") != "synthetic-secret" || form.Get("response") != "good-token" {
			return respond(200, "application/json", []byte(`{"success":false}`)), nil
		}
		return respond(200, "application/json", []byte(reply)), nil
	}
	signup := func(name, token string) int {
		body := `{"username":"` + name + `","password":"a long passphrase","turnstile":"` + token + `"}`
		return h.do(post("/cove/signup", strings.NewReader(body))).Code
	}
	if code := signup("no_token", ""); code != 400 || h.fetches.Load() != 0 {
		t.Fatalf("missing token = %d after %d fetches", code, h.fetches.Load())
	}
	if code := signup("bad_token", "forged"); code != 400 {
		t.Fatalf("rejected token = %d", code)
	}
	reply = `{"success":true,"action":"login","hostname":"vrcove.example.test"}`
	if code := signup("wrong_action", "good-token"); code != 400 {
		t.Fatalf("wrong action = %d", code)
	}
	reply = `{"success":true,"action":"signup","hostname":"evil.example"}`
	if code := signup("wrong_host", "good-token"); code != 400 {
		t.Fatalf("wrong hostname = %d", code)
	}
	reply = `{"success":true,"action":"signup","hostname":"vrcove.example.test"}`
	if code := signup("verified_user", "good-token"); code != 200 {
		t.Fatalf("verified signup = %d", code)
	}
	if body := h.do(get("/stats")).Body.String(); !strings.Contains(body, `"turnstileSiteKey":"site-key"`) || strings.Contains(body, "synthetic-secret") {
		t.Fatalf("stats = %s", body)
	}
}
