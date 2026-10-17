package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// LoggerConfig enables ingestion using session-bound, user-generated API keys.
type LoggerConfig struct {
	Catalog *sql.DB
}

const (
	// loggerBatches ingestion requests of up to loggerBatchSize IDs are accepted
	// per VRChat account in each rate-limit window (ten minutes). Requests that
	// reach verification count even when every ID is rejected.
	loggerBatches   = 40
	loggerBatchSize = 5
	// loggerChecks bounds the catalog-only preflight, which never contacts
	// VRChat and does not consume ingestion quota.
	loggerChecks = 600
	// loggerBusyWait is suggested when another verification is in progress.
	loggerBusyWait = 15 * time.Second
)

var loggerAvatarID = regexp.MustCompile(`^avtr_[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type loggerResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (s *Server) loggerAvatars(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		return errStatus(http.StatusMethodNotAllowed, "Use POST to submit avatar IDs.")
	}
	cfg := s.cfg.Logger
	if cfg == nil {
		return errStatus(http.StatusServiceUnavailable, "Logger ingestion is not enabled.")
	}
	key, session, err := s.authenticateLoggerKey(r)
	if err != nil {
		return err
	}
	ids, err := loggerIDs(r)
	if err != nil {
		return err
	}
	// A busy refusal does no verification, so it does not use quota.
	select {
	case s.loggerSlot <- struct{}{}:
		defer func() { <-s.loggerSlot }()
	default:
		loggerRetry(w, "vrcove", loggerBusyWait)
		return errStatus(http.StatusTooManyRequests, "Logger verification is busy. Try again later.")
	}
	if wait := s.limiter.allowN("logger-ingestion:"+string(s.linkKey(session.UserID)), loggerBatches, s.now()); wait > 0 {
		loggerRetry(w, "vrcove", wait)
		return errStatus(http.StatusTooManyRequests, "Logger submission limit reached. Try again later.")
	}
	results := []loggerResult{}
	seen := map[string]bool{}
	// Avatars removed at their owner's request are never added again.
	removed, err := s.blacklisted(r.Context(), ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if !loggerAvatarID.MatchString(id) || removed[id] {
			results = append(results, loggerResult{id, "rejected"})
			continue
		}
		data, updates, err := s.vrc(r.Context(), "/avatars/"+id, session, vrcRequest{})
		session.apply(updates)
		if err != nil {
			var failure *sessionFailure
			if errors.As(err, &failure) {
				_ = s.forgetSession(r.Context(), session)
				return invalidLoggerKey()
			}
			var he *httpError
			// VRChat limited the server's verification: tell the desktop when to
			// retry, whether or not earlier IDs in this batch completed.
			if errors.As(err, &he) && he.status == http.StatusTooManyRequests {
				loggerRetry(w, "vrchat", s.backoff.remaining(backoffKey(session), s.now()))
			}
			if errors.As(err, &he) && (he.status == 404 || he.status == 403) {
				results = append(results, loggerResult{id, "rejected"})
				continue
			}
			// Transient/auth failures remain in the desktop queue. Return any
			// completed work so it isn't repeated because a later ID failed.
			if len(results) == 0 {
				return err
			}
			break
		}
		a, ok := data.(map[string]any)
		if !ok || field(a, "id") != id || field(a, "releaseStatus") != "public" {
			results = append(results, loggerResult{id, "rejected"})
			continue
		}
		if !s.loggerKeyLive(r.Context(), key, session) {
			return invalidLoggerKey()
		}
		// Verify validity/public visibility BEFORE checking for an existing row.
		var exists int
		err = cfg.Catalog.QueryRowContext(r.Context(), "SELECT 1 FROM catalog WHERE id = ?", id).Scan(&exists)
		if err == nil {
			results = append(results, loggerResult{id, "existing"})
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		name, author := field(a, "name"), field(a, "authorName")
		pc, quest, rating := false, false, "Unknown"
		if packages, ok := a["unityPackages"].([]any); ok {
			for _, raw := range packages {
				p, ok := raw.(map[string]any)
				if !ok || field(p, "variant") == "impostor" {
					continue
				}
				switch field(p, "platform") {
				case "standalonewindows":
					pc = true
					if v := field(p, "performanceRating"); ratings[v] {
						rating = v
					}
				case "android":
					quest = true
				}
			}
		}
		tags := []string{}
		if raw, ok := a["tags"].([]any); ok {
			for _, tag := range raw {
				if v, ok := tag.(string); ok {
					tags = append(tags, v)
				}
			}
		}
		entry := map[string]any{"id": id, "name": name, "author": author, "authorId": field(a, "authorId"), "description": field(a, "description"), "image": field(a, "imageUrl"), "thumbnail": field(a, "thumbnailImageUrl"), "pc": pc, "quest": quest, "rating": rating, "tags": tags, "updated": field(a, "updated_at"), "created": field(a, "created_at")}
		encoded, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		tx, err := cfg.Catalog.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(r.Context(), "INSERT INTO catalog(id,name,author,updated,pc,quest,rating,data) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", id, name, author, field(a, "updated_at"), pc, quest, rating, string(encoded))
		if err != nil {
			tx.Rollback()
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			tx.Rollback()
			return err
		}
		if n == 0 {
			tx.Rollback()
			results = append(results, loggerResult{id, "existing"})
			continue
		}
		var rowid int64
		if err = tx.QueryRowContext(r.Context(), "SELECT rowid FROM catalog WHERE id=?", id).Scan(&rowid); err == nil {
			_, err = tx.ExecContext(r.Context(), "INSERT INTO catalog_fts(rowid,name,author,description,tags) VALUES(?,?,?,?,?)", rowid, name, author, field(a, "description"), strings.Join(tags, " "))
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		results = append(results, loggerResult{id, "added"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
	return nil
}

// loggerIDs reads the one to five avatar IDs of a logger request.
func loggerIDs(r *http.Request) ([]string, error) {
	body, err := readBounded(r.Body, 8192, "Request too large.")
	if err != nil {
		return nil, err
	}
	var input struct {
		IDs []string `json:"ids"`
	}
	if json.Unmarshal(body, &input) != nil || len(input.IDs) == 0 || len(input.IDs) > loggerBatchSize {
		return nil, errStatus(http.StatusBadRequest, "Send between one and five avatar IDs.")
	}
	return input.IDs, nil
}

// loggerRetry tells the desktop when to retry and which service limited it.
func loggerRetry(w http.ResponseWriter, source string, wait time.Duration) {
	w.Header().Set("X-RateLimit-Source", source)
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int((wait+time.Second-1)/time.Second))))
}

// loggerKnown reports which IDs the catalog already has, so the desktop can skip
// them without asking VRChat. It only reads the catalog: it never contacts
// VRChat and does not use ingestion quota. Ingestion still checks every ID.
func (s *Server) loggerKnown(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		return errStatus(http.StatusMethodNotAllowed, "Use POST to check avatar IDs.")
	}
	cfg := s.cfg.Logger
	if cfg == nil {
		return errStatus(http.StatusServiceUnavailable, "Logger ingestion is not enabled.")
	}
	_, session, err := s.authenticateLoggerKey(r)
	if err != nil {
		return err
	}
	ids, err := loggerIDs(r)
	if err != nil {
		return err
	}
	if wait := s.limiter.allowN("logger-known:"+string(s.linkKey(session.UserID)), loggerChecks, s.now()); wait > 0 {
		loggerRetry(w, "vrcove", wait)
		return errStatus(http.StatusTooManyRequests, "Catalog check limit reached. Try again later.")
	}
	// Removed avatars are reported as existing so desktops skip them without
	// spending ingestion quota.
	removed, err := s.blacklisted(r.Context(), ids)
	if err != nil {
		return err
	}
	existing := []string{}
	for _, id := range ids {
		if !loggerAvatarID.MatchString(id) || slices.Contains(existing, id) {
			continue
		}
		if removed[id] {
			existing = append(existing, id)
			continue
		}
		var found int
		err := cfg.Catalog.QueryRowContext(r.Context(), "SELECT 1 FROM catalog WHERE id = ?", id).Scan(&found)
		if err == nil {
			existing = append(existing, id)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"existing": existing})
	return nil
}
