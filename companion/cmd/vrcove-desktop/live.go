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
	wait := func(t time.Duration, changed <-chan struct{}) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(t):
			return true
		case <-changed:
			return true
		}
	}
	delay := 10 * time.Second
	for ctx.Err() == nil {
		_, _, changed := d.store.localSnapshot()
		if id, _ := d.user(); id == "" {
			if !wait(30*time.Second, changed) {
				return
			}
			continue
		}
		opened := time.Now()
		d.listen(ctx)
		select {
		case <-changed:
			delay = 10 * time.Second
			continue
		default:
		}
		// Reconnect quickly after a long connection, slowly after failures.
		if time.Since(opened) > 2*time.Minute {
			delay = 10 * time.Second
		} else {
			delay = min(delay*2, 5*time.Minute)
		}
		if !wait(delay, changed) {
			return
		}
	}
}

// listen reads one pipeline connection until it closes.
func (d *desktop) listen(ctx context.Context) {
	cookies, version, changed := d.store.localSnapshot()
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		select {
		case <-changed:
			stop()
		case <-ctx.Done():
		}
	}()
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/account/live", nil)
	for name, value := range cookies {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	owner, _ := d.api.LocalUser(r)
	if owner == "" {
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	pipe, err := d.api.OpenPipeline(dialCtx, r)
	cancel()
	if err != nil {
		return
	}
	defer pipe.Close()
	go func() { <-ctx.Done(); pipe.Close() }()
	for {
		message, err := pipe.Read(ctx)
		if err != nil {
			return
		}
		_, current, _ := d.store.localSnapshot()
		if current != version || ctx.Err() != nil {
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
		note, err := d.history.RecordFor(ctx, owner, message, time.Now())
		if err == nil && note != nil && ctx.Err() == nil {
			_, latest, _ := d.store.localSnapshot()
			if latest == version {
				d.alert(note)
			}
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
