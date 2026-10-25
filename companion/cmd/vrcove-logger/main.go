package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
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
	"vrcove/companion/internal/collector"
	"vrcove/companion/internal/logger"
	"vrcove/companion/internal/vrchat"
)

var (
	bg     = color.NRGBA{R: 20, G: 24, B: 31, A: 255}
	panel  = color.NRGBA{R: 29, G: 35, B: 45, A: 255}
	fg     = color.NRGBA{R: 237, G: 242, B: 249, A: 255}
	muted  = color.NRGBA{R: 151, G: 163, B: 181, A: 255}
	accent = color.NRGBA{R: 128, G: 220, B: 190, A: 255}
	line   = color.NRGBA{R: 49, G: 60, B: 75, A: 255}
	warn   = color.NRGBA{R: 230, G: 180, B: 124, A: 255}
)

type gui struct {
	th                            *material.Theme
	config                        logger.Config
	snap                          collector.Snapshot
	preview, settings, login      bool
	message                       string
	commands                      chan collector.Command
	toggle, connect, save, cancel widget.Clickable
	quit                          widget.Clickable
	clearCache                    widget.Clickable
	autoStart                     widget.Clickable
	auto                          bool
	clearReply                    chan string
	onQuit                        func()
	folder, server, token         widget.Editor
	list                          widget.List

	// The desktop's own VRChat login, used for client-side avatar checks.
	vrc                                   *vrchat.Client
	username, password, code              widget.Editor
	method                                widget.Enum
	methods                               [3]widget.Clickable
	manage, signIn, verify, signOut, back widget.Clickable
	loginMsg                              string
	loginReply                            chan string
	invalidate                            func()
}

func main() {
	preview := flag.Bool("preview", false, "Show sample activity without reading analytics, saving state or submitting")
	path := flag.String("state", "", "Override the state path")
	version := flag.Bool("version", false, "Print version and build information, then exit")
	screen := flag.String("screen", "", "With --preview, open on settings, login, code or signed-in")
	flag.Parse()
	if *version {
		fmt.Println(buildinfo.Summary())
		return
	}
	go func() {
		if err := run(*preview, *path, *screen); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(preview bool, path, screen string) error {
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
	var slot windowSlot
	g := &gui{th: newTheme(), config: s.Config, commands: make(chan collector.Command, 4), clearReply: make(chan string, 1), preview: preview, loginReply: make(chan string, 1), invalidate: slot.invalidate}
	if preview {
		g.vrc = previewVRChat(slot.invalidate)
		g.settings, g.login = screen == "settings" || screen == "login", screen == "login"
	} else {
		g.vrc = vrchat.New(path, slot.invalidate)
	}
	// Secure storage can block on an unlock prompt, so read it off the GUI thread.
	go func() {
		g.vrc.Load()
		// Preview screens for the later login steps, using the local fake.
		if preview && (screen == "code" || screen == "signed-in") {
			g.settings, g.login = true, true
			_ = g.vrc.Login("preview", "preview")
			if screen == "signed-in" {
				_ = g.vrc.Verify(vrchat.Authenticator, "123456")
			}
		}
	}()
	s.Verifier = verifier{g.vrc}
	g.username.SingleLine, g.password.SingleLine, g.code.SingleLine = true, true, true
	g.username.Submit, g.password.Submit, g.code.Submit = true, true, true
	g.password.Mask = '•'
	g.folder.SetText(g.config.Folder)
	g.server.SetText(g.config.Server)
	g.token.SetText(g.config.Token)
	g.auto = g.config.AutoStart
	g.list.Axis = layout.Vertical
	g.folder.SingleLine = true
	g.server.SingleLine = true
	g.token.SingleLine = true
	g.token.Mask = '•'
	g.snap = collector.Snapshot{Status: "Collection paused", Detected: len(s.Seen), Added: s.Added, Existing: s.Existing, Rejected: s.Rejected, Pending: len(s.Pending), Events: append([]logger.Event(nil), s.Events...), Paused: !s.Config.AutoStart}
	if g.snap.Paused {
		g.snap.Status = "Paused"
	} else {
		g.snap.Status = "Watching VRChat analytics"
	}
	if preview {
		g.snap = previewSnapshot()
	}
	updates := make(chan collector.Snapshot, 1)
	// Stop cleanly on Ctrl+C or a service manager's TERM, saving the queue.
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signals)
	g.onQuit = cancel
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
		go func() { defer close(done); collector.Run(ctx, path, s, g.commands, updates, slot.invalidate) }()
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
		w.Option(app.Title("VRCove Logger "+buildinfo.Version), app.Size(460, 500), app.MinSize(400, 400))
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

func (g *gui) windowLoop(ctx context.Context, w *app.Window, updates <-chan collector.Snapshot, show chan struct{}, tray *trayController) error {
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
				g.settings, g.login = true, false
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
			select {
			case message := <-g.loginReply:
				g.loginMsg = message
			default:
			}
			gtx := app.NewContext(&ops, e)
			g.actions(gtx)
			paint.Fill(gtx.Ops, bg)
			layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if g.login {
					return g.loginView(gtx)
				}
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

func (g *gui) send(c collector.Command) bool {
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
	g.loginActions(gtx)
	if g.clearCache.Clicked(gtx) {
		if g.preview {
			g.message = "Preview only · no cache file was changed."
		} else if strings.TrimSpace(g.folder.Text()) != g.config.Folder {
			g.message = "Save folder changes before clearing the cache."
		} else if g.send(collector.Command{Kind: "clear-cache", Reply: g.clearReply}) {
			g.message = "Saving avatar IDs and clearing the analytics cache…"
		}
	}
	if g.toggle.Clicked(gtx) {
		if g.preview {
			g.snap.Paused = !g.snap.Paused
			if g.snap.Paused {
				g.snap.Status = "Paused"
			} else {
				g.snap.Status = "Watching VRChat analytics"
			}
		} else {
			kind := "pause"
			if g.snap.Paused {
				kind = "start"
			}
			if g.send(collector.Command{Kind: kind}) {
				g.snap.Paused = !g.snap.Paused
				g.snap.Status = "Updating collection…"
			}
		}
	}
	if g.autoStart.Clicked(gtx) {
		g.auto = !g.auto
	}
	if g.connect.Clicked(gtx) {
		g.auto = g.config.AutoStart
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
		candidate := logger.Config{Folder: folder, Server: origin, Token: strings.TrimSpace(g.token.Text()), AutoStart: g.auto}
		if !g.preview && !g.send(collector.Command{Kind: "config", Config: candidate}) {
			return
		}
		g.config = candidate
		g.settings = false
		g.message = ""
		g.list.Position = layout.Position{}
	}
}

func (g *gui) label(value string, size unit.Sp, c color.NRGBA, bold bool) layout.Widget {
	return g.text(value, size, c, bold, 0)
}

// text is a label limited to maxLines (0 for no limit).
func (g *gui) text(value string, size unit.Sp, c color.NRGBA, bold bool, maxLines int) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Label(g.th, size, value)
		l.Color, l.MaxLines = c, maxLines
		if bold {
			l.Font.Weight = font.Bold
		}
		return l.Layout(gtx)
	}
}
func gap(n unit.Dp) layout.FlexChild  { return layout.Rigid(layout.Spacer{Height: n}.Layout) }
func hgap(n unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Width: n}.Layout) }
func space(n unit.Dp) layout.Widget   { return layout.Spacer{Height: n}.Layout }
func (g *gui) button(click *widget.Clickable, title string, primary bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		b := material.Button(g.th, click, title)
		b.TextSize = unit.Sp(13)
		b.CornerRadius = unit.Dp(7)
		b.Inset = layout.Inset{Top: 7, Bottom: 7, Left: 12, Right: 12}
		if !primary {
			b.Background = line
			b.Color = fg
		}
		return b.Layout(gtx)
	}
}
func divider(gtx layout.Context) layout.Dimensions {
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
	paint.FillShape(gtx.Ops, line, clip.Rect{Max: size}.Op())
	return layout.Dimensions{Size: size}
}

