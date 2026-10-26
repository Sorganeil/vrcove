package server

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The app shell's #root stays empty until JavaScript runs, so without this
// every address would look like the same blank page to search engines.
// seoPage fills #root with the page's real text and links; React replaces it
// as soon as the app starts.
type seoPage struct {
	Meta  pageMeta
	Body  string
	Graph []any // JSON-LD nodes; nil keeps the shell's own structured data
}

var rankNames = map[string]string{"Excellent": "Excellent", "Good": "Good", "Medium": "Medium", "Poor": "Poor", "VeryPoor": "Very Poor"}

// Section pages: a heading and a paragraph in the words people search with.
var sectionCopy = map[string][2]string{
	"":          {"VRChat avatar search and web client", "VRCove is a private VRChat frontend and web client for phone and PC. Search public VRChat avatars by name, creator or avatar ID, for PC and Quest. Sign in with VRChat to see which friends are online, browse worlds and groups, manage favorites and change avatars from your phone or any browser. Handy if you play on Quest without a PC. Free and open source, with no ads, analytics or tracking."},
	"avatars":   {"VRChat avatar search", "Find public VRChat avatars by name, creator or tag, or paste an avatar ID (avtr_…) to open that avatar. Filter PC and Quest avatars by performance rank, find Quest fallbacks, and hide adult, gore or horror avatars. No account needed to search, and it works as a VRCX avatar search provider. Creators can ask to have their avatars removed."},
	"worlds":    {"VRChat world search and reviews", "Browse VRChat worlds and read community reviews from your phone or PC. No ads or analytics."},
	"groups":    {"VRChat group search and reviews", "Discover VRChat groups and read community reviews from your phone or PC. No ads or analytics."},
	"users":     {"Find VRChat users", "Look up VRChat players by name from your phone or PC after signing in with VRChat. No ads or analytics."},
	"search":    {"Search VRChat", "Search VRChat avatars, people, worlds and groups in one place, on phone or PC. No ads, analytics or tracking."},
	"about":     {"About VRCove", "VRCove is a private VRChat web client and avatar search that works in any browser, including Android, iPhone and iPad. It shows friends, worlds, groups, favorites and notifications, lets you change avatars, and adds reviews and collections. VRCX users can add VRCove as an avatar search provider."},
	"companion": {"VRCove Desktop: VRChat app and avatar logger", "VRCove as an app for Windows and Linux with a built-in VRChat avatar logger that adds public avatars you see to VRCove's avatar search. It talks to VRChat directly from your computer, so your VRChat login never reaches VRCove."},
	"donations": {"Support VRCove", "Help keep VRCove independent, private and free of ads."},
}

const seoNav = `<nav class="seo-nav" aria-label="VRCove"><a href="/">VRCove</a><a href="/avatars">Avatar search</a><a href="/worlds">Worlds</a><a href="/groups">Groups</a><a href="/users">Users</a><a href="/companion">Desktop app</a><a href="/about">About</a></nav>`

type catalogAvatar struct {
	ID, Name, Author, AuthorID, Description, Image, Full, Created, Updated, Rank string
	PC, Quest                                                                    bool
	Tags                                                                         []string
}

// platforms names where an avatar works, such as "PC and Quest".
func (a catalogAvatar) platforms() string {
	switch {
	case a.PC && a.Quest:
		return "PC and Quest"
	case a.Quest:
		return "Quest"
	case a.PC:
		return "PC"
	}
	return ""
}

// linkText spots web addresses, which spam avatars put in their name or
// description to borrow VRCove's search ranking.
var linkText = regexp.MustCompile(`(?i)https?://|www\.|discord\.gg/|\b[a-z0-9-]+\.(com|net|org|gg|io|xyz|ru|shop|me|co|tv|link|site|online|store|app|live|fun)\b`)

func (a catalogAvatar) hasLinks() bool {
	return linkText.MatchString(a.Name) || linkText.MatchString(a.Description)
}

// indexable is whether search engines may list the avatar's page; the avatar
// sitemaps use the same rule.
func (a catalogAvatar) indexable() bool { return !a.adult() && !a.hasLinks() }

func (a catalogAvatar) adult() bool {
	for _, t := range a.Tags {
		if t == "content_sex" || t == "content_adult" {
			return true
		}
	}
	return false
}

// keywords are the tags the creator chose, such as "furry" or "face tracking".
func (a catalogAvatar) keywords() []string {
	var out []string
	for _, t := range a.Tags {
		if k, ok := strings.CutPrefix(t, "author_tag_"); ok && strings.TrimSpace(k) != "" {
			out = append(out, k)
		}
	}
	return out
}

