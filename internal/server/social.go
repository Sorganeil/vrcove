package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Public profiles and avatar/world collections.
//
// Profiles are private by default. A public profile lists the account's
// reviews and public collections at #cove/<username>. A public collection can
// be opened by its unguessable link either way, but it names its owner only
// when the owner's profile is public. Linked VRChat accounts never appear.

// accountMigrations run in order; PRAGMA user_version records how many have
// been applied, so existing databases upgrade in place.
var accountMigrations = []string{
	`ALTER TABLE accounts ADD COLUMN public INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE accounts ADD COLUMN bio TEXT NOT NULL DEFAULT '';
	ALTER TABLE reviews ADD COLUMN target_name TEXT NOT NULL DEFAULT '';
	CREATE INDEX reviews_account ON reviews(account_id, updated DESC);
	CREATE TABLE collections (id TEXT PRIMARY KEY, account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		kind TEXT NOT NULL CHECK (kind IN ('avatar', 'world')), name TEXT NOT NULL, description TEXT NOT NULL, public INTEGER NOT NULL,
		created INTEGER NOT NULL, updated INTEGER NOT NULL);
	CREATE INDEX collections_account ON collections(account_id, updated DESC);
	CREATE TABLE collection_items (collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE, target TEXT NOT NULL,
		name TEXT NOT NULL, image TEXT NOT NULL, added INTEGER NOT NULL, PRIMARY KEY (collection_id, target));
	CREATE INDEX collection_items_target ON collection_items(target);`,
	`CREATE TABLE account_pictures (account_id INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE, image BLOB NOT NULL, version TEXT NOT NULL);`,
	`ALTER TABLE accounts ADD COLUMN status TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE accounts ADD COLUMN link_reminder_seen INTEGER NOT NULL DEFAULT 0;
 UPDATE accounts SET link_reminder_seen = 1 WHERE EXISTS (SELECT 1 FROM vrchat_links WHERE account_id = accounts.id);`,
	`CREATE TABLE vrchat_sessions (id INTEGER PRIMARY KEY, token_hash BLOB NOT NULL UNIQUE, account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		vrc_key BLOB NOT NULL, device TEXT NOT NULL, browser TEXT NOT NULL, created TEXT NOT NULL, expires INTEGER NOT NULL);
	CREATE INDEX vrchat_sessions_link ON vrchat_sessions(account_id, vrc_key);`,
	`ALTER TABLE vrchat_sessions ADD COLUMN sealed TEXT NOT NULL DEFAULT '';
	CREATE TABLE logger_keys (id INTEGER PRIMARY KEY AUTOINCREMENT, account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		session_hash BLOB NOT NULL UNIQUE REFERENCES vrchat_sessions(token_hash) ON DELETE CASCADE, key_hash BLOB NOT NULL UNIQUE, created INTEGER NOT NULL);
	CREATE INDEX logger_keys_account ON logger_keys(account_id);`,
}

