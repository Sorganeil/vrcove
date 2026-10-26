// Package server is the VRCove HTTP backend: the avatar catalog, the image
// proxy and the VRChat account API, plus the embedded frontend.
//
// All credentials live only in request memory. Never log request bodies,
// cookies or upstream headers.
package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Config is the runtime configuration. SessionKey, Contact and the database
// are optional; account features stay disabled without them.
type Config struct {
	// PublicOrigin is the origin browsers use, e.g. https://vrcove.example.
	// Behind a tunnel the server only sees plain HTTP, so the origin check and
	// the Secure cookie flag are derived from this rather than the request.
	PublicOrigin string
	SessionKey   []byte
	Contact      string
	// TunnelOnly drops every request that did not arrive through cloudflared
	// on this host for PublicOrigin's hostname, so scanners probing the
	// server directly get nothing to fingerprint.
	TunnelOnly bool
	// Accounts is the writable VRCove accounts database and AccountKey its
	// 32-byte link-encryption key. Without them VRCove accounts and reviews
	// are disabled.
	Accounts   *sql.DB
	AccountKey []byte
	// TurnstileSiteKey and TurnstileSecret enable Cloudflare Turnstile on
	// registration. Both must be set; without them signup has no challenge.
	TurnstileSiteKey string
	TurnstileSecret  string
	// Logger is an optional, separately authenticated ingestion service. The
	// desktop receives only its API key, never VRChat credentials.
	Logger *LoggerConfig
}

type Server struct {
	previews previewStore
	recent   recentCache
	cfg      Config
	secure   bool
	host     string
	db       *sql.DB
	static   fs.FS
	limiter  *limiter
	backoff  backoff
	images   imageCache
	// accounts, keys and hashSlots back VRCove's own accounts.
	accounts  *sql.DB
	keys      accountKeys
	hashSlots chan struct{}
	// pictureSlots bounds concurrent avatar picture decodes.
	pictureSlots chan struct{}
	loggerSlot   chan struct{}
	sitemaps     sitemapStore
	// imageSlots bounds concurrent image fetches from VRChat.
	imageSlots chan struct{}
	// Client sends every upstream request. Redirects are never followed
	// automatically; the image proxy re-validates each hop itself.
	Client *http.Client
	now    func() time.Time
	// live counts open live-notification relays; dialPipeline opens VRChat's
	// pipeline and is replaced in tests.
	live         liveStreams
	dialPipeline func(ctx context.Context, auth string) (pipelineConn, error)
}

