# VRCove

A VRChat web frontend with 1.49 million searchable public avatars.

[Website](https://vrcove.st) · [GitHub](https://github.com/Sorganeil/vrcove)

Browse avatars, worlds, users and groups. Link VRChat to manage friends, favorites and avatars. VRCove accounts add reviews and collections. No ads or analytics. Not affiliated with VRChat.

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
