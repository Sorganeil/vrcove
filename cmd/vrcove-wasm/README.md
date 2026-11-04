# vrcove-wasm (phase 0 spike)

This spike runs the `vrchat/` SDK as WebAssembly in a browser Web Worker. The
worker dials TLS itself through a WebSocket relay, so the relay carries only
ciphertext. It fetches VRChat's public `GET /config` three times over one
tunnel.

`spikerelay/` is a local test relay. It serves the page, copies bytes to
`api.vrchat.cloud:443` or `pipeline.vrchat.cloud:443`, and logs byte counts. It
also checks that the stream starts with a TLS handshake and contains no
plaintext HTTP. Phase 3 replaces it with the hardened production relay.

## Run

```sh
mkdir -p /tmp/vrcove-wasm
cp cmd/vrcove-wasm/web/* "$(go env GOROOT)/lib/wasm/wasm_exec.js" /tmp/vrcove-wasm/
GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o /tmp/vrcove-wasm/vrcove-wasm.wasm ./cmd/vrcove-wasm
go run ./cmd/vrcove-wasm/spikerelay -dir /tmp/vrcove-wasm
```

Open http://localhost:18200. The page shows the worker's log, and the relay
prints one line per tunnel when it closes.

## Results, November 3, 2026

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

- `vrchat/pipeline_js.go` stubs the live pipeline. In the browser,
  `coder/websocket` wraps the browser's WebSocket and cannot run over the
  tunnel. Phase 4 needs a pure-Go WebSocket client that dials through the
  tunnel `net.Conn`.
- Only Chromium was tested. Safari and Firefox, mobile devices, and slow
  networks are untested.
- Cloudflare in front of VRChat accepted Go's TLS fingerprint from this
  machine. The relay's address is what VRChat sees, the same as today.
