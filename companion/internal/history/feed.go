package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var (
	userIDPattern  = regexp.MustCompile(`^usr_[0-9a-f-]{36}$`)
	worldIDPattern = regexp.MustCompile(`^wrld_[0-9a-f-]{36}$`)
	notePattern    = regexp.MustCompile(`^not_[a-zA-Z0-9_-]{10,100}$`)
)

func isUnresolvedName(name, userID string) bool {
	trimmed := strings.TrimSpace(name)
	return trimmed == "" || trimmed == userID || userIDPattern.MatchString(trimmed)
}

// Note is a VRChat notification from the live feed, for a desktop alert.
type Note struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	SenderUsername string `json:"senderUsername"`
	Message        string `json:"message"`
	WorldName      string `json:"worldName,omitempty"`
}

// FeedEntry is one change in a friend's activity.
type FeedEntry struct {
	ID          int64  `json:"id"`
	At          int64  `json:"at"`
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	// Kind is online, offline, location, status, added or removed.
	Kind      string `json:"kind"`
	Detail    string `json:"detail,omitempty"`
	Previous  string `json:"previous,omitempty"`
	WorldName string `json:"worldName,omitempty"`
}

type friendUser struct {
	DisplayName       string `json:"displayName"`
	Status            string `json:"status"`
	StatusDescription string `json:"statusDescription"`
}

// decodeContent reads a pipeline message's content, which VRChat sends either
// as JSON or as a JSON string holding JSON.
func decodeContent(raw json.RawMessage, into any) error {
	var inner string
	if json.Unmarshal(raw, &inner) == nil {
		raw = json.RawMessage(inner)
	}
	return json.Unmarshal(raw, into)
}

// Record stores friend activity from one VRChat pipeline message. It returns
// the notification when the message is one, so the app can show it.
func (s *Store) Record(ctx context.Context, message []byte, now time.Time) (*Note, error) {
	return s.RecordFor(ctx, "", message, now)
}

