package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// localAccountID stands in for the VRCove account in local mode, where the
// VRChat session alone identifies the person using the device.
const localAccountID = 1
const maxLocalAccounts = 10

// APIHandler serves the /api routes without the site's pages, for embedding
// in the desktop app.
func (s *Server) APIHandler() http.Handler { return http.HandlerFunc(s.handleAPI) }

// LocalUser reports the VRChat account signed in on this request, if any.
func (s *Server) LocalUser(r *http.Request) (userID, username string) {
	if session := s.readSession(r); session != nil && session.Pending == nil && session.Auth != "" {
		return session.UserID, session.Username
	}
	return "", ""
}

// localSessions includes expired saved profiles so they can be reconnected or
// removed. Only the authenticated local app supplies these sealed cookies.
func (s *Server) localSessions(r *http.Request) map[string]*Session {
	saved := map[string]*Session{}
	for _, c := range r.Cookies() {
		if !strings.HasPrefix(c.Name, s.cookieName()+"-") {
			continue
		}
		session := unseal(c.Value, s.cfg.SessionKey, time.UnixMilli(0))
		if session != nil && session.AccountID == localAccountID && session.UserID != "" && session.Pending == nil && c.Name == s.linkedCookieName(session.UserID) {
			saved[session.UserID] = session
		}
	}
	// Migrate the active session of older single-account builds lazily.
	if c, err := r.Cookie(s.cookieName()); err == nil {
		if session := unseal(c.Value, s.cfg.SessionKey, time.UnixMilli(0)); session != nil && session.AccountID == localAccountID && session.UserID != "" && session.Pending == nil && saved[session.UserID] == nil {
			saved[session.UserID] = session
		}
	}
	return saved
}

// A deterministic 48-bit identifier fits the frontend's numeric link IDs.
func (s *Server) localID(userID string) int64 {
	var id int64
	for _, b := range s.linkKey(userID)[:6] {
		id = id<<8 | int64(b)
	}
	return id
}

// deviceLinks uses the same shape as the website's linked-account list.
func (s *Server) deviceLinks(w http.ResponseWriter, r *http.Request) {
	links := []map[string]any{}
	active := s.readSession(r)
	saved := s.localSessions(r)
	ids := make([]string, 0, len(saved))
	for id := range saved {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		session := saved[id]
		name := session.DisplayName
		if name == "" {
			name = session.Username
		}
		var expires int64
		if session.Auth != "" && session.Expires > s.now().UnixMilli() {
			expires = session.Expires
		}
		links = append(links, map[string]any{"id": s.localID(id), "vrchatId": id, "username": session.Username,
			"displayName": name, "linked": "", "active": active != nil && active.UserID == id, "sessionExpires": expires})
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

func (s *Server) selectedLocalAccount(r *http.Request) (*Session, error) {
	body, err := requestJSON(r)
	if err != nil {
		return nil, err
	}
	n, ok := body["id"].(json.Number)
	id, err := n.Int64()
	if !ok || err != nil {
		return nil, errStatus(400, "Choose a saved VRChat account.")
	}
	for user, session := range s.localSessions(r) {
		if s.localID(user) == id {
			return session, nil
		}
	}
	return nil, errNotFound
}

func (s *Server) invalidateLocalAccount(w http.ResponseWriter, session *Session) {
	session.Auth, session.TwoFactorAuth = "", ""
	session.Expires = s.now().Add(sessionLifetime).UnixMilli()
	if cookie, err := s.namedSessionCookie(s.linkedCookieName(session.UserID), session); err == nil {
		w.Header().Add("Set-Cookie", cookie)
	}
}

func (s *Server) switchLocalAccount(w http.ResponseWriter, r *http.Request) error {
	session, err := s.selectedLocalAccount(r)
	if err != nil {
		return err
	}
	reconnect := func() error {
		writeJSON(w, 401, map[string]string{"code": "vrchat_switch_reconnect", "error": "Reconnect this saved VRChat account before switching. Your active account has not changed."})
		return nil
	}
	if session.Auth == "" || session.Expires <= s.now().UnixMilli() {
		return reconnect()
	}
	me, updates, err := s.vrc(r.Context(), "/auth/user", session, vrcRequest{})
	if err != nil {
		var failure *sessionFailure
		if errors.As(err, &failure) {
			s.invalidateLocalAccount(w, session)
			return reconnect()
		}
		var he *httpError
		if errors.As(err, &he) && he.status == 429 {
			wait := s.backoff.remaining(backoffKey(session), s.now())
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int((wait+time.Second-1)/time.Second))))
		}
		return err
	}
	if field(me, "id") != session.UserID {
		s.invalidateLocalAccount(w, session)
		return reconnect()
	}
	session.apply(updates)
	if err := s.rememberVRChatUsername(r, session, me); err != nil {
		return err
	}
	session.activate = true
	s.writeWithSession(w, map[string]any{"user": publicUser(me)}, session)
	return nil
}

func (s *Server) removeLocalAccount(w http.ResponseWriter, r *http.Request) error {
	session, err := s.selectedLocalAccount(r)
	if err != nil {
		return err
	}
	s.clearNamedSession(w, s.linkedCookieName(session.UserID))
	if c, err := r.Cookie(s.cookieName()); err == nil {
		if active := unseal(c.Value, s.cfg.SessionKey, time.UnixMilli(0)); active != nil && active.UserID == session.UserID {
			s.clearNamedSession(w, s.cookieName())
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

// OpenPipeline connects to VRChat's live feed with the session on r, for the
// desktop app's own background updates.
func (s *Server) OpenPipeline(ctx context.Context, r *http.Request) (Pipeline, error) {
	session := s.readSession(r)
	if session == nil || session.Auth == "" || session.Pending != nil {
		return nil, invalidVRChatSession()
	}
	if s.backoff.coolingDown(backoffKey(session), s.now()) {
		return nil, rateLimitedError()
	}
	return s.dialPipeline(ctx, session.Auth)
}
