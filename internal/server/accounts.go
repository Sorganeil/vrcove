package server

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// VRCove accounts are separate from VRChat: a username and an Argon2id
// password hash, nothing else. No email, IP address or user agent is stored.
// They live in their own writable SQLite file; the catalog stays read-only.
//
// Linked VRChat accounts are private to their owner. The VRChat user ID is
// stored only as an HMAC (for uniqueness and the self-review check) and as an
// AES-GCM sealed record under ACCOUNT_KEY, so the database file alone does not
// tie a VRCove username to a VRChat account. Nothing that other people can
// see ever mentions a link.
const accountSchema = `
CREATE TABLE IF NOT EXISTS accounts (id INTEGER PRIMARY KEY, username TEXT NOT NULL, username_key TEXT NOT NULL UNIQUE, password TEXT NOT NULL, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS account_recovery_codes (account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE, code_hash BLOB NOT NULL, PRIMARY KEY (account_id, code_hash));
CREATE TABLE IF NOT EXISTS account_sessions (token_hash BLOB PRIMARY KEY, account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE, expires INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS account_sessions_account ON account_sessions(account_id);
CREATE TABLE IF NOT EXISTS vrchat_links (id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE, vrc_key BLOB NOT NULL UNIQUE, sealed TEXT NOT NULL, created TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS vrchat_links_account ON vrchat_links(account_id);
CREATE TABLE IF NOT EXISTS reviews (id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE, kind TEXT NOT NULL, target TEXT NOT NULL,
	rating INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5), body TEXT NOT NULL, created INTEGER NOT NULL, updated INTEGER NOT NULL, UNIQUE (account_id, kind, target));
CREATE INDEX IF NOT EXISTS reviews_target ON reviews(kind, target, updated DESC);`

// OpenAccounts opens (and creates) the writable accounts database. Deleted
// rows are overwritten on disk (secure_delete), so a removed account or link
// does not linger in free pages.
func OpenAccounts(path string) (*sql.DB, error) {
	file := "file:" + (&url.URL{Path: path}).EscapedPath() +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=secure_delete(1)"
	db, err := sql.Open("sqlite", file)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(accountSchema); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateAccounts(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

const (
	accountCookieName      = "__Host-veil-account"
	localAccountCookieName = "veil-account-local"
	accountSessionLifetime = 30 * 24 * time.Hour
	maxLinksPerAccount     = 10
	// hashSlots bounds concurrent Argon2id runs, so a burst of sign-ins cannot
	// exhaust memory (each run allocates argonMemory KiB).
	hashSlots = 4
	// pictureSlots bounds concurrent avatar decodes, each up to 64 MiB of pixels.
	pictureSlots = 2
)

// Argon2id parameters, following RFC 9106 and OWASP guidance. They are stored
// in each hash, so raising them later upgrades accounts as they sign in.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)
	b64             = base64.RawStdEncoding
	// dummyHash keeps sign-in for an unknown username as slow as a real one.
	dummyHash = sync.OnceValue(func() string { return hashPassword("vrcove timing equalizer") })
)

func hashPassword(password string) string {
	salt := make([]byte, argonSaltLen)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key))
}

// verifyPassword checks password against a PHC-format Argon2id hash. stale
// reports whether the hash used other parameters than the current ones.
func verifyPassword(encoded, password string) (ok, stale bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return false, false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || m > 1<<20 || t == 0 || t > 16 || p == 0 {
		return false, false
	}
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) < 16 {
		return false, false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	ok = subtle.ConstantTimeCompare(got, want) == 1
	return ok, m != argonMemory || t != argonTime || p != argonThreads || len(want) != argonKeyLen
}

// argon runs fn in one of the bounded hashing slots.
func (s *Server) argon(ctx context.Context, fn func()) error { return runIn(ctx, s.hashSlots, fn) }

// runIn runs fn once one of slots is free, giving up after 15 seconds.
func runIn(ctx context.Context, slots chan struct{}, fn func()) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return errStatus(http.StatusServiceUnavailable, "The server is busy. Try again in a moment.")
	}
	defer func() { <-slots }()
	fn()
	return nil
}

// accountKeys derives independent subkeys from ACCOUNT_KEY.
type accountKeys struct{ linkID, linkSeal []byte }

