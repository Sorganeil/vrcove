package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	vrchatAPI = "https://api.vrchat.cloud/api/1"
	// appVersion is reported in the User-Agent; keep it in step with package.json.
	appVersion = "0.1.0"
)

var (
	setCookieToken = regexp.MustCompile(`^(auth|twoFactorAuth)=([^;]*)`)
	tokenValue     = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,512}$`)
	contactEmail   = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
)

func vrcCookie(s *Session) string {
	var parts []string
	if s != nil && s.Auth != "" {
		parts = append(parts, "auth="+s.Auth)
	}
	if s != nil && s.TwoFactorAuth != "" {
		parts = append(parts, "twoFactorAuth="+s.TwoFactorAuth)
	}
	return strings.Join(parts, "; ")
}

// validContact reports whether c is an email address or http(s) URL that can
// sit inside a User-Agent comment: printable ASCII with no parentheses or
// backslashes, which would end or escape the comment.
func validContact(c string) bool {
	if c == "" || len(c) > 200 || strings.ContainsAny(c, "()\\") {
		return false
	}
	for _, r := range c {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	if contactEmail.MatchString(c) {
		return true
	}
	u, err := url.Parse(c)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}

// userAgent identifies VRCove to VRChat as "VRCove/<version> (<contact>)",
// as VRChat asks of API clients. New rejects contacts that would break it.
func (s *Server) userAgent() string {
	if s.cfg.Contact == "" {
		return "VRCove/" + appVersion
	}
	return "VRCove/" + appVersion + " (" + s.cfg.Contact + ")"
}

// vrcRequest is one call to the VRChat API. Header is only used for login.
type vrcRequest struct {
	Method string
	Body   any
	Header http.Header
}

// vrc calls the VRChat API and returns the decoded JSON plus any rotated
// session tokens. Upstream error bodies are never surfaced.
func (s *Server) vrc(ctx context.Context, path string, session *Session, req vrcRequest) (any, sessionUpdates, error) {
	var updates sessionUpdates
	if s.cfg.Contact == "" {
		return nil, updates, fmt.Errorf("VRChat connection needs a configured operator contact")
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if req.Body != nil {
		encoded, err := json.Marshal(req.Body)
		if err != nil {
			return nil, updates, err
		}
		body = bytes.NewReader(encoded)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, method, vrchatAPI+path, body)
	if err != nil {
		return nil, updates, err
	}
	for k, v := range req.Header {
		r.Header[k] = v
	}
	r.Header.Set("User-Agent", s.userAgent())
	r.Header.Set("Accept", "application/json")
	if session != nil && session.Auth != "" {
		r.Header.Set("Cookie", vrcCookie(session))
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.Client.Do(r)
	if err != nil {
		return nil, updates, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := fmt.Sprintf("VRChat request failed (%d).", resp.StatusCode)
		switch resp.StatusCode {
		case 401:
			if session != nil && session.Pending == nil && session.AccountID != 0 {
				return nil, updates, invalidVRChatSession()
			}
			msg = "VRChat could not authenticate this request. Sign in again."
		case 403:
			msg = "VRChat denied this action. The avatar or account may be restricted."
		case 429:
			msg = "VRChat is rate limiting requests. Please wait before trying again."
		}
		return nil, updates, errStatus(resp.StatusCode, msg)
	}
	for _, c := range resp.Header.Values("Set-Cookie") {
		if m := setCookieToken.FindStringSubmatch(c); m != nil && tokenValue.MatchString(m[2]) {
			if m[1] == "auth" {
				updates.Auth = m[2]
			} else {
				updates.TwoFactorAuth = m[2]
			}
		}
	}
	raw, err := readBounded(resp.Body, 20_000_000, "VRChat response too large.")
	if err != nil {
		return nil, updates, err
	}
	var data any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return nil, updates, err
	}
	return data, updates, nil
}

var publicUserFields = []string{"id", "displayName", "bio", "status", "statusDescription",
	"currentAvatarImageUrl", "currentAvatarThumbnailImageUrl", "userIcon", "profilePicOverride",
	"location", "state", "tags"}

// publicUser keeps only the profile fields the frontend shows.
func publicUser(user any) map[string]any {
	out := map[string]any{}
	if m, ok := user.(map[string]any); ok {
		for _, k := range publicUserFields {
			if v, present := m[k]; present {
				out[k] = v
			}
		}
		// VRChat now sends iconUrl/thumbnailUrl in place of the legacy names.
		firstTruthy(out, "userIcon", m["userIcon"], m["iconUrl"])
		firstTruthy(out, "currentAvatarThumbnailImageUrl", m["currentAvatarThumbnailImageUrl"], m["thumbnailUrl"])
	}
	return out
}

func publicUsers(data any) ([]map[string]any, error) {
	list, ok := data.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected VRChat user list")
	}
	out := make([]map[string]any, len(list))
	for i, u := range list {
		out[i] = publicUser(u)
	}
	return out, nil
}

// field reads a string field from a decoded JSON object.
func field(data any, key string) string {
	if m, ok := data.(map[string]any); ok {
		if v, ok := m[key].(string); ok {
			return v
		}
	}
	return ""
}

// encodeURIComponent matches JavaScript's escaping, which VRChat expects
// inside the Basic credentials.
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
