package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type linkPreview struct{ Title, Description, Image string }
type previewEntry struct {
	Preview linkPreview
	Until   time.Time
}
type previewStore struct {
	sync.Mutex
	Entries         map[string]previewEntry
	Fetches, Active int
	Reset           time.Time
}

func detailRoute(path string) (kind, id string, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return
	}
	kind, id = parts[0], parts[1]
	switch kind {
	case "avatar":
		ok = avatarID.MatchString(id)
	case "world":
		ok = worldID.MatchString(id)
	case "user":
		ok = userID.MatchString(id)
	case "group":
		ok = groupID.MatchString(id)
	}
	return
}

// Only public display fields can leave an authenticated detail response.
// Activity, account relationships, private worlds and private groups stay out.
func publicPreview(kind string, data any) (linkPreview, bool) {
	m := asObject(data)
	var p linkPreview
	switch kind {
	case "world":
		if field(m, "releaseStatus") != "public" {
			return p, false
		}
		p = linkPreview{field(m, "name"), field(m, "description"), field(m, "thumbnailImageUrl")}
	case "group":
		privacy := field(m, "privacy")
		if privacy != "default" && privacy != "public" {
			return p, false
		}
		p = linkPreview{field(m, "name"), field(m, "description"), field(m, "iconUrl")}
	case "user":
		p = linkPreview{field(m, "displayName"), "VRChat user on VRCove.", field(m, "userIcon")}
		if p.Image == "" {
			p.Image = field(m, "profilePicOverride")
		}
	default:
		return p, false
	}
	return p, p.Title != ""
}

func (s *Server) cachePreview(kind, id string, data any) {
	p, ok := publicPreview(kind, data)
	s.previews.Lock()
	defer s.previews.Unlock()
	key := kind + "/" + id
	if !ok {
		delete(s.previews.Entries, key)
		return
	}
	if s.previews.Entries == nil {
		s.previews.Entries = make(map[string]previewEntry)
	}
	if len(s.previews.Entries) >= 512 {
		for k := range s.previews.Entries {
			delete(s.previews.Entries, k)
			break
		}
	}
	s.previews.Entries[key] = previewEntry{p, s.now().Add(10 * time.Minute)}
}

func (s *Server) pagePreview(r *http.Request) (linkPreview, string) {
	if kind, id, ok := coveRoute(r.URL.Path); ok {
		return s.covePreview(r, kind, id)
	}
	p := linkPreview{homeTitle, homeDescription, ""}
	canonical := s.cfg.PublicOrigin + "/"
	kind, id, ok := detailRoute(r.URL.Path)
	if !ok {
		return p, canonical
	}
	canonical = s.cfg.PublicOrigin + "/" + kind + "/" + id
	p.Title = "VRChat " + kind + " · VRCove"
	p.Description = "Open this " + kind + " on VRCove."
	if kind == "avatar" {
		var raw string
		if s.db.QueryRowContext(r.Context(), "SELECT data FROM catalog WHERE id = ?", id).Scan(&raw) == nil {
			var a object
			if json.Unmarshal([]byte(raw), &a) == nil {
				p.Title = field(a, "name") + " · VRCove"
				p.Description = field(a, "description")
				if p.Description == "" {
					p.Description = "Avatar by " + field(a, "author") + " on VRCove."
				}
				p.Image = field(a, "thumbnail")
				if p.Image == "" {
					p.Image = field(a, "image")
				}
			}
		}
		return p, canonical
	}
	key := kind + "/" + id
	s.previews.Lock()
	entry, found := s.previews.Entries[key]
	if found && s.now().Before(entry.Until) {
		s.previews.Unlock()
		if entry.Preview.Title != "" {
			return entry.Preview, canonical
		}
		return p, canonical
	}
	if s.previews.Entries == nil {
		s.previews.Entries = make(map[string]previewEntry)
	}
	if len(s.previews.Entries) >= 512 {
		for k := range s.previews.Entries {
			delete(s.previews.Entries, k)
			break
		}
	}
	// Coalesce cache misses and briefly cache unavailable public metadata.
	s.previews.Entries[key] = previewEntry{Until: s.now().Add(time.Minute)}
	if !s.now().Before(s.previews.Reset) {
		s.previews.Fetches = 0
		s.previews.Reset = s.now().Add(time.Minute)
	}
	fetch := s.cfg.Contact != "" && s.previews.Fetches < 60 && s.previews.Active < 4
	if fetch {
		s.previews.Fetches++
		s.previews.Active++
	}
	s.previews.Unlock()
	if fetch {
		defer func() { s.previews.Lock(); s.previews.Active--; s.previews.Unlock() }()
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		// Never use visitor credentials for a crawler's public preview.
		result, _, err := s.vrc(ctx, "/"+kind+"s/"+id, nil, vrcRequest{})
		if err == nil {
			s.cachePreview(kind, id, result)
			if public, ok := publicPreview(kind, result); ok {
				return public, canonical
			}
		}
	}
	return p, canonical
}

func previewText(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > limit {
		s = string(r[:limit]) + "…"
	}
	return html.EscapeString(s)
}

