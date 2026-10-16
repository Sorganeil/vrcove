package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

// The schema matches scripts/import-dump.py, which builds the catalog file.
// FTS rows are written by the importer; the server only reads.
const schema = `
CREATE TABLE IF NOT EXISTS catalog (id TEXT PRIMARY KEY, name TEXT, author TEXT, updated TEXT, pc INTEGER, quest INTEGER, rating TEXT, data TEXT);
CREATE VIRTUAL TABLE IF NOT EXISTS catalog_fts USING fts5(name,author,description,tags,content="");
CREATE INDEX IF NOT EXISTS catalog_updated ON catalog(updated DESC);
CREATE INDEX IF NOT EXISTS catalog_platform ON catalog(quest,pc,updated DESC);
CREATE INDEX IF NOT EXISTS catalog_rating ON catalog(rating,updated DESC);`

// OpenCatalog ensures the schema exists, then returns a read-only pool.
func OpenCatalog(path string) (*sql.DB, error) {
	file := "file:" + (&url.URL{Path: path}).EscapedPath()
	setup, err := sql.Open("sqlite", file+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	_, err = setup.Exec(schema)
	setup.Close()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", file+"?_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

var (
	avatarID   = regexp.MustCompile(`^avtr_[a-f0-9-]{36}$`)
	searchTerm = regexp.MustCompile(`[\p{L}\p{N}_]+`)
	ratings    = map[string]bool{"Excellent": true, "Good": true, "Medium": true, "Poor": true, "VeryPoor": true, "Unknown": true}
)

const catalogPageSize = 36

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	q := []rune(query.Get("q"))
	if len(q) > 150 {
		q = q[:150]
	}
	term := strings.TrimSpace(string(q))
	page := queryInt(query.Get("page"), 50000)
	var where []string
	var args []any
	if term != "" {
		if avatarID.MatchString(term) {
			where, args = append(where, "id=?"), append(args, term)
		} else {
			terms := searchTerm.FindAllString(term, -1)
			if len(terms) == 0 {
				writeJSON(w, http.StatusOK, map[string]any{"avatars": []any{}, "total": 0, "page": page})
				return nil
			}
			for i, t := range terms {
				terms[i] = `"` + t + `"*`
			}
			where = append(where, "rowid IN (SELECT rowid FROM catalog_fts WHERE catalog_fts MATCH ?)")
			args = append(args, strings.Join(terms, " AND "))
		}
	}
	switch query.Get("platform") {
	case "quest":
		where = append(where, "quest=1")
	case "pc":
		where = append(where, "pc=1")
	case "both":
		where = append(where, "pc=1 AND quest=1")
	}
	if rating := query.Get("rating"); ratings[rating] {
		where, args = append(where, "rating=?"), append(args, rating)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	order := "updated DESC, id ASC"
	switch query.Get("sort") {
	case "name":
		order = "name COLLATE NOCASE ASC, id ASC"
	case "oldest":
		order = "updated ASC, id ASC"
	}
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM catalog"+clause+" ORDER BY "+order+" LIMIT ? OFFSET ?",
		append(args, catalogPageSize, page*catalogPageSize)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	avatars := []json.RawMessage{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return err
		}
		avatars = append(avatars, json.RawMessage(data))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM catalog"+clause, args...).Scan(&total); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"avatars": avatars, "total": total, "page": page})
	return nil
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) error {
	var n int
	if err := s.db.QueryRowContext(r.Context(), "SELECT count(*) FROM catalog").Scan(&n); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"indexed": n, "sourcePublic": 1492676, "privateExcluded": 1605632,
		"loginConfigured":    s.cfg.SessionKey != nil && s.cfg.Contact != "",
		"accountsConfigured": s.accounts != nil,
		"turnstileSiteKey":   s.cfg.TurnstileSiteKey,
	})
	return nil
}
