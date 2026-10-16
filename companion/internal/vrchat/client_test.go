package vrchat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vrcove/companion/internal/retry"
)

// All credentials and IDs here are synthetic.
const (
	publicAvatar  = "avtr_00000000-0000-0000-0000-000000000001"
	privateAvatar = "avtr_00000000-0000-0000-0000-000000000002"
	missingAvatar = "avtr_00000000-0000-0000-0000-000000000003"
	hiddenAvatar  = "avtr_00000000-0000-0000-0000-000000000004"
	wrongAvatar   = "avtr_00000000-0000-0000-0000-000000000005"
	expiryAvatar  = "avtr_00000000-0000-0000-0000-000000000006"
	limitAvatar   = "avtr_00000000-0000-0000-0000-000000000007"
	rotateAvatar  = "avtr_00000000-0000-0000-0000-000000000008"
	slowAvatar    = "avtr_00000000-0000-0000-0000-000000000009"
	password      = "synthetic pass:word&"
)

// memoryStore is a fake secure store whose operations can fail or block.
type memoryStore struct {
	mu                       sync.Mutex
	value                    string
	saved                    bool
	getErr, setErr, deleteEr error
	blockSet                 chan struct{}
	history                  []string
}

func (m *memoryStore) Get() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return "", m.getErr
	}
	if !m.saved {
		return "", ErrNoLogin
	}
	return m.value, nil
}
func (m *memoryStore) Set(v string) error {
	m.mu.Lock()
	block := m.blockSet
	m.mu.Unlock()
	if block != nil {
		<-block
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	m.value, m.saved = v, true
	m.history = append(m.history, "set")
	return nil
}
func (m *memoryStore) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteEr != nil {
		return m.deleteEr
	}
	m.value, m.saved = "", false
	m.history = append(m.history, "delete")
	return nil
}
func (m *memoryStore) snapshot() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.value, m.saved
}

// fakeVRChat imitates the VRChat endpoints the client uses.
type fakeVRChat struct {
	*httptest.Server
	avatarCalls, logouts atomic.Int32
	release              chan struct{}
	retryAfter           string
}

func newFakeVRChat(t *testing.T) *fakeVRChat {
	f := &fakeVRChat{release: make(chan struct{})}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := func(name string) string {
			c, _ := r.Cookie(name)
			if c == nil {
				return ""
			}
			return c.Value
		}
		user := func() {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "usr_synthetic", "displayName": "Synthetic"})
		}
		switch {
		case r.URL.Path == "/auth/user":
			if cookie("auth") != "" {
				if cookie("auth") == "authcookie_pending" && cookie("twoFactorAuth") == "" {
					w.WriteHeader(401)
					return
				}
				user()
				return
			}
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(r.Header.Get("Authorization"), "Basic "))
			name, secret, _ := strings.Cut(string(raw), ":")
			if secret != encodeURIComponent(password) {
				w.WriteHeader(401)
				return
			}
			switch name {
			case "plain%20user":
				http.SetCookie(w, &http.Cookie{Name: "auth", Value: "authcookie_1"})
				user()
			case "totp":
				http.SetCookie(w, &http.Cookie{Name: "auth", Value: "authcookie_pending"})
				_ = json.NewEncoder(w).Encode(map[string]any{"requiresTwoFactorAuth": []string{"totp", "otp"}})
			case "email":
				http.SetCookie(w, &http.Cookie{Name: "auth", Value: "authcookie_pending"})
				_ = json.NewEncoder(w).Encode(map[string]any{"requiresTwoFactorAuth": []string{"emailOtp"}})
			default:
				w.WriteHeader(401)
			}
		case strings.HasPrefix(r.URL.Path, "/auth/twofactorauth/"):
			var body struct{ Code string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if cookie("auth") != "authcookie_pending" {
				w.WriteHeader(401)
				return
			}
			if body.Code != "123456" && body.Code != "abcd-1234" {
				w.WriteHeader(400)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "twoFactorAuth", Value: "tfacookie_1"})
			_ = json.NewEncoder(w).Encode(map[string]any{"verified": true})
		case r.URL.Path == "/logout":
			if cookie("auth") != "" {
				f.logouts.Add(1)
			}
		case strings.HasPrefix(r.URL.Path, "/avatars/"):
			f.avatarCalls.Add(1)
			id := strings.TrimPrefix(r.URL.Path, "/avatars/")
			switch id {
			case publicAvatar:
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "releaseStatus": "public"})
			case privateAvatar:
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "releaseStatus": "private"})
			case wrongAvatar:
				_ = json.NewEncoder(w).Encode(map[string]any{"id": publicAvatar, "releaseStatus": "public"})
			case missingAvatar:
				w.WriteHeader(404)
			case hiddenAvatar:
				w.WriteHeader(403)
			case expiryAvatar:
				w.WriteHeader(401)
			case limitAvatar:
				w.Header().Set("Retry-After", f.retryAfter)
				w.WriteHeader(429)
			case rotateAvatar:
				http.SetCookie(w, &http.Cookie{Name: "auth", Value: "authcookie_2"})
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "releaseStatus": "public"})
			case slowAvatar:
				<-f.release
				http.SetCookie(w, &http.Cookie{Name: "auth", Value: "authcookie_late"})
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "releaseStatus": "public"})
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func newClient(t *testing.T, f *fakeVRChat, store *memoryStore) *Client {
	c := NewWith(f.URL, f.Client(), store, nil)
	c.Load()
	return c
}

