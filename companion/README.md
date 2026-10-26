# VRCove Desktop

VRCove for Windows and Linux. It looks and works like
[vrcove.st](https://vrcove.st), but talks to VRChat directly from your
computer: your VRChat password and session never pass through VRCove's server.
A VRCove account is optional; avatar search, reviews and collections still come
from vrcove.st.

It replaces the old VRCove Logger, which is retired. The avatar logger is built
in, and the app imports the old logger's saved settings and queue.

Setup and details: https://vrcove.st/companion

## Use

1. Download the latest release, extract it and run `vrcove-desktop`.
2. Sign in to VRChat in the app. Your password only goes to VRChat and isn't
   saved; the session is kept in a file encrypted with a key in Windows
   Credential Manager or your Linux keyring.
3. Optional, to add avatars to VRCove's search: link VRChat on your VRCove
   account page on the website, generate an API key, and paste it into
   Avatar logger in the app.

Closing the window keeps it running in the tray. On Linux that needs a tray
host (KDE Plasma, or a GNOME tray extension). Updates are offered in Local
Settings and installed only after their checksums match the release.

Also in the app: instance history and people you met (read from VRChat's logs),
a friend activity feed, screenshots with the world and people, desktop
notifications with the window closed, and Tools (username checker, OSC chatbox
and Unity package extractor). All of it is stored only on your computer.

## Build

Go 1.26+ and Node.js. Linux also needs gcc, pkg-config and the GTK 4 and
WebKitGTK 6 dev packages.

```sh
bash build.sh linux     # dist/vrcove-desktop
bash build.sh windows   # dist/vrcove-desktop.exe, cross-built, no cgo
go test -race ./...
```

The app shows the website's own frontend (`npm run build:desktop`) and runs the
website's VRChat API code (`internal/server` in local mode) inside the app.
`VRCOVE_SITE=http://localhost:8080` points a development build at a local
server, and `VRCOVE_START=/history` opens another page first.

Releases are built by GitHub Actions when a `v*` tag matching `VERSION` is
pushed.
