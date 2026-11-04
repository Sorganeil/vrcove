# VRCove Desktop

VRCove Desktop runs on Windows and Linux with the same interface as
[vrcove.st](https://vrcove.st). The app connects to VRChat directly from your
computer. Your VRChat password and session never pass through VRCove's server.
You can use it without a VRCove account. Avatar search, reviews and collections
still come from vrcove.st.

The avatar logger is built in. If you used the retired VRCove Logger, the app
imports its saved settings and queue.

Setup and details: https://vrcove.st/companion

## Use

1. Download the latest release, extract it and run `vrcove-desktop`.
2. Sign in to VRChat in the app. Your password goes to VRChat and isn't saved.
   The app saves your session in an encrypted file, with its key in Windows
   Credential Manager or your Linux keyring.
3. To contribute avatars to VRCove's search, open Avatar logger in the app
   and press Start. This step is optional and needs no VRCove account. The
   app checks each avatar with your VRChat session, then sends only the
   avatar IDs. VRCove checks them again with its own VRChat account.

The app keeps running in the tray when you close its window. On Linux, you
need a tray host such as KDE Plasma or a GNOME tray extension. You can install
updates from Local Settings. The app checks each download's checksum against
the release before installing it.

Instance history shows where you went and who you met. The game log reads
joins, leaves, avatar changes and videos from VRChat's logs. There's also a
friend activity feed and a screenshot browser that shows the world and people
from each photo. Desktop notifications work with the window closed.

Tools includes a username checker, OSC chatbox and Unity package extractor.
The app stores this local data only on your computer.

## Build

You'll need Go 1.26+, Node.js and the Wails CLI version listed in `go.mod`
(`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.34`). On Linux,
install gcc, pkg-config and the GTK 4 and WebKitGTK 6 development packages too.

Release builds use `wails3 build` (see `cmd/vrcove-desktop/Taskfile.yml`). The
`production` tag turns off developer tools. Builds strip symbols and trim
paths, and Windows builds include the icon and version details.

```sh
bash build.sh linux     # dist/vrcove-desktop
bash build.sh windows   # dist/vrcove-desktop.exe, cross-built, no cgo
go test -race ./...
```

The app uses the website's frontend (`npm run build:desktop`) and runs its
VRChat API code (`internal/server` in local mode) on your computer.
`VRCOVE_SITE=http://localhost:8080` points a development build at a local
server, and `VRCOVE_START=/history` opens another page first.

GitHub Actions builds a release when you push a `v*` tag matching `VERSION`.
