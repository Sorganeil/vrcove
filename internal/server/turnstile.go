package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// verifyTurnstile checks a registration challenge token with Cloudflare. It
// passes when Turnstile is not configured. A Cloudflare outage returns an
// error rather than letting signups through unchecked.
func (s *Server) verifyTurnstile(r *http.Request, token string) (bool, error) {
	if s.cfg.TurnstileSecret == "" {
		return true, nil
	}
	if token == "" || len(token) > 2048 {
		return false, nil
	}
	form := url.Values{"secret": {s.cfg.TurnstileSecret}, "response": {token}}
	if ip := clientIP(r); ip != "local" {
		form.Set("remoteip", ip)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.Client.Do(req)
	if err != nil {
		return false, errStatus(http.StatusBadGateway, "Verification is unavailable. Try again shortly.")
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body, 64<<10, "Verification response too large.")
	if err != nil {
		return false, err
	}
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &result) != nil {
		return false, errStatus(http.StatusBadGateway, "Verification is unavailable. Try again shortly.")
	}
	if !result.Success || result.Action != "signup" {
		return false, nil
	}
	if origin, err := url.Parse(s.cfg.PublicOrigin); err == nil && result.Hostname != "" && !strings.EqualFold(result.Hostname, origin.Hostname()) {
		return false, nil
	}
	return true, nil
}
