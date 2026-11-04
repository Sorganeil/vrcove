package vrchat

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidAssetURL = errors.New("invalid VRChat asset URL")
	ErrInvalidRedirect = errors.New("invalid VRChat asset redirect")
)

// DownloadOptions bounds one asset request. Zero values use the client's API
// timeout and response limit. Asset redirects are returned without following them.
type DownloadOptions struct {
	Timeout  time.Duration
	MaxBytes int64
}

// AssetResponse is a bounded binary response or a redirect. ContentType is the
// upstream value, so callers must check it against the formats they accept.
// Authenticated reports whether this request carried VRChat cookies. Never cache
// the separately returned token rotations with a shared asset response.
type AssetResponse struct {
	StatusCode    int
	ContentType   string
	ContentLength int64
	Body          []byte
	RedirectURL   *url.URL
	Authenticated bool
}

// IsAPIURL reports whether target belongs to the configured API origin and path.
// Cookies are sent only to that scope, never to a CDN or another asset host.
func (c *Client) IsAPIURL(target *url.URL) bool {
	if target == nil || target.User != nil {
		return false
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return false
	}
	root := strings.TrimRight(base.EscapedPath(), "/")
	return strings.EqualFold(target.Scheme, base.Scheme) && strings.EqualFold(target.Hostname(), base.Hostname()) &&
		effectivePort(target) == effectivePort(base) && (target.EscapedPath() == root || strings.HasPrefix(target.EscapedPath(), root+"/"))
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

// FetchImage requests an image URL supplied by VRChat, including API file/image
// paths and signed CDN URLs. The caller must authorize the target and every
// returned redirect before making another request. No redirect is followed here.
func (c *Client) FetchImage(ctx context.Context, tokens Tokens, target string, options DownloadOptions) (AssetResponse, Tokens, error) {
	return c.fetchAsset(ctx, tokens, target, "image/*", options)
}

// DownloadFileVersion requests the bytes of a file version. A redirect is returned
// in RedirectURL so the caller can authorize the next host before downloading it.
func (c *Client) DownloadFileVersion(ctx context.Context, tokens Tokens, fileID string, versionID int, options DownloadOptions) (AssetResponse, Tokens, error) {
	op := newOperation[AssetResponse](http.MethodGet, "/file/{fileId}/{versionId}", []pathParameter{{"fileId", fileID}, {"versionId", strconv.Itoa(versionID)}}, nil, nil)
	path, _, err := op.build()
	if err != nil {
		return AssetResponse{}, Tokens{}, err
	}
	return c.fetchAsset(ctx, tokens, c.baseURL+path, "application/octet-stream", options)
}

type assetRequestError struct{ cause error }

func (e *assetRequestError) Error() string { return "VRChat asset request failed" }
func (e *assetRequestError) Unwrap() error { return e.cause }

func (c *Client) fetchAsset(ctx context.Context, tokens Tokens, target, accept string, options DownloadOptions) (AssetResponse, Tokens, error) {
	var result AssetResponse
	var updates Tokens
	u, err := url.Parse(target)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" ||
		(!strings.EqualFold(u.Scheme, "https") && !(strings.EqualFold(u.Scheme, "http") && c.IsAPIURL(u))) {
		return result, updates, ErrInvalidAssetURL
	}
	if options.MaxBytes < 0 || options.MaxBytes == math.MaxInt64 || options.Timeout < 0 {
		return result, updates, errors.New("invalid VRChat download limits")
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = c.maxResponseBytes
	}
	if options.Timeout == 0 {
		options.Timeout = c.timeout
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, updates, ErrInvalidAssetURL
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", accept)
	if c.IsAPIURL(u) {
		if cookie := tokens.Cookie(); cookie != "" {
			req.Header.Set("Cookie", cookie)
			result.Authenticated = true
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, updates, &assetRequestError{cause: err}
	}
	defer resp.Body.Close()
	result.StatusCode, result.ContentType, result.ContentLength = resp.StatusCode, resp.Header.Get("Content-Type"), resp.ContentLength
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		next, err := u.Parse(location)
		if err != nil || location == "" {
			return result, updates, ErrInvalidRedirect
		}
		result.RedirectURL = next
		return result, updates, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return result, updates, &APIError{StatusCode: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
	}
	if c.IsAPIURL(u) {
		updates = rotatedTokens(resp.Header)
	}
	if resp.ContentLength > options.MaxBytes {
		return result, updates, ErrResponseTooLarge
	}
	result.Body, err = io.ReadAll(io.LimitReader(resp.Body, options.MaxBytes+1))
	if err != nil {
		return result, updates, &assetRequestError{cause: err}
	}
	if int64(len(result.Body)) > options.MaxBytes {
		result.Body = nil
		return result, updates, ErrResponseTooLarge
	}
	return result, updates, nil
}
