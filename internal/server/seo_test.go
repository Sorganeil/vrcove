package server

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func seoHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.srv.static = fstest.MapFS{"index.html": {Data: []byte(`<!doctype html><html><head><meta name="robots" content="noindex, nofollow"><meta name="description" content="old"><title>Old</title></head><body></body></html>`)}}
	return h
}

func TestPageMetaIndexing(t *testing.T) {
	h := seoHarness(t)
	for _, tc := range []struct {
		path, title, robots, canonical string
	}{
		{"/", homeTitle, "index, follow", origin + "/"},
		{"/avatars", "VRChat Avatar Search | VRCove", "index, follow", origin + "/avatars"},
		{"/friends", homeTitle, "noindex", origin + "/friends"},
		{"/my-groups", homeTitle, "noindex", origin + "/my-groups"},
		{"/cove/someone", "VRCove profile | VRCove", "noindex", origin + "/cove/someone"},
		{"/no/such/page", homeTitle, "noindex", origin + "/"},
	} {
		body := h.do(httptest.NewRequest("GET", "http://localhost"+tc.path, nil)).Body.String()
		for _, want := range []string{"<title>" + tc.title + "</title>", `name="robots" content="` + tc.robots, `rel="canonical" href="` + tc.canonical + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q: %s", tc.path, want, body)
			}
		}
		if strings.Contains(body, "nofollow") || strings.Contains(body, `content="old"`) || strings.Count(body, `name="description"`) != 1 {
			t.Errorf("%s kept stale metadata: %s", tc.path, body)
		}
	}
}

func TestUserPagesAreNotIndexed(t *testing.T) {
	h := seoHarness(t)
	body := h.do(httptest.NewRequest("GET", "http://localhost/user/usr_00000000-0000-0000-0000-000000000001", nil)).Body.String()
	if !strings.Contains(body, `name="robots" content="noindex`) || !strings.Contains(body, "<title>VRChat User | VRCove</title>") {
		t.Fatal(body)
	}
}

func TestRobotsAndSitemaps(t *testing.T) {
	h := seoHarness(t)
	var databasePath string
	if err := h.srv.db.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&databasePath); err != nil {
		t.Fatal(err)
	}
	writable, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer writable.Close()
	id := "avtr_00000000-0000-0000-0000-00000000abcd"
	if _, err := writable.Exec("INSERT INTO catalog(rowid,id,updated,data) VALUES (50001,?,'2024-05-06T07:08:09.000Z','{}')", id); err != nil {
		t.Fatal(err)
	}
	robots := h.do(httptest.NewRequest("GET", "/robots.txt", nil)).Body.String()
	if !strings.Contains(robots, "Disallow: /api/") || !strings.Contains(robots, "Sitemap: "+origin+"/sitemap.xml") {
		t.Fatal(robots)
	}
	index := h.do(httptest.NewRequest("GET", "/sitemap.xml", nil))
	if index.Header().Get("Content-Type") != "application/xml; charset=utf-8" || !strings.Contains(index.Body.String(), origin+"/sitemaps/pages.xml") || !strings.Contains(index.Body.String(), origin+"/sitemaps/avatars-2.xml") {
		t.Fatal(index.Body.String())
	}
	pages := h.do(httptest.NewRequest("GET", "/sitemaps/pages.xml", nil)).Body.String()
	if !strings.Contains(pages, origin+"/avatars</loc>") || strings.Contains(pages, "/user/") || strings.Contains(pages, "friends") {
		t.Fatal(pages)
	}
	avatars := h.do(httptest.NewRequest("GET", "/sitemaps/avatars-2.xml", nil)).Body.String()
	if !strings.Contains(avatars, "<loc>"+origin+"/avatar/"+id+"</loc><lastmod>2024-05-06</lastmod>") {
		t.Fatal(avatars)
	}
	if w := h.do(httptest.NewRequest("GET", "/sitemaps/avatars-9.xml", nil)); w.Code != 404 {
		t.Fatalf("empty sitemap = %d", w.Code)
	}
}

func TestLegacyAccountPrefix(t *testing.T) {
	h := newHarness(t)
	legacy, current := h.do(get("/veil/me")), h.do(get("/cove/me"))
	if legacy.Code == 404 || legacy.Code != current.Code || legacy.Body.String() != current.Body.String() {
		t.Fatalf("legacy prefix = %d, current = %d", legacy.Code, current.Code)
	}
}

func TestManifestContentType(t *testing.T) {
	h := newHarness(t)
	h.srv.static = fstest.MapFS{"index.html": {Data: []byte("<title>x</title>")}, "manifest.webmanifest": {Data: []byte(`{"name":"VRCove"}`)}}
	w := h.do(httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/manifest+json" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("%d %q %q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"))
	}
}
