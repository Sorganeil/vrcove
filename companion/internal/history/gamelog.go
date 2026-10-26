package history

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// VRChat writes output_log_<date>.txt in its LocalLow folder. These are the
// lines that matter for history; everything else is skipped.
var (
	logLine      = regexp.MustCompile(`^(\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2}) +\S+ +- +(.*)$`)
	joining      = regexp.MustCompile(`^\[Behaviour\] Joining ((wrld_[0-9a-f-]+)(?::\S*)?)$`)
	enteringRoom = regexp.MustCompile(`^\[Behaviour\] (?:Joining or Creating|Entering) Room: (.+)$`)
	playerJoined = regexp.MustCompile(`^\[Behaviour\] OnPlayerJoined (.+?)(?: \((usr_[0-9a-f-]+)\))?$`)
	playerLeft   = regexp.MustCompile(`^\[Behaviour\] OnPlayerLeft (.+?)(?: \((usr_[0-9a-f-]+)\))?$`)
	leftRoom     = regexp.MustCompile(`^\[Behaviour\] OnLeftRoom$`)
	appQuit      = regexp.MustCompile(`^(?:\[Behaviour\] OnApplicationQuit|VRCApplication: HandleApplicationQuit).*$`)
	avatarSwitch = regexp.MustCompile(`^\[Behaviour\] Switching (.+) to avatar (.+)$`)
)

// LogFolder finds VRChat's log folder from its analytics folder, which the
// avatar logger already detects: both sit in the same AppData folder, under
// Local\Temp\VRChat\VRChat and LocalLow\VRChat\VRChat.
func LogFolder(analytics string) string {
	appData := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(analytics))))
	return filepath.Join(appData, "LocalLow", "VRChat", "VRChat")
}

// gameLog tracks the instance being read, across lines and files.
type gameLog struct {
	visit     int64
	location  string
	worldID   string
	worldName string
	joined    int64
	people    int
	lastAt    int64
}

