package server

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	homeTitle       = "VRCove: VRChat Avatar Search & Web Client for Phone and PC"
	homeDescription = "Search public VRChat avatars by name, creator or avatar ID for PC and Quest, and use VRChat friends, worlds and groups from any browser. Free, with no ads or tracking."
	sitemapSize     = 50000
)

// Public pages that work without a VRChat session and are worth indexing.
var indexedSections = map[string]linkPreview{
	"avatars":   {"VRChat Avatar Search: Find PC & Quest Avatars | VRCove", "Search public VRChat avatars by name, creator, tag or avatar ID. Filter PC and Quest avatars by performance rank and find Quest fallbacks. Free, with no account, ads or tracking.", ""},
	"worlds":    {"VRChat World Search & Reviews | VRCove", "Browse VRChat worlds and read community reviews from your phone or PC. No ads or analytics.", ""},
	"groups":    {"VRChat Group Search & Reviews | VRCove", "Discover VRChat groups and read community reviews from your phone or PC. No ads or analytics.", ""},
	"users":     {"Find VRChat Users by Name | VRCove", "Find VRChat players by name from your phone or PC. No ads or analytics.", ""},
	"search":    {"Search VRChat Avatars, Worlds, Users & Groups | VRCove", "Search VRChat avatars, people, worlds and groups in one place, on phone or PC. No ads, analytics or tracking.", ""},
	"donations": {"Support VRCove", "Help keep VRCove independent, private and free of ads.", ""},
	"companion": {"VRChat Avatar Logger for Windows & Linux | VRCove Companion", "A small Windows and Linux app that collects public VRChat avatar IDs while you play and adds new ones to VRCove's avatar search.", ""},
	"about":     {"About VRCove: VRChat in Your Browser, on Phone or PC", "VRCove is a free VRChat web client and avatar search for phone and PC: friends, worlds, groups, favorites, avatar switching, reviews and collections, with no ads or tracking.", ""},
}

// Personal pages need a session or describe a person, so crawlers skip them.
var personalSections = map[string]bool{"friends": true, "favorites": true, "bookmarks": true, "profile": true,
	"notifications": true, "settings": true, "uploads": true, "account": true, "collections": true, "my-groups": true, "admin": true}

var (
	titleTag       = regexp.MustCompile(`(?is)<title>.*?</title>`)
	descriptionTag = regexp.MustCompile(`(?is)<meta\s+name="description"[^>]*>`)
	robotsTag      = regexp.MustCompile(`(?is)<meta\s+name="robots"[^>]*>`)
	structuredData = regexp.MustCompile(`(?is)<script type="application/ld\+json">.*?</script>`)
)

// pageMeta describes how a path should appear to search engines.
type pageMeta struct {
	Title, Description, Canonical string
	Index                         bool
}

func (s *Server) pageMeta(path string, p linkPreview, canonical string) pageMeta {
	slug := strings.Trim(path, "/")
	if slug == "" {
		return pageMeta{homeTitle, homeDescription, s.cfg.PublicOrigin + "/", true}
	}
	if kind, _, ok := detailRoute(path); ok {
		name := strings.TrimSuffix(p.Title, " · VRCove")
		label := map[string]string{"avatar": "VRChat Avatar", "world": "VRChat World", "user": "VRChat User", "group": "VRChat Group"}[kind]
		title := label + " | VRCove"
		if !strings.HasPrefix(name, "VRChat ") && name != "" {
			title = name + " | " + title
		}
		// Profiles are shareable but never indexed, to keep people out of search results.
		return pageMeta{title, p.Description, canonical, kind != "user"}
	}
	if _, _, ok := coveRoute(path); ok {
		// Shareable by link, but people's pages stay out of search results.
		return pageMeta{strings.TrimSuffix(p.Title, " · VRCove") + " | VRCove", p.Description, canonical, false}
	}
	if m, ok := indexedSections[slug]; ok {
		return pageMeta{m.Title, m.Description, s.cfg.PublicOrigin + "/" + slug, true}
	}
	m := pageMeta{homeTitle, homeDescription, s.cfg.PublicOrigin + "/", false}
	if personalSections[slug] {
		m.Canonical = s.cfg.PublicOrigin + "/" + slug
	}
	return m
}

// applyPageMeta rewrites the document title, description and robots policy.
func applyPageMeta(doc string, m pageMeta) string {
	title, description := previewText(m.Title, 120), previewText(m.Description, 300)
	robots := "index, follow, max-image-preview:large"
	if !m.Index {
		robots = "noindex, follow"
	}
	doc = titleTag.ReplaceAllLiteralString(doc, "<title>"+title+"</title>")
	doc = descriptionTag.ReplaceAllLiteralString(doc, "")
	doc = robotsTag.ReplaceAllLiteralString(doc, "")
	return strings.Replace(doc, "</head>", `<meta name="description" content="`+description+`"><meta name="robots" content="`+robots+`">`+"</head>", 1)
}

func (s *Server) robots(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nDisallow: /api/\nAllow: /api/image\nAllow: /api/cove/picture\n\nSitemap: %s/sitemap.xml\n", s.cfg.PublicOrigin)
}

