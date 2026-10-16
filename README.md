# VRCove

A privacy-focused VRChat web frontend, with avatar search built in.

[Website](https://vrcove.st) · [GitHub](https://github.com/Sorganeil/vrcove)

Browse avatars, worlds, users and groups. Link VRChat to manage friends, favorites and avatars. VRCove accounts add reviews and collections. No ads or analytics. Not affiliated with VRChat.

## Features

### Private by default

- No ads, analytics, telemetry, session replay or third-party error reporting. No Google Analytics, Amplitude, Mixpanel or Sentry.
- Images are proxied through VRCove, so your browser never contacts VRChat's image hosts directly.
- No friend-location history or play-time tracking. Account data loads only when you open a section or press Refresh.
- Bookmarks and notes stay in your browser. You can export and import them as a backup.
- Linked VRChat sessions are stored encrypted. The full breakdown of what is and isn't shared is on the site's privacy page.

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

### Installable app (PWA)

- Install VRCove from your browser on desktop or phone and it opens in its own window like a native app.
- No Electron, bundled Chromium or separate webview to download. It runs on the browser you already have.

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

## License

VRCove is released under the [MIT License](LICENSE).
