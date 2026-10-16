# VRCove Logger

A small native companion that extracts avatar IDs from the local VRChat
`amplitude.cache` analytics file.
Go + Gio on Linux and Windows. Gio draws the same interface on both platforms;
it does not use GTK or WinUI controls. No browser, frontend bundle, avatar
downloads or raw-log uploads. This is separate from the PWA.

The window has collection controls, analytics-folder selection, persistent counters,
recent activity and connection settings. Closing the window keeps the logger
running in the system tray, preserving the current collection state. Click the
tray icon or right-click it and choose **Show** to restore the window. **Quit** in the tray
menu or Settings stops collection, saves the queue and exits. Collection starts
paused. `--preview` shows sample
activity without reading logs, saving state or making network requests; its
VRChat login is a local fake that accepts anything. `--preview --screen settings`
(or `login`, `code`, `signed-in`) opens a specific screen for screenshots.
The default window is 500×530 logical pixels. Both screens scroll when resized
smaller. The version is shown in the header and title; Settings shows the commit,
build timestamp and platform. `--version` prints the same details without opening
a window.

Linux tray integration uses StatusNotifierItem over D-Bus, with no GTK runtime.
The desktop must provide a tray host (for example KDE Plasma, or an appropriate
GNOME tray extension). If none is available, the app keeps its window open and
explains how to minimize or quit instead. Preview mode does not register a tray
icon and still exits when closed.

## Log compatibility