func (s *Server) previewHTML(r *http.Request, data []byte) []byte {
	p, canonical := s.pagePreview(r)
	meta := s.pageMeta(r.URL.Path, p, canonical)
	_, _, detail := detailRoute(r.URL.Path)
	_, _, cove := coveRoute(r.URL.Path)
	if !detail && !cove {
		p.Title, p.Description, canonical = meta.Title, meta.Description, meta.Canonical
	}
	// Bump the version when the card changes so chat apps refetch it.
	image, imageSize := s.cfg.PublicOrigin+"/social-preview.png?v=2", `<meta property="og:image:width" content="1200"><meta property="og:image:height" content="630"><meta property="og:image:alt" content="VRCove: VRChat in your browser, without the tracking">`
	card := "summary_large_image"
	if u, err := url.Parse(p.Image); err == nil && allowedImage(u) {
		image, imageSize = s.cfg.PublicOrigin+"/api/image?url="+url.QueryEscape(p.Image), ""
	} else if strings.HasPrefix(p.Image, "/api/cove/picture?") {
		// Profile pictures are small squares, so they get the compact card.
		image, imageSize, card = s.cfg.PublicOrigin+p.Image, `<meta property="og:image:width" content="256"><meta property="og:image:height" content="256">`, "summary"
	}
	title, description := previewText(p.Title, 120), previewText(p.Description, 300)
	tags := `<meta property="og:site_name" content="VRCove"><meta property="og:type" content="website">` +
		`<meta property="og:title" content="` + title + `"><meta property="og:description" content="` + description + `">` +
		`<meta property="og:url" content="` + html.EscapeString(canonical) + `"><meta property="og:image" content="` + html.EscapeString(image) + `">` + imageSize +
		`<meta name="twitter:card" content="` + card + `"><meta name="twitter:title" content="` + title + `">` +
		`<meta name="twitter:description" content="` + description + `"><meta name="twitter:image" content="` + html.EscapeString(image) + `">` +
		`<meta name="theme-color" content="#75c7e6"><link rel="canonical" href="` + html.EscapeString(canonical) + `">`
	return []byte(strings.Replace(applyPageMeta(string(data), meta), "</head>", tags+"</head>", 1))
}

// coveRoute matches the shareable /cove/<username> and /collection/<id> paths.
func coveRoute(path string) (kind, id string, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return
	}
	kind, id = parts[0], parts[1]
	switch kind {
	case "cove":
		ok = usernamePattern.MatchString(id)
	case "collection":
		ok = collectionIDPattern.MatchString(id)
	}
	return
}

// covePreview describes a public VRCove profile or collection. Private and
// missing ones get the same generic card, so a preview reveals nothing.
func (s *Server) covePreview(r *http.Request, kind, id string) (linkPreview, string) {
	canonical := s.cfg.PublicOrigin + "/" + kind + "/" + id
	ctx := r.Context()
	if kind == "cove" {
		p := linkPreview{"VRCove profile", "A profile on VRCove.", ""}
		if s.accounts == nil {
			return p, canonical
		}
		var accountID int64
		var username, bio, status string
		var public bool
		err := s.accounts.QueryRowContext(ctx, "SELECT id, username, bio, status, public FROM accounts WHERE username_key = ?",
			strings.ToLower(id)).Scan(&accountID, &username, &bio, &status, &public)
		if err != nil || !public {
			return p, canonical
		}
		p = linkPreview{username + " · VRCove", bio, s.accountPictureURL(r, accountID, username)}
		if p.Description == "" {
			p.Description = status
		}
		if p.Description == "" {
			p.Description = "Reviews and collections by " + username + " on VRCove."
		}
		return p, s.cfg.PublicOrigin + "/cove/" + username
	}
	p := linkPreview{"VRCove collection", "A collection on VRCove.", ""}
	if s.accounts == nil {
		return p, canonical
	}
	var name, description, kindName, owner string
	var public, ownerPublic bool
	var count int
	err := s.accounts.QueryRowContext(ctx, `SELECT c.name, c.description, c.public, c.kind, a.username, a.public,
		(SELECT count(*) FROM collection_items i WHERE i.collection_id = c.id)
		FROM collections c JOIN accounts a ON a.id = c.account_id WHERE c.id = ?`, id).
		Scan(&name, &description, &public, &kindName, &owner, &ownerPublic, &count)
	if err != nil || !public {
		return p, canonical
	}
	p.Title, p.Description = name+" · VRCove", description
	if p.Description == "" {
		// The owner is named only when their profile is public, as on the page.
		if !ownerPublic {
			owner = "a VRCove user"
		}
		items := fmt.Sprintf("%d %ss", count, kindName)
		if count == 1 {
			items = "1 " + kindName
		}
		p.Description = "A collection of " + items + " by " + owner + " on VRCove."
	}
	s.accounts.QueryRowContext(ctx, "SELECT image FROM collection_items WHERE collection_id = ? AND image != '' ORDER BY added DESC, rowid DESC LIMIT 1", id).Scan(&p.Image)
	return p, canonical
}