func signedIn(t *testing.T, f *fakeVRChat, store *memoryStore) *Client {
	t.Helper()
	c := newClient(t, f, store)
	if err := c.Login("plain user", password); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoginSavesSessionButNotPassword(t *testing.T) {
	f, store := newFakeVRChat(t), &memoryStore{}
	c := signedIn(t, f, store)
	if info := c.Info(); info.State != Ready || info.Name != "Synthetic" {
		t.Fatalf("info %+v", info)
	}
	saved, ok := store.snapshot()
	if !ok || !strings.Contains(saved, "authcookie_1") || strings.Contains(saved, "synthetic pass") || strings.Contains(saved, encodeURIComponent(password)) {
		t.Fatalf("saved login %q", saved)
	}
	// A restart loads the saved session without asking for the password.
	if again := newClient(t, f, store); again.Info().State != Ready {
		t.Fatal("saved session was not loaded")
	}
}

func TestWrongPasswordLeavesNothingSaved(t *testing.T) {
	f, store := newFakeVRChat(t), &memoryStore{}
	c := newClient(t, f, store)
	if err := c.Login("plain user", "not it"); err == nil || !strings.Contains(err.Error(), "username or password") {
		t.Fatalf("err %v", err)
	}
	if _, ok := store.snapshot(); ok || c.Info().State != SignedOut {
		t.Fatal("rejected login changed state")
	}
}

func TestTwoFactorMethods(t *testing.T) {
	for _, tc := range []struct{ user, method, code string }{
		{"totp", Authenticator, "123456"},
		{"totp", RecoveryCode, "abcd-1234"},
		{"email", EmailCode, "123456"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			f, store := newFakeVRChat(t), &memoryStore{}
			c := newClient(t, f, store)
			if err := c.Login(tc.user, password); err != nil {
				t.Fatal(err)
			}
			info := c.Info()
			if info.State != NeedsCode || !strings.Contains(strings.Join(info.Methods, ","), tc.method) {
				t.Fatalf("info %+v", info)
			}
			if _, ok := store.snapshot(); ok {
				t.Fatal("unverified login was saved")
			}
			if err := c.Verify(tc.method, "000000"); err == nil || c.Info().State != NeedsCode {
				t.Fatal("wrong code accepted or ended the sign-in")
			}
			if err := c.Verify(tc.method, "12"); err == nil {
				t.Fatal("malformed code sent")
			}
			if err := c.Verify(tc.method, tc.code); err != nil {
				t.Fatal(err)
			}
			saved, _ := store.snapshot()
			if c.Info().State != Ready || !strings.Contains(saved, "tfacookie_1") {
				t.Fatalf("after verification: %+v", c.Info())
			}
		})
	}
}

func TestSecureStorageFailuresNeverKeepPlaintext(t *testing.T) {
	f := newFakeVRChat(t)
	locked := errors.New("org.freedesktop.Secret.Error.IsLocked")

	store := &memoryStore{setErr: locked}
	c := newClient(t, f, store)
	if err := c.Login("plain user", password); err == nil || !strings.Contains(err.Error(), "couldn’t be saved") {
		t.Fatalf("save failure: %v", err)
	}
	if info := c.Info(); info.State != SignedOut || !strings.Contains(info.Message, "unlock") {
		t.Fatalf("signed in without secure storage: %+v", info)
	}

	store = &memoryStore{getErr: locked}
	if info := newClient(t, f, store).Info(); info.State != SignedOut || !strings.Contains(info.Message, "Couldn’t read") {
		t.Fatalf("locked store on load: %+v", info)
	}

	store = &memoryStore{}
	c = signedIn(t, f, store)
	store.mu.Lock()
	store.deleteEr = locked
	store.mu.Unlock()
	if err := c.Logout(); err == nil || c.Info().State != SignedOut || !strings.Contains(c.Info().Message, "couldn’t be removed") {
		t.Fatalf("logout with locked storage: %v %+v", err, c.Info())
	}

	// A prompt nobody answers times out; a save that completes later is undone.
	store = &memoryStore{}
	c = newClient(t, f, store)
	c.storeTimeout = 50 * time.Millisecond
	block := make(chan struct{})
	store.mu.Lock()
	store.blockSet = block
	store.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- c.Login("plain user", password) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("login succeeded while storage was blocked")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked storage froze the login")
	}
	close(block)
	deadline := time.Now().Add(2 * time.Second)
	for {
		store.mu.Lock()
		history := strings.Join(store.history, ",")
		store.mu.Unlock()
		if strings.HasSuffix(history, "set,delete") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("late save was not removed: %s", history)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := store.snapshot(); ok {
		t.Fatal("late save left a login behind")
	}
	if c.Info().State != SignedOut {
		t.Fatal("timed-out save left the desktop signed in")
	}
}