var avatarSitemap = regexp.MustCompile(`^sitemaps/avatars-([1-9][0-9]{0,3})\.xml$`)

// sitemapStore keeps each generated sitemap gzip-compressed for a few hours.
// An avatar sitemap is about 6 MB of XML, which Cloudflare doesn't cache, so
// rebuilding it for every request would be slow for search engines and cheap
// to abuse. Builds run one at a time.
type sitemapStore struct {
	sync.Mutex
	files map[string]sitemapFile
}

type sitemapFile struct {
	gz    []byte // nil: no such sitemap
	until time.Time
}

const sitemapTTL = 6 * time.Hour

// sitemap serves a sitemap index, the public pages, and the avatar catalog in
// fixed rowid ranges so each file is a cheap range scan.
func (s *Server) sitemap(w http.ResponseWriter, r *http.Request, name string) bool {
	if name != "sitemap.xml" && name != "sitemaps/pages.xml" && !avatarSitemap.MatchString(name) {
		return false
	}
	s.sitemaps.Lock()
	file, ok := s.sitemaps.files[name]
	if !ok || !s.now().Before(file.until) {
		body, found, err := s.buildSitemap(r, name)
		if err != nil {
			s.sitemaps.Unlock()
			http.Error(w, "Sitemap unavailable", http.StatusServiceUnavailable)
			return true
		}
		file = sitemapFile{until: s.now().Add(sitemapTTL)}
		if found {
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			zw.Write(body)
			zw.Close()
			file.gz = buf.Bytes()
		}
		if s.sitemaps.files == nil {
			s.sitemaps.files = map[string]sitemapFile{}
		}
		s.sitemaps.files[name] = file
	}
	s.sitemaps.Unlock()
	if file.gz == nil {
		http.NotFound(w, r)
		return true
	}
	h := w.Header()
	h.Set("Content-Type", "application/xml; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=86400")
	h.Set("Vary", "Accept-Encoding")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		h.Set("Content-Length", strconv.Itoa(len(file.gz)))
		w.Write(file.gz)
		return true
	}
	zr, err := gzip.NewReader(bytes.NewReader(file.gz))
	if err != nil {
		http.Error(w, "Sitemap unavailable", http.StatusServiceUnavailable)
		return true
	}
	io.Copy(w, zr)
	return true
}

// buildSitemap renders one sitemap; found is false when it has no entries.
func (s *Server) buildSitemap(r *http.Request, name string) (body []byte, found bool, err error) {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	origin := html.EscapeString(s.cfg.PublicOrigin)
	switch {
	case name == "sitemap.xml":
		var last int
		if err := s.db.QueryRowContext(r.Context(), "SELECT coalesce(max(rowid), 0) FROM catalog").Scan(&last); err != nil {
			return nil, false, err
		}
		buf.WriteString(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		buf.WriteString(`<sitemap><loc>` + origin + `/sitemaps/pages.xml</loc></sitemap>`)
		for i := 1; i <= (last+sitemapSize-1)/sitemapSize; i++ {
			buf.WriteString(`<sitemap><loc>` + origin + `/sitemaps/avatars-` + strconv.Itoa(i) + `.xml</loc></sitemap>`)
		}
		buf.WriteString(`</sitemapindex>`)
	case name == "sitemaps/pages.xml":
		buf.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		buf.WriteString(`<url><loc>` + origin + `/</loc><priority>1.0</priority></url>`)
		for _, slug := range []string{"avatars", "search", "worlds", "groups", "users", "about", "companion", "donations"} {
			buf.WriteString(`<url><loc>` + origin + `/` + slug + `</loc></url>`)
		}
		buf.WriteString(`</urlset>`)
	default:
		n, _ := strconv.Atoi(avatarSitemap.FindStringSubmatch(name)[1])
		rows, err := s.db.QueryContext(r.Context(), "SELECT id, coalesce(name, ''), coalesce(updated, ''), coalesce(data, '{}') FROM catalog WHERE rowid > ? AND rowid <= ? ORDER BY rowid",
			(n-1)*sitemapSize, n*sitemapSize)
		if err != nil {
			return nil, false, err
		}
		defer rows.Close()
		buf.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		// A range with rows stays a valid (possibly empty) sitemap, because the
		// index lists it; only a range past the catalog's end is missing.
		scanned := 0
		for rows.Next() {
			var id, name, updated, data string
			if rows.Scan(&id, &name, &updated, &data) != nil {
				continue
			}
			scanned++
			// Only pages search engines may index: no adult or link-spam avatars.
			if !avatarID.MatchString(id) || !scanAvatar(id, name, "", updated, "", false, false, data).indexable() {
				continue
			}
			buf.WriteString(`<url><loc>` + origin + `/avatar/` + id + `</loc>`)
			if t, err := time.Parse(time.RFC3339, updated); err == nil {
				buf.WriteString(`<lastmod>` + t.UTC().Format("2006-01-02") + `</lastmod>`)
			}
			buf.WriteString(`</url>`)
		}
		if err := rows.Err(); err != nil {
			return nil, false, err
		}
		if scanned == 0 {
			return nil, false, nil
		}
		buf.WriteString(`</urlset>`)
	}
	return buf.Bytes(), true, nil
}
