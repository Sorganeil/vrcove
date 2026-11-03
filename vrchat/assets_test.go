package vrchat_test

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
)

func TestImageBytesAndCookieScope(t *testing.T) {
	tokens := vrchat.Tokens{Auth: "synthetic_auth", TwoFactorAuth: "synthetic_tfa"}
	cases := []struct {
		url           string
		authenticated bool
	}{
		{vrchat.DefaultBaseURL + "/image/file_one/1/256", true},
		{"https://api.vrchat.cloud:443/api/1/file/file_one/1/file", true},
		{"https://assets.vrchat.com/image.png", false},
		{"https://files.vrchat.cloud/file_one?signature=private", false},
		{"https://api.vrchat.cloud.evil.example/api/1/file/file_one/1", false},
		{"https://api.vrchat.cloud:8443/api/1/file/file_one/1", false},
		{"https://api.vrchat.cloud/outside-api/image", false},
		{"https://api.vrchat.cloud/api/10/file/file_one/1", false},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.Header.Get("Accept") != "image/*" || r.UserAgent() != "SDKTest/1.0 (test@example.com)" {
					t.Errorf("request=%s %v", r.Method, r.Header)
				}
				if got := r.Header.Get("Cookie"); (got != "") != tc.authenticated {
					t.Errorf("cookie=%q", got)
				}
				return response(200, "\x00\xffbinary", http.Header{"Content-Type": {"image/png"}, "Set-Cookie": {"auth=rotated"}}), nil
			})
			result, updates, err := c.FetchImage(context.Background(), tokens, tc.url, vrchat.DownloadOptions{})
			if err != nil || string(result.Body) != "\x00\xffbinary" || result.ContentType != "image/png" || result.Authenticated != tc.authenticated {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if tc.authenticated && updates.Auth != "rotated" || !tc.authenticated && updates != (vrchat.Tokens{}) {
				t.Errorf("updates=%+v", updates)
			}
		})
	}
}

func TestAssetRedirectDoesNotForwardCredentials(t *testing.T) {
	calls := 0
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if r.Header.Get("Cookie") != "auth=synthetic_auth" {
				t.Error("API request omitted its session")
			}
			return response(302, "ignored", http.Header{"Location": {"https://files.vrchat.cloud/image.png?signature=private"}}), nil
		}
		if r.Header.Get("Cookie") != "" {
			t.Error("CDN request carried credentials")
		}
		return response(200, "image", http.Header{"Content-Type": {"image/png"}}), nil
	})
	ctx := context.Background()
	first, _, err := c.FetchImage(ctx, vrchat.Tokens{Auth: "synthetic_auth"}, vrchat.DefaultBaseURL+"/file/file_one/1/file", vrchat.DownloadOptions{})
	if err != nil || first.RedirectURL == nil || calls != 1 || len(first.Body) != 0 {
		t.Fatalf("redirect=%+v calls=%d error=%v", first, calls, err)
	}
	next, _, err := c.FetchImage(ctx, vrchat.Tokens{Auth: "synthetic_auth"}, first.RedirectURL.String(), vrchat.DownloadOptions{})
	if err != nil || string(next.Body) != "image" || next.Authenticated || calls != 2 {
		t.Fatalf("next=%+v calls=%d error=%v", next, calls, err)
	}
}

func TestRelativeAssetRedirect(t *testing.T) {
	c := client(t, vrchat.Config{}, func(*http.Request) (*http.Response, error) {
		return response(302, "", http.Header{"Location": {"../next.png?token=private"}}), nil
	})
	result, _, err := c.FetchImage(context.Background(), vrchat.Tokens{}, "https://assets.vrchat.com/a/image.png", vrchat.DownloadOptions{})
	if err != nil || result.RedirectURL == nil || result.RedirectURL.String() != "https://assets.vrchat.com/next.png?token=private" {
		t.Fatalf("redirect=%+v error=%v", result, err)
	}
}

