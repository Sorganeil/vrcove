package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"vrcove/companion/internal/retry"
	"vrcove/companion/internal/usernamecheck"
)

func (d *desktop) serveUsernameCheck(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Action    string   `json:"action"`
			Usernames []string `json:"usernames"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&body) != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid username list."})
			return
		}
		switch body.Action {
		case "stop":
			d.tools.usernames.Stop()
		case "start":
			id, _ := d.user()
			if id == "" {
				writeJSON(w, 401, map[string]string{"error": "Sign in to VRChat to check usernames."})
				return
			}
			// The app lifetime, rather than the POST or the page, owns the queue.
			ctx := d.toolContext
			if ctx == nil {
				ctx = context.Background()
			}
			err := d.tools.usernames.Start(ctx, body.Usernames, func(ctx context.Context, name string) usernamecheck.Reply {
				active, _ := d.user()
				if active != id {
					return usernamecheck.Reply{Fatal: true, Error: "Your VRChat connection changed. Start a new check after signing in."}
				}
				return d.checkUsername(ctx, name, id)
			})
			if err != nil {
				status := http.StatusBadRequest
				if d.tools.usernames.Snapshot().Running {
					status = http.StatusConflict
				}
				writeJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
		default:
			writeJSON(w, 400, map[string]string{"error": "Unknown username checker action."})
			return
		}
	case http.MethodGet:
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, 200, d.tools.usernames.Snapshot())
}

func (d *desktop) checkUsername(ctx context.Context, name, userID string) usernamecheck.Reply {
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/account/username-check?"+url.Values{"username": {name}}.Encode(), nil)
	r.Header.Set("X-VRChat-Active-User", userID)
	w := httptest.NewRecorder()
	d.serveAPI(w, r)
	switch w.Code {
	case http.StatusOK:
		var result struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) == nil {
			switch result.Status {
			case "available", "taken", "invalid", "unconfirmed":
				return usernamecheck.Reply{Status: result.Status}
			}
		}
		return usernamecheck.Reply{Transient: true, Error: "VRChat returned an unreadable username result."}
	case http.StatusTooManyRequests:
		return usernamecheck.Reply{RetryAt: retry.After(w.Header().Get("Retry-After"), time.Now())}
	case http.StatusUnauthorized, http.StatusConflict:
		return usernamecheck.Reply{Fatal: true, Error: "Your VRChat connection changed. Start a new check after signing in."}
	case http.StatusForbidden:
		return usernamecheck.Reply{Fatal: true, Error: "VRChat denied username checks. Try again later."}
	case http.StatusBadRequest:
		return usernamecheck.Reply{Status: "invalid"}
	default:
		return usernamecheck.Reply{Transient: true, Error: "VRChat is unavailable. Try again later."}
	}
}
