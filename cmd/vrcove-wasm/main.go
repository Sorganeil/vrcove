//go:build js && wasm

// Command vrcove-wasm runs VRCove's VRChat routes in a browser worker. It is
// the server's local mode, as in the desktop app, but every VRChat request
// travels over TLS that starts and ends in this worker. The website's
// /api/relay only copies the encrypted bytes.
//
// Before go.run, the worker sets self.vrcoveConfig to
// {origin: string, sessionKey: Uint8Array(32)}. main then defines
// self.vrcoveHandle(request, onChunk), which answers one /api/account request
// with a Promise, and self.vrcoveCancel(id), which ends a request still
// running, such as a live notification stream. It then keeps running.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/coder/websocket"
	_ "golang.org/x/crypto/x509roots/fallback"

	"vrcove/internal/server"
)

// contact goes in the VRChat User-Agent. Release builds may set it with
// -ldflags "-X main.contact=...".
var contact = "admin@vrcove.st"

const (
	apiHost      = "api.vrchat.cloud"
	pipelineHost = "pipeline.vrchat.cloud"
	// Selectors for /api/relay. The relay picks the host, not the browser.
	toAPI      = 1
	toPipeline = 2
)

func main() {
	config := js.Global().Get("vrcoveConfig")
	if config.Type() != js.TypeObject {
		js.Global().Get("console").Call("error", "vrcove-wasm: set self.vrcoveConfig before running")
		return
	}
	origin := config.Get("origin").String()
	key := make([]byte, config.Get("sessionKey").Get("length").Int())
	js.CopyBytesToGo(key, config.Get("sessionKey"))
	srv, err := newServer(origin, key)
	if err != nil {
		js.Global().Get("console").Call("error", "vrcove-wasm: "+err.Error())
		return
	}
	js.Global().Set("vrcoveHandle", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return promise(func() (js.Value, error) { return handle(srv, origin, args[0], args[1]) })
	}))
	js.Global().Set("vrcoveCancel", js.FuncOf(func(_ js.Value, args []js.Value) any {
		running.cancel(args[0].Int())
		return nil
	}))
	select {}
}

func newServer(origin string, sessionKey []byte) (*server.Server, error) {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid origin")
	}
	relay := "wss://" + u.Host + "/api/relay"
	if u.Scheme == "http" {
		relay = "ws://" + u.Host + "/api/relay"
	}
	srv, err := server.New(server.Config{
		PublicOrigin: origin,
		SessionKey:   sessionKey,
		Contact:      contact,
		Local:        true,
		Remote:       http.NotFoundHandler(),
	}, nil, nil)
	if err != nil {
		return nil, err
	}
	tunnel := &http.Transport{
		ForceAttemptHTTP2: true,
		IdleConnTimeout:   2 * time.Minute,
		DialTLSContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			if addr != apiHost+":443" {
				return nil, fmt.Errorf("no relay route for %s", addr)
			}
			return dialRelay(ctx, relay, toAPI, apiHost, "h2", "http/1.1")
		},
	}
	srv.Client = &http.Client{
		Transport:     router{tunnel: tunnel, fetch: http.DefaultTransport, origin: origin},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	srv.PipelineDial = func(ctx context.Context) (net.Conn, error) {
		return dialRelay(ctx, relay, toPipeline, pipelineHost, "http/1.1")
	}
	return srv, nil
}

// dialRelay opens a tunnel through the website and starts TLS to host inside
// it, so the website only ever sees ciphertext.
func dialRelay(ctx context.Context, relay string, selector byte, host string, alpn ...string) (net.Conn, error) {
	ws, _, err := websocket.Dial(ctx, relay, nil)
	if err != nil {
		return nil, fmt.Errorf("VRChat relay unavailable: %w", err)
	}
	ws.SetReadLimit(1 << 20)
	if err := ws.Write(ctx, websocket.MessageBinary, []byte{selector}); err != nil {
		ws.CloseNow()
		return nil, err
	}
	conn := tls.Client(websocket.NetConn(context.Background(), ws, websocket.MessageBinary),
		&tls.Config{ServerName: host, NextProtos: alpn, MinVersion: tls.VersionTLS12})
	if err := conn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("TLS to %s: %w", host, err)
	}
	return conn, nil
}

