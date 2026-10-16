package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	knownAvatar   = "avtr_00000000-0000-0000-0000-00000000000a"
	missingAvatar = "avtr_00000000-0000-0000-0000-00000000000b"
)

func loggerPost(h *harness, path, key string, ids ...string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]any{"ids": ids})
	return h.do(post(path, strings.NewReader(string(body)), "Authorization", "Bearer "+key))
}

// privateUpstream answers every avatar lookup as a private avatar, so ingestion
// rejects it after verification.
func privateUpstream(h *harness) {
	h.upstream = func(r *http.Request) (*http.Response, error) {
		id := strings.TrimPrefix(r.URL.Path, "/api/1/avatars/")
		return respond(200, "application/json", []byte(`{"id":"`+id+`","releaseStatus":"private"}`)), nil
	}
}

func TestLoggerKnownChecksCatalogWithoutVRChatOrQuota(t *testing.T) {
	h, _, key := newLoggerHarness(t)
	if _, err := h.srv.cfg.Logger.Catalog.Exec("INSERT INTO catalog(id,name,data) VALUES(?,?,?)", knownAvatar, "Known", "{}"); err != nil {
		t.Fatal(err)
	}
	privateUpstream(h)
	before := h.fetches.Load()
	w := loggerPost(h, "/logger/known", key, knownAvatar, missingAvatar, "not-an-avatar", knownAvatar)
	if w.Code != 200 {
		t.Fatalf("known: %d %s", w.Code, w.Body)
	}
	var out struct{ Existing []string }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || len(out.Existing) != 1 || out.Existing[0] != knownAvatar {
		t.Fatalf("existing = %s", w.Body)
	}
	if w := loggerPost(h, "/logger/known", "wrong", knownAvatar); w.Code != 401 {
		t.Fatalf("preflight without a valid key: %d", w.Code)
	}
	if w := loggerPost(h, "/logger/known", key, knownAvatar, knownAvatar, knownAvatar, knownAvatar, knownAvatar, knownAvatar); w.Code != 400 {
		t.Fatalf("six IDs accepted: %d", w.Code)
	}
	// Many preflights leave the whole ingestion quota available.
	for range 100 {
		if w := loggerPost(h, "/logger/known", key, missingAvatar); w.Code != 200 {
			t.Fatalf("preflight: %d", w.Code)
		}
	}
	if h.fetches.Load() != before {
		t.Fatal("catalog preflight contacted VRChat")
	}
	for i := range loggerBatches {
		if w := loggerPost(h, "/logger/avatars", key, missingAvatar); w.Code != 200 {
			t.Fatalf("ingestion %d after preflights: %d", i+1, w.Code)
		}
	}
}

func TestLoggerIngestionLimitIsExactAndResets(t *testing.T) {
	h, _, key := newLoggerHarness(t)
	privateUpstream(h)
	now := time.Now()
	h.srv.now = func() time.Time { return now }
	// Rejected IDs still count: verification work was done for each request.
	for i := range loggerBatches {
		w := loggerPost(h, "/logger/avatars", key, missingAvatar)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"rejected"`) {
			t.Fatalf("batch %d: %d %s", i+1, w.Code, w.Body)
		}
	}
	before := h.fetches.Load()
	w := loggerPost(h, "/logger/avatars", key, missingAvatar)
	if w.Code != 429 || w.Header().Get("X-RateLimit-Source") != "vrcove" || w.Header().Get("Retry-After") != "600" {
		t.Fatalf("41st batch: %d source=%q retry=%q", w.Code, w.Header().Get("X-RateLimit-Source"), w.Header().Get("Retry-After"))
	}
	if h.fetches.Load() != before {
		t.Fatal("over-limit batch reached VRChat")
	}
	now = now.Add(9 * time.Minute)
	if w := loggerPost(h, "/logger/avatars", key, missingAvatar); w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("before reset: %d retry=%q", w.Code, w.Header().Get("Retry-After"))
	}
	now = now.Add(time.Minute + time.Second)
	if w := loggerPost(h, "/logger/avatars", key, missingAvatar); w.Code != 200 {
		t.Fatalf("after reset: %d", w.Code)
	}
}

func TestLoggerBusyRefusalDoesNotUseQuota(t *testing.T) {
	h, _, key := newLoggerHarness(t)
	privateUpstream(h)
	h.srv.loggerSlot <- struct{}{}
	w := loggerPost(h, "/logger/avatars", key, missingAvatar)
	<-h.srv.loggerSlot
	if w.Code != 429 || w.Header().Get("Retry-After") != "15" || w.Header().Get("X-RateLimit-Source") != "vrcove" {
		t.Fatalf("busy: %d %v", w.Code, w.Header())
	}
	for i := range loggerBatches {
		if w := loggerPost(h, "/logger/avatars", key, missingAvatar); w.Code != 200 {
			t.Fatalf("batch %d after a busy refusal: %d", i+1, w.Code)
		}
	}
}

func TestLoggerReportsVRChatLimitsDuringServerVerification(t *testing.T) {
	h, _, key := newLoggerHarness(t)
	h.upstream = func(*http.Request) (*http.Response, error) {
		return respond(429, "application/json", []byte(`{}`), "Retry-After", "120"), nil
	}
	w := loggerPost(h, "/logger/avatars", key, missingAvatar)
	if w.Code != 429 || w.Header().Get("X-RateLimit-Source") != "vrchat" || w.Header().Get("Retry-After") != "120" {
		t.Fatalf("server-side VRChat limit: %d source=%q retry=%q", w.Code, w.Header().Get("X-RateLimit-Source"), w.Header().Get("Retry-After"))
	}

	// A limit after the first ID returns the completed result plus when to retry.
	h2, _, key2 := newLoggerHarness(t)
	calls := 0
	h2.upstream = func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return respond(200, "application/json", []byte(`{"id":"`+knownAvatar+`","name":"Public","releaseStatus":"public"}`)), nil
		}
		return respond(429, "application/json", []byte(`{}`), "Retry-After", "45"), nil
	}
	w = loggerPost(h2, "/logger/avatars", key2, knownAvatar, missingAvatar)
	if w.Code != 200 || w.Header().Get("Retry-After") != "45" || w.Header().Get("X-RateLimit-Source") != "vrchat" {
		t.Fatalf("partial batch: %d %v %s", w.Code, w.Header(), w.Body)
	}
	var out struct{ Results []loggerResult }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || len(out.Results) != 1 || out.Results[0].Status != "added" {
		t.Fatalf("partial results: %s", w.Body)
	}
}