func (a catalogAvatar) authorPath() string {
	v := url.Values{"author": {a.Author}}
	if userID.MatchString(a.AuthorID) {
		v.Set("authorId", a.AuthorID)
	}
	return "/avatars?" + v.Encode()
}

func scanAvatar(id, name, author, updated, rank string, pc, quest bool, data string) catalogAvatar {
	a := catalogAvatar{ID: id, Name: name, Author: author, Updated: updated, Rank: rank, PC: pc, Quest: quest}
	var d struct {
		AuthorID    string          `json:"authorId"`
		Description string          `json:"description"`
		Image       string          `json:"image"`
		Thumbnail   string          `json:"thumbnail"`
		Created     string          `json:"created"`
		Tags        json.RawMessage `json:"tags"`
	}
	if json.Unmarshal([]byte(data), &d) == nil {
		a.AuthorID, a.Description, a.Created = d.AuthorID, d.Description, d.Created
		a.Image, a.Full = d.Thumbnail, d.Image
		if a.Image == "" {
			a.Image = d.Image
		}
		// Imported records store tags as a JSON array, sometimes inside a string.
		if json.Unmarshal(d.Tags, &a.Tags) != nil {
			var inner string
			if json.Unmarshal(d.Tags, &inner) == nil {
				json.Unmarshal([]byte(inner), &a.Tags)
			}
		}
	}
	return a
}

const avatarColumns = "id, coalesce(name, ''), coalesce(author, ''), coalesce(updated, ''), coalesce(rating, ''), coalesce(pc, 0), coalesce(quest, 0), coalesce(data, '{}')"

func (s *Server) queryAvatars(ctx context.Context, query string, args ...any) ([]catalogAvatar, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+avatarColumns+" FROM catalog "+query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []catalogAvatar
	for rows.Next() {
		var id, name, author, updated, rank, data string
		var pc, quest bool
		if err := rows.Scan(&id, &name, &author, &updated, &rank, &pc, &quest, &data); err != nil {
			return nil, err
		}
		out = append(out, scanAvatar(id, name, author, updated, rank, pc, quest, data))
	}
	return out, rows.Err()
}

// authorAvatars lists a creator's avatars, newest first, the same way the
// catalog's creator filter matches them.
func (s *Server) authorAvatars(ctx context.Context, author, authorID string, limit int) ([]catalogAvatar, error) {
	phrase := ftsPhrase(author, false)
	if phrase == "" {
		return nil, nil
	}
	where, args := "rowid IN (SELECT rowid FROM catalog_fts WHERE catalog_fts MATCH ?) AND author = ? COLLATE NOCASE", []any{"author : " + phrase, author}
	if userID.MatchString(authorID) {
		where, args = where+" AND json_extract(data, '$.authorId') = ?", append(args, authorID)
	}
	return s.queryAvatars(ctx, "WHERE "+where+" ORDER BY updated DESC LIMIT ?", append(args, limit)...)
}

// recentCache holds the newest catalog entries without adult, gore or horror
// tags, cached briefly because the home and search pages show them.
type recentCache struct {
	sync.Mutex
	list  []catalogAvatar
	until time.Time
}

func (s *Server) recentAvatars(ctx context.Context, n int) []catalogAvatar {
	s.recent.Lock()
	defer s.recent.Unlock()
	if s.now().Before(s.recent.until) {
		return s.recent.list[:min(n, len(s.recent.list))]
	}
	list, err := s.queryAvatars(ctx, "ORDER BY updated DESC LIMIT 200")
	if err != nil {
		return nil
	}
	s.recent.list = s.recent.list[:0]
	for _, a := range list {
		if !a.adult() && !a.hasLinks() && !hasAny(a.Tags, "content_gore", "content_violence", "content_horror") && len(s.recent.list) < 48 {
			s.recent.list = append(s.recent.list, a)
		}
	}
	s.recent.until = s.now().Add(10 * time.Minute)
	return s.recent.list[:min(n, len(s.recent.list))]
}

// forgetRecent drops the cached list after a moderator removes an avatar.
func (s *Server) forgetRecent() {
	s.recent.Lock()
	s.recent.until = time.Time{}
	s.recent.Unlock()
}

func hasAny(tags []string, want ...string) bool {
	for _, t := range tags {
		for _, w := range want {
			if t == w {
				return true
			}
		}
	}
	return false
}

