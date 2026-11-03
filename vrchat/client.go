// Package vrchat provides a stateless VRChat API and live pipeline client.
// Callers own credential storage, token rotation, and retry policy.
package vrchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL     = "https://api.vrchat.cloud/api/1"
	DefaultPipelineURL = "wss://pipeline.vrchat.cloud/"
)

var ErrResponseTooLarge = errors.New("VRChat response too large")

// Config identifies the calling application and optionally overrides transport
// settings. UserAgent should include the application's name, version and an
// operator contact, for example "MyApp/1.0 (operator@example.com)".
type Config struct {
	UserAgent   string
	HTTPClient  *http.Client
	BaseURL     string
	PipelineURL string
	// Zero values use 20 seconds and 20,000,000 bytes respectively.
	Timeout          time.Duration
	MaxResponseBytes int64
}

// Client is safe for concurrent use. Credentials are supplied per call, so one
// client can serve multiple accounts without sharing their cookies.
type Client struct {
	httpClient       *http.Client
	userAgent        string
	baseURL          string
	pipelineURL      string
	timeout          time.Duration
	maxResponseBytes int64
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.UserAgent) == "" {
		return nil, errors.New("VRChat client needs a User-Agent")
	}
	for _, r := range cfg.UserAgent {
		if r < ' ' || r > '~' {
			return nil, errors.New("VRChat User-Agent must be printable ASCII")
		}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.PipelineURL == "" {
		cfg.PipelineURL = DefaultPipelineURL
	}
	if !validEndpoint(cfg.BaseURL, "https", "http") {
		return nil, errors.New("invalid VRChat API endpoint")
	}
	if !validEndpoint(cfg.PipelineURL, "wss", "ws") {
		return nil, errors.New("invalid VRChat pipeline endpoint")
	}
	if cfg.Timeout < 0 || cfg.MaxResponseBytes < 0 {
		return nil, errors.New("VRChat timeout and response limit must not be negative")
	}
	// Reading one extra byte detects responses above the configured limit.
	if cfg.MaxResponseBytes == math.MaxInt64 {
		return nil, errors.New("VRChat response limit must leave room for one extra byte")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 20_000_000
	}
	client := http.Client{}
	if cfg.HTTPClient != nil {
		client = *cfg.HTTPClient
	}
	// Never share implicit cookies between accounts or forward credentials to
	// a redirect target. Do not mutate the caller's HTTP client.
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{httpClient: &client, userAgent: cfg.UserAgent,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"), pipelineURL: cfg.PipelineURL,
		timeout: cfg.Timeout, maxResponseBytes: cfg.MaxResponseBytes}, nil
}

func validEndpoint(value, secureScheme, plainScheme string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" &&
		(u.Scheme == secureScheme || u.Scheme == plainScheme)
}

// Request describes an API call. Method defaults to GET; Body is JSON encoded.
// Header can carry Basic authentication for login and is never modified.
type Request struct {
	Method string
	Body   any
	Header http.Header
}

// APIError reports an upstream HTTP failure without exposing its response body.
// RetryAfter is the original Retry-After header; RetryDelay parses it for callers
// implementing cooldowns. Requests are never retried automatically.
type APIError struct {
	StatusCode int
	RetryAfter string
}

func (e *APIError) Error() string { return fmt.Sprintf("VRChat request failed (%d).", e.StatusCode) }

// Do calls a path relative to the API root, e.g. "/users/usr_...?n=50".
// JSON numbers use json.Number. Rotated tokens are returned separately, including
// when a successful HTTP response contains malformed or oversized JSON.
func (c *Client) Do(ctx context.Context, path string, tokens Tokens, req Request) (any, Tokens, error) {
	var updates Tokens
	u, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return nil, updates, errors.New("VRChat API path must start with a single slash")
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
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, updates, err
	}
	r.Header = req.Header.Clone()
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	r.Header.Set("User-Agent", c.userAgent)
	r.Header.Set("Accept", "application/json")
	// Tokens are the sole source of session cookies.
	r.Header.Del("Cookie")
	if cookie := tokens.Cookie(); cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return nil, updates, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, updates, &APIError{StatusCode: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
	}
	updates = rotatedTokens(resp.Header)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusResetContent {
		return nil, updates, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		return nil, updates, err
	}
	if int64(len(raw)) > c.maxResponseBytes {
		return nil, updates, ErrResponseTooLarge
	}
	var data any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return nil, updates, err
	}
	return data, updates, nil
}
