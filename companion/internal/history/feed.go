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
	userIDPattern = regexp.MustCompile(`^usr_[0-9a-f-]{36}$`)
	notePattern   = regexp.MustCompile(`^not_[a-zA-Z0-9_-]{10,100}$`)
)

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
	err := s.db.QueryRowContext(ctx, `SELECT display_name, status, status_description, location, online FROM friends WHERE user_id = ?`, c.UserID).
		Scan(&known.DisplayName, &known.Status, &known.StatusDescription, &known.Location, &known.Online)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	known.Found = err == nil
	next := known
	if c.User.DisplayName != "" {
		next.friendUser = c.User
	}
	var entries []FeedEntry
	add := func(kind, detail, previous, world string) {
		entries = append(entries, FeedEntry{Kind: kind, Detail: detail, Previous: previous, WorldName: world})
	}
	switch kind {
	case "online":
		next.Online, next.Location = true, c.Location
		if !known.Online || !known.Found {
			add("online", c.Location, "", c.World.Name)
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
				add("location", c.Location, known.Location, c.World.Name)
			}
		}
	case "added", "removed":
		add(kind, "", "", "")
	}
	if known.Found && c.User.DisplayName != "" && (c.User.Status != known.Status || c.User.StatusDescription != known.StatusDescription) {
		add("status", strings.TrimSpace(c.User.Status+" "+c.User.StatusDescription), strings.TrimSpace(known.Status+" "+known.StatusDescription), "")
	}
	if next.DisplayName == "" {
		next.DisplayName = c.UserID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO feed (at, user_id, display_name, kind, detail, previous, world_name) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			now.UnixMilli(), c.UserID, next.DisplayName, e.Kind, e.Detail, e.Previous, e.WorldName); err != nil {
			return nil, err
		}
	}
	if kind == "removed" {
		_, err = tx.ExecContext(ctx, `DELETE FROM friends WHERE user_id = ?`, c.UserID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO friends (user_id, display_name, status, status_description, location, online) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id) DO UPDATE SET display_name = excluded.display_name, status = excluded.status,
			status_description = excluded.status_description, location = excluded.location, online = excluded.online`,
			c.UserID, next.DisplayName, next.Status, next.StatusDescription, next.Location, next.Online)
	}
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// Feed lists friend activity before `before` (Unix ms; 0 for now), newest
// first, optionally only of one kind or matching a name.
func (s *Store) Feed(ctx context.Context, before int64, kind, q string, limit int) ([]FeedEntry, error) {
	if before <= 0 {
		before = time.Now().UnixMilli() + 1
	}
	query := `SELECT id, at, user_id, display_name, kind, detail, previous, world_name FROM feed WHERE at < ?`
	args := []any{before}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	if q = strings.TrimSpace(q); q != "" {
		query += ` AND (display_name LIKE ? ESCAPE '\' OR world_name LIKE ? ESCAPE '\')`
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		args = append(args, like, like)
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
