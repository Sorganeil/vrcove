# VRCove

VRCove is a private VRChat web frontend with built-in avatar search. It works in a browser on your computer, phone or tablet.

[Website](https://vrcove.st) -
[Desktop App](https://github.com/Sorganeil/vrcove/releases/latest)

## Privacy

- No ads, analytics, telemetry, session replay or third-party error reporting. That includes Google Analytics, Amplitude, Mixpanel and Sentry.
- Images on the website go through VRCove, so your browser doesn't contact VRChat's image hosts directly. The desktop app fetches images from your computer.
- The website doesn't keep a history of your browsing, friends' locations or play time. The desktop app keeps its history on your computer.
- Account pages load when you open or refresh them. If you turn on notifications, VRCove also listens for live updates and checks periodically when a live connection isn't available.
- Bookmarks and notes stay in your browser. Export a backup to save them or move them to another browser.

When you link VRChat on the website, your login passes through VRCove's server to VRChat. Session tokens and linked-account details are encrypted, and the server decrypts them when needed to make requests. Creating a desktop logger API key also saves encrypted session credentials on the server so it can verify avatars. See **Privacy & settings** for what gets stored and shared.

## On your phone

Open [vrcove.st](https://vrcove.st) on Android, iPhone or iPad. There's nothing to install, and the layouts are built for touch and smaller screens.

Avatar search, friends lists and account tools work on phones and tablets. If you play on Quest without a PC, you can manage your friends and avatars from your phone.

### Install as an app (PWA)

You can also install VRCove as a web app on desktop or mobile if your browser supports it. It runs through your existing browser, without a separate browser or desktop runtime bundled in. It caches interface files on your device and loads site updates when you're online. Avatar search and account features need an internet connection.

- Choose **Install app** from VRCove's menu if it appears, or use your browser's install option.
- On iPhone and iPad, tap Share, then Add to Home Screen.
- In Safari on a Mac, choose File, then Add to Dock.

On iPhone and iPad, the installed app stores its data separately from Safari. On iOS/iPadOS 17.2 and later, [login cookies are copied when you install it](https://webkit.org/blog/14787/webkit-features-in-safari-17-2/). Bookmarks and notes don't copy over. Use Export/Import backup in **Privacy & settings** to move them.

## Avatar search

You don't need to sign in to search VRCove's public avatar database. Open an avatar to see its details, copy its ID or bookmark it. Link VRChat to switch to it from VRCove.

The logger in [VRCove Desktop](#vrcove-desktop) lets you contribute public avatars to the database.

## Reviews and collections

Sign in to VRCove and link a VRChat account to post reviews or manage collections. You can rate and review users, groups, worlds and avatars, though you can't review a VRChat user account you've linked yourself.

Collections hold avatars or worlds. Keep them private or share them by link. Anyone can read public reviews and view public collections without signing in.

Your profile starts private. Make it public to show your bio, reviews and public collections.

## Manage your VRChat account

Sign in to VRCove and link VRChat to manage friends, friend requests, favorites, notifications and your current avatar. You can also see all the avatars and worlds you've uploaded, including private ones.

## VRCove Desktop

For Windows and Linux, [download the latest release](https://github.com/Sorganeil/vrcove/releases/latest). The app uses the same interface as the website and connects to VRChat directly from your computer.

When you sign in to VRChat in the app, your password and session go straight to VRChat. Saved sessions are encrypted, with the key in Windows Credential Manager or your Linux keyring. If the keyring isn't available, you'll need to sign in again after quitting the app.

You can search avatars and use the local VRChat tools without a VRCove account. The catalog, reviews and collections still come from vrcove.st. To post reviews, manage collections or get a logger API key, you'll need a VRCove account with VRChat linked on the website. That separate website login passes through VRCove's server, as described under Privacy.

The app also has:

- Instance history and a record of the people you met.
- A game log of joins, leaves, avatar changes and videos.
- A friend activity feed.
- Screenshots with the world and people who were there.
- Desktop notifications that keep working when you close the window and leave the app running in the tray.
- An avatar logger that reads avatar IDs from VRChat's local analytics cache. It checks public avatars using your local VRChat session, then sends them to VRCove for another check before adding them to search.
- A username checker, OSC chatbox and Unity package extractor.
- Saved VRChat accounts with account switching.
- Updates you can install in the app after it checks that the download's checksum matches the release.

Your history, game logs, activity feed and screenshots stay on your computer.

For logger uploads, generate an API key on your VRCove account page on the website. The key expires when its linked website session ends.

It's built with Go and Wails and shares frontend and server code with the website. Source and build instructions are in [companion/](companion/). On Linux, you'll need GTK 4, WebKitGTK 6 and a tray host such as KDE Plasma or a GNOME tray extension.

## Development

Requires Node.js 22.13+ and Go 1.26+.

```sh
npm install
mkdir -p .data
npm run dev
# In another terminal:
PUBLIC_ORIGIN=http://localhost:5173 go run ./cmd/vrcove
```

Open `http://localhost:5173`. The server creates an empty catalog at `.data/catalog.sqlite`. To get avatar search results, put a populated catalog there or point `DATABASE_PATH` to one.

Export the settings from [.env.example](.env.example) in the server's environment. The server doesn't read `.env` files automatically.

Set `ACCOUNT_KEY` to enable VRCove accounts. Add `SESSION_KEY` and `VRC_CONTACT` to enable VRChat login. Use a different base64-encoded 32-byte value for each key. With those settings in place, `LOGGER_ENABLED=true` lets the server accept desktop logger submissions.

### VRChat SDK

The API client is a standalone Go module in [vrchat/](vrchat/). Both the server
and desktop app use it through local module replacements. See its
[README](vrchat/README.md) for authentication, API requests, live events, and
use from another project. Run its tests separately with `cd vrchat && go test -race ./...`.

## Production

Run `npm run build`, then `CGO_ENABLED=0 go build -o vrcove ./cmd/vrcove`. Deployment templates are in `deploy/`. The app supports Cloudflare Tunnel over a Unix socket.

Keep databases and encryption keys out of Git.

## Contributing

Pull requests are welcome. Be decent to other contributors and reviewers. [CONTRIBUTING.md](CONTRIBUTING.md) covers branch names, commits and pull requests.

### AI policy

You can use AI tools for contributions. Review what you send them and check their output before submitting it. You're responsible for every line in your pull request.

Keep pull requests small and focused. We may close or skip large ones that take too long to review.

### AI disclosure

We use AI to speed up development and write code ourselves where it makes sense. We review every prompt and output, and we're responsible for the code and decisions we ship.

We're proud of what we build, and we understand if you'd prefer a project developed without AI assistance.

## Contact

VRChat staff, or anyone else who wants to get in touch, can email [vrcove-contact.overbid399@passmail.net](mailto:vrcove-contact.overbid399@passmail.net) or [open an issue](https://github.com/Sorganeil/vrcove/issues).

## License

VRCove is released under the [MIT License](LICENSE).
