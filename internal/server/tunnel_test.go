package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestTunnelOnly(t *testing.T) {
	srv, err := New(Config{PublicOrigin: "https://vrcove.example.test", TunnelOnly: true}, nil,
		fstest.MapFS{"index.html": {Data: []byte("app")}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	fetch := func(host string, cf bool) (int, error) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
		req.Host = host
		if cf {
			req.Header.Set("CF-Connecting-IP", "198.51.100.7")
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}
	if code, err := fetch("vrcove.example.test", true); err != nil || code != 200 {
		t.Fatalf("tunnel request = %d, %v", code, err)
	}
	// Scanner-style requests: the bare IP, another hostname, or no Cloudflare
	// header. The connection is closed with no response at all.
	for _, c := range []struct {
		host string
		cf   bool
	}{{ts.Listener.Addr().String(), true}, {"other.example", true}, {"vrcove.example.test", false}} {
		if code, err := fetch(c.host, c.cf); err == nil {
			t.Errorf("host %s cf=%v answered %d", c.host, c.cf, code)
		}
	}
	// A non-local peer is refused even with the right Host and header.
	r := httptest.NewRequest(http.MethodGet, "https://vrcove.example.test/", nil)
	r.RemoteAddr = "203.0.113.5:4444"
	r.Header.Set("CF-Connecting-IP", "198.51.100.7")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusMisdirectedRequest || w.Body.Len() != 0 {
		t.Errorf("direct peer = %d %q", w.Code, w.Body)
	}
}

func TestLocalPeers(t *testing.T) {
	for addr, want := range map[string]bool{"@": true, "": true, "127.0.0.1:1": true, "[::1]:1": true, "10.0.0.1:1": false, "203.0.113.5:1": false} {
		if got := fromLocalPeer(&http.Request{RemoteAddr: addr}); got != want {
			t.Errorf("%q = %v", addr, got)
		}
	}
}
