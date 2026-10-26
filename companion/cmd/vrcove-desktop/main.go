// Command vrcove-desktop is the experimental VRCove desktop app. It shows the
// website's own frontend, built into the app, and runs the website's VRChat
// API code on this device, so VRChat requests go straight to VRChat instead of
// through VRCove's server. VRCove's own features, such as the avatar catalog,
// reviews and collections, still come from the website.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"vrcove/companion/internal/buildinfo"
	"vrcove/companion/internal/history"
	"vrcove/companion/internal/updater"
)

//go:embed all:frontend
var frontendFiles embed.FS

// defaultSite is the VRCove website; VRCOVE_SITE points a development build
// at a local server instead.
const defaultSite = "https://vrcove.st"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(buildinfo.DesktopSummary())
		return
	}
	updater.CleanupOldExecutable()
	site, err := url.Parse(defaultSite)
	if v := os.Getenv("VRCOVE_SITE"); v != "" {
		site, err = url.Parse(v)
	}
	if err != nil || (site.Scheme != "https" && site.Scheme != "http") || site.Host == "" {
		log.Fatal("VRCOVE_SITE must be an http(s) origin")
	}
	config, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	st, err := openStore(filepath.Join(config, "VRCove Desktop"))
	if err != nil {
		log.Fatal(err)
	}
	if !st.persistent {
		log.Print("system keyring unavailable: sign-ins last until the app closes")
	}
	assets, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		log.Fatal("frontend not built: run `npm run build:desktop` first")
	}
	handler, err := newDesktop(st, site, assets)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	handler.toolContext = ctx
	if handler.logger, err = startLogger(ctx, handler, filepath.Join(config, "VRCove Desktop"), site.Scheme+"://"+site.Host); err != nil {
		log.Print("avatar logger unavailable: ", err)
	}
	if handler.history, err = history.Open(filepath.Join(config, "VRCove Desktop", "history.sqlite")); err != nil {
		log.Print("history unavailable: ", err)
	} else {
		defer handler.history.Close()
		id, _ := handler.user()
		if err := handler.history.ClaimLegacyFeed(ctx, id); err != nil {
			log.Print("could not assign legacy friend feed: ", err)
		}
	}
	go handler.readLogs(ctx)
	go handler.runLive(ctx)
	go handler.runChatbox(ctx)
	if buildinfo.Version != "dev" {
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
				_, _ = handler.updater.Check(ctx)
			}
		}()
	}
	alerts := notifications.New()

	// VRCOVE_START opens another page first, for development and screenshots.
	start := "/"
	if v := os.Getenv("VRCOVE_START"); strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//") {
		start = v
	}
	// The app, window and tray use VRCove's icon instead of Wails' default.
	icon, _ := fs.ReadFile(assets, "icon-192.png")
	var window *application.WebviewWindow
	app := application.New(application.Options{
		Name:        "VRCove",
		Icon:        icon,
		Description: "VRChat avatar search and a private VRChat client",
		Assets:      application.AssetOptions{Handler: handler, DisableLogging: true},
		Services:    []application.Service{application.NewService(alerts)},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "st.vrcove.desktop",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window != nil {
					window.Show()
					window.Focus()
				}
			},
		},
		Linux: application.LinuxOptions{ProgramName: "vrcove"},
	})
	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "VRCove",
		Width:            1280,
		Height:           820,
		MinWidth:         380,
		MinHeight:        560,
		BackgroundColour: application.NewRGB(14, 19, 26),
		URL:              start,
	})
	handler.choosePackage = func() (string, error) {
		return app.Dialog.OpenFile().AttachToWindow(window).SetTitle("Choose a Unity package").AddFilter("Unity packages", "*.unitypackage").PromptForSingleSelection()
	}
	handler.chooseFolder = func() (string, error) {
		return app.Dialog.OpenFile().AttachToWindow(window).SetTitle("Choose where to extract").CanChooseFiles(false).CanChooseDirectories(true).CanCreateDirectories(true).PromptForSingleSelection()
	}
	handler.openURL = app.Browser.OpenURL
	handler.notify = func(id, title, body string) {
		alerts.SendNotification(notifications.NotificationOptions{ID: id, Title: title, Body: body})
	}
	// Clicking an alert opens the Notifications page.
	alerts.OnNotificationResponse(func(notifications.NotificationResult) {
		window.Show()
		window.Focus()
		window.ExecJS("history.pushState(null,'','/notifications');dispatchEvent(new PopStateEvent('popstate'))")
	})
	// Closing the window keeps VRCove running in the tray.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	tray := app.SystemTray.New()
	if icon != nil {
		tray.SetIcon(icon)
	}
	// Linux trays show the label as the name; without it Wails says "Wails".
	tray.SetLabel("VRCove")
	tray.SetTooltip("VRCove")
	menu := app.NewMenu()
	menu.Add("Open VRCove").OnClick(func(*application.Context) {
		window.Show()
		window.Focus()
	})
	menu.Add("Check for updates").OnClick(func(*application.Context) {
		go handler.updater.Check(ctx)
		window.Show()
		window.Focus()
		window.ExecJS("history.pushState(null,'','/settings');dispatchEvent(new PopStateEvent('popstate'))")
	})
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	tray.OnClick(func() {
		window.Show()
		window.Focus()
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
