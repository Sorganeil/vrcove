package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"vrcove/companion/internal/chatbox"
	"vrcove/companion/internal/unitypackage"
	"vrcove/companion/internal/usernamecheck"
)

type chatPrefs struct {
	Template string `json:"template"`
	Status   string `json:"status"`
	Port     int    `json:"port"`
	Interval int    `json:"interval"`
	Sound    bool   `json:"sound"`
}

type desktopTools struct {
	mu          sync.Mutex
	prefs       chatPrefs
	automatic   bool // Deliberately off each time the app starts.
	lastAttempt time.Time
	lastError   string
	extracting  bool
	sender      chatbox.Sender
	usernames   usernamecheck.Checker
}

func (d *desktop) serveChatbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	t := &d.tools
	t.mu.Lock()
	defer t.mu.Unlock()
	if r.Method == http.MethodPost {
		var body struct {
			chatPrefs
			Action string `json:"action"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&body) != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid chatbox settings."})
			return
		}
		if body.Action == "stop" {
			t.automatic = false
		} else {
			if body.Action != "save" && body.Action != "send" && body.Action != "start" {
				writeJSON(w, 400, map[string]string{"error": "Unknown chatbox action."})
				return
			}
			if body.Port < 1024 || body.Port > 65535 || body.Interval < 5 || body.Interval > 300 || len(body.Template) > 2048 || len(body.Status) > 1024 {
				writeJSON(w, 400, map[string]string{"error": "Use a port from 1024 to 65535 and an interval from 5 to 300 seconds."})
				return
			}
			if body.Action == "send" || body.Action == "start" {
				if err := t.sender.Send(chatbox.Render(body.Template, body.Status, time.Now()), body.Port, body.Sound); err != nil {
					writeJSON(w, 400, map[string]string{"error": err.Error()})
					return
				}
				t.lastAttempt = time.Now()
				t.lastError = ""
			}
			if err := d.store.update(func(s *state) { s.Chatbox = body.chatPrefs }); err != nil {
				writeJSON(w, 500, map[string]string{"error": "Could not save chatbox settings."})
				return
			}
			t.prefs = body.chatPrefs
			if body.Action == "start" {
				t.automatic = true
			}
		}
	}
	writeJSON(w, 200, map[string]any{"settings": t.prefs, "automatic": t.automatic, "error": t.lastError})
}

func (d *desktop) runChatbox(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		t := &d.tools
		t.mu.Lock()
		if t.automatic && time.Since(t.lastAttempt) >= time.Duration(t.prefs.Interval)*time.Second {
			t.lastAttempt = time.Now()
			if err := t.sender.Send(chatbox.Render(t.prefs.Template, t.prefs.Status, time.Now()), t.prefs.Port, t.prefs.Sound); err != nil {
				t.lastError = err.Error()
				t.automatic = false
			}
		}
		t.mu.Unlock()
	}
}

func (d *desktop) serveExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Metadata bool `json:"metadata"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid extraction options."})
		return
	}
	if d.choosePackage == nil || d.chooseFolder == nil {
		writeJSON(w, 503, map[string]string{"error": "File selection is unavailable."})
		return
	}
	d.tools.mu.Lock()
	if d.tools.extracting {
		d.tools.mu.Unlock()
		writeJSON(w, 409, map[string]string{"error": "Another package is being extracted."})
		return
	}
	d.tools.extracting = true
	d.tools.mu.Unlock()
	defer func() { d.tools.mu.Lock(); d.tools.extracting = false; d.tools.mu.Unlock() }()
	source, err := d.choosePackage()
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "Could not open the package picker."})
		return
	}
	if source == "" {
		writeJSON(w, 200, map[string]bool{"cancelled": true})
		return
	}
	folder, err := d.chooseFolder()
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "Could not open the destination picker."})
		return
	}
	if folder == "" {
		writeJSON(w, 200, map[string]bool{"cancelled": true})
		return
	}
	result, err := unitypackage.Extract(r.Context(), source, folder, body.Metadata)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, result)
}
