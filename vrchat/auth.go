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
func (c *Client) Login(ctx context.Context, username, password string) (LoginResult, Tokens, error) {
	return Execute(ctx, c, Tokens{}, Login(username, password))
}

// CurrentUser retrieves the authenticated user, including any rotated tokens.
func (c *Client) CurrentUser(ctx context.Context, tokens Tokens) (LoginResult, Tokens, error) {
	return Execute(ctx, c, tokens, GetCurrentUser())
}

// VerifyTwoFactor accepts totp, otp (recovery code), or emailotp. After a
// successful verification, apply the tokens and call CurrentUser.
func (c *Client) VerifyTwoFactor(ctx context.Context, tokens Tokens, method, code string) (TwoFactorVerification, Tokens, error) {
	return Execute(ctx, c, tokens, VerifyTwoFactor(method, code))
}

// Logout ends the VRChat session. The caller should then discard its tokens.
func (c *Client) Logout(ctx context.Context, tokens Tokens) (Success, Tokens, error) {
	return Execute(ctx, c, tokens, Logout())
}

// Login builds an authenticated current-user request using VRChat's credential encoding.
func Login(username, password string) Operation[InlineGetCurrentUserResponse] {
	op := GetCurrentUser()
	op.header = http.Header{"Authorization": {BasicAuth(username, password)}}
	return op
}

// VerifyTwoFactor builds a verification request for a method returned at login.
// The emailOtp challenge uses emailotp in the verification URL.
func VerifyTwoFactor(method, code string) Operation[TwoFactorVerification] {
	if method == "emailOtp" {
		method = "emailotp"
	}
	op := newOperation[TwoFactorVerification](http.MethodPost, "/auth/twofactorauth/"+method+"/verify", nil, nil, TwoFactorAuthCode{Code: code})
	if method != "totp" && method != "otp" && method != "emailotp" {
		op.err = errors.New("unsupported VRChat two-factor method")
	}
	return op
}

// LoginResult contains the current user or a requiresTwoFactorAuth challenge.
type LoginResult = InlineGetCurrentUserResponse

// User is the profile returned by GetUser, including fields VRChat returns for
// the currently authenticated user when it requests its own record.
type User = InlineGetUserResponse

// TwoFactorVerification reports whether VRChat accepted the verification code.
type TwoFactorVerification struct {
	Verified bool `json:"verified"`
}
