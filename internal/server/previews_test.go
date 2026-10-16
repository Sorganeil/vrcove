package server

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func previewHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.srv.static = fstest.MapFS{"index.html": {Data: []byte("<!doctype html><html><head><title>VRCove</title></head><body>app</body></html>")}}
	return h
}

func TestAvatarPreviewHTML(t *testing.T) {
	h := previewHarness(t)
	id := "avtr_00000000-0000-0000-0000-000000000001"
	raw := `{"name":"Avatar <script>bad</script>","description":"A \"quoted\" description","thumbnail":"https://api.vrchat.cloud/api/1/image/file_00000000-0000-0000-0000-000000000001/1/256"}`
	var databasePath string
	if err := h.srv.db.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&databasePath); err != nil {
		t.Fatal(err)
	}
	writable, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer writable.Close()
	if _, err := writable.Exec("INSERT INTO catalog(id,data) VALUES (?,?)", id, raw); err != nil {
		t.Fatal(err)
	}
	w := h.do(httptest.NewRequest("GET", "http://localhost/avatar/"+id+"?title=forged", nil))
	body := w.Body.String()
	for _, want := range []string{`property="og:title" content="Avatar &lt;script&gt;bad&lt;/script&gt; · VRCove"`, `A &#34;quoted&#34; description`, origin + "/avatar/" + id, `/api/image?url=`, `name="twitter:card"`} {
		if !strings.Contains(body, want) {
			t.Errorf("preview missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "forged") || strings.Contains(body, "<script>bad") {
		t.Fatal("unescaped or query-controlled preview")
	}
	r := httptest.NewRequest("HEAD", "http://localhost/avatar/"+id, nil)
	if w := h.do(r); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %s", w.Code, w.Body.String())
	}
}

func TestPublicPreviewsNeverUseViewerCredentials(t *testing.T) {
	for _, tc := range []struct{ kind, id, body, title string }{
		{"world", "wrld_00000000-0000-0000-0000-000000000001", `{"name":"Cozy World","releaseStatus":"public","description":"Visit here","thumbnailImageUrl":"https://evil.example/a.png"}`, "Cozy World"},
		{"group", "grp_00000000-0000-0000-0000-000000000001", `{"name":"Public Group","privacy":"default","description":"Our community"}`, "Public Group"},
		{"user", "usr_00000000-0000-0000-0000-000000000001", `{"displayName":"Public Name","email":"private@example.test","location":"private-instance","statusDescription":"Secret status"}`, "Public Name"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			h := previewHarness(t)
			h.upstream = func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Cookie") != "" {
					t.Fatal("preview sent credentials upstream")
				}
				if r.URL.Path != "/api/1/"+tc.kind+"s/"+tc.id {
					t.Fatalf("unexpected URL %s", r.URL)
				}
				return respond(200, "application/json", []byte(tc.body)), nil
			}
			r := httptest.NewRequest("GET", "http://localhost/"+tc.kind+"/"+tc.id, nil)
			r.Header.Set("Cookie", "auth=must-not-leave")
			body := h.do(r).Body.String()
			if !strings.Contains(body, `property="og:title" content="`+tc.title+`"`) {
				t.Fatal(body)
			}
			for _, private := range []string{"private@example.test", "private-instance", "Secret status", "evil.example"} {
				if strings.Contains(body, private) {
					t.Fatalf("preview leaked %s", private)
				}
			}
			h.do(httptest.NewRequest("GET", "http://localhost/"+tc.kind+"/"+tc.id, nil))
			if h.fetches.Load() != 1 {
				t.Fatal("preview missed cache")
			}
		})
	}
}

func TestRestrictedAndUnavailablePreviewFallback(t *testing.T) {
	for _, kind := range []string{"world", "group", "user"} {
		h := previewHarness(t)
		prefix := map[string]string{"world": "wrld_", "group": "grp_", "user": "usr_"}[kind]
		id := prefix + "00000000-0000-0000-0000-000000000001"
		h.upstream = func(*http.Request) (*http.Response, error) {
			return respond(401, "application/json", []byte(`{"error":"private response"}`)), nil
		}
		body := h.do(httptest.NewRequest("GET", "http://localhost/"+kind+"/"+id, nil)).Body.String()
		if !strings.Contains(body, "VRChat "+kind+" · VRCove") || strings.Contains(body, "private response") {
			t.Fatal(body)
		}
	}
	h := previewHarness(t)
	for _, kind := range []string{"world", "group"} {
		if _, ok := publicPreview(kind, object{"name": "Secret", "releaseStatus": "private", "privacy": "private"}); ok {
			t.Fatal("private entity accepted")
		}
	}
	h.srv.cachePreview("world", "id", object{"name": "Secret", "releaseStatus": "private"})
	if len(h.srv.previews.Entries) != 0 {
		t.Fatal("private world cached")
	}
}

func TestLinkReminderPersistsDismissal(t *testing.T) {
	h, _ := newAccountHarness(t)
	cookie := h.signup("onboarding", "a synthetic long passphrase")
	me := h.do(as(get("/veil/me"), cookie))
	if !strings.Contains(me.Body.String(), `"showLinkReminder":true`) {
		t.Fatal(me.Body.String())
	}
	dismissed := h.do(as(post("/veil/link-reminder/dismiss", jsonBody(`{}`)), cookie))
	if dismissed.Code != 200 || !strings.Contains(dismissed.Body.String(), `"showLinkReminder":false`) {
		t.Fatal(dismissed.Body.String())
	}
	signedIn := h.do(post("/veil/login", jsonBody(`{"username":"onboarding","password":"a synthetic long passphrase"}`)))
	if signedIn.Code != 200 || !strings.Contains(signedIn.Body.String(), `"showLinkReminder":false`) {
		t.Fatal(signedIn.Body.String())
	}
	if w := h.do(post("/veil/link-reminder/dismiss", jsonBody(`{}`))); w.Code != 401 {
		t.Fatalf("anonymous dismiss: %d", w.Code)
	}
}
