package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	"vrcove/companion/internal/buildinfo"
	"vrcove/internal/server"
)

// apiOrigin is the origin the in-app API expects. Requests reach it only from
// this app's own window, never over a network port.
const apiOrigin = "http://wails.localhost"

// desktop serves the window: the bundled frontend, the website's own API
// code running locally for VRChat, and the VRCove website for everything else.
type desktop struct {
	store  *store
	api    *server.Server
	remote *httputil.ReverseProxy
	assets fs.FS
	logger *avatarLogger
}

func newDesktop(st *store, site *url.URL, assets fs.FS) (*desktop, error) {
	d := &desktop{store: st, assets: assets}
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
	if r.URL.Path == "/api/desktop/logger" && d.logger != nil {
		d.logger.serve(w, r)
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
	r = r.Clone(r.Context())
	r.Header.Del("Cookie")
	for name, value := range d.store.cookies(false) {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		r.Header.Set("Origin", apiOrigin)
	}
	d.api.APIHandler().ServeHTTP(&cookieKeeper{ResponseWriter: w, store: d.store}, r)
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
}

func (c *cookieKeeper) WriteHeader(status int) {
	if !c.wroteHeader {
		c.wroteHeader = true
		keep(c.store, false, c.Header())
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
