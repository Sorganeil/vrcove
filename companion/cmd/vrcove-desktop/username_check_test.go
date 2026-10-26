package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"vrcove/companion/internal/usernamecheck"
)

func TestDesktopUsernameQueueUsesLocalSessionAndRetriesSameName(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	if w := do(d, "POST", "/api/desktop/tools/usernames", `{"action":"start","usernames":["tester"]}`); w.Code != 401 {
		t.Fatalf("anonymous=%d", w.Code)
	}
	var mu sync.Mutex
	var names []string
	var starts []time.Time
	d.api.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		h := http.Header{"Content-Type": {"application/json"}}
		body := `{"id":"usr_00000000-0000-0000-0000-000000000001","username":"tester","displayName":"Tester"}`
		code := 200
		if r.URL.Path == "/api/1/auth/user" {
			h.Set("Set-Cookie", "auth=authcookie_desktop; Path=/")
		} else {
			if r.URL.Host != "api.vrchat.cloud" || r.URL.Path != "/api/1/auth/exists" {
				t.Errorf("wrong destination %s", r.URL)
			}
			mu.Lock()
			names = append(names, r.URL.Query().Get("username"))
			starts = append(starts, time.Now())
			n := len(names)
			mu.Unlock()
			if n == 1 {
				code = 429
				h.Set("Retry-After", "2")
				body = `{}`
			} else {
				body = `{"userExists":false,"nameOk":true}`
				if n == 2 {
					h.Set("Set-Cookie", "auth=authcookie_rotated; Path=/")
				}
				if n == 3 && !strings.Contains(r.Header.Get("Cookie"), "auth=authcookie_rotated") {
					t.Error("rotated cookie not reused")
				}
			}
		}
		return &http.Response{StatusCode: code, Header: h, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if w := do(d, "POST", "/api/account/login", `{"username":"tester","password":"synthetic passphrase"}`); w.Code != 200 {
		t.Fatalf("login=%d %s", w.Code, w.Body)
	}
	if w := do(d, "POST", "/api/desktop/tools/usernames", `{"action":"start","usernames":["one & two","second"]}`); w.Code != 200 {
		t.Fatalf("start=%d %s", w.Code, w.Body)
	}
	t.Cleanup(d.tools.usernames.Stop)
	deadline := time.Now().Add(8 * time.Second)
	for d.tools.usernames.Snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	w := do(d, "GET", "/api/desktop/tools/usernames", "")
	var s usernamecheck.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Running || s.Phase != "complete" || len(s.Rows) != 2 || s.Rows[0].Status != "available" {
		t.Fatalf("snapshot=%+v", s)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(names, ",") != "one & two,one & two,second" {
		t.Fatalf("names=%v", names)
	}
	if starts[1].Sub(starts[0]) < 2500*time.Millisecond || starts[2].Sub(starts[1]) < 2500*time.Millisecond {
		t.Errorf("requests were too close: %v", starts)
	}
}