[VRC-LOG](https://github.com/ShayBox/VRC-LOG) is the reference for VRChat's
analytics source and Windows/Proton paths. Output logs are no longer a reliable
avatar source; the logger reads `amplitude.cache` snapshots, including records
without a final newline.
It checks the Windows temporary folder and the selected Proton prefix's
`AppData/Local/Temp/VRChat/VRChat` directory. Rewrites and truncations are followed;
duplicate IDs are queued once. Tests use synthetic fixtures, not a live VRChat
session. Analytics may not expose every avatar in every VRChat version.

The analytics file contains other telemetry. Collection opens it read-only;
its contents are never uploaded. **Clear Amplitude analytics cache** in Settings
first saves discovered IDs, then resets the selected cache to an empty JSON
array. Clearing is refused if saving the queue fails. This action is manual;
normal collection never clears the file. Saved state contains avatar IDs, counters and
file fingerprints; surrounding telemetry is discarded. Analytics snapshots
larger than 32 MiB report a scan-limit error. Keep collection running during
VRChat: analytics may be cleared quickly, and changes can still be missed if
file notifications are unavailable or the file is cleared before it can be read.

## Build

Go 1.26+ and the pinned module dependencies are required. Linux also needs a C
toolchain and X11/Wayland, xkbcommon and EGL development packages.

Linux (Debian/Ubuntu prerequisites):

```sh
sudo apt install gcc pkg-config libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev libegl1-mesa-dev libgles2-mesa-dev libffi-dev libxcursor-dev libxfixes-dev
```

Build and run:

```sh
bash build.sh linux
./dist/vrcove-logger
./dist/vrcove-logger --preview
```

The Linux executable uses the system's EGL/OpenGL graphics stack and supports
X11 and Wayland. It does not require GTK or WebKitGTK. Vulkan is disabled to
avoid carrying a second rendering backend.

Windows cross-build from Linux (no Windows C/C++ cross-compiler needed):

```sh
bash build.sh windows
```

Windows builds use Direct3D and `CGO_ENABLED=0`. They need no WinUI runtime,
GTK runtime or browser runtime. OpenGL is disabled on Windows to avoid carrying
a second rendering backend. For a Windows-native build:

```powershell
$env:CGO_ENABLED='0'
go build -trimpath -buildvcs=false -tags 'noopengl netgo osusergo' -ldflags='-s -w -buildid= -H=windowsgui' -o dist/vrcove-logger.exe ./cmd/vrcove-logger
```

`build.sh` accepts `VERSION`, `COMMIT` and `BUILD_DATE` environment variables and
embeds them using linker flags. Defaults are `dev`, the current commit, and the
current UTC timestamp. A direct `go build` uses `dev` / `unknown` metadata.

Release builds strip Go/native debugging information, remove build paths and
build IDs, size-optimize the C/C++ code and discard unused native sections.
Only regular and bold font faces are embedded; unused font families are omitted.
The app redraws on events and state changes rather than running an animation
loop. No executable packer is used. Compare total deployment size including
system-library requirements, not just executable bytes.

## Tagged releases

Build/release setup reference: [Proton-Inject](https://github.com/Jilwer/Proton-Inject).

Pushing a reviewed `vMAJOR.MINOR.PATCH` tag (optionally with a prerelease suffix,
such as `v1.2.3-rc.1`) runs `.github/workflows/companion-release.yml`. It builds
Linux amd64 and Windows amd64 from that tag on Ubuntu 22.04, runs the logger
tests and code checks, and publishes both packages with `SHA256SUMS` to a GitHub
Release. Prerelease tags produce prereleases. Each archive contains only the
binary, README, license and build metadata; settings, tokens and raw logs are
excluded. Linux packages require the system EGL/OpenGL and windowing libraries.

The local equivalent builds packages without publishing anything:

```sh
bash companion/release.sh v1.2.3
```

To publish after reviewing and committing the source, create and push the tag;
the workflow handles publishing. Alternatively, an authenticated operator can
run this against an already pushed tag:

```sh
bash companion/release.sh --publish v1.2.3
```

The script never creates or pushes tags. Publishing requires a clean working
tree, a local tag at HEAD and the matching commit on GitHub. Uploads happen in a
draft first; a missing asset leaves the draft unpublished. Published assets are
not overwritten. Failed draft uploads can be retried. Release timestamps derive
from the tagged commit, so rebuilding the same source produces stable archives
with the same toolchain. GitHub Actions uses its repository token; local
publishing uses `gh` authentication and the repository's default remote (or an
explicit `GH_REPO`). Outputs go in `companion/dist/releases/<tag>/`.

## Connection

Choose the folder containing VRChat's `amplitude.cache`. Windows temporary and
Linux Steam/Proton locations are detected, including additional drives listed in
Steam's `libraryfolders.vdf`, Flatpak Steam and custom Proton user folders.
When multiple caches exist, the newest is selected. Custom prefix environment
variables are also checked. Paste a custom analytics folder in Settings if
needed. The scanner reacts to native file-change notifications, with a
three-second polling fallback. Uploads run separately so network waits do not
block collection during world switches. File fingerprints and discovered IDs
survive restarts. An ID must
appear in analytics to be discovered; this does not enumerate every avatar in an
instance. No game injection or memory access is used.
`--enable-sdk-log-levels` is not required for analytics collection; it only
enables additional SDK logging.

To enable submissions, the deployment sets `LOGGER_ENABLED=true` with
`ACCOUNT_KEY`, `SESSION_KEY`, and `VRC_CONTACT` configured. There is no shared
logger credential or operator VRChat login.

### Two separate logins

Uploading needs both, and they do different jobs:

1. **VRChat login on this PC** (Settings → VRChat login). The companion signs in
   to VRChat directly with your username and password, then an authenticator,
   recovery or email code when VRChat asks for one. The password goes only to
   VRChat; it is never saved and never sent to VRCove. The resulting session
   cookies are stored in Windows Credential Manager or the Linux Secret Service
   (your desktop keyring). If secure storage is locked or unavailable, the app
   says so and keeps nothing; there is no plaintext fallback. **Sign out**
   removes the saved session and ends it at VRChat. Cookies VRChat rotates are
   saved again; an expired session signs the desktop out and keeps the queue.
   This login performs the client checks below.
2. **VRCove API key.** It identifies an existing encrypted VRChat session on
   the VRCove server, which the server uses to verify every avatar again on its
   own. The key expires when that linked session ends or expires.

Without either login, the companion keeps collecting locally and uploads
nothing.

On VRCove, sign in, connect your linked VRChat account, and open **Account →
Companion API keys → Generate API key**. Copy the key into the companion's
**API key** field. The key is shown only once and stored as a hash on the server.
Generating another key for the same session replaces the previous key; you can
revoke keys from the account page.

Generating an API key enables encrypted credential storage on the existing
server-side VRChat session record. This allows the server to fetch avatar metadata
while your browser is closed. The desktop receives only the API key, never the
VRChat session or credentials. API keys are tied to the session's lifetime:
expiry, logout, ending the session, or unlinking the account invalidates them.
Reconnect VRChat and generate a new API key after a session ends.

### Upload flow

For each batch of up to five queued IDs:

1. **Catalog check.** `/api/logger/known` reports which IDs VRCove already has.
   Those are marked **Existing** and skipped without asking VRChat.
2. **Client check.** Each remaining ID is looked up with this PC's VRChat
   session. Private, invalid, mismatched or inaccessible avatars are marked
   **Rejected** locally and never uploaded.
3. **Ingestion.** Only IDs that passed are posted to `/api/logger/avatars`. The
   server does not trust the client: it authenticates the API key and its linked
   server-side session, validates each ID, fetches the public metadata from
   VRChat itself, checks for duplicates, and inserts new metadata and search
   records in one transaction. The desktop's VRChat session is never sent to
   VRCove.

Ingestion allows 40 requests of up to five IDs per ten minutes per VRChat
account. Catalog checks and local rejections don't use this quota; an ingestion
request counts even if the server rejects its IDs. A backlog starts with a burst
of ten requests, then the app sends one every 16 seconds and never more than 38
in any ten minutes, showing **Next batch at** while it waits.

Rate limits from this PC's VRChat calls, VRCove ingestion and the server's
VRChat verification are shown with their source and retry time. `Retry-After`
seconds and HTTP dates are honored, with a one-minute fallback. Expired logins,
rate limits, network errors and other temporary failures keep IDs queued with
capped exponential backoff, and collection continues while the app waits.
Rotated credentials are updated in the encrypted session record.

The state file is in the user's configuration directory under
`vrcove-logger/state.json`. It includes the API key, queue and file
fingerprints, but not the VRChat session, which lives in secure storage; the app
writes it with owner-only permissions on Unix. On Windows,
access follows the user's configuration-folder ACLs. Run one instance at a time.
The queue is limited to 10,000 pending IDs; collection waits when full.

## Verification and screenshots

```sh
go test -race -tags novulkan ./...
python3 -m unittest discover -s tests -p test_release.py -v
```

Release tests use temporary repositories and a fake GitHub CLI. They verify
checksums, executable permissions, reproducible packaging, tag/working-tree
checks, prerelease flags, and that incomplete uploads never become public.

The backend's ingestion tests live in the parent module's
`internal/server/logger_test.go`; all upstream responses are stubbed.

For an actual Gio capture on Windows:

```powershell
powershell -ExecutionPolicy Bypass -File capture-windows.ps1
```

The script launches the built executable in isolated preview mode, captures only
that window, and closes it. It does not commit, push, or contact the backend.
