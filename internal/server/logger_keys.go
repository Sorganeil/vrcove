package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
)

var loggerKeyPattern = regexp.MustCompile(`^Bearer vrcove_logger_[A-Za-z0-9_-]{43}$`)

func invalidLoggerKey() error {
	return errStatus(http.StatusUnauthorized, "This API key is invalid, revoked, or its VRChat session has ended. Generate a new API key on your VRCove account page.")
}
func loggerSessionAAD(hash []byte) string {
	return "vrcove-logger-session-v1:" + hex.EncodeToString(hash)
}
func (s *Server) sealLoggerSession(session *Session) (string, error) {
	plain, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	return sealBytes(s.keys.linkSeal, plain, loggerSessionAAD(tokenHash([]byte(session.SessionToken))))
}
func (s *Server) openLoggerSession(sealed string, hash []byte) *Session {
	plain, err := openBytes(s.keys.linkSeal, sealed, loggerSessionAAD(hash))
	if err != nil {
		return nil
	}
	var session Session
	if json.Unmarshal(plain, &session) != nil || session.Auth == "" || session.SessionToken == "" || session.Pending != nil || session.Expires <= s.now().UnixMilli() {
		return nil
	}
	if hex.EncodeToString(tokenHash([]byte(session.SessionToken))) != hex.EncodeToString(hash) {
		return nil
	}
	return &session
}

// Only users who explicitly enable a companion key get an encrypted server
// credentials on the existing session record, removed when that session ends.
func (s *Server) hydrateLoggerSession(ctx context.Context, session *Session) *Session {
	if session == nil || session.SessionToken == "" || s.accounts == nil {
		return session
	}
	hash := tokenHash([]byte(session.SessionToken))
	var sealed string
	err := s.accounts.QueryRowContext(ctx, "SELECT sealed FROM vrchat_sessions WHERE token_hash=?", hash).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && sealed == "") {
		return session
	}
	if err != nil {
		return nil
	}
	saved := s.openLoggerSession(sealed, hash)
	if saved == nil || saved.AccountID != session.AccountID || saved.UserID != session.UserID {
		return nil
	}
	session.Auth, session.TwoFactorAuth = saved.Auth, saved.TwoFactorAuth
	return session
}
func (s *Server) syncLoggerSession(ctx context.Context, session *Session, updates sessionUpdates) error {
	if s.accounts == nil || session == nil || session.SessionToken == "" || (updates.Auth == "" && updates.TwoFactorAuth == "") {
		return nil
	}
	rotated := *session
	rotated.apply(updates)
	sealed, err := s.sealLoggerSession(&rotated)
	if err != nil {
		return err
	}
	_, err = s.accounts.ExecContext(ctx, "UPDATE vrchat_sessions SET sealed=? WHERE token_hash=? AND sealed!=''", sealed, tokenHash([]byte(session.SessionToken)))
	return err
}

