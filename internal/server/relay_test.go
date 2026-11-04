package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeHello is enough of a TLS record header for the relay's check.
var fakeHello = []byte{0x16, 0x03, 0x01, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o'}

type relayHarness struct {
	*harness
	url     string
	account *http.Cookie
	dials   atomic.Int32
	// upstream receives the server side of each tunnel.
	upstream chan net.Conn
}

func newRelayHarness(t *testing.T) *relayHarness {
	t.Helper()
	h, _ := newAccountHarness(t)
	rh := &relayHarness{harness: h, upstream: make(chan net.Conn, 8)}
	rh.account = h.signup("relay_user", "relay passphrase")
	h.srv.relayDial = func(ctx context.Context, addr string) (net.Conn, error) {
		if addr != "api.vrchat.cloud:443" && addr != "pipeline.vrchat.cloud:443" {
			t.Errorf("relay dialed %s", addr)
		}
		rh.dials.Add(1)
		client, server := net.Pipe()
		rh.upstream <- server
		return client, nil
	}
	ts := httptest.NewServer(h.srv.Handler())
	t.Cleanup(ts.Close)
	rh.url = "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/relay"
	return rh
}

func (rh *relayHarness) dial(t *testing.T, cookie *http.Cookie, from string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{"Origin": {from}}
	if cookie != nil {
		header.Set("Cookie", cookie.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, rh.url, &websocket.DialOptions{HTTPHeader: header})
}

func (rh *relayHarness) open(t *testing.T, selector byte) (*websocket.Conn, net.Conn) {
	t.Helper()
	ws, _, err := rh.dial(t, rh.account, origin)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ws.Write(ctx, websocket.MessageBinary, []byte{selector}); err != nil {
		t.Fatal(err)
	}
	if err := ws.Write(ctx, websocket.MessageBinary, fakeHello); err != nil {
		t.Fatal(err)
	}
	select {
	case up := <-rh.upstream:
		got := make([]byte, len(fakeHello))
		if _, err := io.ReadFull(up, got); err != nil || !bytes.Equal(got, fakeHello) {
			t.Fatalf("upstream got %q, %v", got, err)
		}
		return ws, up
	case <-time.After(5 * time.Second):
		t.Fatal("relay never dialed VRChat")
	}
	return nil, nil
}

func waitRelaysClosed(t *testing.T, h *harness) {
	t.Helper()
	for range 100 {
		h.srv.relays.mu.Lock()
		open := h.srv.relays.total
		h.srv.relays.mu.Unlock()
		if open == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("relay slot was not released")
}

func TestRelayCopiesBytesBothWays(t *testing.T) {
	rh := newRelayHarness(t)
	ws, up := rh.open(t, relayToAPI)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := up.Write([]byte("server record")); err != nil {
		t.Fatal(err)
	}
	if kind, data, err := ws.Read(ctx); err != nil || kind != websocket.MessageBinary || string(data) != "server record" {
		t.Fatalf("browser got %q %v", data, err)
	}
	if err := ws.Write(ctx, websocket.MessageBinary, []byte("client record")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len("client record"))
	if _, err := io.ReadFull(up, got); err != nil || string(got) != "client record" {
		t.Fatalf("upstream got %q %v", got, err)
	}
	ws.Close(websocket.StatusNormalClosure, "")
	waitRelaysClosed(t, rh.harness)
}

func TestRelayRefusesWithoutAccountOrfromOtherSites(t *testing.T) {
	rh := newRelayHarness(t)
	if _, resp, err := rh.dial(t, nil, origin); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without an account: %v %v", resp, err)
	}
	if _, resp, err := rh.dial(t, rh.account, "https://evil.example"); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("other origin: %v %v", resp, err)
	}
	// A banned account loses access.
	if _, err := rh.srv.accounts.Exec("UPDATE accounts SET banned = 1"); err != nil {
		t.Fatal(err)
	}
	if _, resp, err := rh.dial(t, rh.account, origin); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("banned account: %v %v", resp, err)
	}
	if rh.dials.Load() != 0 {
		t.Fatal("a refused request reached VRChat")
	}
}

func TestRelayOnlyCarriesTLSToKnownTargets(t *testing.T) {
	rh := newRelayHarness(t)
	for name, frames := range map[string][][]byte{
		"unknown selector": {{9}, fakeHello},
		"host name":        {[]byte("evil.example:443"), fakeHello},
		"plaintext HTTP":   {{relayToAPI}, []byte("GET /api/1/config HTTP/1.1\r\n\r\n")},
	} {
		ws, _, err := rh.dial(t, rh.account, origin)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		for _, f := range frames {
			ws.Write(ctx, websocket.MessageBinary, f)
		}
		_, _, err = ws.Read(ctx)
		cancel()
		if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
			t.Errorf("%s: closed with %v", name, err)
		}
	}
	if rh.dials.Load() != 0 {
		t.Fatal("a refused tunnel reached VRChat")
	}
	waitRelaysClosed(t, rh.harness)
}

func TestRelayLimitsTunnelsAndBytesPerAccount(t *testing.T) {
	rh := newRelayHarness(t)
	var open []*websocket.Conn
	for range maxRelaysPerAccount {
		ws, _ := rh.open(t, relayToPipeline)
		open = append(open, ws)
	}
	if _, resp, err := rh.dial(t, rh.account, origin); err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("tunnel over the account limit: %v %v", resp, err)
	}
	for _, ws := range open {
		ws.Close(websocket.StatusNormalClosure, "")
	}
	waitRelaysClosed(t, rh.harness)

	// An account that used its byte budget has its tunnel closed.
	ws, _ := rh.open(t, relayToAPI)
	var id int64
	if err := rh.srv.accounts.QueryRow("SELECT id FROM accounts WHERE username = 'relay_user'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	// Earlier tunnels already used part of the budget. Fill the rest exactly.
	for n := relayAccountKiB; n > 0; n /= 2 {
		for rh.srv.limiter.takeN("relay-bytes:"+strconv.FormatInt(id, 10), n, relayAccountKiB, rh.srv.now()) == 0 {
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws.Write(ctx, websocket.MessageBinary, []byte("more"))
	if _, _, err := ws.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("over the byte budget: %v", err)
	}
	waitRelaysClosed(t, rh.harness)
}

func TestRelayDialsOnlyPublicAddresses(t *testing.T) {
	for addr, public := range map[string]bool{
		"104.18.26.36": true, "2606:4700::6812:1a24": true,
		"127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false, "172.16.0.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false,
		"::1": false, "fe80::1": false, "fc00::1": false, "::ffff:127.0.0.1": false,
	} {
		if got := publicIP(netip.MustParseAddr(addr)); got != public {
			t.Errorf("publicIP(%s) = %v", addr, got)
		}
	}
	// A name that resolves to loopback, as in DNS rebinding, is refused.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn, err := dialPublic(ctx, "localhost:443"); err == nil {
		conn.Close()
		t.Fatal("relay dialed a loopback address")
	}
}
