package server

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (s *Server) linkedCookieName(userID string) string {
	return s.cookieName() + "-" + hex.EncodeToString(s.linkKey(userID)[:12])
}
func (s *Server) readLinkedSession(r *http.Request, userID string, accountID int64) *Session {
	c, err := r.Cookie(s.linkedCookieName(userID))
	if err != nil {
		return nil
	}
	session := unseal(c.Value, s.cfg.SessionKey, s.now())
	if session == nil || session.AccountID != accountID || session.UserID != userID || session.Pending != nil || session.Auth == "" || !s.sessionLive(r.Context(), session) {
		return nil
	}
	return s.hydrateLoggerSession(r.Context(), session)
}
func (s *Server) clearNamedSession(w http.ResponseWriter, name string) {
	cookie, _ := s.namedSessionCookie(name, nil)
	w.Header().Add("Set-Cookie", cookie)
}
func (s *Server) clearActiveSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cookieName()); err == nil {
		if session := unseal(c.Value, s.cfg.SessionKey, time.UnixMilli(0)); session != nil && session.UserID != "" && s.accounts != nil {
			s.clearNamedSession(w, s.linkedCookieName(session.UserID))
		}
	}
	s.clearNamedSession(w, s.cookieName())
}

// Invalidate only the session used by this request. A delayed failure from
// another account must never clear a newly selected active account.
func (s *Server) invalidateRequestSession(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Local {
		// One device, one account: a rejected session is simply signed out.
		s.clearNamedSession(w, s.cookieName())
		return
	}
	if c, err := r.Cookie(s.cookieName()); err == nil {
		if old := unseal(c.Value, s.cfg.SessionKey, time.UnixMilli(0)); old != nil && old.UserID != "" && s.accounts != nil {
			w.Header().Set("X-VRChat-Session-User", old.UserID)
			old.Auth = ""
			old.TwoFactorAuth = ""
			old.Expires = s.now().Add(sessionLifetime).UnixMilli()
			if cookie, err := s.namedSessionCookie(s.linkedCookieName(old.UserID), old); err == nil {
				w.Header().Add("Set-Cookie", cookie)
			}
		}
	}
}
func (s *Server) clearVRChatSessions(w http.ResponseWriter, r *http.Request) {
	s.clearNamedSession(w, s.cookieName())
	for _, c := range r.Cookies() {
		if strings.HasPrefix(c.Name, s.cookieName()+"-") {
			s.clearNamedSession(w, c.Name)
		}
	}
}
func (s *Server) readPendingSession(r *http.Request) *Session {
	if c, err := r.Cookie(s.cookieName() + "-pending"); err == nil {
		return unseal(c.Value, s.cfg.SessionKey, s.now())
	}
	return s.readSession(r) // Existing pending cookies can finish verification.
}
func (s *Server) switchVRChat(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	n, ok := body["id"].(json.Number)
	id, err := n.Int64()
	if !ok || err != nil {
		return errStatus(http.StatusBadRequest, "Choose a linked VRChat account.")
	}
	var sealed string
	err = s.accounts.QueryRowContext(r.Context(), "SELECT sealed FROM vrchat_links WHERE id = ? AND account_id = ?", id, a.ID).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	plain, err := openBytes(s.keys.linkSeal, sealed, linkAAD)
	if err != nil {
		return err
	}
	var link linkRecord
	if err := json.Unmarshal(plain, &link); err != nil {
		return err
	}
	session := s.readLinkedSession(r, link.VRChatID, a.ID)
	// Migrate an existing active connection on its first switch.
	if session == nil {
		if current := s.readSession(r); current != nil && current.AccountID == a.ID && current.UserID == link.VRChatID {
			session = current
		}
	}
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "vrchat_switch_reconnect", "error": "This VRChat account’s session has expired or is unavailable in this browser. Reconnect it to switch. Your active account has not changed."})
		return nil
	}
	me, updates, err := s.vrc(r.Context(), "/auth/user", session, vrcRequest{})
	if err != nil {
		var failure *sessionFailure
		if errors.As(err, &failure) {
			s.clearNamedSession(w, s.linkedCookieName(link.VRChatID))
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "vrchat_switch_reconnect", "error": "This VRChat account’s session is no longer valid. Reconnect it to switch. Your active account has not changed."})
			return nil
		}
		return err
	}
	if field(me, "id") != link.VRChatID {
		s.clearNamedSession(w, s.linkedCookieName(link.VRChatID))
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "vrchat_switch_reconnect", "error": "Verify this VRChat account again before switching. Your active account has not changed."})
		return nil
	}
	if err := s.rememberVRChatUsername(r, session, me); err != nil {
		return err
	}
	session.apply(updates)
	session.activate = true
	s.writeWithSession(w, map[string]any{"user": publicUser(me)}, session)
	return nil
}
