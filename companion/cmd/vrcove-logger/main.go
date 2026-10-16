package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"vrcove/companion/internal/buildinfo"
	"vrcove/companion/internal/logger"
)

var (
	bg     = color.NRGBA{R: 20, G: 24, B: 31, A: 255}
	panel  = color.NRGBA{R: 29, G: 35, B: 45, A: 255}
	fg     = color.NRGBA{R: 237, G: 242, B: 249, A: 255}
	muted  = color.NRGBA{R: 151, G: 163, B: 181, A: 255}
	accent = color.NRGBA{R: 128, G: 220, B: 190, A: 255}
	line   = color.NRGBA{R: 49, G: 60, B: 75, A: 255}
)

type gui struct {
	th                                    *material.Theme
	config                                logger.Config
	snap                                  snapshot
	preview, settings                     bool
	message                               string
	commands                              chan command
	toggle, change, connect, save, cancel widget.Clickable
	quit                                  widget.Clickable
	clearCache                            widget.Clickable
	clearReply                            chan string
	onQuit                                func()
	folder, server, token                 widget.Editor
	list                                  widget.List
}

func main() {
	preview := flag.Bool("preview", false, "Show sample activity without reading analytics, saving state or submitting")
	path := flag.String("state", "", "Override the state path")
	version := flag.Bool("version", false, "Print version and build information, then exit")
	flag.Parse()
	if *version {
		fmt.Println(buildinfo.Summary())
		return
	}
	go func() {
		if err := run(*preview, *path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(preview bool, path string) error {
	if path == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		path = filepath.Join(dir, "vrcove-logger", "state.json")
	}
	s := logger.New()
	var err error
	if !preview {
		s, err = logger.Load(path)
		if err != nil {
			return err
		}
	}
	if preview {
		s.Config.Folder = "VRChat / analytics"
	} else if s.Config.Folder == "" {
		s.Config.Folder = detectFolder()
	}
	g := &gui{th: newTheme(), config: s.Config, commands: make(chan command, 4), clearReply: make(chan string, 1), preview: preview}
	g.list.Axis = layout.Vertical
	g.folder.SingleLine = true
	g.server.SingleLine = true
	g.token.SingleLine = true
	g.token.Mask = '•'
	g.snap = snapshot{status: "Collection paused", detected: len(s.Seen), added: s.Added, existing: s.Existing, rejected: s.Rejected, pending: len(s.Pending), events: append([]logger.Event(nil), s.Events...), paused: true}
	if preview {
		g.snap = previewSnapshot()
	}
	updates := make(chan snapshot, 1)
	ctx, cancel := context.WithCancel(context.Background())
	g.onQuit = cancel
	var slot windowSlot
	show := make(chan struct{}, 1)
	requestShow := func() {
		select {
		case show <- struct{}{}:
		default:
		}
		slot.invalidate()
	}
	tray := &trayController{ctx: ctx, show: requestShow, quit: func() { cancel(); slot.invalidate() }}
	if !preview {
		tray.start()
		defer tray.stop()
	}
	go func() { <-ctx.Done(); slot.invalidate() }()
	done := make(chan struct{})
	if !preview {
		go func() { defer close(done); worker(ctx, path, s, g.commands, updates, slot.invalidate) }()
	}
	defer func() {
		cancel()
		if !preview {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		}
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		w := new(app.Window)
		w.Option(app.Title("VRCove Logger "+buildinfo.Version), app.Size(500, 530), app.MinSize(460, 480))
		slot.set(w)
		err := g.windowLoop(ctx, w, updates, show, tray)
		slot.set(nil)
		if err != nil || preview || ctx.Err() != nil {
			return err
		}
		// The collector and queue outlive the closed window. Recreate only the
		// graphics resources when the user opens the logger from its tray icon.
		select {
		case <-ctx.Done():
			return nil
		case <-show:
		}
	}
}

func (g *gui) windowLoop(ctx context.Context, w *app.Window, updates <-chan snapshot, show chan struct{}, tray *trayController) error {
	var ops op.Ops
	controls := newWindowControls(w)
	// Keep tray actions responsive even when a minimized window stops drawing.
	// Acknowledge each event before asking Gio for the next one, so a close
	// request can be aborted before its DestroyEvent is generated.
	events := make(chan event.Event)
	advance := make(chan struct{})
	first := w.Event()
	go func() {
		for e := first; ; e = w.Event() {
			events <- e
			if _, destroyed := e.(app.DestroyEvent); destroyed {
				return
			}
			<-advance
		}
	}()
	quit := ctx.Done()
	closing, reopen := false, false
	for {
		var next event.Event
		select {
		case <-quit:
			quit = nil
			if !closing {
				controls.perform(system.ActionClose)
			}
			continue
		case <-show:
			if closing {
				reopen = true
			} else {
				controls.perform(system.ActionRaise)
			}
			continue
		case next = <-events:
		}
		controls.observe(next)
		// Quit can arrive before Windows has delivered its native handle.
		if view, ok := next.(app.ViewEvent); ok && view.Valid() && ctx.Err() != nil && !closing {
			controls.perform(system.ActionClose)
		}
		switch e := next.(type) {
		case *app.ClosingEvent:
			closing = true
			if ctx.Err() == nil && !g.preview && !tray.canBackground() {
				e.Abort()
				closing = false
				g.settings = true
				g.folder.SetText(g.config.Folder)
				g.server.SetText(g.config.Server)
				g.token.SetText(g.config.Token)
				g.list.Position = layout.Position{}
				g.message = "Tray unavailable. Minimize to keep collecting, or use Quit to exit."
				w.Invalidate()
			}
		case app.DestroyEvent:
			if reopen {
				select {
				case show <- struct{}{}:
				default:
				}
			}
			return e.Err
		case app.FrameEvent:
			select {
			case snap := <-updates:
				g.snap = snap
			default:
			}
			select {
			case message := <-g.clearReply:
				g.message = message
			default:
			}
			gtx := app.NewContext(&ops, e)
			g.actions(gtx)
			paint.Fill(gtx.Ops, bg)
			layout.UniformInset(unit.Dp(18)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if g.settings {
					return g.settingsView(gtx)
				}
				return g.dashboard(gtx)
			})
			e.Frame(gtx.Ops)
		}
		advance <- struct{}{}
	}
}

// Embed only two faces; the full Go font collection adds unused font data.
func newTheme() *material.Theme {
	var faces []font.FontFace
	for _, data := range [][]byte{goregular.TTF, gobold.TTF} {
		parsed, err := opentype.ParseCollection(data)
		if err != nil {
			panic(err)
		}
		faces = append(faces, parsed...)
	}
	return &material.Theme{Shaper: text.NewShaper(text.NoSystemFonts(), text.WithCollection(faces)), Palette: material.Palette{Bg: bg, Fg: fg, ContrastBg: accent, ContrastFg: bg}, TextSize: unit.Sp(14), FingerSize: unit.Dp(36)}
}

func (g *gui) send(c command) bool {
	select {
	case g.commands <- c:
		return true
	default:
		g.message = "Please wait for the current operation, then try again."
		return false
	}
}

func (g *gui) actions(gtx layout.Context) {
	if g.quit.Clicked(gtx) {
		g.onQuit()
		return
	}
	if g.clearCache.Clicked(gtx) {
		if g.preview {
			g.message = "Preview only · no cache file was changed."
		} else if strings.TrimSpace(g.folder.Text()) != g.config.Folder {
			g.message = "Save folder changes before clearing the cache."
		} else if g.send(command{kind: "clear-cache", reply: g.clearReply}) {
			g.message = "Saving avatar IDs and clearing the analytics cache…"
		}
	}
	if g.toggle.Clicked(gtx) {
		if g.preview {
			g.snap.paused = !g.snap.paused
			if g.snap.paused {
				g.snap.status = "Collection paused"
			} else {
				g.snap.status = "Watching VRChat analytics"
			}
		} else {
			kind := "pause"
			if g.snap.paused {
				kind = "start"
			}
			if g.send(command{kind: kind}) {
				g.snap.paused = !g.snap.paused
				g.snap.status = "Updating collection…"
			}
		}
	}
	if g.change.Clicked(gtx) || g.connect.Clicked(gtx) {
		g.folder.SetText(g.config.Folder)
		g.server.SetText(g.config.Server)
		g.token.SetText(g.config.Token)
		g.message = ""
		g.settings = true
		g.list.Position = layout.Position{}
	}
	if g.cancel.Clicked(gtx) {
		g.settings = false
		g.message = ""
		g.list.Position = layout.Position{}
	}
	if g.save.Clicked(gtx) {
		origin, err := logger.ValidateServer(g.server.Text())
		if err != nil {
			g.message = err.Error()
			return
		}
		folder := strings.TrimSpace(g.folder.Text())
		if !g.preview {
			if info, err := os.Stat(folder); err != nil || !info.IsDir() {
				g.message = "Enter an existing VRChat analytics folder."
				return
			}
		}
		candidate := logger.Config{Folder: folder, Server: origin, Token: strings.TrimSpace(g.token.Text())}
		if !g.preview && !g.send(command{kind: "config", config: candidate}) {
			return
		}
		g.config = candidate
		g.settings = false
		g.message = ""
		g.list.Position = layout.Position{}
	}
}

func (g *gui) label(value string, size unit.Sp, c color.NRGBA, bold bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Label(g.th, size, value)
		l.Color = c
		if bold {
			l.Font.Weight = font.Bold
		}
		return l.Layout(gtx)
	}
}
func gap(n unit.Dp) layout.FlexChild  { return layout.Rigid(layout.Spacer{Height: n}.Layout) }
func hgap(n unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Width: n}.Layout) }
func (g *gui) button(click *widget.Clickable, title string, primary bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		b := material.Button(g.th, click, title)
		b.TextSize = unit.Sp(12)
		b.CornerRadius = unit.Dp(9)
		b.Inset = layout.Inset{Top: 8, Bottom: 8, Left: 12, Right: 12}
		if !primary {
			b.Background = line
			b.Color = fg
		}
		return b.Layout(gtx)
	}
}
func card(gtx layout.Context, body layout.Widget) layout.Dimensions {
	return widget.Border{Color: line, CornerRadius: 12, Width: 1}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			r := clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(12))
			paint.FillShape(gtx.Ops, panel, r.Op(gtx.Ops))
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}, func(gtx layout.Context) layout.Dimensions { return layout.UniformInset(unit.Dp(12)).Layout(gtx, body) })
	})
}

