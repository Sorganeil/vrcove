package server

import (
	"crypto/rand"
	"encoding/base32"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
)

// With the VRChat worker, this server never sees anyone's VRChat session, so
// it cannot tell who is signed in to VRChat. To link a VRChat account, the
// owner puts a one-time code in that account's VRChat bio or status. The bot
// reads the public profile, and the worker then restores the old text.

const (
	linkCodeLifetime = 10 * time.Minute
	// linkChallenges caps new codes, and linkVerifies the bot lookups, per
	// VRCove account in the limiter's window.
	linkChallenges = 20
	linkVerifies   = 30
)

type linkCode struct {
	code    string
	expires time.Time
}

// linkCodes holds the open codes per VRCove account and VRChat account.
type linkCodes struct {
	mu    sync.Mutex
	codes map[string]linkCode
}

func linkCodeKey(account int64, vrchatID string) string {
	return strconv.FormatInt(account, 10) + ":" + vrchatID
}

func (l *linkCodes) put(key string, code linkCode, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.codes == nil {
		l.codes = map[string]linkCode{}
	}
	for k, c := range l.codes {
		if !c.expires.After(now) {
			delete(l.codes, k)
		}
	}
	l.codes[key] = code
}

func (l *linkCodes) get(key string, now time.Time) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.codes[key]
	if !ok || !c.expires.After(now) {
		return "", false
	}
	return c.code, true
}

func (l *linkCodes) remove(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.codes, key)
}

// linkChallenge issues the code to put in the VRChat profile.
func (s *Server) linkChallenge(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	vrchatID, _ := str(body, "vrchatId")
	if !userID.MatchString(vrchatID) {
		return errStatus(http.StatusBadRequest, "Choose a VRChat account to link.")
	}
	if wait := s.limiter.allowN("link-challenge:"+strconv.FormatInt(a.ID, 10), linkChallenges, s.now()); wait > 0 {
		return errStatus(http.StatusTooManyRequests, "Too many link attempts. Try again later.")
	}
	random := make([]byte, 5)
	rand.Read(random)
	code := "vrcove-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random))
	expires := s.now().Add(linkCodeLifetime)
	s.linkCodes.put(linkCodeKey(a.ID, vrchatID), linkCode{code, expires}, s.now())
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "expires": expires.UnixMilli()})
	return nil
}

// verifyLinkProof has the bot read the VRChat profile and links the account
// when the code is there.
func (s *Server) verifyLinkProof(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	vrchatID, _ := str(body, "vrchatId")
	username, _ := str(body, "username")
	if !userID.MatchString(vrchatID) || jsLength(username) > 64 {
		return errStatus(http.StatusBadRequest, "Choose a VRChat account to link.")
	}
	key := linkCodeKey(a.ID, vrchatID)
	code, ok := s.linkCodes.get(key, s.now())
	if !ok {
		return errStatus(http.StatusGone, "That link code expired. Start linking again.")
	}
	if s.cfg.Bot.Username == "" || s.accounts == nil {
		return errStatus(http.StatusServiceUnavailable, "Linking VRChat accounts is not available on this deployment.")
	}
	if wait := s.limiter.allowN("link-verify:"+strconv.FormatInt(a.ID, 10), linkVerifies, s.now()); wait > 0 {
		return errStatus(http.StatusTooManyRequests, "Too many link attempts. Try again later.")
	}
	user, err := s.botCall(r.Context(), vrchat.GetUser(vrchatID))
	if err != nil {
		return err
	}
	if field(user, "id") != vrchatID {
		return errStatus(http.StatusBadGateway, "VRChat returned an unexpected account.")
	}
	if !strings.Contains(field(user, "bio"), code) && !strings.Contains(field(user, "statusDescription"), code) {
		return errStatus(http.StatusConflict, "VRCove could not find the code in that VRChat profile yet. Try again in a moment.")
	}
	s.linkCodes.remove(key)
	if refused, err := s.saveLink(r.Context(), a, vrchatID, field(user, "displayName"), username); err != nil || refused != nil {
		return cmpOr(err, refused)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "vrchatId": vrchatID, "displayName": field(user, "displayName")})
	return nil
}
