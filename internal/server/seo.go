package server

import (
	"bytes"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	homeTitle       = "VRCove | Private VRChat Web Client with Avatar Search"
	homeDescription = "VRChat avatar search, friends and reviews from your phone or PC. Great for Quest players without a PC. No ads, analytics or tracking."
	sitemapSize     = 50000
)

// Public pages that work without a VRChat session and are worth indexing.
var indexedSections = map[string]linkPreview{
	"avatars":   {"VRChat Avatar Search | VRCove", "Search PC and Quest VRChat avatars by name, author, platform and performance. Works on phone and PC with no account, ads or tracking.", ""},
	"worlds":    {"VRChat Worlds | VRCove", "Browse VRChat worlds and read community reviews from your phone or PC. No ads or analytics.", ""},
	"groups":    {"VRChat Groups | VRCove", "Discover VRChat groups and read community reviews from your phone or PC. No ads or analytics.", ""},
	"users":     {"Find VRChat Users | VRCove", "Find VRChat players by name from your phone or PC. No ads or analytics.", ""},
	"search":    {"Search VRChat | VRCove", "Search VRChat avatars, people, worlds and groups in one place, on phone or PC. No ads, analytics or tracking.", ""},
	"donations": {"Support VRCove", "Help keep VRCove independent, private and free of ads.", ""},
}

// Personal pages need a session or describe a person, so crawlers skip them.
var personalSections = map[string]bool{"friends": true, "favorites": true, "bookmarks": true, "profile": true,
	"notifications": true, "settings": true, "uploads": true, "account": true, "collections": true, "my-groups": true}

var (
	titleTag       = regexp.MustCompile(`(?is)<title>.*?</title>`)
	descriptionTag = regexp.MustCompile(`(?is)<meta\s+name="description"[^>]*>`)
	robotsTag      = regexp.MustCompile(`(?is)<meta\s+name="robots"[^>]*>`)
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

// sitemap serves a sitemap index, the public pages, and the avatar catalog in
// fixed rowid ranges so each file is a cheap range scan.
func (s *Server) sitemap(w http.ResponseWriter, r *http.Request, name string) bool {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	origin := html.EscapeString(s.cfg.PublicOrigin)
	switch {
	case name == "sitemap.xml":
		var last int
		if err := s.db.QueryRowContext(r.Context(), "SELECT coalesce(max(rowid), 0) FROM catalog").Scan(&last); err != nil {
			http.Error(w, "Sitemap unavailable", http.StatusServiceUnavailable)
			return true
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
		for _, slug := range []string{"avatars", "search", "worlds", "groups", "users", "donations"} {
			buf.WriteString(`<url><loc>` + origin + `/` + slug + `</loc></url>`)
		}
		buf.WriteString(`</urlset>`)
	default:
		m := avatarSitemap.FindStringSubmatch(name)
		if m == nil {
			return false
		}
		n, _ := strconv.Atoi(m[1])
		rows, err := s.db.QueryContext(r.Context(), "SELECT id, coalesce(updated, '') FROM catalog WHERE rowid > ? AND rowid <= ? ORDER BY rowid",
			(n-1)*sitemapSize, n*sitemapSize)
		if err != nil {
			http.Error(w, "Sitemap unavailable", http.StatusServiceUnavailable)
			return true
		}
		defer rows.Close()
		buf.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		count := 0
		for rows.Next() {
			var id, updated string
			if rows.Scan(&id, &updated) != nil || !avatarID.MatchString(id) {
				continue
			}
			count++
			buf.WriteString(`<url><loc>` + origin + `/avatar/` + id + `</loc>`)
			if t, err := time.Parse(time.RFC3339, updated); err == nil {
				buf.WriteString(`<lastmod>` + t.UTC().Format("2006-01-02") + `</lastmod>`)
			}
			buf.WriteString(`</url>`)
		}
		if rows.Err() != nil || count == 0 {
			http.NotFound(w, r)
			return true
		}
		buf.WriteString(`</urlset>`)
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(buf.Bytes()))
	return true
}