func (s *Server) authenticateLoggerKey(r *http.Request) (int64, *Session, error) {
	if s.accounts == nil || !loggerKeyPattern.MatchString(r.Header.Get("Authorization")) {
		return 0, nil, invalidLoggerKey()
	}
	hash := tokenHash([]byte(r.Header.Get("Authorization")[7:]))
	var id, owner, expires int64
	var sealed string
	var sessionHash []byte
	err := s.accounts.QueryRowContext(r.Context(), `SELECT k.id,k.account_id,k.session_hash,v.sealed,v.expires
 FROM logger_keys k JOIN vrchat_sessions v ON v.token_hash=k.session_hash AND v.account_id=k.account_id
 JOIN vrchat_links link ON link.account_id=v.account_id AND link.vrc_key=v.vrc_key
 WHERE k.key_hash=?`, hash).Scan(&id, &owner, &sessionHash, &sealed, &expires)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil, invalidLoggerKey()
		}
		return 0, nil, err
	}
	session := s.openLoggerSession(sealed, sessionHash)
	if session == nil || session.AccountID != owner || expires <= s.now().Unix() || !s.sessionLive(r.Context(), session) {
		_, _ = s.accounts.ExecContext(r.Context(), "DELETE FROM logger_keys WHERE id=?", id)
		return 0, nil, invalidLoggerKey()
	}
	return id, session, nil
}
func (s *Server) loggerKeyLive(ctx context.Context, id int64, session *Session) bool {
	var n int
	err := s.accounts.QueryRowContext(ctx, `SELECT count(*) FROM logger_keys k JOIN vrchat_sessions v ON v.token_hash=k.session_hash
 WHERE k.id=? AND k.account_id=? AND k.session_hash=? AND v.expires>?`, id, session.AccountID, tokenHash([]byte(session.SessionToken)), s.now().Unix()).Scan(&n)
	return err == nil && n == 1
}
func (s *Server) loggerKeys(w http.ResponseWriter, r *http.Request, a *account) error {
	if r.Method == http.MethodGet {
		rows, err := s.accounts.QueryContext(r.Context(), `SELECT k.id,k.created,v.id,v.expires,v.device,v.browser,v.sealed,k.session_hash
  FROM logger_keys k JOIN vrchat_sessions v ON v.token_hash=k.session_hash
  WHERE k.account_id=? AND v.expires>? ORDER BY k.id`, a.ID, s.now().Unix())
		if err != nil {
			return err
		}
		defer rows.Close()
		keys := []map[string]any{}
		for rows.Next() {
			var id, created, sid, expires int64
			var device, browser, sealed string
			var hash []byte
			if err := rows.Scan(&id, &created, &sid, &expires, &device, &browser, &sealed, &hash); err != nil {
				return err
			}
			session := s.openLoggerSession(sealed, hash)
			if session == nil {
				continue
			}
			keys = append(keys, map[string]any{"id": id, "sessionId": sid, "vrchatId": session.UserID, "created": created * 1000, "expires": expires * 1000, "device": device, "browser": browser})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		writeJSON(w, 200, map[string]any{"enabled": s.cfg.Logger != nil, "keys": keys})
		return nil
	}
	if s.cfg.Logger == nil {
		return errStatus(503, "Desktop companion submissions are not enabled on this deployment.")
	}
	session := s.readSession(r)
	if session == nil || session.AccountID != a.ID || session.Auth == "" || session.UserID == "" || session.Pending != nil {
		return s.missingVRChatSession(r)
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	expected, _ := str(body, "vrchatId")
	if expected == "" || expected != session.UserID {
		return errStatus(409, "The active VRChat account changed. Choose the session again.")
	}
	linked, err := s.hasVRChatLink(r, a, session.UserID)
	if err != nil {
		return err
	}
	if !linked {
		return errStatus(403, "Link this VRChat account before generating an API key.")
	}
	if !s.limiter.allow("logger-key-create:"+hex.EncodeToString(s.linkKey(session.UserID)), s.now()) {
		return errStatus(429, "Too many API key requests. Try again later.")
	}
	me, updates, err := s.vrc(r.Context(), "/auth/user", session, vrcRequest{})
	if err != nil {
		return err
	}
	session.apply(updates)
	if field(me, "id") != session.UserID {
		return invalidVRChatSession()
	}
	if err := s.registerVRChatSession(r, session); err != nil {
		return err
	}
	if session.SessionToken == "" || !s.sessionLive(r.Context(), session) {
		return invalidVRChatSession()
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	key := "vrcove_logger_" + base64.RawURLEncoding.EncodeToString(random)
	hash := tokenHash([]byte(session.SessionToken))
	sealed, err := s.sealLoggerSession(session)
	if err != nil {
		return err
	}
	tx, err := s.accounts.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated, err := tx.ExecContext(r.Context(), "UPDATE vrchat_sessions SET sealed=? WHERE token_hash=? AND account_id=? AND expires>?", sealed, hash, a.ID, s.now().Unix())
	if err != nil {
		return err
	}
	if n, err := updated.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return invalidLoggerKey()
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM logger_keys WHERE session_hash=?", hash); err != nil {
		return err
	}
	result, err := tx.ExecContext(r.Context(), "INSERT INTO logger_keys(account_id,session_hash,key_hash,created) VALUES(?,?,?,?)", a.ID, hash, tokenHash([]byte(key)), s.now().Unix())
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	s.writeWithSession(w, map[string]any{"key": key, "id": id, "expires": session.Expires, "vrchatId": session.UserID}, session)
	return nil
}
func (s *Server) revokeLoggerKey(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	number, ok := body["id"].(json.Number)
	if !ok {
		return errStatus(400, "Choose an API key to revoke.")
	}
	id, err := number.Int64()
	if err != nil {
		return errStatus(400, "Choose an API key to revoke.")
	}
	result, err := s.accounts.ExecContext(r.Context(), "DELETE FROM logger_keys WHERE id=? AND account_id=?", id, a.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
