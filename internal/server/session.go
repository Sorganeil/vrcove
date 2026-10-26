package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Same format as the former WebCrypto implementation (AES-256-GCM, 12-byte IV,
// base64(iv) + "." + base64(ciphertext||tag)), so existing cookies stay valid
// when SESSION_KEY is unchanged.
const (
	cookieName      = "__Host-atlas-vrc"
	localCookieName = "atlas-vrc-local"
	sessionAAD      = "atlas-vrc-v1"
)

// Session holds the VRChat tokens. Expires is in Unix milliseconds. Pending is
// non-nil while two-factor verification is outstanding, even when empty.
type Session struct {
	activate       bool
	AccountID      int64    `json:"accountId,omitempty"`
	ExpectedUserID string   `json:"expectedUserId,omitempty"`
	Username       string   `json:"username,omitempty"`
	DisplayName    string   `json:"displayName,omitempty"`
	UserID         string   `json:"userId,omitempty"`
	Auth           string   `json:"auth,omitempty"`
	TwoFactorAuth  string   `json:"twoFactorAuth,omitempty"`
	Expires        int64    `json:"expires"`
	Pending        []string `json:"pending"`
	// SessionToken names this browser's record in vrchat_sessions.
	SessionToken string `json:"sid,omitempty"`
}

// sessionFailure distinguishes connection loss from rejected credentials or permissions.
type sessionFailure struct {
	code, message string
	status        int
}

func (e *sessionFailure) Error() string { return e.message }
func invalidVRChatSession() error {
	return &sessionFailure{"vrchat_session_invalid", "Your VRChat session is no longer valid. Reconnect VRChat. You are still signed in to VRCove.", http.StatusUnauthorized}
}

func (s *Server) missingVRChatSession(r *http.Request) error {
	code, message := "vrchat_session_required", "Your linked VRChat account is disconnected or its session has expired. Reconnect VRChat. You are still signed in to VRCove."
	if c, err := r.Cookie(s.cookieName()); err == nil && c.Value != "" {
		if old := unseal(c.Value, s.cfg.SessionKey, time.UnixMilli(0)); old != nil && old.Expires <= s.now().UnixMilli() {
			code, message = "vrchat_session_expired", "Your VRChat session has expired. Reconnect VRChat. You are still signed in to VRCove."
		} else {
			return invalidVRChatSession()
		}
	}
	return &sessionFailure{code, message, http.StatusUnauthorized}
}

// sessionUpdates are the rotated token cookies VRChat sent with a response.
type sessionUpdates struct{ Auth, TwoFactorAuth string }

func (s *Session) apply(u sessionUpdates) {
	if u.Auth != "" {
		s.Auth = u.Auth
	}
	if u.TwoFactorAuth != "" {
		s.TwoFactorAuth = u.TwoFactorAuth
	}
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(session *Session, key []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	plain, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcm.NonceSize())
	rand.Read(iv)
	ct := gcm.Seal(nil, iv, plain, []byte(sessionAAD))
	return base64.StdEncoding.EncodeToString(iv) + "." + base64.StdEncoding.EncodeToString(ct), nil
}

func unseal(value string, key []byte, now time.Time) *Session {
	ivText, ctText, ok := strings.Cut(value, ".")
	if !ok {
		return nil
	}
	iv, err1 := base64.StdEncoding.DecodeString(ivText)
	ct, err2 := base64.StdEncoding.DecodeString(ctText)
	gcm, err3 := newGCM(key)
	if err := errors.Join(err1, err2, err3); err != nil || len(iv) != gcm.NonceSize() {
		return nil
	}
	plain, err := gcm.Open(nil, iv, ct, []byte(sessionAAD))
	if err != nil {
		return nil
	}
	var s Session
	if json.Unmarshal(plain, &s) != nil || s.Expires <= now.UnixMilli() {
		return nil
	}
	return &s
}

func (s *Server) cookieName() string {
	if s.secure {
		return cookieName
	}
	return localCookieName
}

func (s *Server) readSession(r *http.Request) *Session {
	c, err := r.Cookie(s.cookieName())
	if err != nil || c.Value == "" {
		return nil
	}
	session := unseal(c.Value, s.cfg.SessionKey, s.now())
	if session != nil && session.UserID != "" && (s.accounts != nil || s.cfg.Local) {
		if _, err := r.Cookie(s.linkedCookieName(session.UserID)); err == nil {
			return s.readLinkedSession(r, session.UserID, session.AccountID)
		}
	}
	if !s.sessionLive(r.Context(), session) {
		return nil
	}
	return s.hydrateLoggerSession(r.Context(), session)
}

// sessionCookie builds the Set-Cookie value; a nil session clears the cookie.
func (s *Server) sessionCookie(session *Session) (string, error) {
	return s.namedSessionCookie(s.cookieName(), session)
}
func (s *Server) namedSessionCookie(name string, session *Session) (string, error) {
	value, maxAge := "", int64(0)
	if session != nil {
		var err error
		if value, err = seal(session, s.cfg.SessionKey); err != nil {
			return "", err
		}
		maxAge = max(0, (session.Expires-s.now().UnixMilli())/1000)
		if s.cfg.Local && session.UserID != "" && name == s.linkedCookieName(session.UserID) {
			maxAge = max(1, maxAge)
		}
	}
	secure := ""
	if s.secure {
		secure = "Secure; "
	}
	return fmt.Sprintf("%s=%s; Path=/; HttpOnly; SameSite=Strict; %sMax-Age=%d", name, value, secure, maxAge), nil
}

// writeWithSession responds with data and the re-sealed, possibly rotated session.
func (s *Server) writeWithSession(w http.ResponseWriter, data any, session *Session) {
	names := []string{s.cookieName()}
	if session != nil && session.Pending != nil {
		names = []string{s.cookieName() + "-pending"}
	} else if session != nil && session.AccountID != 0 && session.UserID != "" && (s.accounts != nil || s.cfg.Local) {
		names = []string{s.linkedCookieName(session.UserID)}
		if session.activate {
			names = append([]string{s.cookieName()}, names...)
		}
	}
	for _, name := range names {
		cookie, err := s.namedSessionCookie(name, session)
		if err != nil {
			writeFailure(w, err)
			return
		}
		w.Header().Add("Set-Cookie", cookie)
	}
	if session != nil && session.activate {
		s.clearNamedSession(w, s.cookieName()+"-pending")
	}
	if session != nil && session.Pending == nil && session.AccountID != 0 && session.UserID != "" {
		w.Header().Set("X-VRChat-Session-User", session.UserID)
		w.Header().Set("X-VRChat-Session-Expires", strconv.FormatInt(session.Expires, 10))
	}
	writeJSON(w, http.StatusOK, data)
}
