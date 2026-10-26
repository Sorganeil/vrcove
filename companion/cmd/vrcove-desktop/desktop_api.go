package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"vrcove/companion/internal/history"
	"vrcove/companion/internal/logger"
)

// serveDesktop answers the /api/desktop routes, which only exist in the app:
// the logger, notifications, and the history kept on this device.
func (d *desktop) serveDesktop(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	switch r.URL.Path {
	case "/api/desktop/logger":
		if d.logger == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "The avatar logger could not start."})
			return
		}
		d.logger.serve(w, r)
	case "/api/desktop/notifications":
		d.serveNotifications(w, r)
	case "/api/desktop/open":
		// Links to other sites open in the system browser, https only.
		var body struct{ URL string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
		u, err := url.Parse(body.URL)
		if r.Method != http.MethodPost || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || d.openURL == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Only https links can be opened."})
			return
		}
		d.openURL(u.String())
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case "/api/desktop/history":
		if !d.hasHistory(w) {
			return
		}
		visits, err := d.history.Visits(r.Context(), before, q.Get("q"), 30)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "History is unavailable."})
			return
		}
		if visits == nil {
			visits = []history.Visit{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"visits": visits, "folder": history.LogFolder(d.analyticsFolder())})
	case "/api/desktop/feed":
		if !d.hasHistory(w) {
			return
		}
		entries, err := d.history.Feed(r.Context(), before, q.Get("kind"), q.Get("q"), 100)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "The feed is unavailable."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	case "/api/desktop/screenshots":
		if !d.hasHistory(w) {
			return
		}
		folder := history.ScreenshotFolder(d.analyticsFolder())
		months := history.Months(folder)
		month := q.Get("month")
		if month == "" && len(months) > 0 {
			month = months[0]
		}
		shots, err := d.history.Screenshots(r.Context(), folder, month)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Screenshots are unavailable."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"folder": folder, "months": months, "month": month, "screenshots": shots})
	case "/api/desktop/screenshot":
		path, ok := history.ScreenshotPath(history.ScreenshotFolder(d.analyticsFolder()), q.Get("file"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeFile(w, r, path)
	default:
		http.NotFound(w, r)
	}
}

func (d *desktop) hasHistory(w http.ResponseWriter) bool {
	if d.history == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "History could not be opened on this device."})
	}
	return d.history != nil
}

// analyticsFolder is VRChat's analytics folder as the logger has it; the log
// and screenshot folders are found next to it.
func (d *desktop) analyticsFolder() string {
	if d.logger != nil {
		d.logger.mu.Lock()
		defer d.logger.mu.Unlock()
		if d.logger.config.Folder != "" {
			return d.logger.config.Folder
		}
	}
	return logger.DetectFolder()
}

func (d *desktop) serveNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var body struct {
			Action  string            `json:"action"`
			Enabled *bool             `json:"enabled"`
			Kinds   []string          `json:"kinds"`
			Text    map[string]string `json:"text"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&body) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request."})
			return
		}
		d.store.update(func(s *state) {
			if body.Enabled != nil {
				s.Notify.Enabled = *body.Enabled
			}
			if body.Kinds != nil {
				s.Notify.Kinds = body.Kinds
			}
			if body.Text != nil {
				s.Notify.Text = body.Text
			}
		})
		if body.Action == "test" && d.notify != nil {
			prefs := d.store.notify()
			title, text := prefs.Text["testTitle"], prefs.Text["testBody"]
			if title == "" {
				title, text = "Notifications are on", "New VRChat notifications will show up like this."
			}
			d.notify("vrcove-test", title, text)
		}
	}
	prefs := d.store.notify()
	if prefs.Kinds == nil {
		prefs.Kinds = []string{"friendRequest", "invite", "requestInvite", "response"}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": prefs.Enabled, "kinds": prefs.Kinds})
}

// readLogs keeps the game log history up to date while the app runs.
func (d *desktop) readLogs(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if d.history != nil {
			d.history.ReadLogs(ctx, history.LogFolder(d.analyticsFolder()))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
