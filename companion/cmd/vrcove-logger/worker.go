package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"vrcove/companion/internal/logger"
)

type command struct {
	kind   string
	config logger.Config
	reply  chan string
}
type snapshot struct {
	status                                       string
	detected, added, existing, rejected, pending int
	events                                       []logger.Event
	paused                                       bool
}

type uploadResult struct {
	state *logger.State
	err   error
}

func worker(ctx context.Context, path string, s *logger.State, commands <-chan command, updates chan snapshot, wake func()) {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	paused := true
	s.Status = "Collection paused"
	var previous *snapshot
	publish := func() {
		snap := snapshot{s.Status, len(s.Seen), s.Added, s.Existing, s.Rejected, len(s.Pending), append([]logger.Event(nil), s.Events...), paused}
		if previous != nil && reflect.DeepEqual(*previous, snap) {
			return
		}
		previous = &snap
		select {
		case <-updates:
		default:
		}
		select {
		case updates <- snap:
		default:
		}
		wake()
	}
	persist := func() bool {
		if err := s.Save(path); err != nil {
			paused = true
			s.Status = "Storage unavailable · collection stopped"
			return false
		}
		return true
	}
	publish()
	// Watch the directory so replacement/rename writes remain visible. Polling
	// is retained as a fallback if native notifications are unavailable.
	watcher, _ := fsnotify.NewWatcher()
	var watchEvents <-chan fsnotify.Event
	var watchErrors <-chan error
	watchFolder := ""
	if watcher != nil {
		defer watcher.Close()
		watchEvents, watchErrors = watcher.Events, watcher.Errors
	}
	watch := func() {
		if watcher == nil || watchFolder == s.Config.Folder {
			return
		}
		if watchFolder != "" {
			_ = watcher.Remove(watchFolder)
			watchFolder = ""
		}
		if watcher.Add(s.Config.Folder) == nil {
			watchFolder = s.Config.Folder
		}
	}
	watch()
	results := make(chan uploadResult, 1)
	submitting := false
	submit := func() {
		if paused || len(s.Pending) == 0 {
			return
		}
		if s.Config.Token == "" {
			s.Status = "Collecting locally · uploads off"
			return
		}
		if time.Now().Before(s.RetryAt) {
			s.Status = "Uploads waiting to retry · collecting locally"
			return
		}
		if submitting {
			return
		}
		// Submit an isolated batch. The collector remains the sole owner of the
		// live state and can read cache writes while the network is waiting.
		batch := logger.New()
		batch.Config, batch.Failures = s.Config, s.Failures
		batch.Pending = append([]string(nil), s.Pending[:min(5, len(s.Pending))]...)
		for _, id := range batch.Pending {
			batch.Seen[id] = "queued"
		}
		submitting = true
		go func() { results <- uploadResult{batch, batch.Submit(ctx, client)} }()
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			persist()
			return
		case result := <-results:
			submitting = false
			batch := result.state
			// A response from an old connection must not remove IDs intended for
			// the newly configured server.
			if batch.Config != s.Config {
				continue
			}
			for id, status := range batch.Seen {
				if status != "queued" {
					s.Seen[id] = status
				}
			}
			s.Pending = slices.DeleteFunc(s.Pending, func(id string) bool {
				status, ok := batch.Seen[id]
				return ok && status != "queued"
			})
			s.Added += batch.Added
			s.Existing += batch.Existing
			s.Rejected += batch.Rejected
			s.Events = append(batch.Events, s.Events...)
			s.Events = s.Events[:min(8, len(s.Events))]
			s.RetryAt, s.Failures = batch.RetryAt, batch.Failures
			if result.err != nil && !paused {
				s.Status = result.err.Error()
			}
			persist()
			publish()
			continue
		case event, ok := <-watchEvents:
			if !ok {
				watchEvents = nil
				continue
			}
			if event.Name == watchFolder && event.Has(fsnotify.Remove|fsnotify.Rename) {
				watchFolder = ""
			}
			if filepath.Base(event.Name) != "amplitude.cache" {
				continue
			}
		case _, ok := <-watchErrors:
			if !ok {
				watchErrors = nil
				continue
			}
		case c := <-commands:
			switch c.kind {
			case "clear-cache":
				if err := s.Scan(); err != nil {
					s.Status = "Cache not cleared · " + err.Error()
				} else if s.QueueFull() {
					if persist() {
						s.Status = "Cache not cleared · upload queued IDs to free space first."
					}
				} else if persist() {
					if err := logger.ClearAmplitudeCache(s.Config.Folder); err != nil {
						s.Status = "Cache not cleared · " + err.Error()
					} else {
						s.Status = "Analytics cache cleared · queue saved"
					}
				}
				select {
				case c.reply <- s.Status:
				default:
				}
				publish()
				continue
			case "start":
				paused = false
			case "pause":
				paused = true
				s.Status = "Collection paused"
			case "config":
				s.Config = c.config
				s.RetryAt = time.Time{}
				s.Failures = 0
			}
			persist()
		case <-ticker.C:
		}
		watch()
		if !paused {
			if err := s.Scan(); err != nil {
				s.Status = err.Error()
			} else if persist() {
				submit()
			}
		}
		publish()
	}
}

func displayFolder(path string) string {
	if path == "" {
		return "Choose your VRChat analytics folder"
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		path = strings.Replace(path, home, "~", 1)
	}
	return path
}

func detectFolder() string {
	return logger.DetectFolder()
}
