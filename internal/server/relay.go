package server

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// The website's VRChat client runs in a browser worker and dials TLS to VRChat
// itself, through a WebSocket to /api/relay. The relay copies bytes to one of
// two fixed VRChat hosts and never parses them, so it only carries ciphertext.
// It keeps no credentials and logs only the account, byte counts and why the
// relay closed.

const (
	relayToAPI      = 1
	relayToPipeline = 2
	maxRelays       = 1000
	// maxRelaysPerAccount allows a few tabs, each with an API and a pipeline
	// connection. maxRelaysPerAddress covers a household behind one address.
	maxRelaysPerAccount = 4
	maxRelaysPerAddress = 8
	// relayConnects caps new relays per account in the limiter's window.
	relayConnects = 120
	// relayBytes caps one relay. relayAccountKiB caps an account's relays
	// together in the limiter's window (ten minutes).
	relayBytes      = 64 << 20
	relayAccountKiB = 256 << 10
	// relayFrame is the largest message a browser may send. TLS records are
	// at most about 16 KiB.
	relayFrame = 64 << 10
	// relayIdle closes a relay with no bytes either way. The client keeps a
	// quiet pipeline open with its own WebSocket pings inside the tunnel.
	relayIdle     = 3 * time.Minute
	relayLifetime = time.Hour
	// relayPing keeps the browser's WebSocket open under Cloudflare's
	// 100-second idle limit.
	relayPing      = 30 * time.Second
	relayRecheck   = 5 * time.Minute
	relayHandshake = 10 * time.Second
)

// relayTargets are the only destinations. The browser picks one with a
// one-byte selector and never names a host.
var relayTargets = map[byte]string{relayToAPI: "api.vrchat.cloud:443", relayToPipeline: "pipeline.vrchat.cloud:443"}

// dialPublic resolves addr on the server and connects only to a public
// address, so DNS rebinding cannot point the relay at the internal network.
func dialPublic(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || !publicIP(ip) {
			return errors.New("relay target is not a public address")
		}
		return nil
	}}
	return d.DialContext(ctx, "tcp", addr)
}

var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !sharedAddressSpace.Contains(ip)
}

// relay opens one tunnel for a signed-in VRCove account.
func (s *Server) relay(w http.ResponseWriter, r *http.Request) error {
	if s.accounts == nil {
		return errStatus(http.StatusServiceUnavailable, "The VRChat relay needs VRCove accounts.")
	}
	if r.Header.Get("Origin") != s.cfg.PublicOrigin {
		return errStatus(http.StatusForbidden, "Request origin did not match.")
	}
	acct, err := s.readAccount(r)
	if err != nil {
		return err
	}
	if acct == nil {
		return errStatus(http.StatusUnauthorized, "Sign in to your VRCove account.")
	}
	id := strconv.FormatInt(acct.ID, 10)
	if wait := s.limiter.allowN("relay-connect:"+id, relayConnects, s.now()); wait > 0 {
		return errStatus(http.StatusTooManyRequests, "Too many VRChat connections. Try again later.")
	}
	slots := []streamSlot{{"account:" + id, maxRelaysPerAccount}, {"address:" + clientKey(r), maxRelaysPerAddress}}
	if !s.relays.acquire(maxRelays, slots...) {
		return errStatus(http.StatusTooManyRequests, "Too many VRChat connections are open. Close other VRCove tabs and try again.")
	}
	defer s.relays.release(slots...)
	// The tunnel outlives the server's normal request timeouts.
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Time{})
	rc.SetWriteDeadline(time.Time{})
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{s.host}})
	if err != nil {
		return nil
	}
	ws.SetReadLimit(relayFrame)
	start := s.now()
	var up, down atomic.Int64
	target, reason := s.pumpRelay(r, ws, id, &up, &down)
	log.Printf("relay account=%s target=%s up=%d down=%d duration=%s close=%q", id, target, up.Load(), down.Load(), s.now().Sub(start).Round(time.Second), reason)
	return nil
}