func (g *gui) dashboard(gtx layout.Context) layout.Dimensions {
	items := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, g.label("V R C O V E", 10, accent, true)), layout.Rigid(g.label(func() string {
					if g.preview {
						return buildinfo.Version + " · PREVIEW"
					}
					return buildinfo.Version
				}(), 10, accent, true)))
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 6, Bottom: 4}.Layout(gtx, g.label("Avatar logger", 24, fg, true))
		},
		layout.Spacer{Height: 14}.Layout,
		func(gtx layout.Context) layout.Dimensions {
			return card(gtx, func(gtx layout.Context) layout.Dimensions {
				title := "Pause"
				if g.snap.paused {
					title = "Start"
				}
				hint := "New avatar IDs are uploaded to your configured VRCove server."
				if g.config.Token == "" {
					hint = "To upload queued IDs, add an API key in Settings."
				}
				if g.snap.paused {
					hint = "Your queue is saved. Resume whenever you’re ready."
				}
				if g.preview {
					hint = "Sample activity · no data read or uploads sent."
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(g.label("●", 20, accent, false)), hgap(10), layout.Flexed(1, g.label(g.snap.status, 14, fg, true)), hgap(10), layout.Rigid(g.button(&g.toggle, title, true)))
					}),
					gap(8), layout.Rigid(g.label(hint, 11, muted, false)))
			})
		},
		layout.Spacer{Height: 14}.Layout,
		func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i, name := range []string{"Detected", "Added", "Already known", "Queued"} {
				n := []int{g.snap.detected, g.snap.added, g.snap.existing, g.snap.pending}[i]
				children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Rigid(g.label(fmt.Sprint(n), 24, fg, true)), gap(4), layout.Rigid(g.label(name, 10, muted, false)))
				}))
			}
			return layout.Flex{}.Layout(gtx, children...)
		},
		layout.Spacer{Height: 18}.Layout,
		g.label("AVATAR SOURCE", 10, muted, true),
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 6, Bottom: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(g.th, 12, displayFolder(g.config.Folder))
						l.Color = fg
						l.MaxLines = 2
						return l.Layout(gtx)
					}), hgap(12), layout.Rigid(g.button(&g.change, "Change", false)))
			})
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx, layout.Flexed(1, g.label("RECENT ACTIVITY", 10, muted, true)), layout.Rigid(g.label(fmt.Sprintf("%d not public / invalid", g.snap.rejected), 11, muted, false)))
		},
		layout.Spacer{Height: 6}.Layout,
	}
	if len(g.snap.events) == 0 {
		items = append(items, g.label("No avatars yet. Start collecting, then play VRChat.", 12, muted, false))
	}
	for _, e := range g.snap.events[:min(4, len(g.snap.events))] {
		items = append(items, func(gtx layout.Context) layout.Dimensions {
			c := accent
			if e.Result == "Existing" {
				c = muted
			}
			if e.Result == "Queued" {
				c = color.NRGBA{R: 139, G: 187, B: 246, A: 255}
			}
			if e.Result == "Rejected" {
				c = color.NRGBA{R: 230, G: 180, B: 124, A: 255}
			}
			id := e.ID
			if len(id) > 26 {
				id = id[:18] + "…" + id[len(id)-4:]
			}
			return layout.Inset{Top: 6, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, layout.Rigid(g.label(e.Time, 11, muted, false)), hgap(16), layout.Flexed(1, g.label(id, 12, fg, false)), layout.Rigid(g.label(e.Result, 11, c, false)))
			})
		})
	}
	items = append(items, layout.Spacer{Height: 14}.Layout, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, layout.Flexed(1, g.label("Only avatar IDs leave your device.", 10, muted, false)), layout.Rigid(g.button(&g.connect, "Settings", false)))
	}, layout.Spacer{Height: 8}.Layout, g.label("Close to tray · Quit from Settings or the tray menu.", 10, muted, false))
	return g.list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions { return items[i](gtx) })
}

