package logger

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const id = "avtr_12345678-1234-1234-1234-123456789abc"

func TestOutputLogsAreNotAnAvatarSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "output_log_2026-10-16.txt"), []byte(id+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Config.Folder = dir
	if err := s.Scan(); err != nil {
		t.Fatal(err)
	}
	if len(s.Pending) != 0 {
		t.Fatal("collected an ID from an obsolete output-log source")
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSubmitOnlyAcknowledgesRequestedIDs(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "state.json"))
	s.Config.Token = "test"
	s.Pending = []string{id}
	s.Seen[id] = "queued"
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "{\"ids\":[\""+id+"\"]}" {
			t.Fatalf("unexpected payload %s", body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[{"id":"unrequested","status":"added"}]}`))}, nil
	})}
	if err := s.Submit(context.Background(), client); err == nil {
		t.Fatal("expected retry for unacknowledged ID")
	}
	if len(s.Pending) != 1 || s.Added != 0 || s.Failures != 1 {
		t.Fatal("invalid result changed queue")
	}
}

func TestServerRequiresSafeOrigin(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://user:password@example.com", "https://example.com/path", "https://example.com?x=1"} {
		if _, err := ValidateServer(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"https://vrcove.st", "http://127.0.0.1:8080"} {
		if _, err := ValidateServer(raw); err != nil {
			t.Fatal(err)
		}
	}
}
