package server

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A burst of unknown images never opens more than the slot count of
// concurrent fetches to VRChat.
func TestImageFetchesAreBounded(t *testing.T) {
	h := newHarness(t)
	var active, peak atomic.Int32
	h.upstream = func(*http.Request) (*http.Response, error) {
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return respond(200, "image/png", []byte("\x89PNG\r\n\x1a\n")), nil
	}
	var wg sync.WaitGroup
	for i := range 60 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := h.do(imageRequest(fmt.Sprintf("https://assets.vrchat.com/%d.png", i))); w.Code != 200 {
				t.Errorf("image %d = %d", i, w.Code)
			}
		}()
	}
	wg.Wait()
	if p := peak.Load(); p > int32(cap(h.srv.imageSlots)) || p < 2 {
		t.Errorf("peak concurrent fetches = %d, limit %d", p, cap(h.srv.imageSlots))
	}
}

// Sitemaps are built once, kept gzip-compressed, and served compressed to
// clients that accept it.
func TestSitemapsAreCached(t *testing.T) {
	h := seoHarness(t)
	var path string
	if err := h.srv.db.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	rw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	first, second := "avtr_00000000-0000-0000-0000-000000000701", "avtr_00000000-0000-0000-0000-000000000702"
	if _, err := rw.Exec("INSERT INTO catalog(rowid,id,updated,data) VALUES (1,?,'2024-05-06T07:08:09.000Z','{}')", first); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/sitemaps/avatars-1.xml", nil)
	r.Header.Set("Accept-Encoding", "gzip, br")
	w := h.do(r)
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("gzip sitemap = %d %v", w.Code, w.Header())
	}
	zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(zr)
	if !strings.Contains(string(body), first) {
		t.Fatalf("sitemap = %s", body)
	}
	// A later catalog change shows up only once the cached copy expires.
	if _, err := rw.Exec("INSERT INTO catalog(rowid,id,updated,data) VALUES (2,?,'2024-05-06T07:08:09.000Z','{}')", second); err != nil {
		t.Fatal(err)
	}
	if plain := h.do(httptest.NewRequest("GET", "/sitemaps/avatars-1.xml", nil)).Body.String(); !strings.Contains(plain, first) || strings.Contains(plain, second) {
		t.Errorf("cached plain sitemap = %s", plain)
	}
	h.srv.now = func() time.Time { return time.Now().Add(sitemapTTL + time.Minute) }
	if fresh := h.do(httptest.NewRequest("GET", "/sitemaps/avatars-1.xml", nil)).Body.String(); !strings.Contains(fresh, second) {
		t.Errorf("expired sitemap was not rebuilt: %s", fresh)
	}
}
