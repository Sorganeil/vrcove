package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"time"

	"vrcove/companion/internal/history"
)

// runLive keeps a connection to VRChat's live feed while VRChat is signed in
// on this device, even with the window closed. It records friend activity and
// shows notifications on the desktop. The connection goes straight to VRChat.
func (d *desktop) runLive(ctx context.Context) {
	wait := func(t time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(t):
			return true
		}
	}
	delay := 10 * time.Second
	for ctx.Err() == nil {
		if id, _ := d.user(); id == "" {
			if !wait(30 * time.Second) {
				return
			}
			continue
		}
		opened := time.Now()
		d.listen(ctx)
		// Reconnect quickly after a long connection, slowly after failures.
		if time.Since(opened) > 2*time.Minute {
			delay = 10 * time.Second
		} else {
			delay = min(delay*2, 5*time.Minute)
		}
		if !wait(delay) {
			return
		}
	}
}

// listen reads one pipeline connection until it closes.
func (d *desktop) listen(ctx context.Context) {
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/account/live", nil)
	for name, value := range d.store.cookies(false) {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	pipe, err := d.api.OpenPipeline(dialCtx, r)
	cancel()
	if err != nil {
		return
	}
	defer pipe.Close()
	for {
		message, err := pipe.Read(ctx)
		if err != nil {
			return
		}
		var envelope struct {
			Err string `json:"err"`
		}
		if json.Unmarshal(message, &envelope) == nil && envelope.Err != "" {
			return
		}
		if d.history == nil {
			continue
		}
		note, err := d.history.Record(ctx, message, time.Now())
		if err == nil && note != nil {
			d.alert(note)
		}
	}
}

// noteKind groups VRChat notification types like the window's settings do.
func noteKind(t string) string {
	switch t {
	case "friendRequest", "invite", "requestInvite":
		return t
	case "inviteResponse", "requestInviteResponse":
		return "response"
	}
	return "other"
}

// alert shows a notification if its kind is switched on.
func (d *desktop) alert(n *history.Note) {
	prefs := d.store.notify()
	kind := noteKind(n.Type)
	if !prefs.Enabled || !slices.Contains(prefs.Kinds, kind) || d.notify == nil {
		return
	}
	text := func(key, fallback string) string {
		if v := prefs.Text[key]; v != "" {
			return v
		}
		return fallback
	}
	name := n.SenderUsername
	if name == "" {
		name = text("someone", "Someone")
	}
	title, body := "", n.Message
	switch kind {
	case "friendRequest":
		title, body = text(kind, "{name} sent you a friend request"), ""
	case "invite":
		title = text(kind, "{name} invited you")
		if n.WorldName != "" {
			body = n.WorldName
		}
	case "requestInvite":
		title = text(kind, "{name} asked for an invite")
	case "response":
		title = text(kind, "{name} replied to your invite")
	default:
		title = name
		if body == "" {
			body = text("other", "New VRChat notification")
		}
	}
	d.notify("vrchat-"+n.ID, strings.ReplaceAll(title, "{name}", name), body)
}
