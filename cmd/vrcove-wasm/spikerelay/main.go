//go:build !js

// Command spikerelay is the phase 0 test relay for vrcove-wasm. It serves the
// spike page and copies bytes between a WebSocket and one of two fixed VRChat
// hosts. It never parses what it carries. It logs byte counts and checks that
// no plaintext HTTP passes through, which shows TLS ends in the browser.
//
// This is a local development tool. Phase 3 builds the production relay.
package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

var targets = map[string]string{"api": "api.vrchat.cloud:443", "pipeline": "pipeline.vrchat.cloud:443"}

// markers would appear in the stream if any HTTP traffic were readable here.
var markers = [][]byte{[]byte("GET /"), []byte("HTTP/1.1"), []byte("HTTP/2"), []byte("User-Agent"), []byte("vrcove"), []byte("VRCove"), []byte("clientApiKey"), []byte("api.vrchat.cloud/api")}

// watch counts bytes and looks for plaintext markers across write boundaries.
type watch struct {
	n     int64
	first []byte
	tail  []byte
	found []string
}

func (w *watch) Write(p []byte) (int, error) {
	if len(w.first) < 3 {
		w.first = append(w.first, p[:min(3-len(w.first), len(p))]...)
	}
	w.n += int64(len(p))
	window := append(w.tail, p...)
	for _, m := range markers {
		if bytes.Contains(window, m) {
			w.found = append(w.found, string(m))
		}
	}
	w.tail = append([]byte(nil), window[max(0, len(window)-32):]...)
	return len(p), nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18200", "listen address")
	dir := flag.String("dir", ".", "directory with the spike page and wasm")
	flag.Parse()
	http.Handle("/", http.FileServer(http.Dir(*dir)))
	http.HandleFunc("/relay", relay)
	log.Printf("spike relay on http://%s serving %s", *addr, *dir)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func relay(w http.ResponseWriter, r *http.Request) {
	target, ok := targets[r.URL.Query().Get("target")]
	if !ok {
		http.Error(w, "unknown target", http.StatusBadRequest)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"127.0.0.1:*", "localhost:*"}})
	if err != nil {
		return
	}
	start := time.Now()
	upstream, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		ws.Close(websocket.StatusInternalError, "dial failed")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := websocket.NetConn(ctx, ws, websocket.MessageBinary)
	var up, down watch
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(upstream, &up), client)
		upstream.(*net.TCPConn).CloseWrite()
	}()
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(client, &down), upstream)
		client.Close()
	}()
	wg.Wait()
	upstream.Close()
	hello := len(up.first) == 3 && up.first[0] == 0x16 && up.first[1] == 0x03
	log.Printf("relay %s: up %d bytes, down %d bytes, %v, starts with TLS handshake: %v, plaintext markers up %v down %v",
		target, up.n, down.n, time.Since(start).Round(time.Millisecond), hello, up.found, down.found)
}
