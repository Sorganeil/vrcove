package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

// Each browser's VRChat connection normally lives in that browser's sealed
// cookie. The server keeps a small record so the owner can see their
// connected devices and end one early: a hash of a random session token, the
// link it belongs to, a coarse device type and browser family, the day it
// started and when it expires. No VRChat token, IP address or user agent is
// stored by default. Generating a companion API key opts that session into
// encrypted credential storage on this same record. Ending a session deletes
// the record and its API key, and the cookie stops working.

func newSessionToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// deviceKind reduces a User-Agent to a device type and browser family.
func deviceKind(ua string) (device, browser string) {
	device = "computer"
	switch {
	case strings.Contains(ua, "iPad") || strings.Contains(ua, "Tablet") || (strings.Contains(ua, "Android") && !strings.Contains(ua, "Mobile")):
		device = "tablet"
	case strings.Contains(ua, "Mobile") || strings.Contains(ua, "iPhone") || strings.Contains(ua, "Android"):
		device = "phone"
	}
	switch {
	case strings.Contains(ua, "Edg"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/"):
		browser = "Opera"
	case strings.Contains(ua, "SamsungBrowser"):
		browser = "Samsung Internet"
	case strings.Contains(ua, "Firefox/") || strings.Contains(ua, "FxiOS"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	return device, browser
}

// registerVRChatSession records a verified, linked session and gives it a
// token. Sessions that already have one are left alone.
func (s *Server) registerVRChatSession(r *http.Request, session *Session) error {
	if s.accounts == nil || session == nil || session.SessionToken != "" || session.AccountID == 0 || session.UserID == "" || session.Pending != nil {
		return nil
	}
	ctx, now := r.Context(), s.now()
	if _, err := s.accounts.ExecContext(ctx, "DELETE FROM vrchat_sessions WHERE expires <= ?", now.Unix()); err != nil {
		return err
	}
	token := newSessionToken()
	device, browser := deviceKind(r.UserAgent())
	if _, err := s.accounts.ExecContext(ctx, "INSERT INTO vrchat_sessions (token_hash, account_id, vrc_key, device, browser, created, expires) VALUES (?, ?, ?, ?, ?, ?, ?)",
		tokenHash([]byte(token)), session.AccountID, s.linkKey(session.UserID), device, browser, s.day(now), session.Expires/1000); err != nil {
		return err
	}
	session.SessionToken = token
	return nil
}

// sessionLive reports whether a session has not been ended from the account.
// Sessions sealed before records existed have no token and stay valid until
// their next account request registers them.
func (s *Server) sessionLive(ctx context.Context, session *Session) bool {
	if session == nil || session.SessionToken == "" || s.accounts == nil {
		return true
	}
	var n int
	err := s.accounts.QueryRowContext(ctx, "SELECT count(*) FROM vrchat_sessions WHERE token_hash = ? AND account_id = ? AND vrc_key = ? AND expires > ?",
		tokenHash([]byte(session.SessionToken)), session.AccountID, s.linkKey(session.UserID), s.now().Unix()).Scan(&n)
	return err == nil && n > 0
}

// forgetSession deletes the record behind one session, if it has one.
func (s *Server) forgetSession(ctx context.Context, session *Session) error {
	if s.accounts == nil || session == nil || session.SessionToken == "" {
		return nil
	}
	_, err := s.accounts.ExecContext(ctx, "DELETE FROM vrchat_sessions WHERE token_hash = ?", tokenHash([]byte(session.SessionToken)))
	return err
}

// browserSessions are the VRChat sessions sealed in this request's cookies,
// expired ones included.
func (s *Server) browserSessions(r *http.Request) []*Session {
	var out []*Session
	for _, c := range r.Cookies() {
		if c.Name != s.cookieName() && !strings.HasPrefix(c.Name, s.cookieName()+"-") {
			continue
		}
		if session := unseal(c.Value, s.cfg.SessionKey, time.UnixMilli(0)); session != nil {
			out = append(out, session)
		}
	}
	return out
}

// forgetBrowserSessions deletes the records of every VRChat session this
// browser holds for the account, before its cookies are cleared.
func (s *Server) forgetBrowserSessions(r *http.Request, accountID int64) error {
	for _, session := range s.browserSessions(r) {
		if session.AccountID == accountID {
			if err := s.forgetSession(r.Context(), session); err != nil {
				return err
			}
		}
	}
	return nil
}

// endOtherVRChatSessions ends every session of the account except the ones
// held by this browser, e.g. after a password change.
func (s *Server) endOtherVRChatSessions(r *http.Request, accountID int64) error {
	query, args := "DELETE FROM vrchat_sessions WHERE account_id = ?", []any{accountID}
	for _, session := range s.browserSessions(r) {
		if session.AccountID == accountID && session.SessionToken != "" {
			query += " AND token_hash != ?"
			args = append(args, tokenHash([]byte(session.SessionToken)))
		}
	}
	_, err := s.accounts.ExecContext(r.Context(), query, args...)
	return err
}

// linkSessions lists the live sessions of one linked VRChat account. current
// is this browser's session for it, if any.
func (s *Server) linkSessions(ctx context.Context, accountID int64, vrcID string, current *Session) ([]map[string]any, error) {
	rows, err := s.accounts.QueryContext(ctx, "SELECT id, token_hash, device, browser, created, expires FROM vrchat_sessions WHERE account_id = ? AND vrc_key = ? AND expires > ? ORDER BY id",
		accountID, s.linkKey(vrcID), s.now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mine []byte
	if current != nil && current.SessionToken != "" {
		mine = tokenHash([]byte(current.SessionToken))
	}
	out := []map[string]any{}
	for rows.Next() {
		var id, expires int64
		var hash []byte
		var device, browser, created string
		if err := rows.Scan(&id, &hash, &device, &browser, &created, &expires); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "device": device, "browser": browser, "started": created, "expires": expires * 1000, "current": mine != nil && string(hash) == string(mine)})
	}
	return out, rows.Err()
}

// endVRChatSession ends one of the account's sessions. When it is this
// browser's, the session is also logged out at VRChat and its cookies cleared.
func (s *Server) endVRChatSession(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	n, ok := body["id"].(interface{ Int64() (int64, error) })
	if !ok {
		return errStatus(http.StatusBadRequest, "Choose a session to end.")
	}
	id, err := n.Int64()
	if err != nil {
		return errStatus(http.StatusBadRequest, "Choose a session to end.")
	}
	var hash []byte
	if err := s.accounts.QueryRowContext(r.Context(), "SELECT token_hash FROM vrchat_sessions WHERE id = ? AND account_id = ?", id, a.ID).Scan(&hash); err != nil {
		return errNotFound
	}
	if _, err := s.accounts.ExecContext(r.Context(), "DELETE FROM vrchat_sessions WHERE id = ? AND account_id = ?", id, a.ID); err != nil {
		return err
	}
	// The active cookie and the per-account cookie can hold the same session.
	loggedOut := false
	for _, c := range r.Cookies() {
		if c.Name != s.cookieName() && !strings.HasPrefix(c.Name, s.cookieName()+"-") {
			continue
		}
		session := unseal(c.Value, s.cfg.SessionKey, time.UnixMilli(0))
		if session == nil || session.SessionToken == "" || string(tokenHash([]byte(session.SessionToken))) != string(hash) {
			continue
		}
		if !loggedOut && session.Auth != "" && session.Expires > s.now().UnixMilli() {
			s.vrc(r.Context(), "/logout", session, vrcRequest{Method: http.MethodPut})
			loggedOut = true
		}
		s.clearNamedSession(w, c.Name)
	}
	return s.accountMe(w, r, a)
}
