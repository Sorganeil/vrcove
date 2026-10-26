package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const aliceID = "usr_00000000-0000-0000-0000-000000000001"
const bobID = "usr_00000000-0000-0000-0000-000000000002"

func accountFixture(r *http.Request) (*http.Response, error) {
	h := http.Header{"Content-Type": {"application/json"}}
	name := "alice"
	if basic := r.Header.Get("Authorization"); strings.HasPrefix(basic, "Basic ") {
		decoded, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(basic, "Basic "))
		name, _, _ = strings.Cut(string(decoded), ":")
		h.Set("Set-Cookie", "auth=authcookie_"+name+"; Path=/")
	} else if strings.Contains(r.Header.Get("Cookie"), "authcookie_bob") {
		name = "bob"
	}
	id := aliceID
	if name == "bob" {
		id = bobID
	}
	body := fmt.Sprintf(`{"id":%q,"username":%q,"displayName":%q}`, id, name, strings.ToUpper(name[:1])+name[1:])
	return &http.Response{StatusCode: 200, Header: h, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func accountDesktop(t *testing.T) *desktop {
	t.Helper()
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	d.api.Client.Transport = roundTrip(accountFixture)
	return d
}

func signIn(t *testing.T, d *desktop, name string) {
	t.Helper()
	if w := do(d, "POST", "/api/account/login", `{"username":"`+name+`","password":"synthetic passphrase"}`); w.Code != 200 {
		t.Fatalf("login %s=%d %s", name, w.Code, w.Body)
	}
}

type localProfile struct {
	ID      int64  `json:"id"`
	User    string `json:"vrchatId"`
	Active  bool   `json:"active"`
	Expires int64  `json:"sessionExpires"`
}

func profiles(t *testing.T, d *desktop) []localProfile {
	t.Helper()
	w := do(d, "GET", "/api/account/device", "")
	var response struct {
		Links []localProfile `json:"links"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 {
		t.Fatalf("profiles=%d %s", w.Code, w.Body)
	}
	return response.Links
}

func profileID(t *testing.T, d *desktop, user string) int64 {
	t.Helper()
	for _, p := range profiles(t, d) {
		if p.User == user {
			return p.ID
		}
	}
	t.Fatalf("profile missing: %s", user)
	return 0
}

func TestSavedAccountsSwitchRemoveAndReopen(t *testing.T) {
	d := accountDesktop(t)
	signIn(t, d, "alice")
	signIn(t, d, "bob")
	if id, _ := d.user(); id != bobID || len(profiles(t, d)) != 2 {
		t.Fatalf("active=%s", id)
	}
	alice := profileID(t, d, aliceID)
	if w := do(d, "POST", "/api/account/switch", fmt.Sprintf(`{"id":%d}`, alice)); w.Code != 200 {
		t.Fatalf("switch=%d %s", w.Code, w.Body)
	}
	if id, _ := d.user(); id != aliceID {
		t.Fatalf("switch chose %s", id)
	}
	// Simulate a process restart using only the encrypted state and keyring key.
	d.store.path = filepath.Join(t.TempDir(), "state.bin")
	d.store.persistent = true
	if err := d.store.update(func(*state) {}); err != nil {
		t.Fatal(err)
	}
	sealed, err := os.ReadFile(d.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "alice") || strings.Contains(string(sealed), "authcookie") {
		t.Fatal("state contains cleartext credentials")
	}
	gcm, _ := newGCM(d.store.fileKey)
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		t.Fatal(err)
	}
	reopened := accountDesktop(t)
	if err := json.Unmarshal(plain, &reopened.store.data); err != nil {
		t.Fatal(err)
	}
	if id, _ := reopened.user(); id != aliceID || len(profiles(t, reopened)) != 2 {
		t.Fatal("saved accounts or selection lost on restart")
	}
	bob := profileID(t, d, bobID)
	if w := do(d, "POST", "/api/account/device/remove", fmt.Sprintf(`{"id":%d}`, bob)); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if id, _ := d.user(); id != aliceID || len(profiles(t, d)) != 1 {
		t.Fatal("removing inactive account affected active account")
	}
	if w := do(d, "POST", "/api/account/device/remove", fmt.Sprintf(`{"id":%d}`, alice)); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if id, _ := d.user(); id != "" || len(profiles(t, d)) != 0 {
		t.Fatal("removing active account left a session")
	}
}

func TestPendingAndRejectedSwitchLeaveActiveAccountAlone(t *testing.T) {
	d := accountDesktop(t)
	signIn(t, d, "alice")
	signIn(t, d, "bob")
	alice := profileID(t, d, aliceID)
	d.api.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		response, _ := accountFixture(r)
		if strings.Contains(r.Header.Get("Cookie"), "authcookie_alice") {
			response.StatusCode = 401
			response.Body = io.NopCloser(strings.NewReader(`{}`))
		}
		if r.Header.Get("Authorization") != "" {
			response.Body = io.NopCloser(strings.NewReader(`{"requiresTwoFactorAuth":["totp"]}`))
			response.Header.Set("Set-Cookie", "auth=authcookie_pending; Path=/")
		}
		return response, nil
	})
	w := do(d, "POST", "/api/account/switch", fmt.Sprintf(`{"id":%d}`, alice))
	if w.Code != 401 || !strings.Contains(w.Body.String(), "vrchat_switch_reconnect") {
		t.Fatalf("rejected=%d %s", w.Code, w.Body)
	}
	if id, _ := d.user(); id != bobID {
		t.Fatal("failed switch replaced active account")
	}
	for _, p := range profiles(t, d) {
		if p.User == aliceID && p.Expires != 0 {
			t.Fatal("rejected target not marked for reconnect")
		}
	}
	if w := do(d, "POST", "/api/account/login", `{"username":"carol","password":"synthetic passphrase"}`); w.Code != 200 || !strings.Contains(w.Body.String(), "requiresTwoFactorAuth") {
		t.Fatal(w.Body)
	}
	if id, _ := d.user(); id != bobID || len(profiles(t, d)) != 2 {
		t.Fatal("pending sign-in replaced active or created a profile")
	}
	// Completing 2FA for a reconnect must verify the expected user ID.
	d.api.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		response, _ := accountFixture(r)
		if strings.Contains(r.URL.Path, "twofactorauth") {
			response.Body = io.NopCloser(strings.NewReader(`{"verified":true}`))
		}
		return response, nil
	})
	if w := do(d, "POST", "/api/account/2fa", `{"type":"totp","code":"123456"}`); w.Code != 200 {
		t.Fatalf("2fa=%d %s", w.Code, w.Body)
	}
	if id, _ := d.user(); id != aliceID {
		t.Fatalf("verified account not activated: %s", id)
	}
}

func TestLateAccountResponseCannotReplaceNewSelection(t *testing.T) {
	for _, status := range []int{200, 401} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			d := accountDesktop(t)
			signIn(t, d, "alice")
			signIn(t, d, "bob")
			alice := profileID(t, d, aliceID)
			started, release := make(chan struct{}), make(chan struct{})
			d.api.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/1/auth/exists" {
					close(started)
					<-release
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"auth=authcookie_late; Path=/"}}, Request: r, Body: io.NopCloser(strings.NewReader(`{"nameOk":true,"userExists":false}`))}, nil
				}
				return accountFixture(r)
			})
			done := make(chan int, 1)
			go func() { done <- do(d, "GET", "/api/account/username-check?username=late", "").Code }()
			<-started
			if w := do(d, "POST", "/api/account/switch", fmt.Sprintf(`{"id":%d}`, alice)); w.Code != 200 {
				t.Fatalf("switch=%d", w.Code)
			}
			close(release)
			if code := <-done; code != 409 {
				t.Fatalf("stale status=%d", code)
			}
			if id, _ := d.user(); id != aliceID || len(profiles(t, d)) != 2 {
				t.Fatal("late response changed selected account")
			}
		})
	}
}

func TestLegacySingleAccountIsKeptWhenAddingAnother(t *testing.T) {
	d := accountDesktop(t)
	signIn(t, d, "alice")
	d.store.update(func(s *state) {
		for name := range s.Local {
			if name != "atlas-vrc-local" {
				delete(s.Local, name)
			}
		}
	})
	signIn(t, d, "bob")
	if len(profiles(t, d)) != 2 {
		t.Fatal("legacy account lost")
	}
	alice := profileID(t, d, aliceID)
	if w := do(d, "POST", "/api/account/switch", fmt.Sprintf(`{"id":%d}`, alice)); w.Code != 200 {
		t.Fatal(w.Body)
	}
}

func TestAddingAccountKeepsRotatedSessionOfPreviousAccount(t *testing.T) {
	d := accountDesktop(t)
	signIn(t, d, "alice")
	d.api.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		response, _ := accountFixture(r)
		if r.Header.Get("Authorization") == "" {
			response.Header.Set("Set-Cookie", "auth=authcookie_rotatedalice; Path=/")
		}
		return response, nil
	})
	if w := do(d, "GET", "/api/account/me", ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
	var savedName, savedValue string
	for name, value := range d.store.cookies(false) {
		if strings.HasPrefix(name, "atlas-vrc-local-") {
			savedName, savedValue = name, value
		}
	}
	signIn(t, d, "bob")
	if d.store.cookies(false)[savedName] != savedValue {
		t.Fatal("adding another account overwrote a rotated saved session")
	}
	alice := profileID(t, d, aliceID)
	if w := do(d, "POST", "/api/account/switch", fmt.Sprintf(`{"id":%d}`, alice)); w.Code != 200 {
		t.Fatal(w.Body)
	}
}

func TestDeviceAccountLimitAndRelogin(t *testing.T) {
	d := accountDesktop(t)
	d.api.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		basic, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(r.Header.Get("Authorization"), "Basic "))
		name, _, _ := strings.Cut(string(basic), ":")
		n, _ := strconv.Atoi(strings.TrimPrefix(name, "user"))
		id := fmt.Sprintf("usr_00000000-0000-0000-0000-%012d", n)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"auth=authcookie_test; Path=/"}}, Request: r, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"id":%q,"username":%q,"displayName":%q}`, id, name, name)))}, nil
	})
	for i := 1; i <= 10; i++ {
		signIn(t, d, fmt.Sprintf("user%d", i))
	}
	if w := do(d, "POST", "/api/account/login", `{"username":"user11","password":"synthetic passphrase"}`); w.Code != 400 {
		t.Fatalf("11th profile=%d", w.Code)
	}
	if len(profiles(t, d)) != 10 {
		t.Fatal("account limit bypassed")
	}
	signIn(t, d, "user1")
	if len(profiles(t, d)) != 10 {
		t.Fatal("reconnecting duplicated profile")
	}
}
