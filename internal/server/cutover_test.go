package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCutoverMovesVRChatOutOfTheServer(t *testing.T) {
	h, cookie, _ := sessionHarness(t)
	browser := newAccountBrowser(h, cookie)
	browserLogin(t, browser, computerAgent)
	var stored int
	if err := h.srv.accounts.QueryRow("SELECT count(*) FROM vrchat_sessions").Scan(&stored); err != nil || stored == 0 {
		t.Fatalf("no session to forget: %d %v", stored, err)
	}
	h.srv.cfg.VRChatWorker = "only"
	before := h.fetches.Load()
	for _, r := range []*http.Request{get("/account/me"), post("/account/login", jsonBody(`{"username":"vrc","password":"synthetic"}`)), post("/cove/link", jsonBody(`{}`))} {
		w := browser.do(r)
		if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "vrchat_relink") {
			t.Errorf("%s %s: %d %s", r.Method, r.URL.Path, w.Code, w.Body)
		}
	}
	// The image proxy no longer sends anyone's VRChat cookie.
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" {
			t.Errorf("image fetched with a cookie: %q", r.Header.Get("Cookie"))
		}
		return respond(200, "image/png", []byte("\x89PNG\r\n\x1a\n")), nil
	}
	browser.do(get("/image?url=" + "https%3A%2F%2Fapi.vrchat.cloud%2Fapi%2F1%2Fimage%2Ffile_00000000-0000-0000-0000-000000000000%2F1%2F256"))
	if h.fetches.Load() != before+1 {
		t.Errorf("%d upstream requests, want only the image", h.fetches.Load()-before)
	}
	n, err := ForgetVRChatSessions(context.Background(), h.srv.accounts)
	if err != nil || n != int64(stored) {
		t.Fatalf("forgot %d of %d sessions: %v", n, stored, err)
	}
	if err := h.srv.accounts.QueryRow("SELECT count(*) FROM vrchat_sessions").Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("%d sessions left", stored)
	}
}
