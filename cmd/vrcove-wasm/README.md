# vrcove-wasm

The website's VRChat client. It is the server's local mode, the same code the
desktop app runs, compiled to WebAssembly and run in a browser worker
(`app/lib/vrchat-worker.ts`). Every VRChat request travels over TLS that starts
and ends in the worker. The website's `/api/relay` only copies the encrypted
bytes, so VRCove's server never sees VRChat passwords, tokens or responses.

- VRChat API calls and the live pipeline each dial a tunnel through
  `/api/relay`. The first message picks the host by a one-byte selector.
- Image redirects go to signed CDN URLs that carry no credentials. The worker
  fetches those through the website's image proxy.
- Session cookies stay in the browser's IndexedDB, encrypted with a
  non-extractable WebCrypto key. The page talks to the worker through
  `app/lib/vrchat-client.ts`.

`npm run build` builds it into `public/vrcove/` with `scripts/build-wasm.mjs`.
Run `npm run build:wasm` alone after changing Go code during development.

## Phase 0 spike results, November 3, 2026

Go 1.27.3, Chromium (Claude desktop browser pane), relay on the same machine.

- Setting `DialTLSContext` makes Go's js/wasm `http.Transport` use real
  connections instead of `fetch`, so CORS never applies.
- TLS 1.3 completes inside the worker, with roots from
  `x509roots/fallback`. VRChat negotiates `h2` over ALPN, and one tunnel
  carried all three requests.
- The relay saw about 2.3 KB up and 43 KB down per run. Every tunnel started
  with a TLS handshake and showed no plaintext markers in either direction. A
  deliberately plaintext control request through the same relay was flagged,
  so the check works.
- Requests on a warm tunnel took 0.3 to 0.6 s. Native `curl` to the same
  endpoint took 0.7 to 1.0 s with a new handshake each time, so the tunnel
  adds no noticeable delay.

WASM size (`-trimpath -ldflags='-s -w'`):

| Build | Raw | Brotli |
|---|---|---|
| Go runtime only | 2.34 MB | 0.52 MB |
| Transport, TLS, HTTP/2, WebSocket, roots | 10.05 MB | 2.03 MB |
| Same plus the full VRChat SDK | 12.13 MB | 2.36 MB |

The SDK adds only 0.33 MB compressed. Most of the size is Go's standard
library networking and TLS.

## Known gaps

- The spike stubbed the live pipeline. `vrchat/pipeline_js.go` now speaks
  WebSocket with `gorilla/websocket` over a tunnel from `Config.PipelineDial`.
- Only Chromium was tested. Safari and Firefox, mobile devices, and slow
  networks are untested.
- Cloudflare in front of VRChat accepted Go's TLS fingerprint from this
  machine. The relay's address is what VRChat sees, the same as today.
