package main

import (
	"context"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"github.com/godbus/dbus/v5"
)

// Do not hide the only window on desktops without an active tray host.
func trayHostAvailable() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var value dbus.Variant
	err = conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher").CallWithContext(ctx,
		"org.freedesktop.DBus.Properties.Get", 0, "org.kde.StatusNotifierWatcher", "IsStatusNotifierHostRegistered").Store(&value)
	available, _ := value.Value().(bool)
	return err == nil && available
}

type windowControls struct{ window *app.Window }

func newWindowControls(w *app.Window) *windowControls { return &windowControls{window: w} }
func (c *windowControls) observe(event.Event)         {}
func (c *windowControls) perform(a system.Action)     { c.window.Perform(a) }
