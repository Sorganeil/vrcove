package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vrcove/companion/internal/collector"
	"vrcove/companion/internal/logger"
	"vrcove/companion/internal/retry"
)

// avatarLogger runs the shared collector inside the desktop app. It checks
// avatars with the VRChat session signed in on this device, through the same
// local API the window uses, and uploads with the VRCove API key.
type avatarLogger struct {
	d        *desktop
	commands chan collector.Command
	mu       sync.Mutex
	snap     collector.Snapshot
	config   logger.Config
}

func startLogger(ctx context.Context, d *desktop, dir string, site string) (*avatarLogger, error) {
	path := filepath.Join(dir, "logger.json")
	s, err := logger.Load(path)
	if err != nil {
		return nil, err
	}
	if s.Config.Folder == "" {
		s.Config.Folder = logger.DetectFolder()
	}
	s.Config.Server = site
	s.Verifier = verifier{d}
	l := &avatarLogger{d: d, commands: make(chan collector.Command, 4), config: s.Config}
	updates := make(chan collector.Snapshot, 1)
	go collector.Run(ctx, path, s, l.commands, updates, func() {})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case snap := <-updates:
				l.mu.Lock()
				l.snap = snap
				l.mu.Unlock()
			}
		}
	}()
	return l, nil
}

// serve answers /api/desktop/logger for the logger page.
func (l *avatarLogger) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var body struct {
			Action    string  `json:"action"`
			Token     *string `json:"token"`
			Folder    *string `json:"folder"`
			AutoStart *bool   `json:"autoStart"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request."})
			return
		}
		l.mu.Lock()
		next := l.config
		if body.Token != nil {
			next.Token = strings.TrimSpace(*body.Token)
		}
		if body.Folder != nil {
			next.Folder = strings.TrimSpace(*body.Folder)
		}
		if body.AutoStart != nil {
			next.AutoStart = *body.AutoStart
		}
		changed := next != l.config
		l.config = next
		l.mu.Unlock()
		switch {
		case body.Action == "start" || body.Action == "pause":
			l.commands <- collector.Command{Kind: body.Action}
		case body.Action == "clear-cache":
			reply := make(chan string, 1)
			l.commands <- collector.Command{Kind: "clear-cache", Reply: reply}
			select {
			case <-reply:
			case <-time.After(10 * time.Second):
			}
		case changed:
			l.commands <- collector.Command{Kind: "config", Config: next}
		}
	}
	l.mu.Lock()
	snap, config := l.snap, l.config
	l.mu.Unlock()
	events := []map[string]string{}
	for _, e := range snap.Events {
		events = append(events, map[string]string{"time": e.Time, "id": e.ID, "result": e.Result})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": snap.Status, "paused": snap.Paused, "detected": snap.Detected, "added": snap.Added,
		"existing": snap.Existing, "rejected": snap.Rejected, "pending": snap.Pending, "events": events,
		"folder": config.Folder, "autoStart": config.AutoStart, "hasKey": config.Token != "",
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// verifier checks avatars with this device's VRChat session.
type verifier struct{ d *desktop }

func (v verifier) Ready() bool {
	id, _ := v.d.user()
	return id != ""
}

func (v verifier) Check(ctx context.Context, id string) (bool, error) {
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/account/avatar?id="+id, nil)
	w := httptest.NewRecorder()
	v.d.serveAPI(w, r)
	switch w.Code {
	case http.StatusOK:
		var avatar struct{ ID, ReleaseStatus string }
		if json.Unmarshal(w.Body.Bytes(), &avatar) != nil {
			return false, errors.New("VRChat sent an unreadable avatar · collecting locally")
		}
		return avatar.ID == id && avatar.ReleaseStatus == "public", nil
	case http.StatusForbidden, http.StatusNotFound, http.StatusBadRequest:
		return false, nil
	case http.StatusUnauthorized:
		return false, errors.New("VRChat sign-in expired · sign in again · collecting locally")
	case http.StatusTooManyRequests:
		return false, &retry.Limited{Source: "VRChat", Until: time.Now().Add(retry.Fallback)}
	default:
		return false, errors.New("VRChat is unavailable · collecting locally")
	}
}
