package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
)

// Owners manage the bot from the admin panel: they see its state, sign it in,
// enter the two-factor code VRChat asks for, check the session and sign it out.
// A failed attempt leaves the working session alone. The status never includes
// the password or session tokens.

const (
	// botSignIns caps sign-in and code attempts per limiter window, because
	// VRChat locks accounts after repeated failures.
	botSignIns = 10
	// botCodeLifetime is how long a sign-in waits for its two-factor code.
	botCodeLifetime = 10 * time.Minute
	maxBotUsername  = 256
	maxBotPassword  = 1024

	botProblemCode   = "code"
	botProblemFailed = "failed"
)

var botCode = regexp.MustCompile(`^[0-9A-Za-z-]{6,16}$`)

// botPending is a sign-in waiting for its code. The password stays in memory
// only, and is saved once the sign-in completes.
type botPending struct {
	login   BotConfig
	tokens  vrchat.Tokens
	methods []string
	expires time.Time
}

// botFresh reloads the stored session unless memory has a usable one, and
// drops a sign-in whose code has expired. Callers hold mu.
func (s *Server) botFresh(ctx context.Context) error {
	if p := s.bot.pending; p != nil && !s.now().Before(p.expires) {
		s.bot.pending = nil
	}
	if s.bot.tokens.Auth != "" {
		return nil
	}
	return s.loadBot(ctx)
}

func (s *Server) adminBotStatus(w http.ResponseWriter, r *http.Request) error {
	s.bot.mu.Lock()
	defer s.bot.mu.Unlock()
	if err := s.botFresh(r.Context()); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s.botStatus())
	return nil
}

// botStatus describes the bot for the admin panel. Callers hold mu.
func (s *Server) botStatus() map[string]any {
	bot := s.botCredentials()
	state := "needs-sign-in"
	switch {
	case s.bot.pending != nil:
		state = "code"
	case s.bot.tokens.Auth != "" && s.bot.userID != "":
		state = "active"
	case s.bot.signedOut:
		state = "signed-out"
	}
	status := map[string]any{
		"state": state, "username": bot.Username, "displayName": s.bot.displayName, "userId": s.bot.userID,
		"problem": s.bot.problem, "passwordSaved": s.bot.saved.Password != "", "canRetry": bot.Password != "" && !s.bot.signedOut,
		"loggerEnabled": s.cfg.Logger != nil, "methods": []string{}, "signedIn": int64(0), "nextSignIn": int64(0),
		"rateLimitedFor": int64(s.backoff.remaining(backoffKey(s.bot.session()), s.now()).Seconds()),
	}
	if p := s.bot.pending; p != nil {
		status["methods"], status["username"] = p.methods, p.login.Username
	}
	if !s.bot.signedIn.IsZero() {
		status["signedIn"] = s.bot.signedIn.Unix()
	}
	if state == "needs-sign-in" && s.bot.loginAfter.After(s.now()) {
		status["nextSignIn"] = s.bot.loginAfter.Unix()
	}
	return status
}

