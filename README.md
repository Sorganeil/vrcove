# VRCove

A privacy-focused VRChat web frontend, with avatar search built in. Works in any browser on PC or phone.

[Website](https://vrcove.st) · [GitHub](https://github.com/Sorganeil/vrcove)

Browse avatars, worlds, users and groups. Link VRChat to manage friends, favorites and avatars. VRCove accounts add reviews and collections. No ads or analytics. Not affiliated with VRChat.

## Features

### Private by default

- No ads, analytics, telemetry, session replay or third-party error reporting. No Google Analytics, Amplitude, Mixpanel or Sentry.
- Images are proxied through VRCove, so your browser never contacts VRChat's image hosts directly.
- No friend-location history or play-time tracking. Account data loads only when you open a section or press Refresh.
- Bookmarks and notes stay in your browser. You can export and import them as a backup.
- Linked VRChat sessions are stored encrypted. The full breakdown of what is and isn't shared is on the site's privacy page.

### Mobile compatible

No PC? No problem. Most VRChat tools are Windows desktop apps, but VRCove runs in the browser you already have, so it works on Android, iPhone and iPad as well as desktop.

- No installer or setup. Open [vrcove.st](https://vrcove.st) and start using it.
- Play on Quest without a PC? Manage your friends and avatars from your phone.
- Avatar search, your friends list and account tools all work on a phone or tablet.
- Layouts are built for touch and small screens, not shrunk down from desktop.
- Prefer an app? You can install VRCove on your phone like a regular app (see below).

### Install as an app (PWA)

Install VRCove as an app on desktop or mobile, straight from your browser.

- **No bloat.** No Electron, .NET runtime, JVM, bundled Chromium or webview. Just your browser.
- It takes almost no storage and updates itself whenever the site updates.
- On Windows, macOS, Linux and Android, click **Install app** in VRCove's menu, or use your browser's install option.
- On iPhone and iPad, tap Share, then Add to Home Screen. In Safari on a Mac, choose File, then Add to Dock.
- On iPhone and iPad, the app keeps its own sign-in and bookmarks, separate from Safari.

### Avatar search

- We maintain our own large avatar database, so you can search public avatars without linking a VRChat account.
- Open any avatar to see its details, copy its ID, bookmark it, or switch to it if your VRChat account is linked.

### Reviews and collections

- Rate and review any user, group, world or avatar you can find on VRCove.
- Group avatars and worlds into collections. Keep a collection private or share it by link.
- Profiles start private. Make yours public to show your bio, reviews and public collections.

### Your VRChat account, in the browser

- Link VRChat to manage friends, friend requests, favorites, notifications and your current avatar.
- See every avatar and world you've uploaded, including private ones.

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

Run `npm run build`, then `CGO_ENABLED=0 go build -o vrcove ./cmd/vrcove`. Deployment templates are in `deploy/`; the app supports Cloudflare Tunnel over a Unix socket. Keep databases and encryption keys out of Git.

## Contributing

Pull requests are welcome. Be decent to other contributors and reviewers.

### AI policy

AI-assisted contributions are allowed. Follow AI best practices: review what you give the tool and review everything it produces before you submit it. You are responsible for every line in your pull request.

Keep pull requests small and focused. Monolithic pull requests that take forever to review will be ignored or closed.

## Contact

VRChat staff, or anyone else who wants to get in touch, can email [vrcove-contact.overbid399@passmail.net](mailto:vrcove-contact.overbid399@passmail.net) or [open an issue](https://github.com/Sorganeil/vrcove/issues).

## License

VRCove is released under the [MIT License](LICENSE).