func (g *gui) field(title, hint string, editor *widget.Editor) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(g.label(title, 12, fg, true)), gap(8), layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return widget.Border{Color: line, CornerRadius: 8, Width: 1}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(12).Layout(gtx, material.Editor(g.th, editor, hint).Layout)
				})
			}))
	}
}
func (g *gui) settingsView(gtx layout.Context) layout.Dimensions {
	items := []layout.Widget{
		g.label("VRCOVE · "+buildinfo.Version, 10, accent, true), layout.Spacer{Height: 8}.Layout,
		g.label("Logger settings", 24, fg, true), layout.Spacer{Height: 16}.Layout,
		g.field("VRChat analytics folder", "Paste the folder containing amplitude.cache", &g.folder), layout.Spacer{Height: 14}.Layout,
		g.button(&g.clearCache, "Clear Amplitude analytics cache", false), layout.Spacer{Height: 6}.Layout,
		g.label("Saves avatar IDs first, then empties amplitude.cache. Your queue stays saved.", 11, muted, false), layout.Spacer{Height: 14}.Layout,
		g.field("Server", "https://vrcove.st", &g.server), layout.Spacer{Height: 14}.Layout,
		g.field("API key", "Generate one on your VRCove account page", &g.token), layout.Spacer{Height: 10}.Layout,
		g.label("Your API key is tied to your VRChat session’s lifetime and expires when that session expires or ends. Leave blank to collect locally.", 11, muted, false), layout.Spacer{Height: 12}.Layout,
		g.label(g.message, 11, color.NRGBA{R: 230, G: 180, B: 124, A: 255}, false), layout.Spacer{Height: 12}.Layout,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx, layout.Rigid(g.button(&g.quit, "Quit", false)), hgap(10), layout.Rigid(g.button(&g.cancel, "Cancel", false)), hgap(10), layout.Rigid(g.button(&g.save, "Save settings", true)))
		},
		layout.Spacer{Height: 16}.Layout,
		g.label(buildinfo.Summary(), 10, muted, false),
	}
	return g.list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions { return items[i](gtx) })
}
func previewSnapshot() snapshot {
	return snapshot{status: "Watching VRChat analytics", detected: 128, added: 86, existing: 31, rejected: 8, pending: 3, events: []logger.Event{
		{Time: "14:32", ID: "avtr_8a4207d1-1284-4fe6-b087-804c8f2ac039", Result: "Added"},
		{Time: "14:32", ID: "avtr_29f6b0ac-1845-4ad5-a894-1832ec10a185", Result: "Existing"},
		{Time: "14:31", ID: "avtr_7462edc8-7845-48a3-9413-71c8100ad407", Result: "Added"},
		{Time: "14:31", ID: "avtr_b713c260-8874-4c2d-92b9-12c183fbe592", Result: "Queued"},
	}}
}
