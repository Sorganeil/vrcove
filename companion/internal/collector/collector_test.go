package collector

import (
	"context"
	"encoding/json"
	"fmt"
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

// blockingVerifier stands in for the desktop VRChat login and can hold a check.
type blockingVerifier struct {
	block   string
	started chan struct{}
	release chan struct{}
}

func (v *blockingVerifier) Ready() bool { return true }
func (v *blockingVerifier) Check(ctx context.Context, id string) (bool, error) {
	if id == v.block {
		v.started <- struct{}{}
		select {
		case <-v.release:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return true, nil
}

func TestCacheWritesAreCollectedWhileNetworkWaits(t *testing.T) {
	for _, stage := range []string{"catalog check", "VRChat check", "ingestion"} {
		t.Run(stage, func(t *testing.T) { collectWhileBlocked(t, stage) })
	}
}

func collectWhileBlocked(t *testing.T, stage string) {
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
	hold := func(r *http.Request, want string) bool {
		if stage != want {
			return true
		}
		started <- struct{}{}
		select {
		case <-release:
			return true
		case <-r.Context().Done():
			return false
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		// With burst pacing the next batch goes out at once; hold it so the
		// later ID stays queued.
		if !slices.Equal(body.IDs, []string{first}) {
			<-r.Context().Done()
			return
		}
		switch r.URL.Path {
		case "/api/logger/known":
			if hold(r, "catalog check") {
				_ = json.NewEncoder(w).Encode(map[string]any{"existing": []string{}})
			}
		case "/api/logger/avatars":
			if hold(r, "ingestion") {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []logger.Result{{ID: first, Status: "added"}}})
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	verifier := &blockingVerifier{started: started, release: release}
	if stage == "VRChat check" {
		verifier.block = first
	}
	state := logger.New()
	state.Config = logger.Config{Folder: folder, Server: server.URL, Token: "test-only"}
	state.Verifier = verifier
	path := filepath.Join(t.TempDir(), "state.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	commands := make(chan Command, 1)
	updates := make(chan Snapshot, 1)
	go func() { defer close(done); Run(ctx, path, state, commands, updates, func() {}) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("collector did not stop")
		}
	}()
	commands <- Command{Kind: "start"}
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
	// Capture this update before VRChat clears it, even with the request blocked.
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
			commands := make(chan Command, 1)
			updates, reply := make(chan Snapshot, 1), make(chan string, 1)
			go func() { defer close(done); Run(ctx, path, state, commands, updates, func() {}) }()
			defer func() { cancel(); <-done }()
			commands <- Command{Kind: "clear-cache", Reply: reply}
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

func TestAutoStartCollectsAsSoonAsTheWorkerStarts(t *testing.T) {
	for _, auto := range []bool{true, false} {
		t.Run(fmt.Sprint("autostart=", auto), func(t *testing.T) {
			id := "avtr_12345678-1234-1234-1234-123456789abc"
			folder := t.TempDir()
			if err := os.WriteFile(filepath.Join(folder, "amplitude.cache"), []byte(`[{"avatar":"`+id+`"}]`), 0600); err != nil {
				t.Fatal(err)
			}
			state := logger.New()
			state.Config = logger.Config{Folder: folder, AutoStart: auto}
			path := filepath.Join(t.TempDir(), "state.json")
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			updates := make(chan Snapshot, 1)
			go func() { defer close(done); Run(ctx, path, state, make(chan Command), updates, func() {}) }()
			defer func() { cancel(); <-done }()
			// Well before the three-second polling tick.
			deadline := time.Now().Add(time.Second)
			collected := false
			for time.Now().Before(deadline) && !collected {
				saved, err := logger.Load(path)
				collected = err == nil && slices.Contains(saved.Pending, id)
				time.Sleep(20 * time.Millisecond)
			}
			if collected != auto {
				t.Fatalf("collected = %v", collected)
			}
			if snap := <-updates; snap.Paused == auto {
				t.Fatalf("paused = %v", snap.Paused)
			}
		})
	}
}

func TestOnlyServerOrKeyChangesResetRateLimitWait(t *testing.T) {
	until := time.Now().Add(5 * time.Minute).Truncate(time.Second)
	for _, tc := range []struct {
		name  string
		edit  func(*logger.Config)
		reset bool
	}{
		{"auto-start", func(c *logger.Config) { c.AutoStart = true }, false},
		{"api key", func(c *logger.Config) { c.Token = "other-key" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := logger.New()
			state.Config = logger.Config{Folder: t.TempDir(), Server: "https://vrcove.example", Token: "test-only"}
			state.RetryAt = until
			path := filepath.Join(t.TempDir(), "state.json")
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			commands := make(chan Command)
			go func() { defer close(done); Run(ctx, path, state, commands, make(chan Snapshot, 1), func() {}) }()
			next := state.Config
			tc.edit(&next)
			commands <- Command{Kind: "config", Config: next}
			cancel()
			<-done
			saved, err := logger.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if saved.RetryAt.IsZero() != tc.reset || saved.Config != next {
				t.Fatalf("retry %v config %+v", saved.RetryAt, saved.Config)
			}
		})
	}
}
