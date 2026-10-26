package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
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
	case "/api/desktop/tools/usernames":
		d.serveUsernameCheck(w, r)
	case "/api/desktop/tools/chatbox":
		d.serveChatbox(w, r)
	case "/api/desktop/tools/extract":
		d.serveExtract(w, r)
	case "/api/desktop/update":
		d.serveUpdate(w, r)
	case "/api/desktop/autostart":
		d.serveAutostart(w, r)
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
	case "/api/desktop/gamelog":
		if !d.hasHistory(w) {
			return
		}
		entries, err := d.history.GameLog(r.Context(), before, q.Get("kind"), q.Get("q"), 100)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "The game log is unavailable."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	case "/api/desktop/feed":
		if !d.hasHistory(w) {
			return
		}
		owner, _ := d.user()
		if owner == "" {
			writeJSON(w, 200, map[string]any{"entries": []any{}})
			return
		}
		entries, err := d.history.FeedFor(r.Context(), owner, before, q.Get("kind"), q.Get("q"), 100)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "The feed is unavailable."})
			return
		}
		d.resolveFeedEntries(r.Context(), owner, entries)
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
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
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

func (d *desktop) serveUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if d.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Updater is unavailable."})
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Action string `json:"action"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid update request."})
			return
		}
		switch body.Action {
		case "check":
			st, err := d.updater.Check(r.Context())
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, st)
			return
		case "install":
			st, err := d.updater.Install(r.Context())
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, st)
			return
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unknown update action."})
			return
		}
	}
	writeJSON(w, http.StatusOK, d.updater.Status())
}

func (d *desktop) serveAutostart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if d.autostart == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Startup settings are unavailable."})
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || body.Enabled == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid startup request."})
			return
		}
		if err := d.autostart.SetEnabled(*body.Enabled); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}
	enabled, err := d.autostart.Enabled()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
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

func isUnresolvedDisplayName(name, userID string) bool {
	trimmed := strings.TrimSpace(name)
	return trimmed == "" || trimmed == userID || strings.HasPrefix(trimmed, "usr_")
}

func (d *desktop) localGet(ctx context.Context, path string) ([]byte, int) {
	reqCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	r := httptest.NewRequestWithContext(reqCtx, http.MethodGet, "http://wails.localhost"+path, nil)
	w := httptest.NewRecorder()
	d.serveAPI(w, r)
	return w.Body.Bytes(), w.Code
}

// resolveFeedEntries looks up VRChat display names for any feed rows that
// recorded only a raw usr_ identifier (such as an initial friend-offline event)
// and persists the resolved names into the local history database.
func (d *desktop) resolveFeedEntries(ctx context.Context, owner string, entries []history.FeedEntry) {
	if d.history == nil || len(entries) == 0 {
		return
	}
	missingUsers := []string{}
	seenUsers := map[string]bool{}
	for _, e := range entries {
		if isUnresolvedDisplayName(e.DisplayName, e.UserID) && strings.HasPrefix(e.UserID, "usr_") && !seenUsers[e.UserID] {
			seenUsers[e.UserID] = true
			missingUsers = append(missingUsers, e.UserID)
		}
	}
	if len(missingUsers) == 0 {
		return
	}
	resolved := map[string]string{}
	limit := min(len(missingUsers), 10)
	for _, id := range missingUsers[:limit] {
		body, code := d.localGet(ctx, "/api/account/user?id="+url.QueryEscape(id))
		if code != http.StatusOK {
			continue
		}
		var u struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		}
		if json.Unmarshal(body, &u) == nil && !isUnresolvedDisplayName(u.DisplayName, id) {
			resolved[id] = strings.TrimSpace(u.DisplayName)
		}
	}
	if len(resolved) == 0 {
		return
	}
	_ = d.history.RememberUsers(ctx, owner, resolved)
	for i := range entries {
		if name, ok := resolved[entries[i].UserID]; ok && isUnresolvedDisplayName(entries[i].DisplayName, entries[i].UserID) {
			entries[i].DisplayName = name
		}
	}
}

// learnAccountEntities records display names from successful local VRChat
// account responses so the feed and history stay resolved.
func (d *desktop) learnAccountEntities(ctx context.Context, path string, body []byte) {
	if d.history == nil || len(body) == 0 {
		return
	}
	owner, _ := d.user()
	switch path {
	case "/api/account/friends":
		var list []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		}
		if json.Unmarshal(body, &list) == nil && len(list) > 0 {
			users := make(map[string]string, len(list))
			for _, u := range list {
				if !isUnresolvedDisplayName(u.DisplayName, u.ID) {
					users[u.ID] = u.DisplayName
				}
			}
			_ = d.history.RememberUsers(ctx, owner, users)
		}
	case "/api/account/user", "/api/account/me":
		var u struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		}
		if json.Unmarshal(body, &u) == nil && !isUnresolvedDisplayName(u.DisplayName, u.ID) {
			_ = d.history.RememberUsers(ctx, owner, map[string]string{u.ID: u.DisplayName})
		}
	case "/api/account/world":
		var w struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(body, &w) == nil && w.ID != "" && w.Name != "" {
			_ = d.history.RememberWorld(ctx, w.ID, w.Name)
		}
	}
}
