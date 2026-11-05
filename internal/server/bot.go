package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
)

// The bot is a VRChat account the operator owns. It checks logger
// submissions, so the server never needs a user's VRChat session for them.
// Its cookies are sealed under ACCOUNT_KEY in the accounts database, so a
// restart does not need a new email code. Owners manage it in the admin panel
// (bot_admin.go) or with `vrcove bot-login`.

// BotConfig is the bot account's VRChat sign-in.
type BotConfig struct{ Username, Password string }

const (
	botAAD = "vrcove-bot-session-v1"
	// botLoginGap spaces automatic sign-ins. When VRChat asks for a new email
	// code, the bot waits for an owner to sign it in instead of mailing one per
	// request.
	botLoginGap = time.Hour
)

var errBotUnavailable = errStatus(http.StatusServiceUnavailable, "Avatar checks are paused while VRCove's VRChat account signs in again. Try again later.")

type botState struct {
	mu         sync.Mutex
	tokens     vrchat.Tokens
	userID     string
	loginAfter time.Time
	// displayName and signedIn describe the session for the admin panel.
	displayName string
	signedIn    time.Time
	// saved is the sign-in an owner entered in the admin panel. It replaces
	// the environment's and is empty when the two match.
	saved BotConfig
	// signedOut stops automatic sign-ins until an owner signs in again.
	signedOut bool
	// pending is a sign-in waiting for a two-factor code. It stays apart from
	// tokens, which always hold a usable session or none.
	pending *botPending
	// problem is why the last automatic sign-in failed, for the admin panel.
	problem string
}

