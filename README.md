# VRCove

VRCove is a VRChat web frontend with built-in avatar search. It works in a browser on your computer, phone or tablet.

[Website](https://vrcove.st) 
[Desktop App](https://github.com/Sorganeil/vrcove/releases/latest)

Browse avatars, worlds, users and groups, or link your VRChat account to manage friends, favorites and avatars. A VRCove account lets you write reviews and create collections.

VRCove has no ads or analytics and is not affiliated with VRChat.

## Privacy

- No ads, analytics, telemetry, session replay or third-party error reporting. That includes Google Analytics, Amplitude, Mixpanel and Sentry.
- Images pass through VRCove, so your browser never contacts VRChat's image hosts directly.
- VRCove doesn't track friend locations or play time. Account data loads when you open a section or press Refresh.
- Bookmarks and notes stay in your browser. You can export them for backup and import them later.
- Linked VRChat sessions are stored encrypted. The site's privacy page explains what is and isn't shared.

## On your phone

VRCove works on Android, iPhone and iPad, with touch layouts for smaller screens. Open [vrcove.st](https://vrcove.st) to use it without installing anything.

Avatar search, friends lists and account tools work on phones and tablets. If you play on Quest without a PC, you can manage your friends and avatars from your phone.

### Install as an app (PWA)

You can install VRCove from your browser on desktop or mobile. It uses your existing browser, takes very little storage and updates whenever the site does. It doesn't bundle Electron, a .NET runtime, a JVM, Chromium or a webview.

- On Windows, macOS, Linux and Android, choose **Install app** from VRCove's menu or use your browser's install option.
- On iPhone and iPad, tap Share, then Add to Home Screen.
- In Safari on a Mac, choose File, then Add to Dock.

On iPhone and iPad, the installed app has its own sign-in and bookmarks, separate from Safari.

## Avatar search

Search public avatars in VRCove's own large database without linking a VRChat account. Open an avatar to view its details, copy its ID or bookmark it. With VRChat linked, you can also switch to it.

To contribute public avatars to the database, use the logger built into [VRCove Desktop](#vrcove-desktop).

## Reviews and collections

Rate and review users, groups, worlds and avatars you find on VRCove. You can organize avatars and worlds into collections, then keep them private or share them by link.

Profiles start private. Making yours public lets others see your bio, reviews and public collections.

## Manage your VRChat account

Link VRChat to manage friends, friend requests, favorites, notifications and your current avatar. You can also see all the avatars and worlds you've uploaded, including private ones.

## VRCove Desktop

VRCove Desktop runs on Windows and Linux. It uses the same interface as the website and connects to VRChat directly from your computer.

[Download the latest release](https://github.com/Sorganeil/vrcove/releases/latest).

Your VRChat password and session go directly to VRChat and never pass through VRCove's server. The session is stored encrypted, with its key kept in Windows Credential Manager or your Linux keyring.

A VRCove account is optional. Avatar search, reviews and collections still come from vrcove.st.

The desktop app also includes:

- Instance history and a record of the people you met.
- A game log of joins, leaves, avatar changes and videos.
- A friend activity feed.
- Screenshots with the world and people who were there.
- Desktop notifications that keep working with the window closed while the app runs in the tray.
- An avatar logger that adds public avatars you see in VRChat to VRCove's search. Uploading requires an API key from your VRCove account page.
- A username checker, OSC chatbox and Unity package extractor.
- Saved VRChat accounts with account switching.
- In-app updates that install only after their checksums match the release.

Your history, game logs, activity feed and screenshots stay on your computer.

VRCove Desktop uses Go and Wails, sharing frontend and server code with the website. See [companion/](companion/) for source, build instructions and details. Linux requires GTK 4, WebKitGTK and a tray host such as KDE Plasma.

## Development

Requires Node.js 22.13+ and Go 1.26+.

```sh
npm install
npm run dev
# In another terminal:
PUBLIC_ORIGIN=http://localhost:5173 go run ./cmd/vrcove
```

See `.env.example` for account settings. The avatar catalog goes in `.data/catalog.sqlite`.

## Production

Run `npm run build`, then `CGO_ENABLED=0 go build -o vrcove ./cmd/vrcove`. Deployment templates are in `deploy/`. The app supports Cloudflare Tunnel over a Unix socket.

Keep databases and encryption keys out of Git.

## Contributing

Pull requests are welcome. Be decent to other contributors and reviewers. See [CONTRIBUTING.md](CONTRIBUTING.md) for branch naming, commits and pull request guidelines.

### AI policy

AI-assisted contributions are allowed. Review what you send to a tool and everything it produces before submitting it. You're responsible for every line in your pull request.

Keep pull requests small and focused. Large pull requests that take too long to review may be ignored or closed.

### AI disclosure

We use AI to speed up development and write code ourselves wherever practical. We review every prompt and output and take responsibility for the code and decisions we ship.

We're proud of the work. We also understand that some people prefer projects developed without AI assistance.

## Contact

VRChat staff and anyone else who wants to get in touch can email [vrcove-contact.overbid399@passmail.net](mailto:vrcove-contact.overbid399@passmail.net) or [open an issue](https://github.com/Sorganeil/vrcove/issues).

## License

VRCove is released under the [MIT License](LICENSE).