func New(cfg Config, db *sql.DB, static fs.FS) (*Server, error) {
	u, err := url.Parse(cfg.PublicOrigin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return nil, errors.New("PUBLIC_ORIGIN must be an origin such as https://vrcove.example")
	}
	cfg.PublicOrigin = u.Scheme + "://" + u.Host
	cfg.Contact = strings.TrimSpace(cfg.Contact)
	if cfg.Contact != "" && !validContact(cfg.Contact) {
		return nil, errors.New("VRC_CONTACT must be an email address or http(s) URL in printable ASCII, without parentheses")
	}
	if cfg.SessionKey != nil && len(cfg.SessionKey) != 32 {
		return nil, errors.New("SESSION_KEY must be a base64-encoded 32-byte key")
	}
	if cfg.Logger != nil && (cfg.Logger.Catalog == nil || cfg.Accounts == nil || len(cfg.SessionKey) != 32 || cfg.Contact == "") {
		return nil, errors.New("logger ingestion needs accounts, session encryption, a writable catalog and operator contact")
	}
	var keys accountKeys
	if cfg.Accounts != nil {
		if len(cfg.AccountKey) != 32 {
			return nil, errors.New("ACCOUNT_KEY must be a base64-encoded 32-byte key")
		}
		if keys, err = deriveAccountKeys(cfg.AccountKey); err != nil {
			return nil, err
		}
	}
	srv := &Server{
		cfg: cfg, secure: u.Scheme == "https", host: strings.ToLower(u.Host), db: db, static: static, limiter: newLimiter(),
		accounts: cfg.Accounts, keys: keys, hashSlots: make(chan struct{}, hashSlots), pictureSlots: make(chan struct{}, pictureSlots), loggerSlot: make(chan struct{}, 1), imageSlots: make(chan struct{}, 16),
		Client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		now:    time.Now,
	}
	srv.dialPipeline = srv.dialVRChatPipeline
	return srv, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.handleAPI)
	mux.HandleFunc("/", s.handleStatic)
	if !s.cfg.TunnelOnly {
		return mux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fromLocalPeer(r) || !strings.EqualFold(r.Host, s.host) || r.Header.Get("CF-Connecting-IP") == "" {
			drop(w)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// drop closes the connection without a response, so nothing identifies the
// service to a direct visitor.
func drop(w http.ResponseWriter) {
	if hj, ok := w.(http.Hijacker); ok {
		if conn, _, err := hj.Hijack(); err == nil {
			conn.Close()
			return
		}
	}
	w.WriteHeader(http.StatusMisdirectedRequest)
}

// fromLocalPeer reports whether the connection came from this host: a Unix
// socket or a loopback address, i.e. cloudflared.
func fromLocalPeer(r *http.Request) bool {
	if r.RemoteAddr == "" || r.RemoteAddr == "@" {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// httpError carries a client-safe message and status. Any other error becomes
// a generic 503 so upstream details are never echoed.
type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func errStatus(status int, msg string) error { return &httpError{status, msg} }

func writeJSON(w http.ResponseWriter, status int, data any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	var body []byte
	if err := enc.Encode(data); err != nil {
		status, body = http.StatusServiceUnavailable, []byte(`{"error":"The request could not be completed. Try again in a moment."}`)
	} else {
		body = buf.Bytes()
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	w.Write(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeFailure(w http.ResponseWriter, err error) {
	var he *httpError
	if errors.As(err, &he) && he.status >= 400 && he.status <= 599 {
		writeError(w, he.status, he.msg)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "The request could not be completed. Try again in a moment.")
}

// readBounded reads at most max bytes and fails with 413 beyond that, whether
// or not the sender declared a Content-Length.
func readBounded(r io.Reader, max int64, msg string) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errStatus(http.StatusRequestEntityTooLarge, msg)
	}
	return data, nil
}

func requestText(r *http.Request, max int64) ([]byte, error) {
	if r.ContentLength > max {
		return nil, errStatus(http.StatusRequestEntityTooLarge, "Request too large.")
	}
	body, err := readBounded(r.Body, max, "Request too large.")
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(body) {
		return nil, errStatus(http.StatusBadRequest, "Request body must be valid UTF-8.")
	}
	return bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF}), nil
}

func requestJSON(r *http.Request) (map[string]any, error) {
	body, err := requestText(r, 8192)
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&parsed) != nil || parsed == nil || dec.Decode(new(any)) != io.EOF {
		return nil, errStatus(http.StatusBadRequest, "Request body must be a JSON object.")
	}
	return parsed, nil
}

// clientIP trusts CF-Connecting-IP only from a local peer, i.e. cloudflared
// on the same host. Direct connections cannot spoof it.
func clientIP(r *http.Request) string {
	if fromLocalPeer(r) {
		if cf := r.Header.Get("CF-Connecting-IP"); cf != "" {
			return cf
		}
		return "local"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host
}

// clientKey identifies the client for rate limiting; see rateKey.
func clientKey(r *http.Request) string { return rateKey(clientIP(r)) }

// queryInt mirrors the old clamp(Number(value)||0) parsing of numeric query parameters.
func queryInt(v string, max int) int {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f != f || f < 0 {
		return 0
	}
	if f > float64(max) {
		return max
	}
	return int(f)
}

// contentSecurityPolicy allows this site, Turnstile on the signup form, and
// inline scripts carrying nonce when one is given.
func contentSecurityPolicy(nonce string) string {
	script := "script-src 'self' https://challenges.cloudflare.com"
	if nonce != "" {
		script += " 'nonce-" + nonce + "'"
	}
	return "default-src 'self'; " + script + "; frame-src https://challenges.cloudflare.com; " +
		"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; worker-src 'self'; manifest-src 'self'; " +
		"form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'"
}

func newNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", contentSecurityPolicy(""))
	if s.secure {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "robots.txt" {
		s.robots(w)
		return
	}
	if s.sitemap(w, r, name) {
		return
	}
	if name == "" || name == ".gitkeep" {
		name = "index.html"
	}
	data, err := fs.ReadFile(s.static, name)
	if err != nil {
		// The frontend routes with the URL hash, so every unknown path gets the app shell.
		name = "index.html"
		if data, err = fs.ReadFile(s.static, name); err != nil {
			http.Error(w, "Frontend not built. Run `npm run build` before `go build`.", http.StatusNotFound)
			return
		}
	}
	if name == "index.html" {
		data = s.previewHTML(r, data)
		// Cloudflare copies this nonce onto the security scripts it injects
		// into HTML, such as bot detection, so they run without 'unsafe-inline'.
		h.Set("Content-Security-Policy", contentSecurityPolicy(newNonce()))
	}
	if strings.HasSuffix(name, ".webmanifest") {
		h.Set("Content-Type", "application/manifest+json")
	}
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