type botRecord struct {
	UserID        string `json:"userId"`
	Auth          string `json:"auth,omitempty"`
	TwoFactorAuth string `json:"twoFactorAuth,omitempty"`
	DisplayName   string `json:"displayName,omitempty"`
	SignedIn      int64  `json:"signedIn,omitempty"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	SignedOut     bool   `json:"signedOut,omitempty"`
}

func (b *botState) session() *Session {
	return &Session{UserID: b.userID, Auth: b.tokens.Auth, TwoFactorAuth: b.tokens.TwoFactorAuth}
}

// botWait is how long the bot must still wait after a VRChat 429.
func (s *Server) botWait() time.Duration {
	s.bot.mu.Lock()
	defer s.bot.mu.Unlock()
	return s.backoff.remaining(backoffKey(s.bot.session()), s.now())
}

// loadBot reads the stored session, which `vrcove bot-login` may have
// replaced. Callers hold mu.
func (s *Server) loadBot(ctx context.Context) error {
	var sealed string
	err := s.accounts.QueryRowContext(ctx, "SELECT sealed FROM bot_session WHERE id = 1").Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	plain, err := openBytes(s.keys.linkSeal, sealed, botAAD)
	if err != nil {
		return err
	}
	var rec botRecord
	if err := json.Unmarshal(plain, &rec); err != nil {
		return err
	}
	s.bot.tokens = vrchat.Tokens{Auth: rec.Auth, TwoFactorAuth: rec.TwoFactorAuth}
	s.bot.userID = rec.UserID
	s.bot.displayName = rec.DisplayName
	s.bot.signedIn = time.Time{}
	if rec.SignedIn != 0 {
		s.bot.signedIn = time.Unix(rec.SignedIn, 0)
	}
	s.bot.saved = BotConfig{Username: rec.Username, Password: rec.Password}
	s.bot.signedOut = rec.SignedOut
	return nil
}

// saveBot stores the bot's current cookies. Callers hold mu.
func (s *Server) saveBot(ctx context.Context) error {
	rec := botRecord{UserID: s.bot.userID, Auth: s.bot.tokens.Auth, TwoFactorAuth: s.bot.tokens.TwoFactorAuth, DisplayName: s.bot.displayName,
		Username: s.bot.saved.Username, Password: s.bot.saved.Password, SignedOut: s.bot.signedOut}
	if !s.bot.signedIn.IsZero() {
		rec.SignedIn = s.bot.signedIn.Unix()
	}
	plain, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	sealed, err := sealBytes(s.keys.linkSeal, plain, botAAD)
	if err != nil {
		return err
	}
	_, err = s.accounts.ExecContext(ctx, "INSERT INTO bot_session (id, sealed, updated) VALUES (1, ?, ?) ON CONFLICT (id) DO UPDATE SET sealed = excluded.sealed, updated = excluded.updated", sealed, s.now().Unix())
	return err
}

// botReady makes sure the bot has a session, signing in with its password
// when it has none. The stored twoFactorAuth cookie normally spares VRChat's
// email code. Callers hold mu.
func (s *Server) botReady(ctx context.Context) error {
	if s.bot.tokens.Auth != "" {
		return nil
	}
	if err := s.loadBot(ctx); err != nil {
		return err
	}
	if s.bot.tokens.Auth != "" {
		s.bot.loginAfter = time.Time{}
		return nil
	}
	bot := s.botCredentials()
	if s.bot.signedOut || bot.Username == "" || s.now().Before(s.bot.loginAfter) {
		return errBotUnavailable
	}
	s.bot.loginAfter = s.now().Add(botLoginGap)
	pending, err := s.botLogin(ctx, bot.Username, bot.Password)
	if err != nil {
		s.bot.problem = botProblemFailed
		log.Printf("VRChat bot sign-in failed: %v", err)
		return errBotUnavailable
	}
	if pending != nil {
		s.bot.tokens.Auth = ""
		s.bot.problem = botProblemCode
		log.Printf("VRChat bot needs a two-factor code (%v). Sign it in from the admin panel or run `vrcove bot-login` on the server.", pending)
		return errBotUnavailable
	}
	s.bot.loginAfter, s.bot.problem = time.Time{}, ""
	return nil
}

// botCredentials is the sign-in for automatic sign-ins: the one saved in the
// admin panel, or else the environment's. Callers hold mu.
func (s *Server) botCredentials() BotConfig {
	if s.bot.saved.Username != "" {
		return s.bot.saved
	}
	if s.cfg.Logger != nil {
		return s.cfg.Logger.Bot
	}
	return BotConfig{}
}

// botLogin signs the bot in. When VRChat asks for two-factor verification it
// returns the methods it offered and keeps the partial session in memory
// only. Callers hold mu.
func (s *Server) botLogin(ctx context.Context, username, password string) ([]string, error) {
	data, updates, err := s.vrc(ctx, &Session{TwoFactorAuth: s.bot.tokens.TwoFactorAuth}, vrchat.Login(username, password))
	if err != nil {
		return nil, err
	}
	tokens := vrchat.Tokens{TwoFactorAuth: s.bot.tokens.TwoFactorAuth}
	tokens.Apply(updates)
	if tokens.Auth == "" {
		return nil, errors.New("VRChat did not issue a session")
	}
	user, _ := data.(map[string]any)
	if raw, ok := user["requiresTwoFactorAuth"].([]any); ok && len(raw) > 0 {
		methods := []string{}
		for _, m := range raw {
			if m, ok := m.(string); ok {
				methods = append(methods, m)
			}
		}
		s.bot.tokens, s.bot.userID = tokens, ""
		return methods, nil
	}
	s.bot.tokens, s.bot.userID, s.bot.displayName, s.bot.signedIn = tokens, field(user, "id"), field(user, "displayName"), s.now()
	return nil, s.saveBot(ctx)
}

// BotLogin signs the bot in from the command line, asking code for any
// two-factor code VRChat wants, and stores its session for the server.
// It returns the bot's display name.
func (s *Server) BotLogin(ctx context.Context, username, password string, code func(method string) (string, error)) (string, error) {
	if s.accounts == nil {
		return "", errors.New("the bot session is stored with VRCove accounts: set ACCOUNT_KEY")
	}
	s.bot.mu.Lock()
	defer s.bot.mu.Unlock()
	if err := s.loadBot(ctx); err != nil {
		return "", err
	}
	s.bot.tokens.Auth = ""
	pending, err := s.botLogin(ctx, username, password)
	if err != nil {
		return "", err
	}
	if pending != nil {
		method := ""
		switch {
		case slices.Contains(pending, "emailOtp"):
			method = "emailOtp"
		case slices.Contains(pending, "totp"):
			method = "totp"
		default:
			return "", errors.New("VRChat asked for an unsupported two-factor method")
		}
		answer, err := code(method)
		if err != nil {
			return "", err
		}
		result, updates, err := s.vrc(ctx, s.bot.session(), vrchat.VerifyTwoFactor(method, answer))
		if err != nil {
			return "", err
		}
		if status, _ := result.(map[string]any); status["verified"] != true {
			return "", errors.New("VRChat did not accept that code")
		}
		s.bot.tokens.Apply(updates)
	}
	user, updates, err := s.vrc(ctx, s.bot.session(), vrchat.GetCurrentUser())
	if err != nil {
		return "", err
	}
	s.bot.tokens.Apply(updates)
	if s.bot.userID = field(user, "id"); s.bot.userID == "" {
		return "", errors.New("VRChat did not return the bot's account")
	}
	// The command line signs in with the environment's account.
	s.bot.displayName, s.bot.signedIn, s.bot.saved, s.bot.signedOut = field(user, "displayName"), s.now(), BotConfig{}, false
	s.bot.pending, s.bot.problem, s.bot.loginAfter = nil, "", time.Time{}
	if err := s.saveBot(ctx); err != nil {
		return "", err
	}
	return field(user, "displayName"), nil
}

// botCall makes call as the bot. When VRChat has ended the bot's session it
// signs in again once and retries.
func (s *Server) botCall(ctx context.Context, call vrchat.Call) (any, error) {
	s.bot.mu.Lock()
	defer s.bot.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if err := s.botReady(ctx); err != nil {
			return nil, err
		}
		data, updates, err := s.vrc(ctx, s.bot.session(), call)
		var he *httpError
		if errors.As(err, &he) && he.status == http.StatusUnauthorized {
			// Keep twoFactorAuth so signing in again needs no code.
			s.bot.tokens.Auth = ""
			if err := s.saveBot(ctx); err != nil {
				return nil, err
			}
			if attempt == 0 {
				continue
			}
			return nil, errBotUnavailable
		}
		if updates != (vrchat.Tokens{}) {
			s.bot.tokens.Apply(updates)
			if saveErr := s.saveBot(ctx); saveErr != nil {
				return nil, saveErr
			}
		}
		return data, err
	}
}