func (s *Store) RecordFor(ctx context.Context, owner string, message []byte, now time.Time) (*Note, error) {
	var envelope struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(message, &envelope) != nil {
		return nil, nil
	}
	if envelope.Type == "notification" {
		var n struct {
			ID             string          `json:"id"`
			Type           string          `json:"type"`
			SenderUsername string          `json:"senderUsername"`
			Message        string          `json:"message"`
			Details        json.RawMessage `json:"details"`
		}
		if decodeContent(envelope.Content, &n) != nil || !notePattern.MatchString(n.ID) {
			return nil, nil
		}
		note := &Note{ID: n.ID, Type: n.Type, SenderUsername: n.SenderUsername, Message: n.Message}
		var d struct {
			WorldName string `json:"worldName"`
		}
		if len(n.Details) > 0 && decodeContent(n.Details, &d) == nil {
			note.WorldName = d.WorldName
		}
		return note, nil
	}
	kind, ok := map[string]string{"friend-online": "online", "friend-offline": "offline", "friend-location": "location",
		"friend-update": "status", "friend-active": "status", "friend-add": "added", "friend-delete": "removed"}[envelope.Type]
	if !ok {
		return nil, nil
	}
	var c struct {
		UserID   string     `json:"userId"`
		User     friendUser `json:"user"`
		Location string     `json:"location"`
		World    struct {
			Name string `json:"name"`
		} `json:"world"`
	}
	if decodeContent(envelope.Content, &c) != nil || !userIDPattern.MatchString(c.UserID) {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var known struct {
		friendUser
		Location string
		Online   bool
		Found    bool
	}
	err := s.db.QueryRowContext(ctx, `SELECT display_name, status, status_description, location, online FROM friends WHERE owner = ? AND user_id = ?`, owner, c.UserID).
		Scan(&known.DisplayName, &known.Status, &known.StatusDescription, &known.Location, &known.Online)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	known.Found = err == nil
	if isUnresolvedName(known.DisplayName, c.UserID) {
		known.DisplayName = ""
	}
	next := known
	if !isUnresolvedName(c.User.DisplayName, c.UserID) {
		next.friendUser = c.User
	} else if c.User.Status != "" || c.User.StatusDescription != "" {
		next.Status = c.User.Status
		next.StatusDescription = c.User.StatusDescription
	}
	if isUnresolvedName(next.DisplayName, c.UserID) {
		next.DisplayName = s.lookupUserNameLocked(ctx, c.UserID)
	}
	worldName := c.World.Name
	if worldName == "" && strings.HasPrefix(c.Location, "wrld_") {
		worldName = s.lookupWorldNameLocked(ctx, c.Location)
	}
	var entries []FeedEntry
	add := func(kind, detail, previous, world string) {
		entries = append(entries, FeedEntry{Kind: kind, Detail: detail, Previous: previous, WorldName: world})
	}
	switch kind {
	case "online":
		next.Online, next.Location = true, c.Location
		if !known.Online || !known.Found {
			add("online", c.Location, "", worldName)
		}
	case "offline":
		if known.Online || !known.Found {
			add("offline", "", known.Location, "")
		}
		next.Online, next.Location = false, "offline"
	case "location":
		next.Online = true
		// "traveling" is the moment between two instances; record the arrival.
		if c.Location != "" && c.Location != "traveling" {
			next.Location = c.Location
			if c.Location != known.Location {
				add("location", c.Location, known.Location, worldName)
			}
		}
	case "added", "removed":
		add(kind, "", "", "")
	}
	if known.Found && !isUnresolvedName(c.User.DisplayName, c.UserID) && (c.User.Status != known.Status || c.User.StatusDescription != known.StatusDescription) {
		add("status", strings.TrimSpace(c.User.Status+" "+c.User.StatusDescription), strings.TrimSpace(known.Status+" "+known.StatusDescription), "")
	}
	resolvedName := !isUnresolvedName(next.DisplayName, c.UserID)
	if !resolvedName {
		next.DisplayName = c.UserID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO feed (owner, at, user_id, display_name, kind, detail, previous, world_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			owner, now.UnixMilli(), c.UserID, next.DisplayName, e.Kind, e.Detail, e.Previous, e.WorldName); err != nil {
			return nil, err
		}
	}
	if resolvedName {
		if _, err := tx.ExecContext(ctx, `UPDATE feed SET display_name = ? WHERE user_id = ? AND (display_name = '' OR display_name = user_id OR display_name LIKE 'usr_%')`,
			next.DisplayName, c.UserID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE friends SET display_name = ? WHERE user_id = ? AND (display_name = '' OR display_name = user_id OR display_name LIKE 'usr_%')`,
			next.DisplayName, c.UserID); err != nil {
			return nil, err
		}
	}
	if kind == "removed" {
		_, err = tx.ExecContext(ctx, `DELETE FROM friends WHERE owner = ? AND user_id = ?`, owner, c.UserID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO friends (owner,user_id, display_name, status, status_description, location, online) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (owner,user_id) DO UPDATE SET
			display_name = CASE WHEN excluded.display_name != '' AND excluded.display_name != excluded.user_id AND excluded.display_name NOT LIKE 'usr_%' THEN excluded.display_name ELSE friends.display_name END,
			status = excluded.status,
			status_description = excluded.status_description, location = excluded.location, online = excluded.online`,
			owner, c.UserID, next.DisplayName, next.Status, next.StatusDescription, next.Location, next.Online)
	}
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

func (s *Store) lookupUserNameLocked(ctx context.Context, userID string) string {
	for _, query := range []string{
		`SELECT display_name FROM friends WHERE user_id = ? AND display_name != '' AND display_name != user_id AND display_name NOT LIKE 'usr_%' LIMIT 1`,
		`SELECT display_name FROM feed WHERE user_id = ? AND display_name != '' AND display_name != user_id AND display_name NOT LIKE 'usr_%' ORDER BY at DESC LIMIT 1`,
		`SELECT display_name FROM encounters WHERE user_id = ? AND display_name != '' AND display_name != user_id AND display_name NOT LIKE 'usr_%' ORDER BY joined DESC LIMIT 1`,
	} {
		var name string
		if err := s.db.QueryRowContext(ctx, query, userID).Scan(&name); err == nil && !isUnresolvedName(name, userID) {
			return name
		}
	}
	return ""
}

func (s *Store) lookupWorldNameLocked(ctx context.Context, location string) string {
	worldID := strings.SplitN(location, ":", 2)[0]
	if !worldIDPattern.MatchString(worldID) {
		return ""
	}
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT world_name FROM visits WHERE world_id = ? AND world_name != '' ORDER BY joined DESC LIMIT 1`, worldID).Scan(&name); err == nil && name != "" {
		return name
	}
	if err := s.db.QueryRowContext(ctx, `SELECT world_name FROM feed WHERE (detail = ? OR detail LIKE ?) AND world_name != '' ORDER BY at DESC LIMIT 1`, worldID, worldID+":%").Scan(&name); err == nil && name != "" {
		return name
	}
	return ""
}

// RememberUsers records resolved display names for user IDs and backfills any
// feed or friend rows that only had raw usr_ IDs.
func (s *Store) RememberUsers(ctx context.Context, owner string, users map[string]string) error {
	if len(users) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, name := range users {
		name = strings.TrimSpace(name)
		if !userIDPattern.MatchString(id) || isUnresolvedName(name, id) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE feed SET display_name = ? WHERE user_id = ? AND (display_name = '' OR display_name = user_id OR display_name LIKE 'usr_%')`, name, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE friends SET display_name = ? WHERE user_id = ? AND (display_name = '' OR display_name = user_id OR display_name LIKE 'usr_%')`, name, id); err != nil {
			return err
		}
		if owner != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO friends (owner, user_id, display_name, status, status_description, location, online)
				VALUES (?, ?, ?, '', '', 'offline', 0)
				ON CONFLICT (owner, user_id) DO UPDATE SET display_name = excluded.display_name`, owner, id, name); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// RememberWorld backfills a world's display name onto feed and visit rows that
// only recorded its wrld_ identifier.
func (s *Store) RememberWorld(ctx context.Context, worldID, worldName string) error {
	worldName = strings.TrimSpace(worldName)
	if !worldIDPattern.MatchString(worldID) || worldName == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE feed SET world_name = ? WHERE (detail = ? OR detail LIKE ?) AND world_name = ''`, worldName, worldID, worldID+":%"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE visits SET world_name = ? WHERE world_id = ? AND world_name = ''`, worldName, worldID); err != nil {
		return err
	}
	return tx.Commit()
}

// Feed lists friend activity before `before` (Unix ms; 0 for now), newest
// first, optionally only of one kind or matching a name.
func (s *Store) Feed(ctx context.Context, before int64, kind, q string, limit int) ([]FeedEntry, error) {
	return s.FeedFor(ctx, "", before, kind, q, limit)
}

func (s *Store) FeedFor(ctx context.Context, owner string, before int64, kind, q string, limit int) ([]FeedEntry, error) {
	if before <= 0 {
		before = time.Now().UnixMilli() + 1
	}
	resolvedNameExpr := `CASE
		WHEN feed.display_name != '' AND feed.display_name != feed.user_id AND feed.display_name NOT LIKE 'usr_%' THEN feed.display_name
		ELSE COALESCE(
			(SELECT f.display_name FROM friends f WHERE f.user_id = feed.user_id AND f.display_name != '' AND f.display_name != f.user_id AND f.display_name NOT LIKE 'usr_%' LIMIT 1),
			(SELECT f2.display_name FROM feed f2 WHERE f2.user_id = feed.user_id AND f2.display_name != '' AND f2.display_name != f2.user_id AND f2.display_name NOT LIKE 'usr_%' ORDER BY f2.at DESC LIMIT 1),
			(SELECT e.display_name FROM encounters e WHERE e.user_id = feed.user_id AND e.display_name != '' AND e.display_name != e.user_id AND e.display_name NOT LIKE 'usr_%' ORDER BY e.joined DESC LIMIT 1),
			feed.display_name
		)
	END`
	resolvedWorldExpr := `CASE
		WHEN feed.world_name != '' THEN feed.world_name
		WHEN feed.detail LIKE 'wrld_%' THEN COALESCE(
			(SELECT v.world_name FROM visits v WHERE (feed.detail = v.world_id OR feed.detail LIKE v.world_id || ':%') AND v.world_name != '' ORDER BY v.joined DESC LIMIT 1),
			''
		)
		ELSE ''
	END`
	query := `SELECT id, at, user_id, ` + resolvedNameExpr + `, kind, detail, previous, ` + resolvedWorldExpr + ` FROM feed WHERE owner = ? AND at < ?`
	args := []any{owner, before}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	if q = strings.TrimSpace(q); q != "" {
		query += ` AND ((` + resolvedNameExpr + `) LIKE ? ESCAPE '\' OR (` + resolvedWorldExpr + `) LIKE ? ESCAPE '\' OR user_id LIKE ? ESCAPE '\')`
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		args = append(args, like, like, like)
	}
	query += ` ORDER BY at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []FeedEntry{}
	for rows.Next() {
		var e FeedEntry
		if err := rows.Scan(&e.ID, &e.At, &e.UserID, &e.DisplayName, &e.Kind, &e.Detail, &e.Previous, &e.WorldName); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