func deriveAccountKeys(key []byte) (accountKeys, error) {
	id, err1 := hkdf.Key(sha256.New, key, nil, "vrcove vrchat-link id", 32)
	sealKey, err2 := hkdf.Key(sha256.New, key, nil, "vrcove vrchat-link seal", 32)
	return accountKeys{id, sealKey}, errors.Join(err1, err2)
}

const linkAAD = "veil-link-v1"

func sealBytes(key, plain []byte, aad string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcm.NonceSize())
	rand.Read(iv)
	return b64.EncodeToString(iv) + "." + b64.EncodeToString(gcm.Seal(nil, iv, plain, []byte(aad))), nil
}

func openBytes(key []byte, value, aad string) ([]byte, error) {
	ivText, ctText, ok := strings.Cut(value, ".")
	iv, err1 := b64.DecodeString(ivText)
	ct, err2 := b64.DecodeString(ctText)
	gcm, err3 := newGCM(key)
	if err := errors.Join(err1, err2, err3); err != nil || !ok || len(iv) != gcm.NonceSize() {
		return nil, errors.New("malformed sealed value")
	}
	return gcm.Open(nil, iv, ct, []byte(aad))
}

// linkKey is the lookup key for a VRChat user ID: an HMAC, never the ID itself.
func (s *Server) linkKey(vrcID string) []byte {
	mac := hmac.New(sha256.New, s.keys.linkID)
	mac.Write([]byte(vrcID))
	return mac.Sum(nil)
}

type account struct {
	ID       int64
	Username string
}

func (s *Server) accountCookieName() string {
	if s.secure {
		return accountCookieName
	}
	return localAccountCookieName
}

func (s *Server) setAccountCookie(w http.ResponseWriter, value string, maxAge int64) {
	secure := ""
	if s.secure {
		secure = "Secure; "
	}
	w.Header().Add("Set-Cookie", fmt.Sprintf("%s=%s; Path=/; HttpOnly; SameSite=Strict; %sMax-Age=%d", s.accountCookieName(), value, secure, maxAge))
}

func tokenHash(token []byte) []byte {
	sum := sha256.Sum256(token)
	return sum[:]
}

// hasVRChatLink checks ownership without exposing private link records.
func (s *Server) hasVRChatLink(r *http.Request, a *account, userID string) (bool, error) {
	var n int
	query := "SELECT count(*) FROM vrchat_links WHERE account_id = ?"
	args := []any{a.ID}
	if userID != "" {
		query += " AND vrc_key = ?"
		args = append(args, s.linkKey(userID))
	}
	err := s.accounts.QueryRowContext(r.Context(), query, args...).Scan(&n)
	return n > 0, err
}

