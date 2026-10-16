// Package vrchat signs the desktop into VRChat and checks avatars with that
// session. Credentials go only to VRChat. The resulting session is kept in the
// operating system's secure storage (Windows Credential Manager or the Linux
// Secret Service); the password is never saved.
package vrchat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
	"vrcove/companion/internal/buildinfo"
	"vrcove/companion/internal/retry"
)

const (
	api        = "https://api.vrchat.cloud/api/1"
	service    = "VRCove Logger"
	contact    = "vrcove-contact.overbid399@passmail.net"
	limitLabel = "VRChat (this PC)"
)

var (
	token    = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,512}$`)
	avatarID = regexp.MustCompile(`^avtr_[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
	sixDigit = regexp.MustCompile(`^[0-9]{6}$`)
	recovery = regexp.MustCompile(`^[a-zA-Z0-9]{4}-?[a-zA-Z0-9]{4}$`)
)

// Verification methods VRChat may ask for after the password.
const (
	Authenticator = "totp"
	RecoveryCode  = "otp"
	EmailCode     = "emailOtp"
)

// State is where the desktop's VRChat login stands.
type State int

const (
	Loading State = iota
	SignedOut
	NeedsCode
	Ready
)

// Info describes the login for display. Message explains storage, expiry and
// verification problems; Methods lists accepted codes while State is NeedsCode.
type Info struct {
	State   State
	Name    string
	Methods []string
	Message string
	Busy    bool
}

// Verdict is the result of checking one avatar.
type Verdict int

const (
	Public Verdict = iota
	Rejected
)

var (
	// ErrSignedOut keeps avatars queued until the desktop signs in to VRChat.
	ErrSignedOut = errors.New("Sign in to VRChat in Settings to check avatars · collecting locally")
	// ErrChanged means the login changed while a check was running.
	ErrChanged = errors.New("VRChat login changed during a check · will retry")
	// ErrNoLogin is returned by a Store that has nothing saved.
	ErrNoLogin = errors.New("no saved login")
	errBusy    = errors.New("Please wait for the current VRChat sign-in step to finish")
	errStorage = errors.New("Secure storage is locked or unavailable · unlock your system keyring (Windows Credential Manager) and try again")
)

// Store keeps the session string in secure storage.
type Store interface {
	Get() (string, error)
	Set(string) error
	Delete() error
}

type keyringStore struct{ user string }

