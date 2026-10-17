package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Moderation: reports, avatar removal requests and account administration.
//
// Accounts have a role: user, admin or owner. Owners are set only with the
// `vrcove set-role` command on the server. Owners can make users admins;
// admins and owners can review reports and removal requests and suspend, ban
// or delete accounts with a lower role. Admin routes answer 404 to everyone
// else, so the panel stays hidden. Every action is recorded in moderation_log.

const (
	roleUser  = "user"
	roleAdmin = "admin"
	roleOwner = "owner"

	reportsPerHour      = 20
	removalsPerHour     = 10
	maxReportDetails    = 1000
	maxRemovalProof     = 2000
	minRemovalProof     = 20
	adminPageSize       = 50
	maxSuspensionDays   = 365
	moderationLogLength = 30
)

var reportReasons = map[string]bool{"spam": true, "harassment": true, "hate": true, "sexual": true, "personal-info": true, "impersonation": true, "other": true}

func rank(role string) int {
	switch role {
	case roleOwner:
		return 2
	case roleAdmin:
		return 1
	}
	return 0
}

func isStaff(a *account) bool { return a != nil && rank(a.Role) > 0 }

// integer returns body[key] if it is a whole JSON number.
func integer(body map[string]any, key string) int64 {
	n, _ := body[key].(json.Number)
	v, err := n.Int64()
	if err != nil {
		return 0
	}
	return v
}

// SetRole changes an account's role from the server's command line. It is the
// only way to create or remove an owner.
func SetRole(ctx context.Context, db *sql.DB, username, role string) error {
	if role != roleUser && role != roleAdmin && role != roleOwner {
		return errors.New("role must be user, admin or owner")
	}
	res, err := db.ExecContext(ctx, "UPDATE accounts SET role = ? WHERE username_key = ?", role, strings.ToLower(strings.TrimSpace(username)))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no account named %q", username)
	}
	_, err = db.ExecContext(ctx, "INSERT INTO moderation_log (actor_id, actor, action, target, created) VALUES (NULL, 'server', ?, ?, ?)",
		"set role "+role, strings.TrimSpace(username), time.Now().Unix())
	return err
}

// accountBlock explains why an account may not sign in, or returns "".
func (s *Server) accountBlock(ctx context.Context, id int64) (string, error) {
	var banned bool
	var until int64
	if err := s.accounts.QueryRowContext(ctx, "SELECT banned, suspended_until FROM accounts WHERE id = ?", id).Scan(&banned, &until); err != nil {
		return "", err
	}
	if banned {
		return "This account has been banned.", nil
	}
	if until > s.now().Unix() {
		return "This account is suspended until " + time.Unix(until, 0).UTC().Format("2 January 2006 15:04 UTC") + ".", nil
	}
	return "", nil
}

// signOutEverywhere ends every VRCove and VRChat session of an account. Its
// companion API keys go with the VRChat sessions.
func (s *Server) signOutEverywhere(ctx context.Context, tx *sql.Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM account_sessions WHERE account_id = ?", id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM vrchat_sessions WHERE account_id = ?", id)
	return err
}

func (s *Server) logAction(ctx context.Context, tx *sql.Tx, actor *account, action, target string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO moderation_log (actor_id, actor, action, target, created) VALUES (?, ?, ?, ?, ?)",
		actor.ID, actor.Username, action, target, s.now().Unix())
	return err
}

