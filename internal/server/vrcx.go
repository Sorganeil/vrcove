package server

import (
	"net/http"
	"strings"
)

const (
	// vrcxResults caps a search: VRCX asks for 5,000, but sorting that many
	// matches of a common word takes seconds.
	vrcxResults = 1000
	// vrcxRequests per client in each limiter window. VRCX also looks a
	// creator up every time someone opens a profile.
	vrcxRequests = 120
)

// vrcxAvatar is one result in the shape VRCX's avatar provider setting reads.
type vrcxAvatar struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	AuthorID          string `json:"authorId"`
	AuthorName        string `json:"authorName"`
	Description       string `json:"description"`
	ImageURL          string `json:"imageUrl"`
	ThumbnailImageURL string `json:"thumbnailImageUrl"`
	ReleaseStatus     string `json:"releaseStatus"`
	CreatedAt         string `json:"created_at,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
}

// vrcxAvatars lets VRCX use the catalog as an avatar search provider, at
// /api/vrcx/avatars. VRCX calls it with ?search=<text>, or ?authorId=usr_…
// when someone opens a profile, and expects a JSON array. Nothing about the
// caller is stored, including the VRCX-ID header VRCX sends.
func (s *Server) vrcxAvatars(w http.ResponseWriter, r *http.Request) error {
	if wait := s.limiter.allowN("vrcx:"+clientKey(r), vrcxRequests, s.now()); wait > 0 {
		return errStatus(http.StatusTooManyRequests, "Too many VRCove searches. Wait a few minutes.")
	}
	q := r.URL.Query()
	var list []catalogAvatar
	var err error
	switch {
	case q.Has("authorId"):
		id := q.Get("authorId")
		if !userID.MatchString(id) {
			writeError(w, http.StatusBadRequest, "authorId must be a VRChat user ID.")
			return nil
		}
		list, err = s.queryAvatars(r.Context(), "WHERE json_extract(data, '$.authorId') = ? ORDER BY updated DESC LIMIT ?", id, vrcxResults)
	case q.Has("search"):
		term := []rune(strings.TrimSpace(q.Get("search")))
		if len(term) > 150 {
			term = term[:150]
		}
		limit := vrcxResults
		if q.Has("n") {
			limit = max(queryInt(q.Get("n"), vrcxResults), 1)
		}
		if clause, arg, ok := termFilter(string(term)); ok {
			list, err = s.queryAvatars(r.Context(), "WHERE "+clause+" ORDER BY updated DESC LIMIT ?", arg, limit)
		}
	default:
		// ?fileId= and anything else: VRCX treats a failed lookup as "not found".
		writeError(w, http.StatusNotFound, "Use ?search= or ?authorId=.")
		return nil
	}
	if err != nil {
		return err
	}
	out := make([]vrcxAvatar, len(list))
	for i, a := range list {
		out[i] = vrcxAvatar{ID: a.ID, Name: a.Name, AuthorID: a.AuthorID, AuthorName: a.Author, Description: a.Description,
			ImageURL: a.Full, ThumbnailImageURL: a.Image, ReleaseStatus: "public", CreatedAt: a.Created, UpdatedAt: a.Updated}
		if out[i].ImageURL == "" {
			out[i].ImageURL = a.Image
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, out)
	return nil
}
