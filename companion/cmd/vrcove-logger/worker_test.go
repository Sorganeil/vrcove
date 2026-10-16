package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"vrcove/companion/internal/logger"
)

func TestCacheWritesAreCollectedWhileUploadWaits(t *testing.T) {
	first := "avtr_12345678-1234-1234-1234-123456789abc"
	second := "avtr_87654321-1234-1234-1234-123456789abc"
	folder := t.TempDir()
	cache := filepath.Join(folder, "amplitude.cache")
	write := func(id string) {
		t.Helper()
		if err := os.WriteFile(cache, []byte(`[{"avatar":"`+id+`"}]`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(first)
	started, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if slices.Contains(body.IDs, first) {
			started <- struct{}{}
			select {
			case <-release:
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []logger.Result{{ID: first, Status: "added"}}})
			case <-r.Context().Done():
			}
		} else {
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	state := logger.New()
	state.Config = logger.Config{Folder: folder, Server: server.URL, Token: "test-only"}
	path := filepath.Join(t.TempDir(), "state.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	commands := make(chan command, 1)
	updates := make(chan snapshot, 1)
	go func() { defer close(done); worker(ctx, path, state, commands, updates, func() {}) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("collector did not stop")
		}
	}()
	commands <- command{kind: "start"}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upload did not start")
	}
	waitState := func(check func(*logger.State) bool) {
		t.Helper()
		deadline := time.Now().Add(1500 * time.Millisecond)
		for time.Now().Before(deadline) {
			saved, err := logger.Load(path)
			if err == nil && check(saved) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("cache write was not persisted before the next polling tick")
	}
	// Capture this update before VRChat clears it, even with HTTP blocked.
	write(second)
	waitState(func(s *logger.State) bool { return slices.Contains(s.Pending, second) })
	if err := os.WriteFile(cache, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitState(func(s *logger.State) bool {
		return s.Added == 1 && !slices.Contains(s.Pending, first) && slices.Contains(s.Pending, second)
	})
}

func TestClearCacheSavesIDsBeforeClearing(t *testing.T) {
	for _, storageOK := range []bool{true, false} {
		name := "storage-failure"
		if storageOK {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			folder := t.TempDir()
			cache := filepath.Join(folder, "amplitude.cache")
			id := "avtr_12345678-1234-1234-1234-123456789abc"
			original := `[{"avatar":"` + id + `","telemetry":"discard"}]`
			if err := os.WriteFile(cache, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "state.json")
			if !storageOK {
				if err := os.WriteFile(path, []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, "state.json")
			}
			state := logger.New()
			state.Config.Folder = folder
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			commands := make(chan command, 1)
			updates, reply := make(chan snapshot, 1), make(chan string, 1)
			go func() { defer close(done); worker(ctx, path, state, commands, updates, func() {}) }()
			defer func() { cancel(); <-done }()
			commands <- command{kind: "clear-cache", reply: reply}
			var message string
			select {
			case message = <-reply:
			case <-time.After(2 * time.Second):
				t.Fatal("clear action did not reply")
			}
			contents, err := os.ReadFile(cache)
			if err != nil {
				t.Fatal(err)
			}
			if !storageOK {
				if string(contents) != original || !strings.Contains(message, "Storage unavailable") {
					t.Fatal("cleared analytics despite failed queue persistence")
				}
				return
			}
			saved, err := logger.Load(path)
			if err != nil || !slices.Contains(saved.Pending, id) || string(contents) != "[]" {
				t.Fatal("clear did not preserve the queued ID and reset analytics")
			}
		})
	}
}