// report files a report about a review or a public profile. The reported text
// is copied so later edits or deletion keep the evidence.
func (s *Server) report(w http.ResponseWriter, r *http.Request, a *account) error {
	if s.limiter.allowN("cove-report:"+strconv.FormatInt(a.ID, 10), reportsPerHour, s.now()) > 0 {
		return errStatus(http.StatusTooManyRequests, "You’ve sent a lot of reports. Try again later.")
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	kind, _ := str(body, "kind")
	reason, _ := str(body, "reason")
	details, _ := str(body, "details")
	details, ok := multiLine(details, maxReportDetails)
	if !reportReasons[reason] || !ok {
		return errStatus(http.StatusBadRequest, fmt.Sprintf("Choose a reason and keep details to %d characters.", maxReportDetails))
	}
	ctx := r.Context()
	var target, owner int64
	var snapshot map[string]any
	switch kind {
	case "review":
		id := integer(body, "id")
		var author, reviewKind, reviewTarget, name, text string
		var rating int
		err := s.accounts.QueryRowContext(ctx, `SELECT r.id, r.account_id, a.username, r.kind, r.target, r.target_name, r.rating, r.body
			FROM reviews r JOIN accounts a ON a.id = r.account_id WHERE r.id = ? AND a.banned = 0`, id).
			Scan(&target, &owner, &author, &reviewKind, &reviewTarget, &name, &rating, &text)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "That review no longer exists.")
		} else if err != nil {
			return err
		}
		snapshot = map[string]any{"author": author, "kind": reviewKind, "target": reviewTarget, "name": name, "rating": rating, "body": text}
	case "profile":
		username, _ := str(body, "username")
		var name, bio, status string
		err := s.accounts.QueryRowContext(ctx, "SELECT id, username, bio, status FROM accounts WHERE username_key = ? AND public = 1 AND banned = 0",
			strings.ToLower(username)).Scan(&target, &name, &bio, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "This profile is private or doesn’t exist.")
		} else if err != nil {
			return err
		}
		owner = target
		snapshot = map[string]any{"username": name, "bio": bio, "status": status}
	default:
		return errStatus(http.StatusBadRequest, "Invalid report")
	}
	if owner == a.ID {
		return errStatus(http.StatusBadRequest, "You can’t report your own content.")
	}
	saved, _ := json.Marshal(snapshot)
	_, err = s.accounts.ExecContext(ctx, `INSERT INTO reports (reporter_id, kind, target_id, account_id, reason, details, snapshot, created)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, kind, target, owner, reason, details, string(saved), s.now().Unix())
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return errStatus(http.StatusConflict, "You’ve already reported this. Moderators will review it.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// requestRemoval asks moderators to remove an avatar from the catalog and keep
// it out. A requester whose connected VRChat account authored the avatar is
// marked as a verified owner; anyone else must describe their proof.
func (s *Server) requestRemoval(w http.ResponseWriter, r *http.Request, a *account) error {
	if s.limiter.allowN("cove-removal:"+strconv.FormatInt(a.ID, 10), removalsPerHour, s.now()) > 0 {
		return errStatus(http.StatusTooManyRequests, "You’ve sent a lot of requests. Try again later.")
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	id, _ := str(body, "avatarId")
	proof, _ := str(body, "proof")
	proof, ok := multiLine(proof, maxRemovalProof)
	if !avatarID.MatchString(id) || !ok {
		return errStatus(http.StatusBadRequest, fmt.Sprintf("Describe your proof in up to %d characters.", maxRemovalProof))
	}
	ctx := r.Context()
	var data string
	if s.db == nil || s.db.QueryRowContext(ctx, "SELECT data FROM catalog WHERE id = ?", id).Scan(&data) != nil {
		return errStatus(http.StatusNotFound, "This avatar isn’t in VRCove’s catalog.")
	}
	var avatar struct{ Name, AuthorID string }
	json.Unmarshal([]byte(data), &avatar)
	session := s.readSession(r)
	verified := session != nil && session.AccountID == a.ID && session.Pending == nil && session.UserID != "" && session.UserID == avatar.AuthorID
	if !verified && jsLength(proof) < minRemovalProof {
		return errStatus(http.StatusBadRequest, "Explain how you can prove you own this avatar, for example a link to where you publish it. Or connect the VRChat account that uploaded it.")
	}
	_, err = s.accounts.ExecContext(ctx, `INSERT INTO blacklist_requests (account_id, avatar_id, avatar_name, proof, verified, created)
		VALUES (?, ?, ?, ?, ?, ?)`, a.ID, id, avatar.Name, proof, verified, s.now().Unix())
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return errStatus(http.StatusConflict, "A removal request for this avatar is already waiting for review.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "verified": verified})
	return nil
}

// blacklisted reports which of ids moderators removed from the catalog.
func (s *Server) blacklisted(ctx context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if s.accounts == nil || len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.accounts.QueryContext(ctx, "SELECT avatar_id FROM avatar_blacklist WHERE avatar_id IN (?"+strings.Repeat(",?", len(ids)-1)+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// admin serves cove/admin/*. Anyone who isn't staff gets 404.
func (s *Server) admin(w http.ResponseWriter, r *http.Request, p string, a *account) error {
	if !isStaff(a) {
		writeError(w, http.StatusNotFound, "Not found")
		return nil
	}
	post := r.Method == http.MethodPost
	switch {
	case p == "summary" && !post:
		return s.adminSummary(w, r, a)
	case p == "users" && !post:
		return s.adminUsers(w, r)
	case p == "reports" && !post:
		return s.adminReports(w, r)
	case p == "requests" && !post:
		return s.adminRequests(w, r)
	case p == "user" && post:
		return s.adminUser(w, r, a)
	case p == "report" && post:
		return s.adminReport(w, r, a)
	case p == "request" && post:
		return s.adminRequest(w, r, a)
	}
	writeError(w, http.StatusNotFound, "Not found")
	return nil
}

func (s *Server) adminSummary(w http.ResponseWriter, r *http.Request, a *account) error {
	ctx := r.Context()
	var users, banned, suspended, reports, requests int
	err := s.accounts.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(banned), 0), coalesce(sum(banned = 0 AND suspended_until > ?), 0),
		(SELECT count(*) FROM reports WHERE status = 'open'), (SELECT count(*) FROM blacklist_requests WHERE status = 'open') FROM accounts`,
		s.now().Unix()).Scan(&users, &banned, &suspended, &reports, &requests)
	if err != nil {
		return err
	}
	rows, err := s.accounts.QueryContext(ctx, "SELECT actor, action, target, created FROM moderation_log ORDER BY id DESC LIMIT ?", moderationLogLength)
	if err != nil {
		return err
	}
	defer rows.Close()
	log := []map[string]any{}
	for rows.Next() {
		var actor, action, target string
		var created int64
		if err := rows.Scan(&actor, &action, &target, &created); err != nil {
			return err
		}
		log = append(log, map[string]any{"actor": actor, "action": action, "target": target, "time": created})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"role": a.Role, "users": users, "banned": banned, "suspended": suspended,
		"openReports": reports, "openRequests": requests, "log": log})
	return nil
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	search := strings.ToLower(strings.TrimSpace(query.Get("q")))
	if jsLength(search) > 64 {
		search = ""
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(search) + "%"
	ctx := r.Context()
	var total int
	if err := s.accounts.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE username_key LIKE ? ESCAPE '\'`, pattern).Scan(&total); err != nil {
		return err
	}
	rows, err := s.accounts.QueryContext(ctx, `SELECT id, username, created, role, banned, suspended_until, public,
		(SELECT count(*) FROM reviews WHERE account_id = accounts.id),
		(SELECT count(*) FROM reports WHERE account_id = accounts.id AND status = 'open')
		FROM accounts WHERE username_key LIKE ? ESCAPE '\' ORDER BY id DESC LIMIT ? OFFSET ?`,
		pattern, adminPageSize, queryInt(query.Get("offset"), 1000000))
	if err != nil {
		return err
	}
	defer rows.Close()
	users := []map[string]any{}
	for rows.Next() {
		var id, until int64
		var username, created, role string
		var banned, public bool
		var reviews, reports int
		if err := rows.Scan(&id, &username, &created, &role, &banned, &until, &public, &reviews, &reports); err != nil {
			return err
		}
		if until <= s.now().Unix() {
			until = 0
		}
		users = append(users, map[string]any{"id": id, "username": username, "created": created, "role": role, "banned": banned,
			"suspendedUntil": until, "public": public, "reviews": reviews, "openReports": reports})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users, "total": total, "pageSize": adminPageSize})
	return nil
}

