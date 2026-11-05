package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"vrcove/companion/internal/chatbox"
	"vrcove/companion/internal/sensors"
	"vrcove/companion/internal/unitypackage"
	"vrcove/companion/internal/usernamecheck"
)

type chatPrefs struct {
	Template string   `json:"template"`
	Status   string   `json:"status"`
	Port     int      `json:"port"`
	Interval int      `json:"interval"`
	Sound    bool     `json:"sound"`
	Rotation []string `json:"rotation"`
	AFKText  string   `json:"afkText"`
	Phrases  []string `json:"phrases"`
}

const (
	maxChatBody = 65536
	maxChatList = 30
	maxChatItem = 1024
	// sensorInterval spaces the readings {media} and the usage placeholders
	// take, because each may start a helper program.
	sensorInterval = 5 * time.Second
	// viewedFor keeps sensors running this long after the Tools page last asked.
	viewedFor = 15 * time.Second
)

// sensorNames are the placeholders that need a reading from this computer.
var sensorNames = []string{"media", "cpu", "gpu", "ram"}

type desktopTools struct {
	mu          sync.Mutex
	prefs       chatPrefs
	automatic   bool // Deliberately off each time the app starts.
	lastAttempt time.Time
	lastError   string
	extracting  bool
	sender      chatbox.Sender
	usernames   usernamecheck.Checker
	// rotation is the next rotating message to send.
	rotation int
	// Sensor readings, updated by runChatSensors. wanted and viewed record what
	// the Tools page last asked for, to preview a message that isn't saved yet.
	readings map[string]string
	wanted   []string
	viewed   time.Time
	usage    sensors.Sampler
}

func validChatList(items []string) bool {
	if len(items) > maxChatList {
		return false
	}
	for _, item := range items {
		if len(item) > maxChatItem {
			return false
		}
	}
	return true
}

// chatValues gathers what's known right now. Callers hold t.mu.
func (d *desktop) chatValues(ctx context.Context, status string, rotate bool) chatbox.Values {
	t := &d.tools
	values := chatbox.Values{Status: status, Media: t.readings["media"], CPU: t.readings["cpu"], GPU: t.readings["gpu"], RAM: t.readings["ram"]}
	if rotation := nonEmpty(t.prefs.Rotation); len(rotation) > 0 {
		values.Rotate = rotation[t.rotation%len(rotation)]
		if rotate {
			t.rotation = (t.rotation + 1) % len(rotation)
		}
	}
	if d.history != nil {
		if p, err := d.history.Present(ctx); err == nil && p != nil {
			values.World, values.Players = p.World, strconv.Itoa(p.Players)
			values.Playtime = chatbox.Duration(time.Since(time.UnixMilli(p.Since)))
		}
	}
	return values
}

func nonEmpty(items []string) []string {
	kept := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			kept = append(kept, item)
		}
	}
	return kept
}