func (s *Server) adminBot(w http.ResponseWriter, r *http.Request, a *account) error {
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	action, _ := str(body, "action")
	ctx := r.Context()
	s.bot.mu.Lock()
	defer s.bot.mu.Unlock()
	if err := s.botFresh(ctx); err != nil {
		return err
	}
	switch action {
	case "login":
		username, _ := str(body, "username")
		password, _ := str(body, "password")
		err = s.adminBotLogin(ctx, a, strings.TrimSpace(username), password)
	case "verify":
		method, _ := str(body, "method")
		code, _ := str(body, "code")
		err = s.adminBotVerify(ctx, a, method, strings.TrimSpace(code))
	case "cancel":
		s.bot.pending = nil
	case "check":
		err = s.adminBotCheck(ctx)
	case "logout":
		err = s.adminBotLogout(ctx, a)
	default:
		err = errStatus(http.StatusBadRequest, "Unknown action.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s.botStatus())
	return nil
}

// allowBotSignIn counts an attempt against VRChat's sign-in or code routes.
func (s *Server) allowBotSignIn() error {
	if s.limiter.allowN("cove-bot-sign-in", botSignIns, s.now()) > 0 {
		return errStatus(http.StatusTooManyRequests, "Too many sign-in attempts. Try again later.")
	}
	return nil
}

// botRejected turns a VRChat refusal into message, because a 401 here means
// the bot's sign-in, not the owner's VRCove session. Rate limits pass through.
func botRejected(err error, message string) error {
	var he *httpError
	if errors.As(err, &he) && he.status >= 400 && he.status < 500 && he.status != http.StatusTooManyRequests {
		return errStatus(http.StatusBadRequest, message)
	}
	return err
}

// adminBotLogin starts a sign-in. Without a username or password it retries
// the sign-in the server already has, which is how an owner answers VRChat
// asking for a new code. The current session stays until the new one works.
func (s *Server) adminBotLogin(ctx context.Context, a *account, username, password string) error {
	if username == "" && password == "" {
		if s.bot.signedOut {
			return errStatus(http.StatusBadRequest, "Enter the account's username and password.")
		}
		login := s.botCredentials()
		username, password = login.Username, login.Password
	}
	if username == "" || password == "" {
		return errStatus(http.StatusBadRequest, "Enter the account's username and password.")
	}
	if s.cfg.Contact == "" {
		return errStatus(http.StatusServiceUnavailable, "Set VRC_CONTACT on the server to sign in to VRChat.")
	}
	if len(username) > maxBotUsername || len(password) > maxBotPassword {
		return errStatus(http.StatusBadRequest, "That username or password is too long.")
	}
	if err := s.allowBotSignIn(); err != nil {
		return err
	}
	login := BotConfig{Username: username, Password: password}
	// A stored twoFactorAuth cookie spares the code, but only for its account.
	tokens := vrchat.Tokens{}
	if strings.EqualFold(username, s.botCredentials().Username) {
		tokens.TwoFactorAuth = s.bot.tokens.TwoFactorAuth
	}
	data, updates, err := s.vrc(ctx, &Session{TwoFactorAuth: tokens.TwoFactorAuth}, vrchat.Login(username, password))
	if err != nil {
		return botRejected(err, "VRChat did not accept that username or password.")
	}
	tokens.Apply(updates)
	if tokens.Auth == "" {
		return errStatus(http.StatusBadGateway, "VRChat did not issue a session.")
	}
	user, _ := data.(map[string]any)
	if raw, ok := user["requiresTwoFactorAuth"].([]any); ok && len(raw) > 0 {
		methods := []string{}
		for _, m := range raw {
			if m, ok := m.(string); ok && (m == "emailOtp" || m == "totp" || m == "otp") {
				methods = append(methods, m)
			}
		}
		if len(methods) == 0 {
			return errStatus(http.StatusBadGateway, "VRChat asked for an unsupported two-factor method.")
		}
		s.bot.pending = &botPending{login: login, tokens: tokens, methods: methods, expires: s.now().Add(botCodeLifetime)}
		return nil
	}
	return s.adminBotFinish(ctx, a, login, tokens, user)
}

func (s *Server) adminBotVerify(ctx context.Context, a *account, method, code string) error {
	p := s.bot.pending
	if p == nil {
		return errStatus(http.StatusConflict, "That sign-in has expired. Sign in again.")
	}
	if !slices.Contains(p.methods, method) || !botCode.MatchString(code) {
		return errStatus(http.StatusBadRequest, "Enter the code from VRChat.")
	}
	if err := s.allowBotSignIn(); err != nil {
		return err
	}
	session := &Session{Auth: p.tokens.Auth, TwoFactorAuth: p.tokens.TwoFactorAuth}
	result, updates, err := s.vrc(ctx, session, vrchat.VerifyTwoFactor(method, code))
	if err != nil {
		return botRejected(err, "VRChat did not accept that code.")
	}
	if status, _ := result.(map[string]any); status["verified"] != true {
		return errStatus(http.StatusBadRequest, "VRChat did not accept that code.")
	}
	tokens := p.tokens
	tokens.Apply(updates)
	data, updates, err := s.vrc(ctx, &Session{Auth: tokens.Auth, TwoFactorAuth: tokens.TwoFactorAuth}, vrchat.GetCurrentUser())
	if err != nil {
		return botRejected(err, "VRChat did not accept that code.")
	}
	tokens.Apply(updates)
	user, _ := data.(map[string]any)
	return s.adminBotFinish(ctx, a, p.login, tokens, user)
}

// adminBotFinish makes a completed sign-in the bot's session. The password is
// saved only when it differs from the environment's, sealed like the session.
func (s *Server) adminBotFinish(ctx context.Context, a *account, login BotConfig, tokens vrchat.Tokens, user map[string]any) error {
	id := field(user, "id")
	if id == "" {
		return errStatus(http.StatusBadGateway, "VRChat did not return the account.")
	}
	if s.cfg.Logger != nil && login == s.cfg.Logger.Bot {
		login = BotConfig{}
	}
	s.bot.tokens, s.bot.userID, s.bot.displayName, s.bot.signedIn = tokens, id, field(user, "displayName"), s.now()
	s.bot.saved, s.bot.signedOut = login, false
	s.bot.pending, s.bot.problem, s.bot.loginAfter = nil, "", time.Time{}
	if err := s.saveBot(ctx); err != nil {
		return err
	}
	return s.logBotAction(ctx, a, "signed in the backend account")
}

// adminBotCheck asks VRChat whether the session still works. It never signs
// in, so checking cannot trigger a code email.
func (s *Server) adminBotCheck(ctx context.Context) error {
	if s.bot.tokens.Auth == "" {
		return nil
	}
	data, updates, err := s.vrc(ctx, s.bot.session(), vrchat.GetCurrentUser())
	var he *httpError
	if errors.As(err, &he) && he.status == http.StatusUnauthorized {
		// Keep twoFactorAuth so signing in again needs no code.
		s.bot.tokens.Auth = ""
		return s.saveBot(ctx)
	}
	if err != nil {
		return err
	}
	s.bot.tokens.Apply(updates)
	user, _ := data.(map[string]any)
	if name := field(user, "displayName"); name != "" {
		s.bot.displayName = name
	}
	return s.saveBot(ctx)
}

// adminBotLogout ends the session and forgets the saved sign-in. The bot stays
// signed out, even with a sign-in in the environment, until an owner signs in.
func (s *Server) adminBotLogout(ctx context.Context, a *account) error {
	if s.bot.tokens.Auth != "" {
		// VRChat ends the session on its own side too. The local sign-out
		// stands if this fails, so the owner isn't stuck with the old session.
		_, _, _ = s.vrc(ctx, s.bot.session(), vrchat.Logout())
	}
	s.bot.tokens, s.bot.userID, s.bot.displayName, s.bot.signedIn = vrchat.Tokens{}, "", "", time.Time{}
	s.bot.saved, s.bot.signedOut = BotConfig{}, true
	s.bot.pending, s.bot.problem, s.bot.loginAfter = nil, "", time.Time{}
	if err := s.saveBot(ctx); err != nil {
		return err
	}
	return s.logBotAction(ctx, a, "signed out the backend account")
}

// logBotAction records action without the account's name, because every admin
// can read the log and only owners manage this account.
func (s *Server) logBotAction(ctx context.Context, a *account, action string) error {
	tx, err := s.accounts.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.logAction(ctx, tx, a, action, "VRChat"); err != nil {
		return err
	}
	return tx.Commit()
}