// checkbox draws its own box; the material theme here carries no icons.
func (g *gui) checkbox(click *widget.Clickable, on bool, title string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					n := gtx.Dp(18)
					size := image.Pt(n, n)
					box := clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(4))
					if !on {
						paint.FillShape(gtx.Ops, muted, clip.Stroke{Path: box.Path(gtx.Ops), Width: float32(gtx.Dp(1.5))}.Op())
						return layout.Dimensions{Size: size}
					}
					paint.FillShape(gtx.Ops, accent, box.Op(gtx.Ops))
					f := float32(n)
					var check clip.Path
					check.Begin(gtx.Ops)
					check.MoveTo(f32.Pt(f*0.24, f*0.52))
					check.LineTo(f32.Pt(f*0.42, f*0.70))
					check.LineTo(f32.Pt(f*0.77, f*0.31))
					paint.FillShape(gtx.Ops, bg, clip.Stroke{Path: check.End(), Width: float32(gtx.Dp(2))}.Op())
					return layout.Dimensions{Size: size}
				}),
				hgap(8), layout.Flexed(1, g.label(title, 13, fg, false)))
		})
	}
}

// resultLabel matches the counters: "Existing" avatars are already in VRCove.
func resultLabel(result string) string {
	if result == "Existing" {
		return "Known"
	}
	return result
}

// attention marks statuses that need the user to do something.
func attention(status string) bool {
	for _, s := range []string{"API key", "sign in", "rate limit", "unavailable", "expired", "not cleared", "Storage", "invalid", "Invalid"} {
		if strings.Contains(status, s) {
			return true
		}
	}
	return false
}

