package logger

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAmplitudeSnapshotsAndProtonPath(t *testing.T) {
	appData := filepath.Join(t.TempDir(), "pfx", "drive_c", "users", "steamuser", "AppData")
	logs := filepath.Join(appData, "LocalLow", "VRChat", "VRChat")
	analytics := filepath.Join(appData, "Local", "Temp", "VRChat", "VRChat", "amplitude.cache")
	for _, dir := range []string{logs, filepath.Dir(analytics)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	s.Config.Folder = logs
	second := "avtr_87654321-1234-1234-1234-123456789abc"
	write := func(avatar string) []byte {
		t.Helper()
		// Synthetic analytics, with no final newline and a stable long prefix.
		data := []byte(`{"telemetry":"` + strings.Repeat("private", 800) + `","avatar":"` + avatar + `","duplicate":"` + avatar + `"}`)
		if err := os.WriteFile(analytics, data, 0600); err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, avatar := range []string{id, id, second} {
		before := write(avatar)
		if err := s.Scan(); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(analytics)
		if err != nil || string(before) != string(after) {
			t.Fatal("scanner changed analytics")
		}
	}
	if !reflect.DeepEqual(s.Pending, []string{id, second}) {
		t.Fatalf("missed rewrite or duplicated IDs: %v", s.Pending)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := s.Save(statePath); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(statePath)
	if strings.Contains(string(saved), "telemetry") || strings.Contains(string(saved), "privateprivate") {
		t.Fatal("persisted analytics contents")
	}
	restored, err := Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Scan(); err != nil || len(restored.Pending) != 2 {
		t.Fatalf("restart failed: %v, %v", err, restored.Pending)
	}
	if err := os.WriteFile(analytics, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := restored.Scan(); err != nil || len(restored.Pending) != 2 {
		t.Fatal("clearing source lost queued avatars")
	}
}

func TestAmplitudeResumesAfterQueueFills(t *testing.T) {
	s := New()
	path := filepath.Join(t.TempDir(), "amplitude.cache")
	if err := os.WriteFile(path, []byte(id), 0600); err != nil {
		t.Fatal(err)
	}
	s.Pending = make([]string, maxPending)
	if _, err := s.scanAmplitude(path); err != nil {
		t.Fatal(err)
	}
	if _, exists := s.Seen[id]; exists {
		t.Fatal("marked an unqueued avatar as seen")
	}
	s.Pending = nil
	if _, err := s.scanAmplitude(path); err != nil || !reflect.DeepEqual(s.Pending, []string{id}) {
		t.Fatalf("did not revisit snapshot: %v, %v", err, s.Pending)
	}
}