// readAccount returns the signed-in VRCove account, or nil.
func (s *Server) readAccount(r *http.Request) (*account, error) {
	if s.accounts == nil {
		return nil, nil
	}
	c, err := r.Cookie(s.accountCookieName())
	if err != nil {
		return nil, nil
	}
	token, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(token) != 32 {
		return nil, nil
	}
	var a account
	err = s.accounts.QueryRowContext(r.Context(), `SELECT a.id, a.username FROM account_sessions s JOIN accounts a ON a.id = s.account_id
		WHERE s.token_hash = ? AND s.expires > ?`, tokenHash(token), s.now().Unix()).Scan(&a.ID, &a.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// startAccountSession issues a fresh session token. Only its SHA-256 is stored.
func (s *Server) startAccountSession(ctx context.Context, w http.ResponseWriter, accountID int64) error {
	token := make([]byte, 32)
	rand.Read(token)
	now := s.now()
	if _, err := s.accounts.ExecContext(ctx, "DELETE FROM account_sessions WHERE expires <= ?", now.Unix()); err != nil {
		return err
	}
	if _, err := s.accounts.ExecContext(ctx, "INSERT INTO account_sessions (token_hash, account_id, expires) VALUES (?, ?, ?)",
		tokenHash(token), accountID, now.Add(accountSessionLifetime).Unix()); err != nil {
		return err
	}
	s.setAccountCookie(w, base64.RawURLEncoding.EncodeToString(token), int64(accountSessionLifetime/time.Second))
	return nil
}

// day is the only timestamp precision exposed or kept for account metadata,
// so stored times cannot be lined up against VRChat's own request logs.
func (s *Server) day(t time.Time) string { return t.UTC().Format("2006-01-02") }

func validPassword(username, password string) string {
	switch n := jsLength(password); {
	case n < 10:
		return "Use a password of at least 10 characters."
	case n > 1024:
		return "That password is too long."
	case strings.EqualFold(username, password):
		return "Your password can’t be your username."
	}
	return ""
}

// cove serves /api/cove/*: VRCove accounts, private VRChat links and reviews.
func (s *Server) cove(w http.ResponseWriter, r *http.Request, p string) error {
	if s.accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "VRCove accounts are not configured on this deployment.")
		return nil
	}
	post := r.Method == http.MethodPost
	switch {
	case p == "signup" && post:
		return s.signup(w, r)
	case p == "recover" && post:
		return s.recoverAccount(w, r)
	case p == "login" && post:
		return s.accountLogin(w, r)
	}
	acct, err := s.readAccount(r)
	if err != nil {
		return err
	}
	if p == "picture" && !post {
		return s.accountPicture(w, r, acct)
	}
	if p == "reviews" && !post {
		return s.listReviews(w, r, acct)
	}
	if acct != nil && ((post && (strings.HasPrefix(p, "collections/") || p == "review" || p == "review/delete")) || (!post && p == "collections")) {
		linked, err := s.hasVRChatLink(r, acct, "")
		if err != nil {
			return err
		}
		if !linked {
			return errStatus(http.StatusForbidden, "Link a VRChat account in your VRCove account to use authenticated actions.")
		}
	}
	if handled, err := s.social(w, r, p, acct); handled || err != nil {
		return err
	}
	if acct == nil {
		writeError(w, http.StatusUnauthorized, "Sign in to your VRCove account.")
		return nil
	}
	switch {
	case p == "picture" && post:
		return s.saveAccountPicture(w, r, acct)
	case p == "picture/remove" && post:
		if _, err := s.accounts.ExecContext(r.Context(), "DELETE FROM account_pictures WHERE account_id = ?", acct.ID); err != nil {
			return err
		}
		return s.accountMe(w, r, acct)
	case p == "link-reminder/dismiss" && post:
		if _, err := s.accounts.ExecContext(r.Context(), "UPDATE accounts SET link_reminder_seen = 1 WHERE id = ?", acct.ID); err != nil {
			return err
		}
		return s.accountMe(w, r, acct)
	case p == "me" && !post:
		return s.accountMe(w, r, acct)
	case p == "logout" && post:
		if c, err := r.Cookie(s.accountCookieName()); err == nil {
			if token, err := base64.RawURLEncoding.DecodeString(c.Value); err == nil {
				if _, err := s.accounts.ExecContext(r.Context(), "DELETE FROM account_sessions WHERE token_hash = ?", tokenHash(token)); err != nil {
					return err
				}
			}
		}
		if err := s.forgetBrowserSessions(r, acct.ID); err != nil {
			return err
		}
		s.setAccountCookie(w, "", 0)
		s.clearVRChatSessions(w, r)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return nil
	case p == "vrchat-sessions/end" && post:
		return s.endVRChatSession(w, r, acct)
	case p == "recovery-codes" && post:
		return s.generateRecoveryCodes(w, r, acct)
	case p == "password" && post:
		return s.changePassword(w, r, acct)
	case p == "delete" && post:
		return s.deleteAccount(w, r, acct)
	case p == "link" && post:
		return s.linkVRChat(w, r, acct)
	case p == "unlink" && post:
		return s.unlinkVRChat(w, r, acct)
	case p == "review" && post:
		return s.saveReview(w, r, acct)
	case p == "review/delete" && post:
		return s.deleteReview(w, r, acct)
	}
	writeError(w, http.StatusNotFound, "Not found")
	return nil
}

