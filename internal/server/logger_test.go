package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func newLoggerHarness(t *testing.T) (*harness, *accountBrowser, string) {
	t.Helper()
	h, cookie, _ := sessionHarness(t)
	browser := newAccountBrowser(h, cookie)
	browserLogin(t, browser, computerAgent)
	writer, err := OpenCatalogWriter(filepath.Join(t.TempDir(), "logger.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	h.srv.cfg.Logger = &LoggerConfig{Catalog: writer}
	token := generateLoggerKey(t, browser)
	return h, browser, token
}
func generateLoggerKey(t *testing.T, b *accountBrowser) string {
	t.Helper()
	w := b.do(post("/cove/logger-keys", jsonBody(`{"vrchatId":"`+testVRCUser+`"}`)))
	if w.Code != 200 {
		t.Fatalf("generate key: %d %s", w.Code, w.Body)
	}
	var out struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Key == "" {
		t.Fatal("no API key generated")
	}
	return out.Key
}

func TestLoggerVerifiesBeforeDeduplicating(t *testing.T) {
	h, _, token := newLoggerHarness(t)
	writer := h.srv.cfg.Logger.Catalog
	id := "avtr_12345678-1234-1234-1234-123456789abc"
	public := true
	h.upstream = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/1/avatars/"+id {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		visibility := "private"
		if public {
			visibility = "public"
		}
		return respond(200, "application/json", []byte(`{"id":"`+id+`","name":"Test avatar","authorName":"Creator","description":"Searchable description","tags":["test"],"releaseStatus":"`+visibility+`","unityPackages":[{"platform":"standalonewindows","performanceRating":"Good"}]}`)), nil
	}
	submit := func(auth string) *http.Request {
		return post("/logger/avatars", strings.NewReader(`{"ids":["`+id+`"]}`), "Authorization", "Bearer "+auth)
	}
	beforeInvalid := h.fetches.Load()
	if w := h.do(submit("wrong")); w.Code != 401 || h.fetches.Load() != beforeInvalid {
		t.Fatal("unauthenticated submission reached upstream")
	}
	for _, want := range []string{"added", "existing", "rejected"} {
		if want == "rejected" {
			public = false
		}
		before := h.fetches.Load()
		w := h.do(submit(token))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var result struct {
			Results []loggerResult `json:"results"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Results) != 1 || result.Results[0].Status != want {
			t.Fatalf("wanted %s, got %s", want, w.Body.String())
		}
		if h.fetches.Load() != before+1 {
			t.Fatal("duplicate was not verified with VRChat first")
		}
	}
	var count int
	if err := writer.QueryRow("SELECT count(*) FROM catalog WHERE id=?", id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("catalog rows %d, err %v", count, err)
	}
	if err := writer.QueryRow("SELECT count(*) FROM catalog_fts WHERE catalog_fts MATCH 'Searchable'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("search index rows %d, err %v", count, err)
	}
}

func TestLoggerTransientFailureStaysRetryable(t *testing.T) {
	h, _, token := newLoggerHarness(t)
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(429, "application/json", []byte(`{}`)), nil
	}
	w := h.do(post("/logger/avatars", strings.NewReader(`{"ids":["avtr_12345678-1234-1234-1234-123456789abc"]}`), "Authorization", "Bearer "+token))
	if w.Code != 429 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
