# VRCove Logger

Desktop app for Windows and Linux that picks up avatar IDs from VRChat's
`amplitude.cache` while you play and adds the public ones to
[VRCove](https://vrcove.st) avatar search.

Setup and details: https://vrcove.st/companion

## Use

1. Download the latest release and run `vrcove-logger`.
2. Generate an API key on your VRCove account page and paste it into Settings.
3. Sign in to VRChat in Settings. Your password only goes to VRChat and isn't
   saved; the session is kept in Windows Credential Manager or your Linux keyring.
4. Press Start and play.

Closing the window keeps it running in the tray. On Linux that needs a tray
host (KDE Plasma, or a GNOME tray extension).

## Build

Go 1.26+. Linux also needs gcc, pkg-config and the Wayland/X11, xkbcommon and
EGL dev packages.

```sh
bash build.sh linux     # dist/vrcove-logger
bash build.sh windows   # dist/vrcove-logger.exe, cross-built, no cgo
go test -race -tags novulkan ./...
```

`--preview` runs with sample data and a fake VRChat login, with no file or
network access. Add `--screen settings|login|code|signed-in` to open a screen.

Releases are built by GitHub Actions when a `v*` tag is pushed.