func credentials(r *http.Request) (username, password string, err error) {
	body, err := requestJSON(r)
	if err != nil {
		return "", "", err
	}
	username, _ = str(body, "username")
	password, _ = str(body, "password")
	return strings.TrimSpace(username), password, nil
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) error {
	if !s.limiter.allow("cove-signup:"+clientKey(r), s.now()) {
		return errStatus(http.StatusTooManyRequests, "Too many attempts. Wait 10 minutes.")
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	username, _ := str(body, "username")
	password, _ := str(body, "password")
	username = strings.TrimSpace(username)
	if !usernamePattern.MatchString(username) {
		writeError(w, http.StatusBadRequest, "Usernames are 3–32 letters, numbers, dots, dashes or underscores.")
		return nil
	}
	if msg := validPassword(username, password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return nil
	}
	token, _ := str(body, "turnstile")
	if ok, err := s.verifyTurnstile(r, token); err != nil {
		return err
	} else if !ok {
		writeError(w, http.StatusBadRequest, "Complete the verification check and try again.")
		return nil
	}
	var hash string
	if err := s.argon(r.Context(), func() { hash = hashPassword(password) }); err != nil {
		return err
	}
	res, err := s.accounts.ExecContext(r.Context(), "INSERT INTO accounts (username, username_key, password, created) VALUES (?, ?, ?, ?)",
		username, strings.ToLower(username), hash, s.day(s.now()))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			writeError(w, http.StatusConflict, "That username is taken.")
			return nil
		}
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if err := s.startAccountSession(r.Context(), w, id); err != nil {
		return err
	}
	return s.accountMe(w, r, &account{ID: id, Username: username})
}

