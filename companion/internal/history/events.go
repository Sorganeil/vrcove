package history

import (
	"context"
	"strings"
	"time"
)

// LogEntry is one line of the game log, newest first, like VRCX's Game Log.
type LogEntry struct {
	At int64 `json:"at"`
	// Kind is location, join, leave, avatar or video.
	Kind        string `json:"kind"`
	WorldID     string `json:"worldId,omitempty"`
	WorldName   string `json:"worldName,omitempty"`
	Location    string `json:"location,omitempty"`
	UserID      string `json:"userId,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	// Detail is the avatar name or the video address.
	Detail string `json:"detail,omitempty"`
}

// gameLogQuery joins every kind of event into one timeline. Each part names
// the same columns: at, kind, world id and name, location, user, detail.
const gameLogQuery = `SELECT at, kind, world_id, world_name, location, user_id, display_name, detail FROM (
	SELECT v.joined AS at, 'location' AS kind, v.world_id, v.world_name, v.location, '' AS user_id, '' AS display_name, '' AS detail
		FROM visits v
	UNION ALL SELECT e.joined, 'join', v.world_id, v.world_name, v.location, e.user_id, e.display_name, ''
		FROM encounters e JOIN visits v ON v.id = e.visit_id
	UNION ALL SELECT e.left_at, 'leave', v.world_id, v.world_name, v.location, e.user_id, e.display_name, ''
		FROM encounters e JOIN visits v ON v.id = e.visit_id WHERE e.left_at IS NOT NULL AND (v.left_at IS NULL OR e.left_at < v.left_at)
	UNION ALL SELECT a.at, 'avatar', v.world_id, v.world_name, v.location, '', a.display_name, a.avatar_name
		FROM avatar_changes a JOIN visits v ON v.id = a.visit_id
	UNION ALL SELECT m.at, 'video', v.world_id, v.world_name, v.location, '', '', m.url
		FROM videos m JOIN visits v ON v.id = m.visit_id
) WHERE at < ?`

// GameLog lists game log events before `before` (Unix ms; 0 for now), newest
// first. kinds is a comma-separated list of kinds to include (all when empty);
// q matches world, person, avatar or video.
func (s *Store) GameLog(ctx context.Context, before int64, kinds, q string, limit int) ([]LogEntry, error) {
	if before <= 0 {
		before = time.Now().UnixMilli() + 1
	}
	query, args := gameLogQuery, []any{before}
	if list := kindList(kinds); len(list) > 0 {
		query += ` AND kind IN (?` + strings.Repeat(`, ?`, len(list)-1) + `)`
		for _, k := range list {
			args = append(args, k)
		}
	}
	if q = strings.TrimSpace(q); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		query += ` AND (world_name LIKE ? ESCAPE '\' OR display_name LIKE ? ESCAPE '\' OR detail LIKE ? ESCAPE '\')`
		args = append(args, like, like, like)
	}
	// Within the same second, the instance comes before the people in it.
	query += ` ORDER BY at DESC, CASE kind WHEN 'location' THEN 1 ELSE 0 END LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.At, &e.Kind, &e.WorldID, &e.WorldName, &e.Location, &e.UserID, &e.DisplayName, &e.Detail); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