func TestRotatedCookiesAreSavedButNeverAfterLogout(t *testing.T) {
	f, store := newFakeVRChat(t), &memoryStore{}
	c := signedIn(t, f, store)
	if v, err := c.Check(context.Background(), rotateAvatar); err != nil || v != Public {
		t.Fatalf("check %v %v", v, err)
	}
	if saved, _ := store.snapshot(); !strings.Contains(saved, "authcookie_2") {
		t.Fatalf("rotated cookie not saved: %q", saved)
	}
	// A check started before sign-out must not save its rotated cookie after.
	result := make(chan error, 1)
	go func() { _, err := c.Check(context.Background(), slowAvatar); result <- err }()
	for f.avatarCalls.Load() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	if err := c.Logout(); err != nil {
		t.Fatal(err)
	}
	close(f.release)
	if err := <-result; !errors.Is(err, ErrChanged) {
		t.Fatalf("stale check: %v", err)
	}
	if _, ok := store.snapshot(); ok || c.Info().State != SignedOut {
		t.Fatal("rotated cookie was saved after sign-out")
	}
	deadline := time.Now().Add(2 * time.Second)
	for f.logouts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.logouts.Load() == 0 {
		t.Fatal("sign-out did not end the VRChat session")
	}
}

func TestExpiredLoginSignsOutAndKeepsAvatarsRetryable(t *testing.T) {
	f, store := newFakeVRChat(t), &memoryStore{}
	c := signedIn(t, f, store)
	if _, err := c.Check(context.Background(), expiryAvatar); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err %v", err)
	}
	if _, ok := store.snapshot(); ok || c.Info().State != SignedOut || !strings.Contains(c.Info().Message, "expired") {
		t.Fatalf("expired login kept: %+v", c.Info())
	}
	if _, err := c.Check(context.Background(), publicAvatar); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("signed-out check: %v", err)
	}
}

func TestRateLimitWaitsForRetryAfter(t *testing.T) {
	f, store := newFakeVRChat(t), &memoryStore{}
	c := signedIn(t, f, store)
	f.retryAfter = time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
	_, err := c.Check(context.Background(), limitAvatar)
	var limited *retry.Limited
	if !errors.As(err, &limited) || limited.Source != "VRChat (this PC)" || time.Until(limited.Until) < 100*time.Second {
		t.Fatalf("limit %v", err)
	}
	calls := f.avatarCalls.Load()
	if _, err := c.Check(context.Background(), publicAvatar); !errors.As(err, &limited) || f.avatarCalls.Load() != calls {
		t.Fatal("checked VRChat again during its rate limit")
	}
}

func TestCheckVerdicts(t *testing.T) {
	f, store := newFakeVRChat(t), &memoryStore{}
	c := signedIn(t, f, store)
	for id, want := range map[string]Verdict{publicAvatar: Public, privateAvatar: Rejected, wrongAvatar: Rejected, missingAvatar: Rejected, hiddenAvatar: Rejected} {
		if got, err := c.Check(context.Background(), id); err != nil || got != want {
			t.Errorf("%s: %v %v", id, got, err)
		}
	}
	calls := f.avatarCalls.Load()
	if got, err := c.Check(context.Background(), "avtr_not-an-id"); err != nil || got != Rejected || f.avatarCalls.Load() != calls {
		t.Fatal("invalid ID was sent to VRChat")
	}
}

func TestLogoutCancelsSignInInProgress(t *testing.T) {
	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(block)
	c := NewWith(slow.URL, slow.Client(), &memoryStore{}, nil)
	c.Load()
	result := make(chan error, 1)
	go func() { result <- c.Login("plain user", password) }()
	for !c.Info().Busy {
		time.Sleep(5 * time.Millisecond)
	}
	if err := c.Logout(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled sign-in succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sign-out waited for the sign-in to time out")
	}
}