// checkPassword verifies a password for username, doing the same Argon2id work
// whether or not the account exists, and upgrades a stale hash.
func (s *Server) checkPassword(ctx context.Context, username, password string) (*account, error) {
	var a account
	var hash string
	err := s.accounts.QueryRowContext(ctx, "SELECT id, username, password FROM accounts WHERE username_key = ?", strings.ToLower(username)).Scan(&a.ID, &a.Username, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	found := err == nil
	if !found {
		hash = dummyHash()
	}
	var ok, stale bool
	if err := s.argon(ctx, func() { ok, stale = verifyPassword(hash, password) }); err != nil {
		return nil, err
	}
	if !found || !ok {
		return nil, nil
	}
	if stale {
		var fresh string
		if s.argon(ctx, func() { fresh = hashPassword(password) }) == nil {
			s.accounts.ExecContext(ctx, "UPDATE accounts SET password = ? WHERE id = ?", fresh, a.ID)
		}
	}
	return &a, nil
}

// guessKeys are the limiter keys for failed password or recovery-code checks
// against username: one per client and username, so a stranger's guesses
// cannot lock the owner out, and one per username as a backstop against
// guessing from many addresses.
func (s *Server) guessKeys(r *http.Request, username string) [2]string {
	name := strings.ToLower(strings.TrimSpace(username))
	return [2]string{"cove-guess:" + clientKey(r) + ":" + name, "cove-user:" + name}
}

func (s *Server) tooManyGuesses(keys [2]string, now time.Time) bool {
	return s.limiter.over(keys[0], signInAttempts, now) || s.limiter.over(keys[1], accountFailures, now)
}

func (s *Server) failedGuess(keys [2]string, now time.Time) {
	for _, k := range keys {
		s.limiter.hit(k, now)
	}
}

// reauthKey limits password re-checks by a signed-in account. Only the
// session holder can spend it, unlike the sign-in limits.
func reauthKey(a *account) string { return "cove-reauth:" + strconv.FormatInt(a.ID, 10) }

func (s *Server) accountLogin(w http.ResponseWriter, r *http.Request) error {
	username, password, err := credentials(r)
	if err != nil {
		return err
	}
	now := s.now()
	guesses := s.guessKeys(r, username)
	if !s.limiter.allow("cove-login:"+clientKey(r), now) || s.tooManyGuesses(guesses, now) {
		return errStatus(http.StatusTooManyRequests, "Too many sign-in attempts. Wait 10 minutes.")
	}
	if username == "" || password == "" || jsLength(username) > 64 || jsLength(password) > 1024 {
		writeError(w, http.StatusBadRequest, "Enter your username and password.")
		return nil
	}
	a, err := s.checkPassword(r.Context(), username, password)
	if err != nil {
		return err
	}
	if a == nil {
		s.failedGuess(guesses, now)
		writeError(w, http.StatusUnauthorized, "Wrong username or password.")
		return nil
	}
	if err := s.startAccountSession(r.Context(), w, a.ID); err != nil {
		return err
	}
	return s.accountMe(w, r, a)
}

type linkRecord struct {
	VRChatID    string `json:"vrchatId"`
	DisplayName string `json:"displayName"`
	Username    string `json:"username,omitempty"`
}

// Reconnect names stay private to the link owner. Older links can recover the
// login name from their bound encrypted cookie, even after its session expires.
func (s *Server) reconnectUsername(r *http.Request, accountID int64, link linkRecord) string {
	if link.Username != "" {
		return link.Username
	}
	for _, name := range []string{s.linkedCookieName(link.VRChatID), s.cookieName()} {
		c, err := r.Cookie(name)
		if err != nil {
			continue
		}
		session := unseal(c.Value, s.cfg.SessionKey, time.UnixMilli(0))
		if session != nil && session.AccountID == accountID && session.UserID == link.VRChatID && session.Username != "" {
			return session.Username
		}
	}
	return ""
}

// Refresh legacy links from a verified response without retaining tokens.
func (s *Server) rememberVRChatUsername(r *http.Request, session *Session, me any) error {
	if field(me, "id") != session.UserID {
		return invalidVRChatSession()
	}
	username := field(me, "username")
	if username == "" {
		username = session.Username
	}
	if username == "" {
		return nil
	}
	var sealed string
	key := s.linkKey(session.UserID)
	err := s.accounts.QueryRowContext(r.Context(), "SELECT sealed FROM vrchat_links WHERE account_id = ? AND vrc_key = ?", session.AccountID, key).Scan(&sealed)
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
	if link.Username == username {
		return nil
	}
	link.Username = username
	plain, err = json.Marshal(link)
	if err != nil {
		return err
	}
	sealed, err = sealBytes(s.keys.linkSeal, plain, linkAAD)
	if err != nil {
		return err
	}
	_, err = s.accounts.ExecContext(r.Context(), "UPDATE vrchat_links SET sealed = ? WHERE account_id = ? AND vrc_key = ?", sealed, session.AccountID, key)
	return err
}

func (s *Server) accountMe(w http.ResponseWriter, r *http.Request, a *account) error {
	var remaining int
	if err := s.accounts.QueryRowContext(r.Context(), "SELECT count(*) FROM account_recovery_codes WHERE account_id = ?", a.ID).Scan(&remaining); err != nil {
		return err
	}
	var created, bio, status string
	var public, reminderSeen bool
	if err := s.accounts.QueryRowContext(r.Context(), "SELECT created, bio, status, public, link_reminder_seen FROM accounts WHERE id = ?", a.ID).Scan(&created, &bio, &status, &public, &reminderSeen); err != nil {
		return err
	}
	rows, err := s.accounts.QueryContext(r.Context(), "SELECT id, sealed, created FROM vrchat_links WHERE account_id = ? ORDER BY id", a.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	links := []map[string]any{}
	active := s.readSession(r)
	for rows.Next() {
		var id int64
		var sealed, linked string
		if err := rows.Scan(&id, &sealed, &linked); err != nil {
			return err
		}
		var rec linkRecord
		if plain, err := openBytes(s.keys.linkSeal, sealed, linkAAD); err == nil {
			json.Unmarshal(plain, &rec)
		}
		connected := s.readLinkedSession(r, rec.VRChatID, a.ID)
		isActive := active != nil && active.AccountID == a.ID && active.UserID == rec.VRChatID && active.Pending == nil
		if connected == nil && isActive {
			connected = active
		}
		var expires int64
		if connected != nil {
			expires = connected.Expires
		}
		sessions, err := s.linkSessions(r.Context(), a.ID, rec.VRChatID, connected)
		if err != nil {
			return err
		}
		links = append(links, map[string]any{"active": isActive, "sessionExpires": expires, "sessions": sessions, "id": id, "vrchatId": rec.VRChatID, "displayName": rec.DisplayName, "username": s.reconnectUsername(r, a.ID, rec), "linked": linked})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"showLinkReminder": !reminderSeen && len(links) == 0, "recoveryCodesRemaining": remaining, "picture": s.accountPictureURL(r, a.ID, a.Username), "username": a.Username, "created": created, "bio": bio, "status": status, "public": public, "links": links})
	return nil
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, a *account) error {
	if !s.limiter.allow(reauthKey(a), s.now()) {
		return errStatus(http.StatusTooManyRequests, "Too many attempts. Wait 10 minutes.")
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	current, _ := str(body, "current")
	next, _ := str(body, "next")
	if msg := validPassword(a.Username, next); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return nil
	}
	if ok, err := s.checkPassword(r.Context(), a.Username, current); err != nil || ok == nil {
		if err == nil {
			writeError(w, http.StatusUnauthorized, "Your current password is wrong.")
		}
		return err
	}
	var hash string
	if err := s.argon(r.Context(), func() { hash = hashPassword(next) }); err != nil {
		return err
	}
	if _, err := s.accounts.ExecContext(r.Context(), "UPDATE accounts SET password = ? WHERE id = ?", hash, a.ID); err != nil {
		return err
	}
	// Sign out every other device, including its VRChat connections.
	if _, err := s.accounts.ExecContext(r.Context(), "DELETE FROM account_sessions WHERE account_id = ?", a.ID); err != nil {
		return err
	}
	if err := s.endOtherVRChatSessions(r, a.ID); err != nil {
		return err
	}
	if err := s.startAccountSession(r.Context(), w, a.ID); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// deleteAccount removes the account with its sessions, links and reviews.
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request, a *account) error {
	if !s.limiter.allow(reauthKey(a), s.now()) {
		return errStatus(http.StatusTooManyRequests, "Too many attempts. Wait 10 minutes.")
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	password, _ := str(body, "password")
	if ok, err := s.checkPassword(r.Context(), a.Username, password); err != nil || ok == nil {
		if err == nil {
			writeError(w, http.StatusUnauthorized, "Your password is wrong.")
		}
		return err
	}
	if _, err := s.accounts.ExecContext(r.Context(), "DELETE FROM accounts WHERE id = ?", a.ID); err != nil {
		return err
	}
	s.setAccountCookie(w, "", 0)
	s.clearVRChatSessions(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// foldName lowercases and drops everything but letters and digits.
func foldName(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// similarNames reports whether a VRCove username resembles a VRChat name
// closely enough that someone could tie the two accounts together.
func similarNames(cove, vrchat string) bool {
	a, b := foldName(cove), foldName(vrchat)
	if a == "" || b == "" {
		return false
	}
	if len([]rune(a)) > len([]rune(b)) {
		a, b = b, a
	}
	return a == b || (len([]rune(a)) >= 4 && strings.Contains(b, a))
}

// linkVRChat links the VRChat account of the current VRChat session, which
// proves ownership. The link is visible only to the account's owner.
func (s *Server) linkVRChat(w http.ResponseWriter, r *http.Request, a *account) error {
	if s.cfg.SessionKey == nil {
		writeError(w, http.StatusServiceUnavailable, "Account connection is not configured on this deployment.")
		return nil
	}
	session := s.readSession(r)
	if session == nil || session.Auth == "" || session.Pending != nil {
		writeError(w, http.StatusUnauthorized, "Connect the VRChat account you want to link first.")
		return nil
	}
	me, updates, err := s.vrc(r.Context(), "/auth/user", session, vrcRequest{})
	if err != nil {
		return err
	}
	session.apply(updates)
	vrcID := field(me, "id")
	if !userID.MatchString(vrcID) {
		return errStatus(http.StatusBadGateway, "VRChat returned an unexpected account.")
	}
	return s.storeVRChatLink(w, r, a, session, me, false)
}

func (s *Server) storeVRChatLink(w http.ResponseWriter, r *http.Request, a *account, session *Session, me any, connected bool) error {
	vrcID := field(me, "id")
	if session.ExpectedUserID != "" && session.ExpectedUserID != vrcID {
		return errStatus(http.StatusConflict, "Sign in to the selected VRChat account to reconnect it. Your active account has not changed.")
	}
	if !userID.MatchString(vrcID) {
		return errStatus(http.StatusBadGateway, "VRChat returned an unexpected account.")
	}
	username := field(me, "username")
	if username == "" {
		username = session.Username
	}
	plain, _ := json.Marshal(linkRecord{VRChatID: vrcID, DisplayName: field(me, "displayName"), Username: username})
	sealed, err := sealBytes(s.keys.linkSeal, plain, linkAAD)
	if err != nil {
		return err
	}
	key := s.linkKey(vrcID)
	ctx := r.Context()
	var owner int64
	switch err := s.accounts.QueryRowContext(ctx, "SELECT account_id FROM vrchat_links WHERE vrc_key = ?", key).Scan(&owner); {
	case err == nil && owner != a.ID:
		writeError(w, http.StatusConflict, "This VRChat account is already linked to a different VRCove account.")
		return nil
	case err == nil:
		// Already linked here: refresh the stored account names.
		if _, err := s.accounts.ExecContext(ctx, "UPDATE vrchat_links SET sealed = ? WHERE vrc_key = ?", sealed, key); err != nil {
			return err
		}
	case errors.Is(err, sql.ErrNoRows):
		var n int
		if err := s.accounts.QueryRowContext(ctx, "SELECT count(*) FROM vrchat_links WHERE account_id = ?", a.ID).Scan(&n); err != nil {
			return err
		}
		if n >= maxLinksPerAccount {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("You can link up to %d VRChat accounts.", maxLinksPerAccount))
			return nil
		}
		if _, err := s.accounts.ExecContext(ctx, "INSERT INTO vrchat_links (account_id, vrc_key, sealed, created) VALUES (?, ?, ?, ?)",
			a.ID, key, sealed, s.day(s.now())); err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				writeError(w, http.StatusConflict, "This VRChat account is already linked to a different VRCove account.")
				return nil
			}
			return err
		}
	default:
		return err
	}
	if _, err := s.accounts.ExecContext(ctx, "UPDATE accounts SET link_reminder_seen = 1 WHERE id = ?", a.ID); err != nil {
		return err
	}
	similar := similarNames(a.Username, field(me, "displayName")) || similarNames(a.Username, field(me, "username"))
	if old := s.readSession(r); old != nil && old.AccountID == a.ID && old.UserID != "" && old.UserID != vrcID && old.Pending == nil {
		cookie, err := s.namedSessionCookie(s.linkedCookieName(old.UserID), old)
		if err != nil {
			return err
		}
		w.Header().Add("Set-Cookie", cookie)
	}
	session.AccountID, session.UserID = a.ID, vrcID
	session.activate = true
	// A reconnect replaces this browser's earlier session for the same link.
	for _, earlier := range s.browserSessions(r) {
		if earlier.AccountID == a.ID && earlier.UserID == vrcID && earlier.SessionToken != session.SessionToken {
			if err := s.forgetSession(ctx, earlier); err != nil {
				return err
			}
		}
	}
	if err := s.registerVRChatSession(r, session); err != nil {
		return err
	}

	result := map[string]any{"ok": true, "similarName": similar}
	if connected {
		result["user"] = publicUser(me)
	}
	s.writeWithSession(w, result, session)
	return nil
}

func (s *Server) unlinkVRChat(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	id, ok := body["id"].(json.Number)
	n, err := id.Int64()
	if !ok || err != nil {
		writeError(w, http.StatusBadRequest, "Invalid link")
		return nil
	}
	var sealed string
	if err := s.accounts.QueryRowContext(r.Context(), "SELECT sealed FROM vrchat_links WHERE id = ? AND account_id = ?", n, a.ID).Scan(&sealed); err == nil {
		if plain, err := openBytes(s.keys.linkSeal, sealed, linkAAD); err == nil {
			var link linkRecord
			if json.Unmarshal(plain, &link) == nil {
				s.clearNamedSession(w, s.linkedCookieName(link.VRChatID))
				if current := s.readSession(r); current != nil && current.AccountID == a.ID && current.UserID == link.VRChatID {
					s.clearNamedSession(w, s.cookieName())
				}
			}
		}
	}
	if sealed != "" {
		if plain, err := openBytes(s.keys.linkSeal, sealed, linkAAD); err == nil {
			var link linkRecord
			if json.Unmarshal(plain, &link) == nil {
				if _, err := s.accounts.ExecContext(r.Context(), "DELETE FROM vrchat_sessions WHERE account_id = ? AND vrc_key = ?", a.ID, s.linkKey(link.VRChatID)); err != nil {
					return err
				}
			}
		}
	}
	if _, err := s.accounts.ExecContext(r.Context(), "DELETE FROM vrchat_links WHERE id = ? AND account_id = ?", n, a.ID); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}
