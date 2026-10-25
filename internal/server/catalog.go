package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

// The schema matches scripts/import-dump.py, which builds the catalog file.
// Logger ingestion writes through a separate optional pool. Browse requests
// keep using a read-only pool.
const schema = `
CREATE TABLE IF NOT EXISTS catalog (id TEXT PRIMARY KEY, name TEXT, author TEXT, updated TEXT, pc INTEGER, quest INTEGER, rating TEXT, data TEXT);
CREATE VIRTUAL TABLE IF NOT EXISTS catalog_fts USING fts5(name,author,description,tags,content="");
CREATE INDEX IF NOT EXISTS catalog_updated ON catalog(updated DESC);
CREATE INDEX IF NOT EXISTS catalog_platform ON catalog(quest,pc,updated DESC);
CREATE INDEX IF NOT EXISTS catalog_rating ON catalog(rating,updated DESC);
CREATE INDEX IF NOT EXISTS catalog_author_id ON catalog(json_extract(data, '$.authorId'));
CREATE INDEX IF NOT EXISTS catalog_image_file ON catalog(` + imageFileExpr + `);`

// imageFileExpr is the VRChat file ID of an avatar's picture, such as
// file_<uuid>. A profile's current avatar picture shares it, which is how a
// profile shows the public avatar someone is wearing.
const imageFileExpr = "substr(json_extract(data, '$.image'), instr(json_extract(data, '$.image'), 'file_'), 41)"