// pumpRelay runs one tunnel until either side ends it or a limit is reached.
// It returns the target host and why the tunnel closed.
func (s *Server) pumpRelay(r *http.Request, ws *websocket.Conn, account string, up, down *atomic.Int64) (string, string) {
	ctx, cancel := context.WithTimeout(r.Context(), relayLifetime)
	defer cancel()
	refuse := func(code websocket.StatusCode, reason string) (string, string) {
		ws.Close(code, reason)
		return "", reason
	}
	// The first message picks the destination. The second must start a TLS
	// handshake, so the relay cannot carry plaintext to VRChat.
	start, stop := context.WithTimeout(ctx, relayHandshake)
	kind, selector, err := ws.Read(start)
	if err != nil || kind != websocket.MessageBinary || len(selector) != 1 || relayTargets[selector[0]] == "" {
		stop()
		return refuse(websocket.StatusPolicyViolation, "unknown target")
	}
	target := relayTargets[selector[0]]
	host, _, _ := net.SplitHostPort(target)
	kind, hello, err := ws.Read(start)
	stop()
	if err != nil || kind != websocket.MessageBinary || len(hello) < 3 || hello[0] != 0x16 || hello[1] != 0x03 {
		ws.Close(websocket.StatusPolicyViolation, "not TLS")
		return host, "not TLS"
	}
	upstream, err := s.relayDial(ctx, target)
	if err != nil {
		ws.Close(websocket.StatusTryAgainLater, "VRChat unreachable")
		return host, "dial failed"
	}
	defer upstream.Close()

	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	done := make(chan string, 2)
	finish := func(reason string) {
		select {
		case done <- reason:
		default:
		}
		cancel()
	}
	// charge counts bytes against this tunnel and the account's window.
	var total atomic.Int64
	charge := func(n int) string {
		last.Store(time.Now().UnixNano())
		if total.Add(int64(n)) > relayBytes {
			return "byte limit"
		}
		if s.limiter.takeN("relay-bytes:"+account, max(1, n>>10), relayAccountKiB, s.now()) > 0 {
			return "account byte limit"
		}
		return ""
	}
	if reason := charge(len(hello)); reason != "" {
		ws.Close(websocket.StatusPolicyViolation, reason)
		return host, reason
	}
	if _, err := upstream.Write(hello); err != nil {
		ws.Close(websocket.StatusTryAgainLater, "VRChat closed")
		return host, "upstream closed"
	}
	up.Add(int64(len(hello)))
	go func() {
		for {
			kind, data, err := ws.Read(ctx)
			if err != nil {
				finish("client closed")
				return
			}
			if kind != websocket.MessageBinary {
				finish("text message")
				return
			}
			if reason := charge(len(data)); reason != "" {
				finish(reason)
				return
			}
			if _, err := upstream.Write(data); err != nil {
				finish("upstream closed")
				return
			}
			up.Add(int64(len(data)))
		}
	}()
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := upstream.Read(buf)
			if n > 0 {
				if reason := charge(n); reason != "" {
					finish(reason)
					return
				}
				if ws.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
					finish("client closed")
					return
				}
				down.Add(int64(n))
			}
			if err != nil {
				finish("upstream closed")
				return
			}
		}
	}()
	ping, recheck := time.NewTicker(relayPing), time.NewTicker(relayRecheck)
	defer ping.Stop()
	defer recheck.Stop()
	reason := ""
	for reason == "" {
		select {
		case reason = <-done:
		case <-ctx.Done():
			// A side that ended the tunnel also cancelled ctx. Prefer its reason.
			select {
			case reason = <-done:
			default:
				reason = "lifetime"
				if r.Context().Err() != nil {
					reason = "client closed"
				}
			}
		case <-ping.C:
			if time.Since(time.Unix(0, last.Load())) > relayIdle {
				reason = "idle"
				break
			}
			pctx, pstop := context.WithTimeout(ctx, relayPing)
			if ws.Ping(pctx) != nil {
				reason = "client gone"
			}
			pstop()
		case <-recheck.C:
			// Signing out, a ban or a suspension ends open relays too.
			if acct, err := s.readAccount(r); err != nil || acct == nil {
				reason = "signed out"
			}
		}
	}
	code := websocket.StatusNormalClosure
	if reason != "client closed" && reason != "upstream closed" {
		code = websocket.StatusPolicyViolation
	}
	ws.Close(code, reason)
	return host, reason
}