func (s keyringStore) Get() (string, error) {
	value, err := keyring.Get(service, s.user)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNoLogin
	}
	return value, err
}
func (s keyringStore) Set(value string) error { return keyring.Set(service, s.user, value) }
func (s keyringStore) Delete() error {
	if err := keyring.Delete(service, s.user); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

// secret is the saved session: VRChat's auth cookies and who they belong to.
type secret struct {
	Auth      string `json:"auth"`
	TwoFactor string `json:"twoFactorAuth,omitempty"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
}

func (s secret) valid() bool {
	return token.MatchString(s.Auth) && (s.TwoFactor == "" || token.MatchString(s.TwoFactor)) && strings.HasPrefix(s.UserID, "usr_")
}

// Client is safe for concurrent use. Network calls never hold its locks, so the
// GUI can always read Info and sign out.
type Client struct {
	base         string
	http         *http.Client
	store        Store
	storeTimeout time.Duration
	now          func() time.Time
	onChange     func()

	// ops serializes login, verification, sign-out, expiry and saving rotated
	// cookies, so an older session can never be saved over a newer state.
	ops sync.Mutex

	// queue runs secure-storage calls in order on one goroutine.
	queueOnce sync.Once
	queue     chan func()

	mu          sync.Mutex
	gen         uint64
	session     secret
	methods     []string
	state       State
	message     string
	busy        bool
	until       time.Time
	cancelLogin context.CancelFunc
}

// New uses the system's secure storage. Each state file has its own saved
// login. Call Load once, off the GUI thread, before signing in.
func New(statePath string, onChange func()) *Client {
	absolute, _ := filepath.Abs(statePath)
	store := keyringStore{fmt.Sprintf("session-%x", sha256.Sum256([]byte(absolute)))}
	return NewWith(api, nil, store, onChange)
}

// NewWith is New with a custom VRChat API base, HTTP client and store.
func NewWith(base string, client *http.Client, store Store, onChange func()) *Client {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	// Never forward cookies or credentials through a redirect.
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if onChange == nil {
		onChange = func() {}
	}
	return &Client{base: strings.TrimRight(base, "/"), http: &copied, store: store, storeTimeout: 90 * time.Second, now: time.Now, onChange: onChange}
}

func (c *Client) Info() Info {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Info{State: c.state, Name: c.session.Name, Methods: slices.Clone(c.methods), Message: c.message, Busy: c.busy}
}

// Ready reports whether avatar checks can run.
func (c *Client) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state == Ready
}

// Load reads the saved session. Locked or missing storage leaves the desktop
// signed out with an explanation; it never falls back to a plaintext copy.
func (c *Client) Load() {
	c.ops.Lock()
	defer c.ops.Unlock()
	var raw string
	err := c.storage(func() (err error) { raw, err = c.store.Get(); return err })
	var saved secret
	c.mu.Lock()
	switch {
	case errors.Is(err, ErrNoLogin):
		c.state = SignedOut
	case err != nil:
		c.state, c.message = SignedOut, "Couldn’t read the saved VRChat login. "+errStorage.Error()+"."
	case json.Unmarshal([]byte(raw), &saved) != nil || !saved.valid():
		c.state, c.message = SignedOut, "The saved VRChat login was unreadable · sign in again"
	default:
		c.state, c.session = Ready, saved
	}
	c.mu.Unlock()
	c.onChange()
}

// Login sends the username and password to VRChat only. If VRChat asks for a
// verification code, the state becomes NeedsCode; call Verify next.
func (c *Client) Login(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return errors.New("Enter your VRChat username and password")
	}
	ctx, done, err := c.begin()
	if err != nil {
		return err
	}
	defer done()
	c.ops.Lock()
	defer c.ops.Unlock()
	if ctx.Err() != nil {
		return context.Canceled
	}
	// A new login replaces the old one, so the old one must not stay saved.
	gen := c.reset(SignedOut, "")
	if err := c.storage(c.store.Delete); err != nil {
		return c.fail(gen, "Couldn’t replace the saved VRChat login. "+errStorage.Error()+".")
	}
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(encodeURIComponent(username)+":"+encodeURIComponent(password)))
	reply, err := c.send(ctx, http.MethodGet, "/auth/user", nil, secret{}, basic)
	if err != nil {
		return err
	}
	switch reply.status {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return errors.New("VRChat didn’t accept that username or password")
	default:
		return c.unexpected(reply)
	}
	sec := reply.merge(secret{})
	if methods := supported(reply.data["requiresTwoFactorAuth"]); methods != nil {
		if len(methods) == 0 {
			return errors.New("VRChat asked for a verification method this app doesn’t support")
		}
		if !token.MatchString(sec.Auth) {
			return errors.New("VRChat didn’t start a login session · try again")
		}
		c.mu.Lock()
		if gen == c.gen {
			c.session, c.methods, c.state = sec, methods, NeedsCode
		}
		c.mu.Unlock()
		return nil
	}
	return c.finish(gen, sec, reply.data)
}

// Verify completes a login with an authenticator, recovery or email code.
func (c *Client) Verify(method, code string) error {
	code = strings.TrimSpace(code)
	ctx, done, err := c.begin()
	if err != nil {
		return err
	}
	defer done()
	c.ops.Lock()
	defer c.ops.Unlock()
	c.mu.Lock()
	gen, sec, state, methods := c.gen, c.session, c.state, c.methods
	c.mu.Unlock()
	if state != NeedsCode {
		return errors.New("Sign in again before entering a verification code")
	}
	if !slices.Contains(methods, method) {
		return errors.New("Choose one of the verification methods VRChat offered")
	}
	if method == RecoveryCode && !recovery.MatchString(code) || method != RecoveryCode && !sixDigit.MatchString(code) {
		return errors.New("Enter the code exactly as shown, for example 123456")
	}
	path := map[string]string{Authenticator: "totp", RecoveryCode: "otp", EmailCode: "emailotp"}[method]
	reply, err := c.send(ctx, http.MethodPost, "/auth/twofactorauth/"+path+"/verify", map[string]string{"code": code}, sec, "")
	if err != nil {
		return err
	}
	if reply.status == http.StatusUnauthorized {
		return c.fail(gen, "That sign-in expired · enter your password again")
	}
	if reply.status == http.StatusTooManyRequests {
		return c.unexpected(reply)
	}
	if verified, _ := reply.data["verified"].(bool); reply.status != http.StatusOK || !verified {
		return errors.New("VRChat didn’t accept that code · check it and try again")
	}
	sec = reply.merge(sec)
	reply, err = c.send(ctx, http.MethodGet, "/auth/user", nil, sec, "")
	if err != nil {
		return err
	}
	if reply.status != http.StatusOK {
		if reply.status == http.StatusUnauthorized {
			return c.fail(gen, "That sign-in expired · enter your password again")
		}
		return c.unexpected(reply)
	}
	return c.finish(gen, reply.merge(sec), reply.data)
}

// Logout signs the desktop out, removes the saved session and ends it at
// VRChat. A sign-in in progress is cancelled.
func (c *Client) Logout() error {
	c.mu.Lock()
	if c.cancelLogin != nil {
		c.cancelLogin()
	}
	c.mu.Unlock()
	c.ops.Lock()
	defer c.ops.Unlock()
	c.mu.Lock()
	old := c.session
	c.mu.Unlock()
	gen := c.reset(SignedOut, "")
	c.onChange()
	if old.Auth != "" {
		// Best effort: the local session is already gone either way.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = c.send(ctx, http.MethodPut, "/logout", nil, old, "")
		}()
	}
	if err := c.storage(c.store.Delete); err != nil {
		return c.fail(gen, "Signed out here, but the saved login couldn’t be removed. "+errStorage.Error()+", then sign out again.")
	}
	return nil
}

// Check asks VRChat whether id is a public avatar this session can see.
// Private, missing or inaccessible avatars are Rejected. Signed-out sessions,
// expired logins, rate limits and connection problems return an error so the
// avatar stays queued.
func (c *Client) Check(ctx context.Context, id string) (Verdict, error) {
	c.mu.Lock()
	gen, sec, state, until := c.gen, c.session, c.state, c.until
	c.mu.Unlock()
	if state != Ready {
		return Rejected, ErrSignedOut
	}
	if c.now().Before(until) {
		return Rejected, &retry.Limited{Source: limitLabel, Until: until}
	}
	if !avatarID.MatchString(id) {
		return Rejected, nil
	}
	reply, err := c.send(ctx, http.MethodGet, "/avatars/"+id, nil, sec, "")
	if err != nil {
		return Rejected, err
	}
	c.rotate(gen, reply)
	switch reply.status {
	case http.StatusOK:
		c.mu.Lock()
		changed := gen != c.gen
		c.mu.Unlock()
		if changed {
			return Rejected, ErrChanged
		}
		if reply.data["id"] == id && reply.data["releaseStatus"] == "public" {
			return Public, nil
		}
		return Rejected, nil
	case http.StatusUnauthorized:
		c.expire(gen)
		return Rejected, errors.New("VRChat login expired · sign in again in Settings · collecting locally")
	case http.StatusForbidden, http.StatusNotFound:
		return Rejected, nil
	default:
		return Rejected, c.unexpected(reply)
	}
}

// begin marks a sign-in step as running. done must be called when it ends.
func (c *Client) begin() (context.Context, func(), error) {
	c.mu.Lock()
	if c.busy {
		c.mu.Unlock()
		return nil, nil, errBusy
	}
	if c.state == Loading {
		c.mu.Unlock()
		return nil, nil, errors.New("Still reading the saved VRChat login · try again in a moment")
	}
	if c.now().Before(c.until) {
		until := c.until
		c.mu.Unlock()
		return nil, nil, &retry.Limited{Source: limitLabel, Until: until}
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.busy, c.cancelLogin, c.message = true, cancel, ""
	c.mu.Unlock()
	c.onChange()
	return ctx, func() {
		cancel()
		c.mu.Lock()
		c.busy, c.cancelLogin = false, nil
		c.mu.Unlock()
		c.onChange()
	}, nil
}

// reset starts a new login generation in state with message. Callers hold ops.
func (c *Client) reset(state State, message string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.session, c.methods, c.state, c.message = secret{}, nil, state, message
	return c.gen
}

// fail signs out generation gen with message and returns it as an error.
func (c *Client) fail(gen uint64, message string) error {
	c.mu.Lock()
	if gen == c.gen {
		c.session, c.methods, c.state, c.message = secret{}, nil, SignedOut, message
	}
	c.mu.Unlock()
	return errors.New(message)
}

// finish saves a completed login. If secure storage refuses it, the desktop
// stays signed out rather than keeping an unsaved or plaintext session.
func (c *Client) finish(gen uint64, sec secret, user map[string]any) error {
	sec.UserID, _ = user["id"].(string)
	sec.Name, _ = user["displayName"].(string)
	if !sec.valid() {
		return c.fail(gen, "VRChat didn’t return a complete login · try again")
	}
	raw, _ := json.Marshal(sec)
	if err := c.storage(func() error { return c.store.Set(string(raw)) }, c.store.Delete); err != nil {
		return c.fail(gen, "Signed in, but the login couldn’t be saved securely, so it wasn’t kept. "+errStorage.Error()+".")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return ErrChanged
	}
	c.session, c.methods, c.state, c.message = sec, nil, Ready, ""
	return nil
}

// rotate keeps cookies VRChat replaced during a check, if the login is unchanged.
func (c *Client) rotate(gen uint64, reply *reply) {
	if reply.auth == "" && reply.twoFactor == "" {
		return
	}
	c.ops.Lock()
	defer c.ops.Unlock()
	c.mu.Lock()
	if gen != c.gen || c.state != Ready {
		c.mu.Unlock()
		return
	}
	c.session = reply.merge(c.session)
	raw, _ := json.Marshal(c.session)
	c.mu.Unlock()
	err := c.storage(func() error { return c.store.Set(string(raw)) })
	c.mu.Lock()
	if gen == c.gen {
		if err != nil {
			// Keep using the refreshed session; only the saved copy is behind.
			c.message = "Couldn’t save the refreshed VRChat login. " + errStorage.Error() + "."
		} else if strings.HasPrefix(c.message, "Couldn’t save the refreshed") {
			c.message = ""
		}
	}
	c.mu.Unlock()
	c.onChange()
}

// expire signs out a login VRChat no longer accepts and removes the saved copy.
func (c *Client) expire(gen uint64) {
	c.ops.Lock()
	defer c.ops.Unlock()
	c.mu.Lock()
	current := gen == c.gen
	c.mu.Unlock()
	if !current {
		return
	}
	gen = c.reset(SignedOut, "VRChat login expired · sign in again to resume uploads")
	if err := c.storage(c.store.Delete); err != nil {
		_ = c.fail(gen, "VRChat login expired, and the saved copy couldn’t be removed. "+errStorage.Error()+".")
	}
	c.onChange()
}

// storage runs a secure-storage call with a time limit, since an unlock prompt
// may never be answered. Calls run one at a time in order, so a slow call that
// finishes late can never overtake a newer one. If a call times out, undo (when
// given) is queued right behind it, so a save that eventually succeeds after
// being reported as failed is removed again.
func (c *Client) storage(op func() error, undo ...func() error) error {
	c.queueOnce.Do(func() {
		c.queue = make(chan func(), 16)
		go func() {
			for job := range c.queue {
				job()
			}
		}()
	})
	done := make(chan error, 1)
	c.queue <- func() { done <- op() }
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, ErrNoLogin) {
			return errStorage
		}
		return err
	case <-time.After(c.storeTimeout):
		for _, u := range undo {
			c.queue <- func() { _ = u() }
		}
		return errStorage
	}
}

type reply struct {
	status          int
	header          http.Header
	data            map[string]any
	auth, twoFactor string
}

// merge applies cookies VRChat sent to sec.
func (r *reply) merge(sec secret) secret {
	if r.auth != "" {
		sec.Auth = r.auth
	}
	if r.twoFactor != "" {
		sec.TwoFactor = r.twoFactor
	}
	return sec
}

// unexpected turns a non-success reply into a retryable error, recording rate
// limits so later calls wait.
func (c *Client) unexpected(r *reply) error {
	if r.status == http.StatusTooManyRequests {
		until := retry.After(r.header.Get("Retry-After"), c.now())
		c.mu.Lock()
		if until.After(c.until) {
			c.until = until
		}
		c.mu.Unlock()
		return &retry.Limited{Source: limitLabel, Until: until}
	}
	return fmt.Errorf("VRChat returned %d · will retry", r.status)
}

// send makes one VRChat request with sec's cookies. It touches no client state.
func (c *Client) send(ctx context.Context, method, path string, body any, sec secret, authorization string) (*reply, error) {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, errors.New("Couldn’t create the VRChat request")
	}
	// VRChat asks API clients to identify themselves with a contact.
	req.Header.Set("User-Agent", buildinfo.UserAgent()+" ("+contact+")")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if sec.Auth != "" {
		req.AddCookie(&http.Cookie{Name: "auth", Value: sec.Auth})
	}
	if sec.TwoFactor != "" {
		req.AddCookie(&http.Cookie{Name: "twoFactorAuth", Value: sec.TwoFactor})
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, context.Canceled
		}
		return nil, errors.New("VRChat connection unavailable · will retry")
	}
	defer resp.Body.Close()
	r := &reply{status: resp.StatusCode, header: resp.Header, data: map[string]any{}}
	for _, cookie := range resp.Cookies() {
		if cookie.MaxAge < 0 || !token.MatchString(cookie.Value) {
			continue
		}
		switch cookie.Name {
		case "auth":
			r.auth = cookie.Value
		case "twoFactorAuth":
			r.twoFactor = cookie.Value
		}
	}
	if resp.StatusCode == http.StatusOK {
		if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&r.data) != nil {
			return nil, errors.New("Invalid VRChat response · will retry")
		}
	}
	return r, nil
}

// supported returns the offered verification methods this app handles, nil if
// VRChat asked for none, or an empty slice if it only offered unknown ones.
func supported(raw any) []string {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	methods := []string{}
	for _, candidate := range []string{Authenticator, RecoveryCode, EmailCode} {
		if slices.Contains(list, any(candidate)) {
			methods = append(methods, candidate)
		}
	}
	return methods
}

// encodeURIComponent matches JavaScript's escaping, which VRChat expects
// inside Basic credentials.
func encodeURIComponent(s string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if ch := s[i]; strings.IndexByte(unreserved, ch) >= 0 {
			b.WriteByte(ch)
		} else {
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}
