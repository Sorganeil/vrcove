package main

import (
	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"golang.org/x/sys/windows"
)

func trayHostAvailable() bool { return true }

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	postWindowMessage = user32.NewProc("PostMessageW")
	showWindowAsync   = user32.NewProc("ShowWindowAsync")
	foregroundWindow  = user32.NewProc("SetForegroundWindow")
)

type windowControls struct{ hwnd uintptr }

func newWindowControls(*app.Window) *windowControls { return &windowControls{} }
func (c *windowControls) observe(e event.Event) {
	if view, ok := e.(app.Win32ViewEvent); ok {
		c.hwnd = view.HWND
	}
}
func (c *windowControls) perform(a system.Action) {
	hwnd := c.hwnd
	if hwnd == 0 {
		return
	}
	switch a {
	case system.ActionClose:
		postWindowMessage.Call(hwnd, 0x0010 /* WM_CLOSE */, 0, 0)
	case system.ActionRaise:
		// Synchronous Gio window actions can wait on the very FrameEvent our
		// event loop must draw. Post the restore and focus using native handles
		// instead, without sharing Gio state with another goroutine.
		showWindowAsync.Call(hwnd, 9 /* SW_RESTORE */)
		go foregroundWindow.Call(hwnd)
	}
}
