package vrchat

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	setCookieToken = regexp.MustCompile(`^(auth|twoFactorAuth)=([^;]*)`)
	tokenValue     = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,512}$`)
	contactEmail   = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
)

// Tokens contains VRChat's session cookies. The client does not persist them.
type Tokens struct{ Auth, TwoFactorAuth string }

// Cookie returns only valid token values, so untrusted tokens cannot inject
// additional cookies or headers.
func (t Tokens) Cookie() string {
	var parts []string
	if tokenValue.MatchString(t.Auth) {
		parts = append(parts, "auth="+t.Auth)
	}
	if tokenValue.MatchString(t.TwoFactorAuth) {
		parts = append(parts, "twoFactorAuth="+t.TwoFactorAuth)
	}
	return strings.Join(parts, "; ")
}

// Apply merges nonempty token rotations into the current credentials.
func (t *Tokens) Apply(updates Tokens) {
	if updates.Auth != "" {
		t.Auth = updates.Auth
	}
	if updates.TwoFactorAuth != "" {
		t.TwoFactorAuth = updates.TwoFactorAuth
	}
}

func rotatedTokens(header http.Header) Tokens {
	var updates Tokens
	for _, value := range header.Values("Set-Cookie") {
		if m := setCookieToken.FindStringSubmatch(value); m != nil && tokenValue.MatchString(m[2]) {
			if m[1] == "auth" {
				updates.Auth = m[2]
			} else {
				updates.TwoFactorAuth = m[2]
			}
		}
	}
	return updates
}

// ValidContact reports whether contact is an email address or HTTP(S) URL safe
// to place inside a User-Agent comment.
func ValidContact(contact string) bool {
	if contact == "" || len(contact) > 200 || strings.ContainsAny(contact, "()\\") {
		return false
	}
	for _, r := range contact {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	if contactEmail.MatchString(contact) {
		return true
	}
	u, err := url.Parse(contact)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}

// BasicAuth percent-encodes each credential as VRChat expects before Base64.
func BasicAuth(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(encodeURIComponent(username)+":"+encodeURIComponent(password)))
}

func encodeURIComponent(s string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// Login returns the current user or a requiresTwoFactorAuth challenge. Apply
// the returned tokens even when a two-factor challenge is still outstanding.
func (c *Client) Login(ctx context.Context, username, password string) (any, Tokens, error) {
	return c.Do(ctx, "/auth/user", Tokens{}, Request{Header: http.Header{"Authorization": {BasicAuth(username, password)}}})
}

// CurrentUser retrieves the authenticated user, including any rotated tokens.
func (c *Client) CurrentUser(ctx context.Context, tokens Tokens) (any, Tokens, error) {
	return c.Do(ctx, "/auth/user", tokens, Request{})
}

// VerifyTwoFactor accepts totp, otp (recovery code), or emailotp. After a
// successful verification, apply the tokens and call CurrentUser.
func (c *Client) VerifyTwoFactor(ctx context.Context, tokens Tokens, method, code string) (any, Tokens, error) {
	if method != "totp" && method != "otp" && method != "emailotp" {
		return nil, Tokens{}, errors.New("unsupported VRChat two-factor method")
	}
	return c.Do(ctx, "/auth/twofactorauth/"+method+"/verify", tokens, Request{Method: http.MethodPost, Body: map[string]string{"code": code}})
}

// Logout ends the VRChat session. The caller should then discard its tokens.
func (c *Client) Logout(ctx context.Context, tokens Tokens) (any, Tokens, error) {
	return c.Do(ctx, "/logout", tokens, Request{Method: http.MethodPut})
}
