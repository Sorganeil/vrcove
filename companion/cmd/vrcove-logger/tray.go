package main

import (
	"context"
	_ "embed"
	"encoding/binary"
	"runtime"
	"sync"
	"sync/atomic"

	"fyne.io/systray"
	"gioui.org/app"
	"vrcove/companion/internal/buildinfo"
)

//go:embed tray.png
var trayPNG []byte

// Windows wants ICO; its single image is the existing 192px brand PNG.
func trayIcon() []byte {
	if runtime.GOOS != "windows" {
		return trayPNG
	}
	icon := make([]byte, 22, 22+len(trayPNG))
	binary.LittleEndian.PutUint16(icon[2:4], 1)
	binary.LittleEndian.PutUint16(icon[4:6], 1)
	icon[6], icon[7] = 192, 192
	binary.LittleEndian.PutUint16(icon[10:12], 1)
	binary.LittleEndian.PutUint16(icon[12:14], 32)
	binary.LittleEndian.PutUint32(icon[14:18], uint32(len(trayPNG)))
	binary.LittleEndian.PutUint32(icon[18:22], 22)
	return append(icon, trayPNG...)
}

// Only Invalidate crosses into Gio from the worker/tray goroutines. All other
// window operations are owned by the GUI event loop.
type windowSlot struct {
	mu     sync.Mutex
	window *app.Window
}

func (s *windowSlot) set(w *app.Window) { s.mu.Lock(); defer s.mu.Unlock(); s.window = w }
func (s *windowSlot) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.window != nil {
		s.window.Invalidate()
	}
}

type trayController struct {
	mu         sync.Mutex
	started    bool
	ready      atomic.Bool
	ctx        context.Context
	show, quit func()
}

func (t *trayController) start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started || !trayHostAvailable() {
		return
	}
	t.started = true
	go func() {
		// Win32 must pump messages on the OS thread that owns the tray window.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		systray.Run(func() {
			systray.SetIcon(trayIcon())
			systray.SetTooltip("VRCove Logger " + buildinfo.Version)
			systray.SetOnTapped(t.show)
			version := systray.AddMenuItem("VRCove Logger "+buildinfo.Version, buildinfo.Summary())
			version.Disable()
			open := systray.AddMenuItem("Show", "Show the logger window")
			systray.AddSeparator()
			quit := systray.AddMenuItem("Quit", "Stop collecting and exit VRCove Logger")
			t.ready.Store(true)
			go func() {
				for {
					select {
					case <-open.ClickedCh:
						t.show()
					case <-quit.ClickedCh:
						t.quit()
						return
					case <-t.ctx.Done():
						return
					}
				}
			}()
		}, func() { t.ready.Store(false) })
	}()
}

func (t *trayController) canBackground() bool {
	if !trayHostAvailable() {
		return false
	}
	t.start()
	return t.ready.Load()
}

func (t *trayController) stop() {
	t.mu.Lock()
	started := t.started
	t.mu.Unlock()
	if started {
		systray.Quit()
	}
}