func (g *gui) dashboard(gtx layout.Context) layout.Dimensions {
	title, statusColor := "Pause", accent
	if g.snap.Paused {
		title, statusColor = "Start", muted
	} else if attention(g.snap.Status) {
		statusColor = warn
	}
	version := buildinfo.Version
	if g.preview {
		version += " · preview"
	}
	items := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(g.label("VRCove Logger", 17, fg, true)), hgap(8),
				layout.Flexed(1, g.label(version, 12, muted, false)),
				layout.Rigid(g.button(&g.connect, "Settings", false)), hgap(8),
				layout.Rigid(g.button(&g.toggle, title, true)))
		},
		space(4),
		g.text(g.snap.Status, 13, statusColor, false, 2),
		space(12),
		func(gtx layout.Context) layout.Dimensions {
			var cols []layout.FlexChild
			for i, name := range []string{"Found", "Added", "Known", "Queued", "Rejected"} {
				n := []int{g.snap.Detected, g.snap.Added, g.snap.Existing, g.snap.Pending, g.snap.Rejected}[i]
				cols = append(cols, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Rigid(g.label(fmt.Sprint(n), 19, fg, true)), layout.Rigid(g.label(name, 12, muted, false)))
				}))
			}
			return layout.Flex{}.Layout(gtx, cols...)
		},
		space(12), divider, space(8),
		g.label("Recent", 12, muted, true), space(2),
	}
	if len(g.snap.Events) == 0 {
		items = append(items, space(4), g.label("Nothing yet. Start collecting, then play VRChat.", 13, muted, false))
	}
	for _, e := range g.snap.Events[:min(6, len(g.snap.Events))] {
		items = append(items, func(gtx layout.Context) layout.Dimensions {
			c := map[string]color.NRGBA{"Added": accent, "Existing": muted, "Queued": color.NRGBA{R: 139, G: 187, B: 246, A: 255}, "Rejected": warn}[e.Result]
			if c == (color.NRGBA{}) {
				c = muted
			}
			id := e.ID
			if len(id) > 26 {
				id = id[:18] + "…" + id[len(id)-4:]
			}
			return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, layout.Rigid(g.label(e.Time, 12, muted, false)), hgap(12), layout.Flexed(1, g.label(id, 13, fg, false)), layout.Rigid(g.label(resultLabel(e.Result), 12, c, false)))
			})
		})
	}
	items = append(items, space(10), divider, space(10),
		g.label("Analytics folder", 12, muted, true), space(4),
		func(gtx layout.Context) layout.Dimensions {
			// Paths rarely have spaces, so let them wrap anywhere instead of cutting off.
			l := material.Label(g.th, 13, displayFolder(g.config.Folder))
			l.Color, l.WrapPolicy = fg, text.WrapGraphemes
			return l.Layout(gtx)
		})
	return g.list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions { return items[i](gtx) })
}

func (g *gui) field(title, hint string, editor *widget.Editor) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(g.label(title, 13, fg, false)), gap(4), layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return widget.Border{Color: line, CornerRadius: 6, Width: 1}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					e := material.Editor(g.th, editor, hint)
					e.TextSize, e.HintColor = 14, muted
					return layout.Inset{Top: 8, Bottom: 8, Left: 11, Right: 11}.Layout(gtx, e.Layout)
				})
			}))
	}
}

func (g *gui) settingsView(gtx layout.Context) layout.Dimensions {
	items := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, g.label("Settings", 17, fg, true)),
				layout.Rigid(g.button(&g.cancel, "Cancel", false)), hgap(8), layout.Rigid(g.button(&g.save, "Save", true)))
		},
		space(10),
		g.field("VRChat analytics folder", "Folder containing amplitude.cache", &g.folder), space(6),
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(g.button(&g.clearCache, "Clear analytics cache", false)), hgap(10),
				layout.Flexed(1, g.label("Saves avatar IDs first.", 12, muted, false)))
		},
		space(10),
		g.checkbox(&g.autoStart, g.auto, "Start collecting when the app opens"),
		space(12), divider, space(10),
		g.label("Uploading", 13, fg, true), space(2),
		g.label("Needs both logins. Your VRChat login checks avatars on this PC; VRCove checks them again with the VRChat session your API key belongs to. The key expires when that session ends.", 12, muted, false),
		space(8),
		g.loginRow, space(8),
		g.field("VRCove API key", "From your VRCove account page", &g.token), space(8),
		g.field("Server", "https://vrcove.st", &g.server), space(10),
	}
	if g.message != "" {
		items = append(items, g.label(g.message, 12, warn, false), space(10))
	}
	items = append(items, divider, space(10), func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, g.text(buildinfo.Summary(), 11, muted, false, 2)), hgap(10), layout.Rigid(g.button(&g.quit, "Quit", false)))
	})
	return g.list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions { return items[i](gtx) })
}

func previewSnapshot() collector.Snapshot {
	return collector.Snapshot{Status: "Watching VRChat analytics", Detected: 128, Added: 86, Existing: 31, Rejected: 8, Pending: 3, Events: []logger.Event{
		{Time: "14:32", ID: "avtr_8a4207d1-1284-4fe6-b087-804c8f2ac039", Result: "Added"},
		{Time: "14:32", ID: "avtr_29f6b0ac-1845-4ad5-a894-1832ec10a185", Result: "Existing"},
		{Time: "14:31", ID: "avtr_7462edc8-7845-48a3-9413-71c8100ad407", Result: "Added"},
		{Time: "14:31", ID: "avtr_b713c260-8874-4c2d-92b9-12c183fbe592", Result: "Queued"},
	}}
}