func avatarList(b *strings.Builder, list []catalogAvatar, withAuthor bool) {
	b.WriteString(`<ul class="seo-list">`)
	for _, a := range list {
		b.WriteString(`<li><a href="/avatar/` + a.ID + `">` + html.EscapeString(a.Name) + `</a>`)
		if withAuthor && a.Author != "" {
			b.WriteString(` by ` + html.EscapeString(a.Author))
		}
		if p := a.platforms(); p != "" {
			b.WriteString(` <small>` + p + `</small>`)
		}
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul>`)
}

func crumbs(origin string, items ...[2]string) map[string]any {
	list := make([]any, len(items))
	for i, it := range items {
		list[i] = map[string]any{"@type": "ListItem", "position": i + 1, "name": it[0], "item": origin + it[1]}
	}
	return map[string]any{"@type": "BreadcrumbList", "itemListElement": list}
}

func (s *Server) seoContent(r *http.Request, meta pageMeta) seoPage {
	page := seoPage{Meta: meta}
	origin := s.cfg.PublicOrigin
	website := map[string]any{"@type": "WebSite", "@id": origin + "/#website", "name": "VRCove", "url": origin + "/"}
	slug := strings.Trim(r.URL.Path, "/")
	if kind, id, ok := detailRoute(r.URL.Path); ok && kind == "avatar" {
		return s.avatarPage(r.Context(), page, id, website)
	}
	if slug == "avatars" && r.URL.Query().Get("author") != "" {
		return s.authorPage(r, page, website)
	}
	text, ok := sectionCopy[slug]
	if !ok || (slug != "" && !meta.Index) {
		return page
	}
	var b strings.Builder
	b.WriteString(seoNav + `<main class="seo-page"><h1>` + text[0] + `</h1><p>` + html.EscapeString(text[1]) + `</p>`)
	if slug == "" || slug == "avatars" {
		if list := s.recentAvatars(r.Context(), map[string]int{"": 24, "avatars": 48}[slug]); len(list) > 0 {
			b.WriteString(`<h2>Recently updated VRChat avatars</h2>`)
			avatarList(&b, list, true)
		}
	}
	b.WriteString(`</main>`)
	page.Body = b.String()
	return page
}

func (s *Server) avatarPage(ctx context.Context, page seoPage, id string, website map[string]any) seoPage {
	list, err := s.queryAvatars(ctx, "WHERE id = ?", id)
	if err != nil || len(list) == 0 {
		// Not in the catalog (removed, private or never seen): keep it out of the index.
		page.Meta.Index = false
		return page
	}
	a := list[0]
	origin := s.cfg.PublicOrigin
	name, author := strings.TrimSpace(a.Name), strings.TrimSpace(a.Author)
	if name == "" {
		name = "Unnamed avatar"
	}
	platforms, rank := a.platforms(), rankNames[a.Rank]
	page.Meta.Title = name + " by " + author + " | VRChat Avatar | VRCove"
	summary := name + " is a public VRChat avatar by " + author
	if platforms != "" {
		summary += " for " + platforms
	}
	if rank != "" {
		summary += ", performance rank " + rank
	}
	summary += "."
	description := strings.TrimSpace(a.Description)
	if strings.EqualFold(description, name) {
		description = ""
	}
	page.Meta.Description = summary + " " + description
	if description == "" {
		page.Meta.Description = summary + " See its details and more avatars by " + author + " on VRCove."
	}
	// Pages still work for adult avatars and ones that advertise a site, but
	// search engines are asked to leave them out.
	page.Meta.Index = a.indexable()

	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(seoNav + `<main class="seo-page"><nav class="seo-crumbs" aria-label="Breadcrumb"><a href="/">VRCove</a> › <a href="/avatars">Avatar search</a> › <a href="` + esc(a.authorPath()) + `">` + esc(author) + `</a></nav>`)
	b.WriteString(`<h1>` + esc(name) + `</h1><p class="seo-lead">VRChat avatar by <a href="` + esc(a.authorPath()) + `">` + esc(author) + `</a></p>`)
	image := ""
	if u, err := url.Parse(a.Image); err == nil && allowedImage(u) {
		image = origin + "/api/image?url=" + url.QueryEscape(a.Image)
		b.WriteString(`<img src="` + esc(image) + `" alt="` + esc(name+", a VRChat avatar by "+author) + `" width="256">`)
	}
	b.WriteString(`<dl>`)
	if platforms != "" {
		b.WriteString(`<dt>Platforms</dt><dd>` + platforms + `</dd>`)
	}
	if rank != "" {
		b.WriteString(`<dt>Performance rank</dt><dd>` + rank + `</dd>`)
	}
	if len(a.Updated) >= 10 {
		b.WriteString(`<dt>Updated</dt><dd>` + esc(a.Updated[:10]) + `</dd>`)
	}
	b.WriteString(`<dt>Avatar ID</dt><dd><code>` + a.ID + `</code></dd></dl>`)
	if description != "" {
		b.WriteString(`<p>` + esc(description) + `</p>`)
	}
	keywords := a.keywords()
	if len(keywords) > 0 {
		b.WriteString(`<p>Tags: `)
		for i, k := range keywords {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(`<a href="/avatars?` + esc(url.Values{"tag": {k}}.Encode()) + `">` + esc(k) + `</a>`)
		}
		b.WriteString(`</p>`)
	}
	if more, err := s.authorAvatars(ctx, a.Author, a.AuthorID, 13); err == nil {
		var others []catalogAvatar
		for _, m := range more {
			if m.ID != a.ID && len(others) < 12 {
				others = append(others, m)
			}
		}
		if len(others) > 0 {
			b.WriteString(`<h2>More VRChat avatars by ` + esc(author) + `</h2>`)
			avatarList(&b, others, false)
			b.WriteString(`<p><a href="` + esc(a.authorPath()) + `">All avatars by ` + esc(author) + `</a></p>`)
		}
	}
	b.WriteString(`</main>`)
	page.Body = b.String()

	work := map[string]any{"@type": "CreativeWork", "name": name, "url": origin + "/avatar/" + a.ID, "description": page.Meta.Description,
		"author": map[string]any{"@type": "Person", "name": author}, "genre": "VRChat avatar", "isPartOf": map[string]any{"@id": origin + "/#website"}}
	if image != "" {
		work["image"] = image
	}
	if a.Created != "" {
		work["dateCreated"] = a.Created
	}
	if a.Updated != "" {
		work["dateModified"] = a.Updated
	}
	if len(keywords) > 0 {
		work["keywords"] = strings.Join(keywords, ", ")
	}
	page.Graph = []any{website, crumbs(origin, [2]string{"VRCove", "/"}, [2]string{"Avatar search", "/avatars"}, [2]string{author, a.authorPath()}, [2]string{name, "/avatar/" + a.ID}), work}
	return page
}

// authorPage is a creator's avatar list at /avatars?author=…&authorId=…, the
// same address the app uses for its creator filter.
func (s *Server) authorPage(r *http.Request, page seoPage, website map[string]any) seoPage {
	q := r.URL.Query()
	author, authorID := strings.TrimSpace(q.Get("author")), q.Get("authorId")
	page.Meta.Index = false
	if len([]rune(author)) > 100 {
		return page
	}
	list, err := s.authorAvatars(r.Context(), author, authorID, 101)
	if err != nil || len(list) == 0 {
		return page
	}
	a := catalogAvatar{Author: author, AuthorID: authorID}
	origin := s.cfg.PublicOrigin
	page.Meta.Canonical = origin + a.authorPath()
	// One avatar would only repeat its own page, and creator names without an
	// ID can be shared by several people.
	page.Meta.Index = len(list) >= 2 && userID.MatchString(authorID) && len(q) == 2
	page.Meta.Title = "VRChat Avatars by " + author + " | VRCove"
	count := len(list)
	shown := list[:min(100, count)]
	names := make([]string, 0, 3)
	for _, x := range shown[:min(3, len(shown))] {
		names = append(names, x.Name)
	}
	page.Meta.Description = "Browse public VRChat avatars by " + author + " for PC and Quest, including " + strings.Join(names, ", ") + ". Search, filter and open them on VRCove."

	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(seoNav + `<main class="seo-page"><nav class="seo-crumbs" aria-label="Breadcrumb"><a href="/">VRCove</a> › <a href="/avatars">Avatar search</a></nav>`)
	b.WriteString(`<h1>VRChat avatars by ` + esc(author) + `</h1><p class="seo-lead">`)
	if count > 100 {
		b.WriteString(`The 100 most recently updated of more than 100 public avatars by ` + esc(author) + `.`)
	} else {
		b.WriteString(esc(pluralAvatars(count)) + ` by ` + esc(author) + `, most recently updated first.`)
	}
	b.WriteString(`</p>`)
	avatarList(&b, shown, false)
	b.WriteString(`</main>`)
	page.Body = b.String()
	page.Graph = []any{website, crumbs(origin, [2]string{"VRCove", "/"}, [2]string{"Avatar search", "/avatars"}, [2]string{author, a.authorPath()})}
	return page
}

func pluralAvatars(n int) string {
	if n == 1 {
		return "1 public avatar"
	}
	return strconv.Itoa(n) + " public avatars"
}