// renderChat is the next message: the AFK text while VRChat reports AFK, or
// else the template. Callers hold t.mu.
func (d *desktop) renderChat(ctx context.Context, prefs chatPrefs, rotate bool) string {
	now := time.Now()
	values := d.chatValues(ctx, prefs.Status, rotate)
	if since, afk := d.osc.afkSince(); afk && strings.TrimSpace(prefs.AFKText) != "" {
		values.AFK = chatbox.Duration(now.Sub(since))
		return chatbox.Render(prefs.AFKText, values, now)
	}
	template := prefs.Template
	if values.Rotate != "" {
		// A rotating message may hold other placeholders, so it expands first.
		template = strings.ReplaceAll(template, "{rotate}", values.Rotate)
	}
	return chatbox.Render(template, values, now)
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
			Text   string `json:"text"`
			Open   bool   `json:"open"`
			Typing bool   `json:"typing"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, maxChatBody)).Decode(&body) != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid chatbox settings."})
			return
		}
		switch body.Action {
		case "stop":
			t.automatic = false
			// The message would otherwise stay up until VRChat hides it.
			if err := t.sender.Clear(t.prefs.Port); err != nil {
				t.lastError = err.Error()
			}
		case "clear":
			if err := t.sender.Clear(t.prefs.Port); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
		case "typing":
			if err := t.sender.Typing(body.Typing, t.prefs.Port); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
		case "say":
			var err error
			if body.Open {
				err = t.sender.Fill(body.Text, t.prefs.Port)
			} else {
				err = t.sender.Send(body.Text, t.prefs.Port, t.prefs.Sound)
			}
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			// Leave the message up for a full interval before the next automatic one.
			t.lastAttempt = time.Now()
		case "phrases":
			if !validChatList(body.Phrases) {
				writeJSON(w, 400, map[string]string{"error": "Keep to 30 saved phrases."})
				return
			}
			phrases := nonEmpty(body.Phrases)
			if err := d.store.update(func(s *state) { s.Chatbox.Phrases = phrases }); err != nil {
				writeJSON(w, 500, map[string]string{"error": "Could not save chatbox settings."})
				return
			}
			t.prefs.Phrases = phrases
		case "save", "send", "start":
			if body.Port < 1024 || body.Port > 65535 || body.Interval < 5 || body.Interval > 300 || len(body.Template) > 2048 || len(body.Status) > 1024 {
				writeJSON(w, 400, map[string]string{"error": "Use a port from 1024 to 65535 and an interval from 5 to 300 seconds."})
				return
			}
			if len(body.AFKText) > maxChatItem || !validChatList(body.Rotation) || !validChatList(body.Phrases) {
				writeJSON(w, 400, map[string]string{"error": "Keep to 30 rotating messages, each under 1,024 characters."})
				return
			}
			body.Rotation, body.Phrases = nonEmpty(body.Rotation), nonEmpty(body.Phrases)
			previous := t.prefs
			t.prefs = body.chatPrefs
			if body.Action == "send" || body.Action == "start" {
				if err := t.sender.Send(d.renderChat(r.Context(), body.chatPrefs, true), body.Port, body.Sound); err != nil {
					t.prefs = previous
					writeJSON(w, 400, map[string]string{"error": err.Error()})
					return
				}
				t.lastAttempt = time.Now()
				t.lastError = ""
			}
			if err := d.store.update(func(s *state) { s.Chatbox = body.chatPrefs }); err != nil {
				t.prefs = previous
				writeJSON(w, 500, map[string]string{"error": "Could not save chatbox settings."})
				return
			}
			if body.Action == "start" {
				t.automatic = true
			}
		default:
			writeJSON(w, 400, map[string]string{"error": "Unknown chatbox action."})
			return
		}
	} else {
		t.wanted, t.viewed = strings.Split(r.URL.Query().Get("want"), ","), time.Now()
	}
	settings := t.prefs
	settings.Rotation, settings.Phrases = nonEmpty(settings.Rotation), nonEmpty(settings.Phrases)
	writeJSON(w, 200, map[string]any{"settings": settings, "automatic": t.automatic, "error": t.lastError, "values": d.chatPreview(r.Context())})
}

// chatPreview gives the Tools page the live values, so its preview shows what
// VRChat would. Callers hold t.mu.
func (d *desktop) chatPreview(ctx context.Context) map[string]string {
	v := d.chatValues(ctx, "", false)
	values := map[string]string{"media": v.Media, "cpu": v.CPU, "gpu": v.GPU, "ram": v.RAM, "world": v.World, "players": v.Players, "playtime": v.Playtime}
	if since, afk := d.osc.afkSince(); afk {
		values["afk"] = chatbox.Duration(time.Since(since))
	}
	return values
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
			err := t.sender.Send(d.renderChat(ctx, t.prefs, true), t.prefs.Port, t.prefs.Sound)
			// A manual message can use up the slot, and a template like {media}
			// has nothing to show between songs. Neither should stop the loop.
			if err != nil && !errors.Is(err, chatbox.ErrTooSoon) && !errors.Is(err, chatbox.ErrEmpty) {
				t.lastError = err.Error()
				t.automatic = false
			}
		}
		t.mu.Unlock()
	}
}

// sensorsWanted lists the readings needed by automatic messages and by the
// Tools page, so nothing runs while the chatbox isn't in use.
func (d *desktop) sensorsWanted() []string {
	t := &d.tools
	t.mu.Lock()
	defer t.mu.Unlock()
	wanted := []string{}
	if t.automatic {
		text := t.prefs.Template + t.prefs.AFKText + strings.Join(t.prefs.Rotation, "")
		for _, name := range sensorNames {
			if strings.Contains(text, "{"+name+"}") {
				wanted = append(wanted, name)
			}
		}
	}
	if time.Since(t.viewed) < viewedFor {
		for _, name := range t.wanted {
			if slices.Contains(sensorNames, name) && !slices.Contains(wanted, name) {
				wanted = append(wanted, name)
			}
		}
	}
	return wanted
}

func (d *desktop) runChatSensors(ctx context.Context) {
	ticker := time.NewTicker(sensorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		d.readSensors(ctx, d.sensorsWanted())
	}
}

// readSensors updates the readings without holding t.mu, because a reading can
// take seconds.
func (d *desktop) readSensors(ctx context.Context, wanted []string) {
	readings := map[string]string{}
	var cpu, ram int
	var haveUsage bool
	if slices.Contains(wanted, "cpu") || slices.Contains(wanted, "ram") {
		cpu, ram, haveUsage = d.tools.usage.Read()
	}
	for _, name := range wanted {
		switch name {
		case "media":
			readings[name] = sensors.NowPlaying(ctx).String()
		case "gpu":
			if percent, ok := sensors.GPU(ctx); ok {
				readings[name] = fmt.Sprintf("GPU %d%%", percent)
			}
		case "cpu":
			if haveUsage {
				readings[name] = fmt.Sprintf("CPU %d%%", cpu)
			}
		case "ram":
			if haveUsage {
				readings[name] = fmt.Sprintf("RAM %d%%", ram)
			}
		}
	}
	d.tools.mu.Lock()
	d.tools.readings = readings
	d.tools.mu.Unlock()
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
