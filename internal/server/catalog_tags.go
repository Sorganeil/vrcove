package server

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// tagStore caches the creator tags counted across the catalog. Counting reads
// every avatar, so the list is rebuilt at most every tagTTL.
type tagStore struct {
	sync.Mutex
	tags  []catalogTag
	until time.Time
}

type catalogTag struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

const (
	tagTTL = 6 * time.Hour
	// minTagAvatars drops tags used by one or two avatars, which are mostly
	// avatar names and typos.
	minTagAvatars = 3
	// maxTagRunes matches the longest tag the catalog filter accepts.
	maxTagRunes = 60
)

var tagWord = regexp.MustCompile(`[\p{L}\p{N}]+`)

// tagKey turns a creator tag such as "author_tag_Face_Tracking" into the words
// the tag filter searches for, "face tracking". Spellings that differ only in
// case, spaces, underscores or hyphens share a key. Other tags return "".
func tagKey(raw string) string {
	tag, ok := strings.CutPrefix(raw, "author_tag_")
	if !ok {
		return ""
	}
	key := strings.Join(tagWord.FindAllString(strings.ToLower(tag), -1), " ")
	if len([]rune(key)) > maxTagRunes {
		return ""
	}
	return key
}

// catalogTags lists creator tags by how many avatars use them. Avatars marked
// as adult content are not counted, so their tags stay out of the public list.
func (s *Server) catalogTags(w http.ResponseWriter, r *http.Request) error {
	s.tags.Lock()
	if !s.now().Before(s.tags.until) {
		// The count is shared by every visitor, so a closed request does not cancel it.
		tags, err := s.countTags(context.WithoutCancel(r.Context()))
		if err != nil {
			s.tags.Unlock()
			return err
		}
		s.tags.tags, s.tags.until = tags, s.now().Add(tagTTL)
	}
	tags := s.tags.tags
	s.tags.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
	return nil
}

func (s *Server) countTags(ctx context.Context) ([]catalogTag, error) {
	adult := `tags : ("` + strings.Join(contentWarnings["adult"], `" OR "`) + `")`
	rows, err := s.db.QueryContext(ctx, `SELECT catalog.rowid, j.value FROM catalog, json_each(catalog.data, '$.tags') j
		WHERE j.value LIKE 'author\_tag\_%' ESCAPE '\'
		AND catalog.rowid NOT IN (SELECT rowid FROM catalog_fts WHERE catalog_fts MATCH ?)`, adult)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	// An avatar tagged both "Furry" and "furry" counts once.
	var current int64 = -1
	seen := map[string]bool{}
	for rows.Next() {
		var rowid int64
		var raw string
		if err := rows.Scan(&rowid, &raw); err != nil {
			return nil, err
		}
		if rowid != current {
			current = rowid
			clear(seen)
		}
		if key := tagKey(raw); key != "" && !seen[key] {
			seen[key] = true
			counts[key]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tags := []catalogTag{}
	for tag, n := range counts {
		if n >= minTagAvatars {
			tags = append(tags, catalogTag{tag, n})
		}
	}
	slices.SortFunc(tags, func(a, b catalogTag) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Tag, b.Tag)
	})
	return tags, nil
}
