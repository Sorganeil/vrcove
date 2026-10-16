package server

import (
	"net/http"
	"sync"
	"testing"
)

// Two overlapping link requests from the same VRCove account (a double tap, or two tabs)
// must not report the VRChat account as linked to a different VRCove account.
func TestConcurrentLinksFromSameAccount(t *testing.T) {
	for attempt := range 20 {
		h, _ := newAccountHarness(t)
		account := h.signup("race_owner_"+string(rune('a'+attempt)), "synthetic passphrase")
		h.upstream = func(*http.Request) (*http.Response, error) {
			return respond(200, "application/json", []byte(`{"id":"`+testVRCUser+`","displayName":"Racer"}`), "Set-Cookie", "auth=synthetic_token; Path=/"), nil
		}
		var wg sync.WaitGroup
		codes := make([]int, 6)
		for i := range codes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = h.do(as(post("/account/login", jsonBody(`{"username":"vrc","password":"synthetic"}`)), account)).Code
			}()
		}
		wg.Wait()
		for _, code := range codes {
			if code != 200 {
				t.Fatalf("attempt %d: concurrent link codes %v", attempt, codes)
			}
		}
		var links int
		if err := h.srv.accounts.QueryRow("SELECT count(*) FROM vrchat_links").Scan(&links); err != nil || links != 1 {
			t.Fatalf("links = %d, %v", links, err)
		}
	}
}