// ReadLogs reads new lines from VRChat's logs in folder into the store.
func (s *Store) ReadLogs(ctx context.Context, folder string) error {
	files, _ := filepath.Glob(filepath.Join(folder, "output_log_*.txt"))
	// The names hold the start time, so name order is time order.
	sort.Strings(files)
	s.mu.Lock()
	defer s.mu.Unlock()
	g := gameLog{}
	// Continue the visit still open when the app last read the logs.
	s.db.QueryRowContext(ctx, `SELECT id, location, world_id, world_name, joined,
		(SELECT count(*) FROM encounters e WHERE e.visit_id = visits.id),
		COALESCE((SELECT MAX(COALESCE(e.left_at, e.joined)) FROM encounters e WHERE e.visit_id = visits.id), joined)
		FROM visits WHERE left_at IS NULL ORDER BY joined DESC LIMIT 1`).
		Scan(&g.visit, &g.location, &g.worldID, &g.worldName, &g.joined, &g.people, &g.lastAt)
	for _, file := range files {
		if err := s.readLog(ctx, &g, file); err != nil {
			return err
		}
	}
	if g.visit != 0 && len(files) > 0 {
		if info, err := os.Stat(files[len(files)-1]); err == nil && time.Since(info.ModTime()) > 15*time.Minute {
			endAt := g.lastAt
			if mod := info.ModTime().UnixMilli(); endAt < g.joined || (mod >= g.joined && mod > endAt) {
				endAt = mod
			}
			if endAt < g.joined {
				endAt = g.joined
			}
			if err := s.closeVisit(ctx, &g, endAt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) readLog(ctx context.Context, g *gameLog, file string) error {
	name := filepath.Base(file)
	var offset int64
	err := s.db.QueryRowContext(ctx, `SELECT offset FROM log_files WHERE name = ?`, name).Scan(&offset)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		return nil // VRChat may be rotating it; try again next time.
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() <= offset {
		return nil
	}
	if offset == 0 && g.visit != 0 {
		// A new log means VRChat restarted; the last instance has ended.
		endAt := g.lastAt
		if endAt <= 0 || endAt < g.joined {
			endAt = info.ModTime().UnixMilli()
		}
		if err := s.closeVisit(ctx, g, endAt); err != nil {
			return err
		}
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	// Read at most 32 MB per pass so a huge backlog does not stall the app.
	data, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		return err
	}
	// Leave a line VRChat is still writing for the next pass.
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, line := range strings.Split(string(data[:end]), "\n") {
		if err := g.apply(ctx, tx, strings.TrimRight(line, "\r")); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO log_files (name, offset) VALUES (?, ?) ON CONFLICT (name) DO UPDATE SET offset = excluded.offset`,
		name, offset+int64(end)+1); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) closeVisit(ctx context.Context, g *gameLog, at int64) error {
	err := endVisit(ctx, s.db, g.visit, at)
	g.visit, g.location, g.worldID, g.worldName, g.joined, g.people = 0, "", "", "", 0, 0
	return err
}

// apply records one log line.
func (g *gameLog) apply(ctx context.Context, tx *sql.Tx, line string) error {
	m := logLine.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	t, err := time.ParseInLocation("2006.01.02 15:04:05", m[1], time.Local)
	if err != nil {
		return nil
	}
	at, msg := t.UnixMilli(), m[2]
	g.lastAt = at
	switch {
	case joining.MatchString(msg):
		j := joining.FindStringSubmatch(msg)
		g.location, g.worldID = j[1], j[2]
		if g.visit != 0 && at-g.joined <= 30000 && g.people == 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE visits SET location = ?, world_id = ? WHERE id = ? AND (world_id = '' OR world_id = ?)`,
				g.location, g.worldID, g.visit, g.worldID); err != nil {
				return err
			}
		}
	case enteringRoom.MatchString(msg):
		world := enteringRoom.FindStringSubmatch(msg)[1]
		if g.visit != 0 && world == g.worldName && at-g.joined <= 30000 && g.people == 0 {
			// VRChat logs both "Entering Room" and "Joining or Creating Room"
			// for one join; update the open visit instead of duplicating it.
			_, err := tx.ExecContext(ctx, `UPDATE visits SET
				location = CASE WHEN ? != '' THEN ? ELSE location END,
				world_id = CASE WHEN ? != '' THEN ? ELSE world_id END
				WHERE id = ?`, g.location, g.location, g.worldID, g.worldID, g.visit)
			return err
		}
		if g.visit != 0 {
			if err := endVisit(ctx, tx, g.visit, at); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO visits (location, world_id, world_name, joined) VALUES (?, ?, ?, ?)`, g.location, g.worldID, world, at)
		if err != nil {
			return err
		}
		g.visit, _ = res.LastInsertId()
		g.worldName, g.joined, g.people = world, at, 0
	case g.visit == 0:
		// People and avatars only count inside an instance.
	case playerJoined.MatchString(msg):
		p := playerJoined.FindStringSubmatch(msg)
		g.people++
		_, err := tx.ExecContext(ctx, `INSERT INTO encounters (visit_id, user_id, display_name, joined) VALUES (?, ?, ?, ?)`, g.visit, p[2], p[1], at)
		return err
	case playerLeft.MatchString(msg):
		p := playerLeft.FindStringSubmatch(msg)
		_, err := tx.ExecContext(ctx, `UPDATE encounters SET left_at = ? WHERE id = (SELECT id FROM encounters
			WHERE visit_id = ? AND display_name = ? AND left_at IS NULL ORDER BY joined DESC LIMIT 1)`, at, g.visit, p[1])
		return err
	case avatarSwitch.MatchString(msg):
		a := avatarSwitch.FindStringSubmatch(msg)
		_, err := tx.ExecContext(ctx, `INSERT INTO avatar_changes (visit_id, display_name, avatar_name, at) VALUES (?, ?, ?, ?)`, g.visit, a[1], a[2], at)
		return err
	case leftRoom.MatchString(msg), appQuit.MatchString(msg):
		err := endVisit(ctx, tx, g.visit, at)
		g.visit, g.location, g.worldID, g.worldName, g.joined, g.people = 0, "", "", "", 0, 0
		return err
	}
	return nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// endVisit marks a visit, and everyone still in it, as left at `at`.
func endVisit(ctx context.Context, tx execer, visit, at int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE encounters SET left_at = ? WHERE visit_id = ? AND left_at IS NULL`, at, visit); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE visits SET left_at = ? WHERE id = ? AND left_at IS NULL`, at, visit)
	return err
}
