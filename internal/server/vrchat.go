package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
)

// Keep the User-Agent appVersion in step with package.json.
const appVersion = "0.2.7"

func vrchatTokens(session *Session) vrchat.Tokens {
	if session == nil {
		return vrchat.Tokens{}
	}
	return vrchat.Tokens{Auth: session.Auth, TwoFactorAuth: session.TwoFactorAuth}
}

func (s *Server) userAgent() string {
	if s.cfg.Contact == "" {
		return "VRCove/" + appVersion
	}
	return "VRCove/" + appVersion + " (" + s.cfg.Contact + ")"
}

// Construct from the current transport so embedders can replace Server.Client.
func (s *Server) vrchatClient() (*vrchat.Client, error) {
	return vrchat.New(vrchat.Config{HTTPClient: s.Client, UserAgent: s.userAgent()})
}

// vrc applies VRCove's cooldown, session failure and persistence policies around
// the reusable client. Upstream response bodies never reach browser errors.
func (s *Server) vrc(ctx context.Context, session *Session, call vrchat.Call) (any, sessionUpdates, error) {
	var updates sessionUpdates
	if s.cfg.Contact == "" {
		return nil, updates, fmt.Errorf("VRChat connection needs a configured operator contact")
	}
	key := backoffKey(session)
	if s.backoff.coolingDown(key, s.now()) {
		return nil, updates, rateLimitedError()
	}
	client, err := s.vrchatClient()
	if err != nil {
		return nil, updates, err
	}
	data, updates, err := client.Call(ctx, vrchatTokens(session), call)
	var apiErr *vrchat.APIError
	if errors.As(err, &apiErr) {
		msg := apiErr.Error()
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			if session != nil && session.Pending == nil && session.AccountID != 0 {
				return nil, updates, invalidVRChatSession()
			}
			msg = "VRChat could not authenticate this request. Sign in again."
		case http.StatusForbidden:
			msg = "VRChat denied this action. The avatar or account may be restricted."
		case http.StatusTooManyRequests:
			now, limit := s.now(), maxRetryAfter
			if s.cfg.Local {
				limit = 24 * time.Hour
			}
			s.backoff.limited(key, apiErr.RetryDelay(now, limit), now)
			return nil, updates, rateLimitedError()
		}
		return nil, updates, errStatus(apiErr.StatusCode, msg)
	}
	if errors.Is(err, vrchat.ErrResponseTooLarge) {
		return nil, updates, errStatus(http.StatusRequestEntityTooLarge, "VRChat response too large.")
	}
	return data, updates, err
}

var publicUserFields = []string{"id", "displayName", "bio", "status", "statusDescription",
	"currentAvatarImageUrl", "currentAvatarThumbnailImageUrl", "userIcon", "profilePicOverride",
	"location", "state", "platform", "tags"}

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