func migrateAccounts(db *sql.DB) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(accountMigrations); i++ {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(accountMigrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("accounts migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const (
	maxBioLength          = 500
	maxStatusLength       = 160
	maxCollections        = 100
	maxCollectionItems    = 500
	maxCollectionName     = 80
	maxCollectionText     = 1000
	maxItemName           = 120
	profileReviewPageSize = 50
)

var (
	collectionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16}$`)
	collectionKinds     = map[string]*regexp.Regexp{"avatar": avatarID, "world": worldID}
	errNotFound         = errStatus(http.StatusNotFound, "Not found")
)

func newCollectionID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// singleLine trims s and rejects control characters, newlines included.
func singleLine(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "\n\t") {
		return "", false
	}
	s, ok := cleanReview(s)
	return s, ok && jsLength(s) <= max
}

// multiLine trims s and allows newlines and tabs but no other control characters.
func multiLine(s string, max int) (string, bool) {
	s, ok := cleanReview(s)
	return s, ok && jsLength(s) <= max
}

// itemImage accepts an empty string or a URL the image proxy would fetch.
func itemImage(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	u, err := url.Parse(s)
	return s, err == nil && len(s) <= 1000 && allowedImage(u)
}

// catalogAvatar returns the name and thumbnail the catalog holds for an
// avatar, so collections and reviews show the real name rather than whatever
// the client sent.
func (s *Server) catalogAvatar(ctx context.Context, id string) (name, image string) {
	if s.db == nil {
		return "", ""
	}
	var data string
	if s.db.QueryRowContext(ctx, "SELECT data FROM catalog WHERE id = ?", id).Scan(&data) != nil {
		return "", ""
	}
	var a struct{ Name, Thumbnail string }
	json.Unmarshal([]byte(data), &a)
	if _, ok := itemImage(a.Thumbnail); !ok {
		a.Thumbnail = ""
	}
	return a.Name, a.Thumbnail
}

// targetName picks the display name stored with a review: the catalog's for
// avatars, otherwise the client's, which only ever labels the author's own review.
func (s *Server) targetName(ctx context.Context, kind, id, supplied string) string {
	if kind == "avatar" {
		if name, _ := s.catalogAvatar(ctx, id); name != "" {
			supplied = name
		}
	}
	name, ok := singleLine(supplied, maxItemName)
	if !ok {
		return ""
	}
	return name
}

// social routes the profile and collection endpoints; acct may be nil.
func (s *Server) social(w http.ResponseWriter, r *http.Request, p string, acct *account) (bool, error) {
	post := r.Method == http.MethodPost
	switch {
	case p == "profile" && !post:
		return true, s.profile(w, r, acct)
	case p == "collection" && !post:
		return true, s.collection(w, r, acct)
	}
	if acct == nil {
		return false, nil
	}
	switch {
	case p == "profile" && post:
		return true, s.updateProfile(w, r, acct)
	case p == "collections" && !post:
		return true, s.myCollections(w, r, acct)
	case p == "collections/create" && post:
		return true, s.saveCollection(w, r, acct, true)
	case p == "collections/update" && post:
		return true, s.saveCollection(w, r, acct, false)
	case p == "collections/delete" && post:
		return true, s.collectionChange(w, r, acct, "delete")
	case p == "collections/add" && post:
		return true, s.collectionChange(w, r, acct, "add")
	case p == "collections/remove" && post:
		return true, s.collectionChange(w, r, acct, "remove")
	}
	return false, nil
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request, a *account) error {
	in, err := requestJSON(r)
	if err != nil {
		return err
	}
	// Validate the entire patch before updating: invalid text must not publish
	// a private profile or partially save another field.
	columns := []string{}
	args := []any{}
	if v, present := in["public"]; present {
		public, ok := v.(bool)
		if !ok {
			return errStatus(http.StatusBadRequest, "Invalid visibility")
		}
		columns = append(columns, "public = ?")
		args = append(args, public)
	}
	if v, present := in["bio"]; present {
		text, isString := v.(string)
		bio, ok := multiLine(text, maxBioLength)
		if !isString || !ok {
			return errStatus(http.StatusBadRequest, fmt.Sprintf("Your bio is plain text of up to %d characters.", maxBioLength))
		}
		columns = append(columns, "bio = ?")
		args = append(args, bio)
	}
	if v, present := in["status"]; present {
		text, isString := v.(string)
		status, ok := singleLine(text, maxStatusLength)
		if !isString || !ok {
			return errStatus(http.StatusBadRequest, fmt.Sprintf("Your VRCove status is a single line of up to %d characters.", maxStatusLength))
		}
		columns = append(columns, "status = ?")
		args = append(args, status)
	}
	if len(columns) > 0 {
		args = append(args, a.ID)
		if _, err := s.accounts.ExecContext(r.Context(), "UPDATE accounts SET "+strings.Join(columns, ", ")+" WHERE id = ?", args...); err != nil {
			return err
		}
	}
	return s.accountMe(w, r, a)
}

// profile serves #cove/<username>. A private profile and a missing one get
// the same answer.
func (s *Server) profile(w http.ResponseWriter, r *http.Request, viewer *account) error {
	ctx := r.Context()
	var id int64
	var username, created, bio, status string
	var public bool
	err := s.accounts.QueryRowContext(ctx, "SELECT id, username, created, bio, status, public FROM accounts WHERE username_key = ?",
		strings.ToLower(r.URL.Query().Get("username"))).Scan(&id, &username, &created, &bio, &status, &public)
	mine := viewer != nil && viewer.ID == id
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !public && !mine) {
		writeError(w, http.StatusNotFound, "This profile is private or doesn’t exist.")
		return nil
	}
	if err != nil {
		return err
	}
	var count int
	var average float64
	if err := s.accounts.QueryRowContext(ctx, "SELECT count(*), coalesce(avg(rating), 0) FROM reviews WHERE account_id = ?", id).Scan(&count, &average); err != nil {
		return err
	}
	rows, err := s.accounts.QueryContext(ctx, `SELECT kind, target, target_name, rating, body, created, updated FROM reviews
		WHERE account_id = ? ORDER BY updated DESC, id DESC LIMIT ? OFFSET ?`, id, profileReviewPageSize, queryInt(r.URL.Query().Get("offset"), 100000))
	if err != nil {
		return err
	}
	defer rows.Close()
	reviews := []map[string]any{}
	for rows.Next() {
		var kind, target, name, body string
		var rating int
		var createdAt, updatedAt int64
		if err := rows.Scan(&kind, &target, &name, &rating, &body, &createdAt, &updatedAt); err != nil {
			return err
		}
		reviews = append(reviews, map[string]any{"kind": kind, "target": target, "name": name, "rating": rating, "body": body,
			"date": s.day(time.Unix(createdAt, 0)), "edited": s.day(time.Unix(updatedAt, 0)) != s.day(time.Unix(createdAt, 0))})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	where := "account_id = ? AND public = 1"
	if mine {
		where = "account_id = ?"
	}
	collections, err := s.collectionSummaries(ctx, where, []any{id}, "")
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": username, "created": created, "bio": bio, "status": status, "public": public, "mine": mine,
		"picture": s.accountPictureURL(r, id, username), "reviewCount": count, "averageGiven": average, "reviews": reviews, "pageSize": profileReviewPageSize, "collections": collections})
	return nil
}

// collectionSummaries lists collections matching where, newest first, each
// with its item count, a few preview images and whether it holds contains.
func (s *Server) collectionSummaries(ctx context.Context, where string, args []any, contains string) ([]map[string]any, error) {
	rows, err := s.accounts.QueryContext(ctx, `SELECT c.id, c.kind, c.name, c.description, c.public, c.updated,
		(SELECT count(*) FROM collection_items i WHERE i.collection_id = c.id),
		EXISTS (SELECT 1 FROM collection_items i WHERE i.collection_id = c.id AND i.target = ?)
		FROM collections c WHERE `+where+` ORDER BY c.updated DESC LIMIT ?`, append(append([]any{contains}, args...), maxCollections)...)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		var id, kind, name, description string
		var public, has bool
		var updated int64
		var count int
		if err := rows.Scan(&id, &kind, &name, &description, &public, &updated, &count, &has); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "kind": kind, "name": name, "description": description, "public": public,
			"updated": s.day(time.Unix(updated, 0)), "count": count, "contains": has})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, c := range out {
		preview := []string{}
		images, err := s.accounts.QueryContext(ctx, "SELECT image FROM collection_items WHERE collection_id = ? AND image != '' ORDER BY added DESC LIMIT 4", c["id"])
		if err != nil {
			return nil, err
		}
		for images.Next() {
			var image string
			if err := images.Scan(&image); err != nil {
				images.Close()
				return nil, err
			}
			preview = append(preview, image)
		}
		images.Close()
		c["preview"] = preview
	}
	return out, nil
}

func (s *Server) myCollections(w http.ResponseWriter, r *http.Request, a *account) error {
	query := r.URL.Query()
	where, args := "c.account_id = ?", []any{a.ID}
	if kind := query.Get("kind"); kind != "" {
		if collectionKinds[kind] == nil {
			writeError(w, http.StatusBadRequest, "Invalid collection type")
			return nil
		}
		where, args = where+" AND c.kind = ?", append(args, kind)
	}
	list, err := s.collectionSummaries(r.Context(), where, args, query.Get("contains"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

// collection serves one collection to its owner, or to anyone if it is public.
func (s *Server) collection(w http.ResponseWriter, r *http.Request, viewer *account) error {
	ctx := r.Context()
	id := r.URL.Query().Get("id")
	var owner int64
	var kind, name, description, username string
	var public, ownerPublic bool
	var updated int64
	err := sql.ErrNoRows
	if collectionIDPattern.MatchString(id) {
		err = s.accounts.QueryRowContext(ctx, `SELECT c.account_id, c.kind, c.name, c.description, c.public, c.updated, a.username, a.public
			FROM collections c JOIN accounts a ON a.id = c.account_id WHERE c.id = ?`, id).
			Scan(&owner, &kind, &name, &description, &public, &updated, &username, &ownerPublic)
	}
	mine := viewer != nil && viewer.ID == owner
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !public && !mine) {
		writeError(w, http.StatusNotFound, "This collection is private or doesn’t exist.")
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := s.accounts.QueryContext(ctx, "SELECT target, name, image, added FROM collection_items WHERE collection_id = ? ORDER BY added DESC, rowid DESC", id)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var target, itemName, image string
		var added int64
		if err := rows.Scan(&target, &itemName, &image, &added); err != nil {
			return err
		}
		items = append(items, map[string]any{"id": target, "name": itemName, "image": image, "added": s.day(time.Unix(added, 0))})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var by any
	if ownerPublic || mine {
		by = username
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "kind": kind, "name": name, "description": description, "public": public,
		"updated": s.day(time.Unix(updated, 0)), "owner": by, "mine": mine, "items": items})
	return nil
}

func (s *Server) saveCollection(w http.ResponseWriter, r *http.Request, a *account, create bool) error {
	in, err := requestJSON(r)
	if err != nil {
		return err
	}
	nameText, _ := str(in, "name")
	descriptionText, _ := str(in, "description")
	public, _ := in["public"].(bool)
	name, ok1 := singleLine(nameText, maxCollectionName)
	description, ok2 := multiLine(descriptionText, maxCollectionText)
	if !ok1 || name == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Give the collection a name of up to %d characters.", maxCollectionName))
		return nil
	}
	if !ok2 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Descriptions are plain text of up to %d characters.", maxCollectionText))
		return nil
	}
	ctx, now := r.Context(), s.now().Unix()
	if !create {
		id, _ := str(in, "id")
		res, err := s.accounts.ExecContext(ctx, "UPDATE collections SET name = ?, description = ?, public = ?, updated = ? WHERE id = ? AND account_id = ?",
			name, description, public, now, id, a.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotFound
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
		return nil
	}
	kind, _ := str(in, "kind")
	if collectionKinds[kind] == nil {
		writeError(w, http.StatusBadRequest, "Collections hold avatars or worlds.")
		return nil
	}
	var n int
	if err := s.accounts.QueryRowContext(ctx, "SELECT count(*) FROM collections WHERE account_id = ?", a.ID).Scan(&n); err != nil {
		return err
	}
	if n >= maxCollections {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("You can have up to %d collections.", maxCollections))
		return nil
	}
	id := newCollectionID()
	if _, err := s.accounts.ExecContext(ctx, "INSERT INTO collections (id, account_id, kind, name, description, public, created, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, a.ID, kind, name, description, public, now, now); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
	return nil
}

// collectionChange deletes a collection or adds/removes one item.
func (s *Server) collectionChange(w http.ResponseWriter, r *http.Request, a *account, action string) error {
	in, err := requestJSON(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	id, _ := str(in, "id")
	var kind string
	switch err := s.accounts.QueryRowContext(ctx, "SELECT kind FROM collections WHERE id = ? AND account_id = ?", id, a.ID).Scan(&kind); {
	case errors.Is(err, sql.ErrNoRows):
		return errNotFound
	case err != nil:
		return err
	}
	if action == "delete" {
		if _, err := s.accounts.ExecContext(ctx, "DELETE FROM collections WHERE id = ?", id); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return nil
	}
	target, _ := str(in, "target")
	if !collectionKinds[kind].MatchString(target) {
		writeError(w, http.StatusBadRequest, "That doesn’t belong in a "+kind+" collection.")
		return nil
	}
	now := s.now().Unix()
	if action == "remove" {
		if _, err := s.accounts.ExecContext(ctx, "DELETE FROM collection_items WHERE collection_id = ? AND target = ?", id, target); err != nil {
			return err
		}
	} else {
		var n int
		if err := s.accounts.QueryRowContext(ctx, "SELECT count(*) FROM collection_items WHERE collection_id = ?", id).Scan(&n); err != nil {
			return err
		}
		if n >= maxCollectionItems {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("A collection holds up to %d items.", maxCollectionItems))
			return nil
		}
		nameText, _ := str(in, "name")
		imageText, _ := str(in, "image")
		name, okName := singleLine(nameText, maxItemName)
		// A thumbnail the image proxy wouldn't fetch is dropped, not fatal.
		image, okImage := itemImage(imageText)
		if !okImage {
			image = ""
		}
		if kind == "avatar" {
			if catalogName, catalogImage := s.catalogAvatar(ctx, target); catalogName != "" {
				name, image, okName = catalogName, catalogImage, true
			}
		}
		if !okName {
			writeError(w, http.StatusBadRequest, "Invalid item details")
			return nil
		}
		if _, err := s.accounts.ExecContext(ctx, `INSERT INTO collection_items (collection_id, target, name, image, added) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (collection_id, target) DO UPDATE SET name = excluded.name, image = excluded.image`, id, target, name, image, now); err != nil {
			return err
		}
	}
	if _, err := s.accounts.ExecContext(ctx, "UPDATE collections SET updated = ? WHERE id = ?", now, id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}