// router sends VRChat API requests through the tunnel. Image hosts are not
// reachable through the relay. Their redirect URLs are signed and carry no
// credentials, so the website's own image proxy fetches them.
type router struct {
	tunnel, fetch http.RoundTripper
	origin        string
}

func (r router) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == apiHost {
		return r.tunnel.RoundTrip(req)
	}
	if req.Method != http.MethodGet {
		return nil, fmt.Errorf("no route for %s %s", req.Method, req.URL.Host)
	}
	proxied, err := http.NewRequestWithContext(req.Context(), http.MethodGet, r.origin+"/api/image?url="+url.QueryEscape(req.URL.String()), nil)
	if err != nil {
		return nil, err
	}
	return r.fetch.RoundTrip(proxied)
}

// running holds the cancel function of each request still being answered.
var running = requests{byID: map[int]context.CancelFunc{}}

type requests struct {
	mu   sync.Mutex
	byID map[int]context.CancelFunc
}

func (r *requests) start(id int, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[id] = cancel
}

func (r *requests) cancel(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cancel := r.byID[id]; cancel != nil {
		cancel()
		delete(r.byID, id)
	}
}

// handle answers one request from the page. request is
// {id, method, path, headers: [[name, value]...], body?: Uint8Array}. For
// streamed responses, such as live notifications, each flushed chunk goes to
// onChunk. The result is {status, headers, body}.
func handle(srv *server.Server, origin string, request, onChunk js.Value) (js.Value, error) {
	method, path := request.Get("method").String(), request.Get("path").String()
	if !strings.HasPrefix(path, "/api/account/") && !strings.HasPrefix(path, "/api/image?") {
		return js.Null(), fmt.Errorf("the worker only answers VRChat routes, not %s", path)
	}
	var body []byte
	if b := request.Get("body"); b.Type() == js.TypeObject {
		body = make([]byte, b.Get("length").Int())
		js.CopyBytesToGo(body, b)
	}
	ctx, cancel := context.WithCancel(context.Background())
	id := request.Get("id").Int()
	running.start(id, cancel)
	defer running.cancel(id)
	req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(body))
	if err != nil {
		return js.Null(), err
	}
	headers := request.Get("headers")
	for i := range headers.Length() {
		pair := headers.Index(i)
		req.Header.Add(pair.Index(0).String(), pair.Index(1).String())
	}
	// The page is this origin, and the worker is the only way in.
	req.Header.Set("Origin", origin)
	req.RemoteAddr = ""
	w := &bridgeWriter{header: http.Header{}, onChunk: onChunk}
	srv.APIHandler().ServeHTTP(w, req)
	out := js.Global().Get("Object").New()
	out.Set("status", w.statusCode())
	pairs := js.Global().Get("Array").New()
	for name, values := range w.header {
		for _, v := range values {
			pairs.Call("push", js.ValueOf([]any{name, v}))
		}
	}
	out.Set("headers", pairs)
	out.Set("body", bytesToJS(w.body.Bytes()))
	return out, nil
}

// bridgeWriter collects a response for the page. Flush sends what has been
// written so far to onChunk, so event streams reach the page as they arrive.
type bridgeWriter struct {
	header  http.Header
	status  int
	body    bytes.Buffer
	onChunk js.Value
}

func (w *bridgeWriter) Header() http.Header { return w.header }
func (w *bridgeWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *bridgeWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body.Write(p)
}
func (w *bridgeWriter) Flush() {
	if w.onChunk.Type() != js.TypeFunction || w.body.Len() == 0 {
		return
	}
	w.onChunk.Invoke(bytesToJS(w.body.Bytes()))
	w.body.Reset()
}
func (w *bridgeWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func bytesToJS(b []byte) js.Value {
	out := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(out, b)
	return out
}

// promise runs fn on a goroutine and settles a JavaScript Promise with it.
func promise(fn func() (js.Value, error)) js.Value {
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolve, reject := args[0], args[1]
		go func() {
			v, err := fn()
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
				return
			}
			resolve.Invoke(v)
		}()
		return nil
	})
	defer executor.Release()
	return js.Global().Get("Promise").New(executor)
}
