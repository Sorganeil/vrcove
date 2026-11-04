//go:build js && wasm

// Command vrcove-wasm is the phase 0 spike for running the VRChat SDK in a
// browser worker. It dials TLS itself over a WebSocket relay, so the relay
// only ever carries ciphertext, then fetches VRChat's public /config.
//
// Set VRCOVE_RELAY to the relay's WebSocket URL, for example
// ws://127.0.0.1:18200/relay.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"sync/atomic"
	"syscall/js"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
	"github.com/coder/websocket"
	_ "golang.org/x/crypto/x509roots/fallback"
)

// targets maps the hosts the SDK may dial to the relay's one-word selectors.
// The relay chooses the destination, so the browser never names a host.
var targets = map[string]string{"api.vrchat.cloud:443": "api", "pipeline.vrchat.cloud:443": "pipeline"}

func say(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	fmt.Println(line)
	if post := js.Global().Get("postMessage"); post.Type() == js.TypeFunction {
		post.Invoke(line)
	}
}

func main() {
	relay := os.Getenv("VRCOVE_RELAY")
	if relay == "" {
		say("FAIL: set VRCOVE_RELAY")
		return
	}
	var dials atomic.Int32
	var protocol atomic.Value
	transport := &http.Transport{
		ForceAttemptHTTP2: true,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			target, ok := targets[addr]
			if !ok {
				return nil, fmt.Errorf("no relay route for %s", addr)
			}
			dials.Add(1)
			start := time.Now()
			ws, _, err := websocket.Dial(ctx, relay+"?target="+target, nil)
			if err != nil {
				return nil, fmt.Errorf("relay: %w", err)
			}
			host, _, _ := net.SplitHostPort(addr)
			conn := tls.Client(websocket.NetConn(context.Background(), ws, websocket.MessageBinary),
				&tls.Config{ServerName: host, NextProtos: []string{"h2", "http/1.1"}, MinVersion: tls.VersionTLS12})
			if err := conn.HandshakeContext(ctx); err != nil {
				conn.Close()
				return nil, fmt.Errorf("TLS: %w", err)
			}
			state := conn.ConnectionState()
			protocol.Store(state.NegotiatedProtocol)
			say("tunnel to %s: TLS %s, ALPN %q, %d certs, %v", host, tls.VersionName(state.Version), state.NegotiatedProtocol, len(state.PeerCertificates), time.Since(start).Round(time.Millisecond))
			return conn, nil
		},
	}
	client, err := vrchat.New(vrchat.Config{UserAgent: "VRCove/0.0.0-spike (admin@vrcove.st)", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		say("FAIL: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := range 3 {
		start := time.Now()
		config, _, err := vrchat.Execute(ctx, client, vrchat.Tokens{}, vrchat.GetConfig())
		if err != nil {
			say("FAIL: GET /config: %v", err)
			return
		}
		raw, _ := json.Marshal(config)
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		say("GET /config #%d: %d bytes, %d fields (%v...), %v", i+1, len(raw), len(keys), keys[:min(4, len(keys))], time.Since(start).Round(time.Millisecond))
	}
	say("OK: %d tunnel(s) for 3 requests, protocol %q", dials.Load(), protocol.Load())
	// Closing the tunnel lets the relay log its byte counts.
	transport.CloseIdleConnections()
	time.Sleep(500 * time.Millisecond)
}