func TestFileVersionBinaryContract(t *testing.T) {
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/api/1/file/file_one/2" || r.Header.Get("Accept") != "application/octet-stream" || r.Header.Get("Cookie") != "auth=synthetic_auth" {
			t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
		}
		return response(200, "\x00\xfffile", http.Header{"Content-Type": {"application/octet-stream"}}), nil
	})
	result, _, err := c.DownloadFileVersion(context.Background(), vrchat.Tokens{Auth: "synthetic_auth"}, "file_one", 2, vrchat.DownloadOptions{})
	if err != nil || string(result.Body) != "\x00\xfffile" {
		t.Fatalf("file=%+v error=%v", result, err)
	}
}

type trackedAssetBody struct {
	io.Reader
	closed *bool
}

func (b trackedAssetBody) Close() error { *b.closed = true; return nil }

func TestAssetResponseBoundsAndClose(t *testing.T) {
	for _, length := range []int64{-1, 100} {
		closed := false
		c := client(t, vrchat.Config{}, func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, ContentLength: length, Body: trackedAssetBody{Reader: strings.NewReader("oversized"), closed: &closed}}, nil
		})
		_, _, err := c.FetchImage(context.Background(), vrchat.Tokens{}, "https://assets.vrchat.com/image.png", vrchat.DownloadOptions{MaxBytes: 4})
		if !errors.Is(err, vrchat.ErrResponseTooLarge) || !closed {
			t.Errorf("length=%d error=%v closed=%v", length, err, closed)
		}
	}
}

func TestAssetFailuresDoNotExposeBodyOrSignedURL(t *testing.T) {
	for _, status := range []int{401, 404, 429, 503} {
		c := client(t, vrchat.Config{}, func(*http.Request) (*http.Response, error) {
			return response(status, "private upstream diagnostic", http.Header{"Retry-After": {"120"}}), nil
		})
		result, _, err := c.FetchImage(context.Background(), vrchat.Tokens{}, "https://files.vrchat.cloud/image?signature=private", vrchat.DownloadOptions{})
		var apiErr *vrchat.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.RetryAfter != "120" || len(result.Body) != 0 {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		if strings.Contains(err.Error(), "private") {
			t.Errorf("error leaked response details: %v", err)
		}
	}
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) { return nil, errors.New(r.URL.String()) })
	_, _, err := c.FetchImage(context.Background(), vrchat.Tokens{}, "https://files.vrchat.cloud/image?signature=private", vrchat.DownloadOptions{})
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("signed URL error=%v", err)
	}
}

func TestAssetValidationAndTimeout(t *testing.T) {
	calls := 0
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
		calls++
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	for _, target := range []string{"http://assets.vrchat.com/image.png", "https://user:pass@assets.vrchat.com/image.png", "https://assets.vrchat.com/image.png#fragment", "//assets.vrchat.com/image.png"} {
		if _, _, err := c.FetchImage(context.Background(), vrchat.Tokens{}, target, vrchat.DownloadOptions{}); !errors.Is(err, vrchat.ErrInvalidAssetURL) {
			t.Errorf("target=%q error=%v", target, err)
		}
	}
	for _, options := range []vrchat.DownloadOptions{{MaxBytes: -1}, {MaxBytes: math.MaxInt64}, {Timeout: -1}} {
		if _, _, err := c.FetchImage(context.Background(), vrchat.Tokens{}, "https://assets.vrchat.com/image.png", options); err == nil {
			t.Errorf("accepted limits=%+v", options)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid calls reached upstream %d times", calls)
	}
	if _, _, err := c.FetchImage(context.Background(), vrchat.Tokens{}, "https://assets.vrchat.com/image.png", vrchat.DownloadOptions{Timeout: time.Millisecond}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.FetchImage(ctx, vrchat.Tokens{}, "https://assets.vrchat.com/image.png", vrchat.DownloadOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation=%v", err)
	}
	if c.IsAPIURL(nil) {
		t.Error("nil URL considered API")
	}
	other, _ := url.Parse("https://api.vrchat.cloud/api/10")
	if c.IsAPIURL(other) {
		t.Error("API prefix accepted outside API root")
	}
}
