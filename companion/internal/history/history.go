// Package history keeps the desktop app's local VRChat history: instances
// joined and people met (from VRChat's own logs), friend activity (from
// VRChat's live feed), and screenshots. It is stored only on this device.
package history

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS log_files (name TEXT PRIMARY KEY, offset INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS visits (id INTEGER PRIMARY KEY, location TEXT NOT NULL, world_id TEXT NOT NULL,
	world_name TEXT NOT NULL, joined INTEGER NOT NULL, left_at INTEGER);
CREATE INDEX IF NOT EXISTS visits_joined ON visits (joined);
CREATE TABLE IF NOT EXISTS encounters (id INTEGER PRIMARY KEY, visit_id INTEGER NOT NULL REFERENCES visits(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL, display_name TEXT NOT NULL, joined INTEGER NOT NULL, left_at INTEGER);
CREATE INDEX IF NOT EXISTS encounters_visit ON encounters (visit_id);
CREATE INDEX IF NOT EXISTS encounters_user ON encounters (user_id);
CREATE TABLE IF NOT EXISTS avatar_changes (id INTEGER PRIMARY KEY, visit_id INTEGER NOT NULL REFERENCES visits(id) ON DELETE CASCADE,
	display_name TEXT NOT NULL, avatar_name TEXT NOT NULL, at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS feed (id INTEGER PRIMARY KEY, at INTEGER NOT NULL, user_id TEXT NOT NULL, display_name TEXT NOT NULL,
	kind TEXT NOT NULL, detail TEXT NOT NULL, previous TEXT NOT NULL DEFAULT '', world_name TEXT NOT NULL DEFAULT '', owner TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS feed_at ON feed (at);
CREATE TABLE IF NOT EXISTS friends (owner TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL, display_name TEXT NOT NULL, status TEXT NOT NULL,
	status_description TEXT NOT NULL, location TEXT NOT NULL, online INTEGER NOT NULL, PRIMARY KEY(owner,user_id));
`

// Store is the local history database.
type Store struct {
	db *sql.DB
	// mu serializes writers; SQLite allows one at a time anyway.
	mu         sync.Mutex
	legacyFeed bool
}

// Open opens or creates the history database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	var scoped int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('feed') WHERE name='owner'`).Scan(&scoped); err != nil {
		db.Close()
		return nil, err
	}
	if scoped == 0 {
		tx, err := db.Begin()
		if err != nil {
			db.Close()
			return nil, err
		}
		_, err = tx.Exec(`ALTER TABLE feed ADD COLUMN owner TEXT NOT NULL DEFAULT '';
		ALTER TABLE friends RENAME TO friends_legacy;
		CREATE TABLE friends (owner TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL, display_name TEXT NOT NULL, status TEXT NOT NULL,
		status_description TEXT NOT NULL, location TEXT NOT NULL, online INTEGER NOT NULL, PRIMARY KEY(owner,user_id));
		INSERT INTO friends SELECT '',user_id,display_name,status,status_description,location,online FROM friends_legacy;
		DROP TABLE friends_legacy;`)
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS feed_owner_at ON feed(owner,at)`); err != nil {
		db.Close()
		return nil, err
	}
	cleanupDuplicateVisits(db)
	return &Store{db: db, legacyFeed: scoped == 0}, nil
}

// cleanupDuplicateVisits repairs visits split across VRChat's paired
// "Entering Room" and "Joining or Creating Room" log lines, and closes open
// visits that were superseded by a later visit.
func cleanupDuplicateVisits(db *sql.DB) {
	_, _ = db.Exec(`UPDATE visits SET
		world_id = CASE WHEN world_id = '' THEN COALESCE((SELECT v2.world_id FROM visits v2 WHERE v2.world_name = visits.world_name AND v2.world_id != '' ORDER BY ABS(v2.joined - visits.joined), v2.joined DESC LIMIT 1), '') ELSE world_id END,
		location = CASE WHEN location = '' THEN COALESCE((SELECT v2.location FROM visits v2 WHERE v2.world_name = visits.world_name AND v2.location != '' AND ABS(v2.joined - visits.joined) <= 30000 ORDER BY ABS(v2.joined - visits.joined), v2.joined DESC LIMIT 1), '') ELSE location END
		WHERE world_id = '' OR location = ''`)
	_, _ = db.Exec(`DELETE FROM visits WHERE
		NOT EXISTS (SELECT 1 FROM encounters e WHERE e.visit_id = visits.id)
		AND left_at IS NOT NULL AND left_at - joined <= 5000
		AND EXISTS (SELECT 1 FROM visits v2 WHERE v2.id != visits.id AND v2.world_name = visits.world_name AND ABS(v2.joined - visits.joined) <= 5000)`)
	_, _ = db.Exec(`UPDATE visits SET left_at = COALESCE(
		(SELECT MAX(COALESCE(e.left_at, e.joined)) FROM encounters e WHERE e.visit_id = visits.id),
		joined
	) WHERE left_at IS NULL AND EXISTS (SELECT 1 FROM visits v2 WHERE v2.joined > visits.joined)`)
}

// ClaimLegacyFeed attributes the pre-switcher feed to the account that was
// active during upgrade. If none is known, old rows stay intact and unassigned.
func (s *Store) ClaimLegacyFeed(ctx context.Context, owner string) error {
	if !s.legacyFeed || owner == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE feed SET owner=? WHERE owner=''`, owner); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE friends SET owner=? WHERE owner=''`, owner); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

// Visit is one stay in an instance and the people seen there.
type Visit struct {
	ID        int64       `json:"id"`
	Location  string      `json:"location"`
	WorldID   string      `json:"worldId"`
	WorldName string      `json:"worldName"`
	Joined    int64       `json:"joined"`
	Left      int64       `json:"left,omitempty"`
	People    []Encounter `json:"people"`
}

// Encounter is one person in an instance while you were there.
type Encounter struct {
	UserID      string `json:"userId,omitempty"`
	DisplayName string `json:"displayName"`
	Joined      int64  `json:"joined"`
	Left        int64  `json:"left,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
}

// Visits lists visits that started before `before` (Unix ms; 0 for now),
// newest first. q filters by world or person name.
func (s *Store) Visits(ctx context.Context, before int64, q string, limit int) ([]Visit, error) {
	if before <= 0 {
		before = time.Now().UnixMilli() + 1
	}
	query := `SELECT id, location, world_id, world_name, joined, COALESCE(left_at, 0) FROM visits WHERE joined < ?`
	args := []any{before}
	if q = strings.TrimSpace(q); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		query += ` AND (world_name LIKE ? ESCAPE '\' OR id IN (SELECT visit_id FROM encounters WHERE display_name LIKE ? ESCAPE '\'))`
		args = append(args, like, like)
	}
	query += ` ORDER BY joined DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var visits []Visit
	for rows.Next() {
		var v Visit
		if err := rows.Scan(&v.ID, &v.Location, &v.WorldID, &v.WorldName, &v.Joined, &v.Left); err != nil {
			rows.Close()
			return nil, err
		}
		v.People = []Encounter{}
		visits = append(visits, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range visits {
		people, err := s.db.QueryContext(ctx, `SELECT e.user_id, e.display_name, e.joined, COALESCE(e.left_at, 0),
			COALESCE((SELECT a.avatar_name FROM avatar_changes a WHERE a.visit_id = e.visit_id AND a.display_name = e.display_name ORDER BY a.at DESC LIMIT 1), '')
			FROM encounters e WHERE e.visit_id = ? ORDER BY e.joined`, visits[i].ID)
		if err != nil {
			return nil, err
		}
		for people.Next() {
			var e Encounter
			if err := people.Scan(&e.UserID, &e.DisplayName, &e.Joined, &e.Left, &e.Avatar); err != nil {
				people.Close()
				return nil, err
			}
			visits[i].People = append(visits[i].People, e)
		}
		people.Close()
	}
	return visits, nil
}

// VisitAt returns the visit you were in at t (Unix ms), if any.
func (s *Store) VisitAt(ctx context.Context, t int64) (*Visit, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM visits WHERE joined <= ? AND (left_at IS NULL OR left_at >= ?) ORDER BY joined DESC LIMIT 1`, t, t).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.visit(ctx, id, t)
}

// visit loads one visit with the people present at t.
func (s *Store) visit(ctx context.Context, id, t int64) (*Visit, error) {
	var v Visit
	err := s.db.QueryRowContext(ctx, `SELECT id, location, world_id, world_name, joined, COALESCE(left_at, 0) FROM visits WHERE id = ?`, id).
		Scan(&v.ID, &v.Location, &v.WorldID, &v.WorldName, &v.Joined, &v.Left)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, display_name, joined, COALESCE(left_at, 0) FROM encounters
		WHERE visit_id = ? AND joined <= ? AND (left_at IS NULL OR left_at >= ?) ORDER BY display_name`, id, t, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v.People = []Encounter{}
	for rows.Next() {
		var e Encounter
		if err := rows.Scan(&e.UserID, &e.DisplayName, &e.Joined, &e.Left); err != nil {
			return nil, err
		}
		v.People = append(v.People, e)
	}
	return &v, rows.Err()
}