func statusFilter(r *http.Request) string {
	if r.URL.Query().Get("status") == "closed" {
		return "status <> 'open'"
	}
	return "status = 'open'"
}

func (s *Server) adminReports(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.accounts.QueryContext(r.Context(), `SELECT p.id, p.kind, p.target_id, p.reason, p.details, p.snapshot, p.status, p.resolution, p.created,
		coalesce(reporter.username, ''), coalesce(owner.id, 0), coalesce(owner.username, ''), coalesce(owner.role, ''), coalesce(handler.username, ''),
		EXISTS (SELECT 1 FROM reviews WHERE p.kind = 'review' AND reviews.id = p.target_id)
		FROM reports p LEFT JOIN accounts reporter ON reporter.id = p.reporter_id LEFT JOIN accounts owner ON owner.id = p.account_id
		LEFT JOIN accounts handler ON handler.id = p.handled_by
		WHERE p.`+statusFilter(r)+` ORDER BY p.created ASC, p.id ASC LIMIT ? OFFSET ?`, adminPageSize, queryInt(r.URL.Query().Get("offset"), 1000000))
	if err != nil {
		return err
	}
	defer rows.Close()
	reports := []map[string]any{}
	for rows.Next() {
		var id, target, created, ownerID int64
		var kind, reason, details, snapshot, status, resolution, reporter, owner, ownerRole, handler string
		var live bool
		if err := rows.Scan(&id, &kind, &target, &reason, &details, &snapshot, &status, &resolution, &created, &reporter, &ownerID, &owner, &ownerRole, &handler, &live); err != nil {
			return err
		}
		reports = append(reports, map[string]any{"id": id, "kind": kind, "targetId": target, "reason": reason, "details": details,
			"content": json.RawMessage(snapshot), "status": status, "resolution": resolution, "created": created, "reporter": reporter,
			"accountId": ownerID, "account": owner, "accountRole": ownerRole, "handledBy": handler, "reviewExists": live})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports, "pageSize": adminPageSize})
	return nil
}