// OpenCatalogWriter is only used by explicitly enabled logger ingestion.
func OpenCatalogWriter(path string) (*sql.DB, error) {
	file := "file:" + (&url.URL{Path: path}).EscapedPath()
	db, err := sql.Open("sqlite", file+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// catalogReaders bounds concurrent catalog queries.
const catalogReaders = 8

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
	// A flood of broad searches queues here instead of taking every CPU on
	// the shared server.
	db.SetMaxOpenConns(catalogReaders)
	db.SetMaxIdleConns(catalogReaders)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

var (
	avatarID    = regexp.MustCompile(`^avtr_[a-f0-9-]{36}$`)
	imageFileID = regexp.MustCompile(`^file_[a-f0-9-]{36}$`)
	searchTerm  = regexp.MustCompile(`[\p{L}\p{N}_]+`)
	ratings     = map[string]bool{"Excellent": true, "Good": true, "Medium": true, "Poor": true, "VeryPoor": true, "Unknown": true}
)

const catalogPageSize = 36

// ratingOrder ranks PC performance from best to worst, for "or better" filters.
var ratingOrder = []string{"Excellent", "Good", "Medium", "Poor", "VeryPoor"}

// ratingRank orders avatars from Excellent to Very Poor, then unrated ones.
var ratingRank = func() string {
	var b strings.Builder
	b.WriteString("CASE rating")
	for i, r := range ratingOrder {
		fmt.Fprintf(&b, " WHEN '%s' THEN %d", r, i)
	}
	fmt.Fprintf(&b, " ELSE %d END", len(ratingOrder))
	return b.String()
}()

// relevanceOrder ranks avatars whose name matches the search above ones found
// only through their creator, description or tags: the exact name first, then
// names starting with the search, containing it as a word or anywhere, names
// with every word, and the creator's name. Newer avatars come first within each
// group. Word-frequency scores were slower and put names like "cat cat cat" on top.
func relevanceOrder(term string) (string, []any) {
	phrase := strings.Join(strings.Fields(term), " ")
	like := likeEscape(phrase)
	args := []any{phrase, like + "%", "% " + like + "%", "%" + like + "%"}
	var every []string
	for _, word := range searchTerm.FindAllString(term, -1) {
		every, args = append(every, `name LIKE ? ESCAPE '\'`), append(args, "%"+likeEscape(word)+"%")
	}
	args = append(args, phrase)
	return `CASE WHEN name = ? COLLATE NOCASE THEN 0
		WHEN name LIKE ? ESCAPE '\' THEN 1
		WHEN name LIKE ? ESCAPE '\' THEN 2
		WHEN name LIKE ? ESCAPE '\' THEN 3
		WHEN ` + strings.Join(every, " AND ") + ` THEN 4
		WHEN author = ? COLLATE NOCASE THEN 5
		ELSE 6 END, updated DESC, id ASC`, args
}

// likeEscape makes a search literal inside a LIKE pattern.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

// contentWarnings maps the content filters to the VRChat content tags they
// hide. Tags are tokenized in the search index, so content_sex is "content sex".
var contentWarnings = map[string][]string{
	"adult":    {"content sex", "content adult"},
	"gore":     {"content gore", "content violence"},
	"horror":   {"content horror"},
	"fallback": {"author quest fallback"},
}

// ftsPhrase turns free text into an FTS5 phrase query of its words, so user
// input can never inject query syntax. prefix matches the last word partially.
func ftsPhrase(text string, prefix bool) string {
	terms := searchTerm.FindAllString(text, 12)
	if len(terms) == 0 {
		return ""
	}
	phrase := `"` + strings.Join(terms, " ") + `"`
	if prefix {
		phrase += "*"
	}
	return phrase
}

// termFilter matches a typed search: an exact avatar ID, or every word as a
// prefix of the indexed name, author, description or tags.
func termFilter(term string) (string, any, bool) {
	if avatarID.MatchString(term) {
		return "id=?", term, true
	}
	terms := searchTerm.FindAllString(term, -1)
	if len(terms) == 0 {
		return "", nil, false
	}
	for i, t := range terms {
		terms[i] = `"` + t + `"*`
	}
	return "rowid IN (SELECT rowid FROM catalog_fts WHERE catalog_fts MATCH ?)", strings.Join(terms, " AND "), true
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	limit := func(key string, n int) string {
		v := []rune(strings.TrimSpace(query.Get(key)))
		if len(v) > n {
			v = v[:n]
		}
		return string(v)
	}
	term := limit("q", 150)
	page := queryInt(query.Get("page"), 50000)
	var where []string
	var args []any
	match := func(expr string) {
		where, args = append(where, "rowid IN (SELECT rowid FROM catalog_fts WHERE catalog_fts MATCH ?)"), append(args, expr)
	}
	empty := func() error {
		writeJSON(w, http.StatusOK, map[string]any{"avatars": []any{}, "total": 0, "page": page})
		return nil
	}
	if term != "" {
		clause, arg, ok := termFilter(term)
		if !ok {
			return empty()
		}
		where, args = append(where, clause), append(args, arg)
	}
	// Creator: the index narrows by name, then the exact name (and ID, when
	// opened from an avatar) must match.
	if author := limit("author", 100); author != "" {
		phrase := ftsPhrase(author, false)
		if phrase == "" {
			return empty()
		}
		match("author : " + phrase)
		where, args = append(where, "author = ? COLLATE NOCASE"), append(args, author)
		if id := query.Get("authorId"); userID.MatchString(id) {
			where, args = append(where, "json_extract(data, '$.authorId') = ?"), append(args, id)
		}
	} else if id := query.Get("authorId"); userID.MatchString(id) {
		// A VRChat profile lists that user's avatars by ID alone, whatever name they uploaded under.
		where, args = append(where, "json_extract(data, '$.authorId') = ?"), append(args, id)
	}
	if file := query.Get("imageFile"); imageFileID.MatchString(file) {
		where, args = append(where, imageFileExpr+" = ?"), append(args, file)
	}
	if tag := limit("tag", 60); tag != "" {
		phrase := ftsPhrase(tag, true)
		if phrase == "" {
			return empty()
		}
		match("tags : " + phrase)
	}
	if query.Get("fallback") == "1" {
		match(`tags : "` + contentWarnings["fallback"][0] + `"`)
	}
	var hidden []string
	for _, key := range strings.Split(query.Get("hide"), ",") {
		if key != "fallback" {
			for _, tag := range contentWarnings[key] {
				hidden = append(hidden, `"`+tag+`"`)
			}
		}
	}
	if len(hidden) > 0 {
		where, args = append(where, "rowid NOT IN (SELECT rowid FROM catalog_fts WHERE catalog_fts MATCH ?)"), append(args, "tags : ("+strings.Join(hidden, " OR ")+")")
	}
	switch query.Get("platform") {
	case "quest":
		where = append(where, "quest=1")
	case "pc":
		where = append(where, "pc=1")
	case "both":
		where = append(where, "pc=1 AND quest=1")
	}
	// A trailing + means that rating or better, e.g. Good+ is Excellent or Good.
	if rating := query.Get("rating"); ratings[rating] {
		where, args = append(where, "rating=?"), append(args, rating)
	} else if best, ok := strings.CutSuffix(rating, "+"); ok && ratings[best] {
		placeholders := []string{}
		for _, r := range ratingOrder {
			placeholders, args = append(placeholders, "?"), append(args, r)
			if r == best {
				break
			}
		}
		where = append(where, "rating IN ("+strings.Join(placeholders, ",")+")")
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	order, orderArgs := "updated DESC, id ASC", []any(nil)
	switch query.Get("sort") {
	case "relevance":
		// Best match needs a typed search; without one it is the newest first.
		if term != "" && !avatarID.MatchString(term) {
			order, orderArgs = relevanceOrder(term)
		}
	case "added":
		order = "rowid DESC"
	case "rating":
		order = ratingRank + ", updated DESC, id ASC"
	case "name":
		order = "name COLLATE NOCASE ASC, id ASC"
	case "name-desc":
		order = "name COLLATE NOCASE DESC, id ASC"
	case "author":
		order = "author COLLATE NOCASE ASC, name COLLATE NOCASE ASC, id ASC"
	case "oldest":
		order = "updated ASC, id ASC"
	}
	ctx := r.Context()
	listArgs := append(append(append([]any{}, args...), orderArgs...), catalogPageSize, page*catalogPageSize)
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM catalog"+clause+" ORDER BY "+order+" LIMIT ? OFFSET ?", listArgs...)
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
