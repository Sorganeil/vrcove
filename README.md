# VRCove

[Live site](https://vrcove.st) · [GitHub](https://github.com/Sorganeil/vrcove)

An independent, browser-only VRChat frontend with a searchable archive of 1,492,676 public avatars.

## What works

- Home dashboard, live friends, worlds, notifications, and status updates.
- Public avatar search by name, creator, ID, platform and PC performance; pagination and sorting.
- VRChat login with authenticator, email or recovery-code verification.
- Avatar switching and account favorites, including choosing a favorite group.
- User/world/group search, group membership listings, group details and join requests.
- Local bookmarks, private notes, and JSON backup export/import.
- VRCove accounts (separate from VRChat) for rating and commenting on VRChat users, worlds, avatars and groups, with privately linked VRChat accounts.
- Image proxy, including VRChat CDN redirects, so browsers do not fetch avatar images directly from VRChat.

Account features depend on VRChat API permissions and availability. This is not a claim of complete VRChat website or VRCX parity. Payments, identity verification, account recovery, advanced group administration, and marketplace transactions open the official VRChat website. Local game logs, overlays, Discord integration, process control, and complete played-time history are unavailable in a browser. There is no desktop companion.

## Privacy and authentication

Browsing the catalog does not require a VRCove account; the former hosting platform's access gate no longer applies. To keep the site private, put it behind Cloudflare Access (Zero Trust → Access → Applications) on the tunnel hostname. Authenticated actions require signing in to a VRCove account and linking VRChat. There is no standalone VRChat login. The top-right account menu shows the VRCove username.

Passwords and 2FA codes are processed in request memory and sent over HTTPS to VRChat. Application code never writes them to a file, database, or log. Upstream errors are sanitized. Each connected VRChat account has its own AES-256-GCM encrypted, HttpOnly, Secure, SameSite=Strict session cookie with a 12-hour expiry. A separate encrypted cookie selects the active account, and pending two-factor verification uses its own 10-minute cookie. The server keeps the encryption key in its environment, but no user session records or token database. Disconnect attempts upstream revocation and clears only the active connection. VRCove sign-out clears all saved VRChat session cookies in this browser.

This does not mean the server cannot access a session: it must decrypt it for VRChat requests. Cloudflare and VRChat process network traffic and may keep operational logs. Bookmarks and notes persist in this browser only. No analytics or background activity-history collection is included.

## VRCove accounts and reviews

VRCove has its own accounts, independent of VRChat. An account is a username and an Argon2id password hash (RFC 9106 / OWASP parameters: 64 MiB, 3 passes, 1 lane; stored in PHC format and upgraded on sign-in if the parameters change). No email, IP address or user agent is stored. Users can generate ten single-use recovery codes in account settings after confirming their password. Each code has 128 random bits; only its SHA-256 digest is stored. Codes are shown once and can be downloaded. Generating a new set invalidates the old set. A username and saved code can reset the password, atomically consume that code and revoke all VRCove sessions; remaining codes stay usable. Without a password or saved code, an account cannot be recovered. At most four hashes run at once to bound memory use. Sign-in is rate limited per IP and per username, and unknown usernames cost the same Argon2id work as real ones. Session tokens are random 256-bit values in an HttpOnly, SameSite=Strict `__Host-` cookie; only their SHA-256 is stored, and changing the password signs out every other device.

Users signed in to VRCove with a linked VRChat account can rate (1–5 stars) and comment on any VRChat user, world, avatar or group, one review per target, editable and deletable. Reviews are public, show only the VRCove username, and are dated to the day. Posting a review never contacts VRChat.

VRChat connection loss shows a persistent reconnect notice while leaving VRCove signed in and ownership links intact. The account page shows the session expiry time. The browser checks on entry, on returning to the tab, and once per minute while visible; it also detects the scheduled local expiry and authentication failures from any VRChat panel. Permission errors, rate limits and temporary outages do not mark the session invalid.

You can link up to 10 VRChat accounts and choose the active one using the account selector or “Use account” on the account page. Switching verifies the saved session with VRChat before changing the active account. Expired or invalid targets require reconnection and leave the current account active. Sessions are saved per browser, independently of the persistent ownership links; tokens are never stored in the accounts database. Adding an account preserves existing sessions, including while two-factor verification is pending. Actions carry the expected active identity so a stale tab cannot silently act as a different account.

**Linked VRChat accounts are private.** Linking requires a signed-in VRCove account and verifies VRChat credentials (including two-factor verification when required), then creates the private link automatically. VRChat connections are bound to their VRCove owner; signing out or removing the link prevents further authenticated actions. The VRChat user ID is stored only as an HMAC (for uniqueness and to stop people reviewing their own linked profile) and inside an AES-256-GCM record under `ACCOUNT_KEY`, so the database file alone does not connect a VRCove username to a VRChat account. Links are shown only to their owner and never on reviews. Deleted rows are overwritten on disk (`secure_delete`); deleting an account removes its sessions, links, reviews, collections and collection items. Backups and SQLite journal files may retain earlier data until separately expired; secure deletion is not a guarantee that every historical copy is erased.

The interface tells people to pick a VRCove username unrelated to their VRChat names, warns on the signup form when the name resembles the connected VRChat account, and warns again after linking. The username is the only identity on reviews, so using an unrelated name reduces the risk of VRChat staff or other people correlating activity. This does not guarantee anonymity or protection from account action: review content, reused handles and traffic can still reveal identity. The server operator holds `ACCOUNT_KEY` and can still decrypt links; VRChat sees that an account signed in through VRCove (the User-Agent names it), but not which VRCove account it is linked to.

VRCove accounts can upload a PNG, JPEG or GIF profile picture up to 2 MB (4096 × 4096 pixels maximum). Images are center-cropped to 256 × 256 PNGs, with metadata and animation removed, and stored in the accounts database. Pictures appear in the account menu and on the VRCove profile, follow profile visibility, and can be removed. Account deletion also removes the picture.

Profiles start private. You can opt into a public profile at `#veil/<username>` and add a bio of up to 500 characters plus a single-line status of up to 160 characters. Edit both from the VRCove account page. These fields are independent of VRChat, follow your profile’s visibility, and can be cleared at any time. Public profiles aggregate your reviews and public collections; making a profile private does not hide reviews on the items you reviewed. Private profiles and missing usernames return the same response to visitors. Linked VRChat accounts never appear in public responses.

Avatar and world collections sync to your VRCove account, separately from browser-local bookmarks. Collections start private unless you explicitly choose public. Public collections are accessible to anyone with their random link and appear on your profile when it is public. A public collection names its owner only when the owner's profile is public, but its contents and description may still reveal identity. Limits are 100 collections per account and 500 items per collection. Owners can edit, remove items or delete collections.

Review reporting and administrator moderation tools are not implemented yet.

## Architecture

- `cmd/vrcove`: the Go server. One binary serves the API under `/api/` and the embedded frontend.
- `internal/server`: catalog search (SQLite FTS5 via the pure-Go `modernc.org/sqlite`, no cgo), the image proxy, and the VRChat account API.
- `app/`: the React frontend, bundled by Vite into `web/dist` and embedded with `go:embed`. Routing uses the URL hash, so there is no server-side rendering.

## Runtime configuration

Set these as environment variables (for example in a systemd `EnvironmentFile`):

- `PUBLIC_ORIGIN`: the origin browsers use, e.g. `https://vrcove.example`. Required in production. Behind a tunnel the server only sees plain HTTP, so the cross-origin check and the `__Host-`/`Secure` cookie come from this value. An `https` origin enables Secure cookies.
- `LISTEN_ADDR`: `unix:/path/to.sock` (recommended in production) or a loopback `host:port`; default `127.0.0.1:8080`. `CF-Connecting-IP` is trusted for sign-in rate limiting only from a local peer (cloudflared on the same host).
- `TUNNEL_ONLY`: defaults to `true` when `PUBLIC_ORIGIN` is `https`. See Origin protection below.
- `DATABASE_PATH`: default `.data/catalog.sqlite`. The server opens it read-only after creating any missing schema.
- `ACCOUNT_KEY`: base64-encoded 32-byte key that enables VRCove accounts and reviews and encrypts VRChat links. Keep it stable: a new key makes existing links unreadable (accounts and reviews survive).
- `ACCOUNTS_PATH`: default `.data/accounts.sqlite`. The writable accounts, links and reviews database. Back it up; unlike the catalog it cannot be re-imported.
- `SESSION_KEY`: base64-encoded 32-byte random key (`openssl rand -base64 32`). Rotate it to invalidate every VRChat connection cookie. VRCove account sessions use separate database-backed tokens. Existing standalone VRChat cookies must be replaced by linking VRChat from a signed-in VRCove account.
- `VRC_CONTACT`: operator contact (email address or http(s) URL) sent to VRChat as `User-Agent: VRCove/<version> (<contact>)`. Startup fails if it is malformed.

VRChat login stays disabled until `SESSION_KEY` and `VRC_CONTACT` are both set. VRCove signup and login require `ACCOUNT_KEY`; linking also requires a working VRChat connection. Never commit secret values.

## Development

Requires Go 1.26+ and Node.js 22.13+.

```bash
npm install
npm run dev                                          # Vite on :5173, proxies /api to :8080
PUBLIC_ORIGIN=http://localhost:5173 go run ./cmd/vrcove
```

Checks: `go test ./cmd/... ./internal/...`, `npm run typecheck`, `npm run lint`, `npm run test:frontend`.

Frontend regression tests launch an isolated Go server and Vite on ports 18080 and 15173, with temporary account/catalog databases. They use synthetic VRCove credentials and mocked VRChat metadata, never live VRChat credentials. Install Chromium with `npx playwright install chromium`, or set `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` to an existing Chromium binary. Desktop and phone-sized projects cover account privacy, reviews, profiles, collections and dialog keyboard behavior.

## Building and deploying

```bash
npm run build                                        # writes web/dist
CGO_ENABLED=0 go build -o vrcove ./cmd/vrcove    # embeds web/dist
```

### Origin protection

The VRCove origin uses a Unix socket so it has no public TCP listener for Censys, Shodan or similar scanners to discover. Other services on a shared server can still expose its IP. Public pages remain accessible through Cloudflare; origin isolation does not prevent scraping through the public hostname.

- **No inbound ports.** Cloudflare Tunnel dials out to Cloudflare, so the firewall can drop all inbound traffic (`ufw default deny incoming`). Reach SSH over Tailscale or Cloudflare Access for Infrastructure if you can; otherwise allow port 22 only from your own IP. An open SSH port is visible to scanners even though it does not link the IP to the site.
- **Unix socket.** In production the server listens on `unix:/run/vrcove/vrcove.sock`, so there is no TCP port even on loopback.
- **Tunnel-only mode.** With an `https` `PUBLIC_ORIGIN`, the server refuses to start on a non-loopback address. It also closes, without any response, every connection that does not come from a local peer with your hostname as `Host` and a `CF-Connecting-IP` header. Set `TUNNEL_ONLY=false` only for temporary debugging.
- **Things outside the code that leak an origin IP:** DNS records that ever pointed at the VPS (history sites keep them; use a fresh IP if the domain ever had an A record to it), MX/SPF records or mail sent from the VPS, other sites or services on the same IP, and TLS certificates issued on the VPS for the hostname. Outbound calls to VRChat reveal the IP to VRChat only.

### Install

`deploy/` holds a hardened systemd unit, a cloudflared ingress config and an environment template.

```bash
sudo useradd --system --home /var/lib/vrcove --shell /usr/sbin/nologin vrcove
sudo install -d -o vrcove -g vrcove -m 750 /var/lib/vrcove
sudo install -m 755 vrcove /usr/local/bin/vrcove
sudo install -o vrcove -g vrcove -m 640 .data/catalog.sqlite /var/lib/vrcove/catalog.sqlite
sudo install -d -m 755 /etc/vrcove
sudo install -m 600 deploy/env.example /etc/vrcove/env     # then fill in the values
sudo install -m 644 deploy/vrcove.service /etc/systemd/system/
sudo systemctl enable --now vrcove
```

Point cloudflared at the socket using `deploy/cloudflared.yml`. If cloudflared runs as its own user rather than root, add it to the `vrcove` group so it can open the socket. Afterwards, `node scripts/verify-site.mjs https://vrcove.example` smoke-tests the deployment through Cloudflare.

## Database import

`scripts/import-dump.py <absolute dump path>` parses PostgreSQL COPY records as data; it never executes SQL from the dump. The original gzip stays untouched. Only public avatar metadata is exported; private avatars and bundle download URLs are excluded. The importer writes an ignored `.data/catalog.sqlite`, which the server reads directly (the `chunk-*.json.gz` files it also writes were only for uploading to the old hosted database). Run it in a fresh checkout-local `.data` directory; an already imported SQLite database must not be imported twice.

The catalog is a snapshot. A record's current availability is checked by VRChat when a user selects or favorites it. Android support counts native builds, not impostor packages. Unknown performance and platform values stay unknown.

## Verification limits

Encryption, token tamper/expiry rejection, production cookie flags, and upstream error sanitization are covered by synthetic tests. Live catalog search, filtering, preview loading, unauthenticated action denial, and cross-origin denial are checked separately. Actual login, 2FA, changing an avatar, and account writes require a user-controlled VRChat account and have not been exercised with real credentials by the agent.

## Localization

Use the language picker in the header or **Privacy & settings**. Available languages are English, Spanish, French, German, Japanese, Russian, Ukrainian, Simplified Chinese, and Korean. The first visit uses the first supported browser language; an explicit selection is saved locally under `vrcove-language` and synchronized across tabs. The picker also works when browser storage is blocked, with the choice lasting for that page session. Regional language codes resolve to their supported base language; Chinese browser locales currently select Simplified Chinese.

`app/i18n.ts` owns language selection, English fallback, interpolation, and date/number formatting. Components that render translations call `useLocale()` so switching languages updates existing forms and dialogs without remounting them. Translate interface copy with `t('English source message')`, or pass named values with `t('Welcome back, {name}.', {name})`. Keep complete sentences together so translators can reorder placeholders; do not translate routes, API values, user names, or user-generated content.

The eight translated catalogs are in `app/locales/messages.json`; English source messages act as the default catalog. The initial catalogs cover navigation and the main search, account, avatar, collection, and review controls. Some longer explanations and server-provided errors still use English fallback. To add coverage, add the same source key to every language while retaining all `{placeholder}` names. Locale-aware helpers format dates and numbers according to the selected language. The frontend localization tests check all nine selections on desktop and phone, browser detection, persistence, storage restrictions, tab synchronization, form preservation, catalog parity, and placeholder consistency.
