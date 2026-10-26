package main

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"sync"

	"vrcove/companion/internal/autostart"
	"vrcove/companion/internal/buildinfo"
	"vrcove/companion/internal/history"
	"vrcove/companion/internal/updater"
	"vrcove/internal/server"
)

// apiOrigin is the origin the in-app API expects. Requests reach it only from
// this app's own window, never over a network port.
const (
	apiOrigin  = "http://wails.localhost"
	desktopCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'"
)

// desktop serves the window: the bundled frontend, the website's own API
// code running locally for VRChat, and the VRCove website for everything else.
type desktop struct {
	store  *store
	api    *server.Server
	remote *httputil.ReverseProxy
	assets fs.FS
	logger *avatarLogger
	// history is the local history database; notify shows a desktop alert.
	history       *history.Store
	notify        func(id, title, body string)
	openURL       func(string) error
	tools         desktopTools
	choosePackage func() (string, error)
	chooseFolder  func() (string, error)
	toolContext   context.Context
	updater       *updater.Updater
	autostart     *autostart.Manager
	accountMu     sync.Mutex
}

func newDesktop(st *store, site *url.URL, assets fs.FS) (*desktop, error) {
	d := &desktop{store: st, assets: assets, updater: updater.New(buildinfo.Version), autostart: autostart.New()}
	d.tools.prefs = st.data.Chatbox
	if d.tools.prefs.Port == 0 {
		d.tools.prefs = chatPrefs{Port: 9000, Interval: 10}
	}
	d.remote = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(site)
			pr.Out.Host = site.Host
			// The website sees its own cookies only, and no details of this device.
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Referer")
			for name, value := range st.cookies(true) {
				pr.Out.AddCookie(&http.Cookie{Name: name, Value: value})
			}
			if pr.In.Header.Get("Origin") != "" {
				pr.Out.Header.Set("Origin", site.Scheme+"://"+site.Host)
			}
			pr.Out.Header.Set("User-Agent", userAgent())
		},
		ModifyResponse: func(resp *http.Response) error {
			keep(st, true, resp.Header)
			return nil
		},
	}
	api, err := server.New(server.Config{
		PublicOrigin: apiOrigin,
		SessionKey:   st.SessionKey,
		Contact:      site.Scheme + "://" + site.Host,
		Local:        true,
		Remote:       d.remote,
	}, nil, nil)
	if err != nil {
		return nil, err
	}
	d.api = api
	return d, nil
}

func userAgent() string { return "VRCoveDesktop/" + buildinfo.Version }

func (d *desktop) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/desktop/") {
		d.serveDesktop(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		d.serveAPI(w, r)
		return
	}
	d.serveAsset(w, r)
}

// user reports the VRChat account signed in on this device.
func (d *desktop) user() (id, name string) {
	r := httptest.NewRequest(http.MethodGet, "/api/account/device", nil)
	for n, v := range d.store.cookies(false) {
		r.AddCookie(&http.Cookie{Name: n, Value: v})
	}
	return d.api.LocalUser(r)
}

// serveAPI gives the local API the cookies a browser would send, and keeps
// the ones it sets. The window never holds them.
func (d *desktop) serveAPI(w http.ResponseWriter, r *http.Request) {
	account := strings.HasPrefix(r.URL.Path, "/api/account/")
	action := account && (r.URL.Path == "/api/account/login" || r.URL.Path == "/api/account/2fa" || r.URL.Path == "/api/account/switch" || r.URL.Path == "/api/account/logout" || r.URL.Path == "/api/account/device/remove") && r.Method == http.MethodPost
	if action {
		d.accountMu.Lock()
		defer d.accountMu.Unlock()
	}
	cookies, version, changed := d.store.localSnapshot()
	r = r.Clone(r.Context())
	if r.Body != nil && r.Body != http.NoBody && r.ContentLength <= 0 {
		// WebKitGTK custom-scheme requests omit Content-Length, leaving
		// ContentLength at 0; httputil.ReverseProxy drops the body when
		// ContentLength is 0. Buffer the body so its length is known.
		if body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)); err == nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
	}
	r.Header.Del("Cookie")
	for name, value := range cookies {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		r.Header.Set("Origin", apiOrigin)
	}
	if !account {
		d.api.APIHandler().ServeHTTP(&cookieKeeper{ResponseWriter: w, store: d.store, version: &version}, r)
		return
	}
	// Buffer local account responses so a late result cannot overwrite the
	// cookies or UI of an account selected while the request was in flight.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-changed:
			cancel()
		case <-ctx.Done():
		}
	}()
	r = r.WithContext(ctx)
	if r.URL.Path == "/api/account/live" {
		d.api.APIHandler().ServeHTTP(&cookieKeeper{ResponseWriter: w, store: d.store, version: &version}, r)
		return
	}
	response := httptest.NewRecorder()
	d.api.APIHandler().ServeHTTP(response, r)
	mutated := action && response.Code < 300 && (response.Header().Get("X-VRChat-Session-User") != "" || r.URL.Path == "/api/account/logout" || r.URL.Path == "/api/account/device/remove")
	accepted, err := keepLocal(d.store, version, response.Header(), mutated)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "Could not save accounts on this device."})
		return
	}
	if !accepted {
		writeJSON(w, 409, map[string]string{"code": "vrchat_account_changed", "error": "Your VRChat connection changed. Please try again."})
		return
	}
	_, currentVersion, _ := d.store.localSnapshot()
	if currentVersion != version {
		d.tools.usernames.Stop()
	}
	if response.Code == http.StatusOK && r.Method == http.MethodGet {
		d.learnAccountEntities(r.Context(), r.URL.Path, response.Body.Bytes())
	}
	for name, values := range response.Header() {
		w.Header()[name] = values
	}
	w.WriteHeader(response.Code)
	w.Write(response.Body.Bytes())
}

// serveAsset serves the bundled frontend; unknown paths get the app shell,
// which routes by URL.
func (d *desktop) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if info, err := fs.Stat(d.assets, name); name == "" || err != nil || info.IsDir() {
		name = "index.html"
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", desktopCSP)
	http.ServeFileFS(w, r, d.assets, name)
}

// keep stores cookies from a response in the remote or local set and removes
// them from the response.
func keep(st *store, remote bool, h http.Header) {
	lines := h.Values("Set-Cookie")
	if len(lines) == 0 {
		return
	}
	h.Del("Set-Cookie")
	st.update(func(s *state) {
		set := s.Local
		if remote {
			set = s.Remote
		}
		for _, line := range lines {
			c, err := http.ParseSetCookie(line)
			if err != nil {
				continue
			}
			if c.Value == "" || c.MaxAge < 0 {
				delete(set, c.Name)
			} else {
				set[c.Name] = c.Value
			}
		}
	})
}

type cookieKeeper struct {
	http.ResponseWriter
	store       *store
	wroteHeader bool
	version     *uint64
}

func (c *cookieKeeper) WriteHeader(status int) {
	if !c.wroteHeader {
		c.wroteHeader = true
		if c.version != nil {
			keepLocal(c.store, *c.version, c.Header(), false)
		} else {
			keep(c.store, false, c.Header())
		}
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *cookieKeeper) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(b)
}

func (c *cookieKeeper) Unwrap() http.ResponseWriter { return c.ResponseWriter }
