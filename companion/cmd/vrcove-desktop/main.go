// Command vrcove-desktop is the experimental VRCove desktop app. It shows the
// website's own frontend, built into the app, and runs the website's VRChat
// API code on this device, so VRChat requests go straight to VRChat instead of
// through VRCove's server. VRCove's own features, such as the avatar catalog,
// reviews and collections, still come from the website.
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend
var frontendFiles embed.FS

// defaultSite is the VRCove website; VRCOVE_SITE points a development build
// at a local server instead.
const defaultSite = "https://vrcove.st"

func main() {
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
	if handler.logger, err = startLogger(ctx, handler, filepath.Join(config, "VRCove Desktop"), site.Scheme+"://"+site.Host); err != nil {
		log.Print("avatar logger unavailable: ", err)
	}

	var window *application.WebviewWindow
	app := application.New(application.Options{
		Name:        "VRCove",
		Description: "VRChat avatar search and a private VRChat client",
		Assets:      application.AssetOptions{Handler: handler, DisableLogging: true},
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
		URL:              "/",
	})
	// Closing the window keeps VRCove running in the tray.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	tray := app.SystemTray.New()
	if icon, err := fs.ReadFile(assets, "icon-192.png"); err == nil {
		tray.SetIcon(icon)
	}
	tray.SetTooltip("VRCove")
	menu := app.NewMenu()
	menu.Add("Open VRCove").OnClick(func(*application.Context) {
		window.Show()
		window.Focus()
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