func (s *Server) adminRequests(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.accounts.QueryContext(r.Context(), `SELECT q.id, q.avatar_id, q.avatar_name, q.proof, q.verified, q.status, q.note, q.created,
		coalesce(a.username, ''), coalesce(h.username, '')
		FROM blacklist_requests q LEFT JOIN accounts a ON a.id = q.account_id LEFT JOIN accounts h ON h.id = q.handled_by
		WHERE q.`+statusFilter(r)+` ORDER BY q.created ASC, q.id ASC LIMIT ? OFFSET ?`, adminPageSize, queryInt(r.URL.Query().Get("offset"), 1000000))
	if err != nil {
		return err
	}
	defer rows.Close()
	requests := []map[string]any{}
	for rows.Next() {
		var id, created int64
		var avatar, name, proof, status, note, requester, handler string
		var verified bool
		if err := rows.Scan(&id, &avatar, &name, &proof, &verified, &status, &note, &created, &requester, &handler); err != nil {
			return err
		}
		requests = append(requests, map[string]any{"id": id, "avatarId": avatar, "avatarName": name, "proof": proof, "verified": verified,
			"status": status, "note": note, "created": created, "requester": requester, "handledBy": handler})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": requests, "pageSize": adminPageSize})
	return nil
}

// adminUser changes another account's role or standing. Staff can only act on
// accounts below their own role, and never on themselves.
func (s *Server) adminUser(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	id := integer(body, "id")
	action, _ := str(body, "action")
	ctx := r.Context()
	tx, err := s.accounts.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var target account
	if err := tx.QueryRowContext(ctx, "SELECT id, username, role FROM accounts WHERE id = ?", id).Scan(&target.ID, &target.Username, &target.Role); errors.Is(err, sql.ErrNoRows) {
		return errStatus(http.StatusNotFound, "That account no longer exists.")
	} else if err != nil {
		return err
	}
	if target.ID == a.ID || rank(target.Role) >= rank(a.Role) {
		return errStatus(http.StatusForbidden, "You can only manage accounts with a lower role than yours.")
	}
	label := action
	switch action {
	case "make-admin", "make-user":
		if a.Role != roleOwner {
			return errStatus(http.StatusForbidden, "Only owners can change roles.")
		}
		role := map[string]string{"make-admin": roleAdmin, "make-user": roleUser}[action]
		_, err = tx.ExecContext(ctx, "UPDATE accounts SET role = ? WHERE id = ?", role, target.ID)
	case "suspend":
		days := integer(body, "days")
		if days < 1 || days > maxSuspensionDays {
			return errStatus(http.StatusBadRequest, fmt.Sprintf("Suspend for 1 to %d days.", maxSuspensionDays))
		}
		label = fmt.Sprintf("suspend %d days", days)
		until := s.now().Add(time.Duration(days) * 24 * time.Hour).Unix()
		if _, err = tx.ExecContext(ctx, "UPDATE accounts SET suspended_until = ? WHERE id = ?", until, target.ID); err == nil {
			err = s.signOutEverywhere(ctx, tx, target.ID)
		}
	case "unsuspend":
		_, err = tx.ExecContext(ctx, "UPDATE accounts SET suspended_until = 0 WHERE id = ?", target.ID)
	case "ban":
		if _, err = tx.ExecContext(ctx, "UPDATE accounts SET banned = 1, public = 0 WHERE id = ?", target.ID); err == nil {
			err = s.signOutEverywhere(ctx, tx, target.ID)
		}
	case "unban":
		_, err = tx.ExecContext(ctx, "UPDATE accounts SET banned = 0 WHERE id = ?", target.ID)
	case "delete":
		_, err = tx.ExecContext(ctx, "DELETE FROM accounts WHERE id = ?", target.ID)
	default:
		return errStatus(http.StatusBadRequest, "Unknown action")
	}
	if err != nil {
		return err
	}
	if err := s.logAction(ctx, tx, a, label, target.Username); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// adminReport closes a report, optionally deleting the reported review.
func (s *Server) adminReport(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	id := integer(body, "id")
	action, _ := str(body, "action")
	status := map[string]string{"dismiss": "dismissed", "resolve": "resolved", "delete-review": "resolved"}[action]
	if status == "" {
		return errStatus(http.StatusBadRequest, "Unknown action")
	}
	ctx := r.Context()
	tx, err := s.accounts.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind, owner string
	var target int64
	err = tx.QueryRowContext(ctx, "SELECT p.kind, p.target_id, coalesce(a.username, '') FROM reports p LEFT JOIN accounts a ON a.id = p.account_id WHERE p.id = ? AND p.status = 'open'",
		id).Scan(&kind, &target, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return errStatus(http.StatusConflict, "This report was already handled.")
	} else if err != nil {
		return err
	}
	resolution := action
	if action == "delete-review" {
		if kind != "review" {
			return errStatus(http.StatusBadRequest, "Only review reports can delete a review.")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM reviews WHERE id = ?", target); err != nil {
			return err
		}
		resolution = "review deleted"
		// Other open reports about the same review are settled too.
		if _, err := tx.ExecContext(ctx, `UPDATE reports SET status = 'resolved', resolution = ?, handled_by = ?, handled = ?
			WHERE kind = 'review' AND target_id = ? AND status = 'open' AND id <> ?`, resolution, a.ID, s.now().Unix(), target, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE reports SET status = ?, resolution = ?, handled_by = ?, handled = ? WHERE id = ?",
		status, resolution, a.ID, s.now().Unix(), id); err != nil {
		return err
	}
	if err := s.logAction(ctx, tx, a, "report "+strconv.FormatInt(id, 10)+": "+resolution, owner); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// adminRequest approves or rejects an avatar removal request. Approval removes
// the avatar from the catalog and blocks companion uploads of it.
func (s *Server) adminRequest(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	id := integer(body, "id")
	action, _ := str(body, "action")
	note, _ := str(body, "note")
	note, ok := multiLine(note, maxReportDetails)
	if (action != "approve" && action != "reject") || !ok {
		return errStatus(http.StatusBadRequest, "Unknown action")
	}
	var writer *sql.DB
	if s.cfg.Logger != nil {
		writer = s.cfg.Logger.Catalog
	}
	if action == "approve" && writer == nil {
		return errStatus(http.StatusServiceUnavailable, "Catalog changes are not enabled on this deployment.")
	}
	ctx := r.Context()
	var avatar string
	err = s.accounts.QueryRowContext(ctx, "SELECT avatar_id FROM blacklist_requests WHERE id = ? AND status = 'open'", id).Scan(&avatar)
	if errors.Is(err, sql.ErrNoRows) {
		return errStatus(http.StatusConflict, "This request was already handled.")
	} else if err != nil {
		return err
	}
	// Remove the catalog row first: if that fails the request stays open and
	// can be approved again.
	if action == "approve" {
		if _, err := writer.ExecContext(ctx, "DELETE FROM catalog WHERE id = ?", avatar); err != nil {
			return err
		}
	}
	tx, err := s.accounts.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE blacklist_requests SET status = ?, note = ?, handled_by = ?, handled = ? WHERE id = ? AND status = 'open'",
		map[string]string{"approve": "approved", "reject": "rejected"}[action], note, a.ID, s.now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errStatus(http.StatusConflict, "This request was already handled.")
	}
	if action == "approve" {
		if _, err := tx.ExecContext(ctx, "INSERT INTO avatar_blacklist (avatar_id, request_id, added) VALUES (?, ?, ?) ON CONFLICT(avatar_id) DO NOTHING",
			avatar, id, s.now().Unix()); err != nil {
			return err
		}
	}
	if err := s.logAction(ctx, tx, a, action+"d removal request", avatar); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}
