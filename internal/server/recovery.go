package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

const recoveryCodeCount = 10

// Each code has 128 random bits. Only a domain-separated SHA-256 digest is
// persisted; formatting and letter case do not affect redemption.
func recoveryHash(code string) []byte {
	code = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	raw, err := hex.DecodeString(code)
	if err != nil || len(raw) != 16 {
		return nil
	}
	return tokenHash(append([]byte("vrcove recovery v1:"), raw...))
}

func (s *Server) generateRecoveryCodes(w http.ResponseWriter, r *http.Request, a *account) error {
	if !s.limiter.allow(reauthKey(a), s.now()) {
		return errStatus(http.StatusTooManyRequests, "Too many attempts. Wait 10 minutes.")
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	password, _ := str(body, "password")
	if jsLength(password) > 1024 {
		return errStatus(http.StatusBadRequest, "That password is too long.")
	}
	// Retain the verified hash so a concurrent password reset cannot authorize
	// code replacement using an old password.
	var verified string
	if err := s.accounts.QueryRowContext(r.Context(), "SELECT password FROM accounts WHERE id = ?", a.ID).Scan(&verified); err != nil {
		return err
	}
	var ok bool
	if err := s.argon(r.Context(), func() { ok, _ = verifyPassword(verified, password) }); err != nil {
		return err
	}
	if !ok {
		return errStatus(http.StatusUnauthorized, "Your current password is wrong.")
	}
	codes := make([]string, recoveryCodeCount)
	for i := range codes {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		text := hex.EncodeToString(raw)
		codes[i] = text[:8] + "-" + text[8:16] + "-" + text[16:24] + "-" + text[24:]
	}
	tx, err := s.accounts.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Acquire the write lock before checking the password snapshot.
	res, err := tx.ExecContext(r.Context(), "UPDATE accounts SET password = password WHERE id = ? AND password = ?", a.ID, verified)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return errStatus(http.StatusConflict, "Your password changed. Try again.")
	}
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM account_recovery_codes WHERE account_id = ?", a.ID); err != nil {
		return err
	}
	for _, code := range codes {
		if _, err := tx.ExecContext(r.Context(), "INSERT INTO account_recovery_codes (account_id, code_hash) VALUES (?, ?)", a.ID, recoveryHash(code)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
	return nil
}

func (s *Server) recoverAccount(w http.ResponseWriter, r *http.Request) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	username, _ := str(body, "username")
	username = strings.TrimSpace(username)
	now := s.now()
	guesses := s.guessKeys(r, username)
	if !s.limiter.allow("cove-recover:"+clientKey(r), now) || s.tooManyGuesses(guesses, now) {
		return errStatus(http.StatusTooManyRequests, "Too many recovery attempts. Wait 10 minutes.")
	}
	code, _ := str(body, "code")
	next, _ := str(body, "next")
	if jsLength(username) > 64 || len(code) > 128 || recoveryHash(code) == nil {
		s.failedGuess(guesses, now)
		return errStatus(http.StatusUnauthorized, "Wrong username or recovery code.")
	}
	if msg := validPassword(username, next); msg != "" {
		return errStatus(http.StatusBadRequest, msg)
	}
	var a account
	err = s.accounts.QueryRowContext(r.Context(), `SELECT a.id, a.username FROM accounts a JOIN account_recovery_codes c ON c.account_id = a.id WHERE a.username_key = ? AND c.code_hash = ?`, strings.ToLower(username), recoveryHash(code)).Scan(&a.ID, &a.Username)
	if errors.Is(err, sql.ErrNoRows) {
		s.failedGuess(guesses, now)
		return errStatus(http.StatusUnauthorized, "Wrong username or recovery code.")
	}
	if err != nil {
		return err
	}
	var hash string
	if err := s.argon(r.Context(), func() { hash = hashPassword(next) }); err != nil {
		return err
	}
	tx, err := s.accounts.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), "DELETE FROM account_recovery_codes WHERE account_id = ? AND code_hash = ?", a.ID, recoveryHash(code))
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return errStatus(http.StatusUnauthorized, "Wrong username or recovery code.")
	}
	if _, err := tx.ExecContext(r.Context(), "UPDATE accounts SET password = ? WHERE id = ?", hash, a.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM account_sessions WHERE account_id = ?", a.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM vrchat_sessions WHERE account_id = ?", a.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.setAccountCookie(w, "", 0)
	s.clearVRChatSessions(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}
