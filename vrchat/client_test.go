package vrchat_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string, header http.Header) *http.Response {
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func client(t *testing.T, cfg vrchat.Config, upstream transport) *vrchat.Client {
	t.Helper()
	cfg.UserAgent = "SDKTest/1.0 (test@example.com)"
	cfg.HTTPClient = &http.Client{Transport: upstream}
	c, err := vrchat.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRequestAndTokenRotation(t *testing.T) {
	header := http.Header{"X-Test": {"kept"}, "Cookie": {"auth=ignored"}, "Accept": {"ignored"}}
	original := header.Clone()
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
		if r.Method != "PUT" || r.URL.String() != vrchat.DefaultBaseURL+"/users/usr_test?x=one%20two" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Cookie") != "auth=old; twoFactorAuth=tfa" || r.Header.Get("User-Agent") != "SDKTest/1.0 (test@example.com)" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-Test") != "kept" {
			t.Errorf("headers = %v", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"status":"active"}` {
			t.Errorf("body = %s", body)
		}
		return response(200, `{"id":"usr_test","number":9007199254740993}`, http.Header{"Set-Cookie": {"auth=rotated; Path=/", "twoFactorAuth=invalid value; Path=/", "other=ignored; Path=/"}}), nil
	})
	tokens := vrchat.Tokens{Auth: "old", TwoFactorAuth: "tfa"}
	data, updates, err := c.Do(context.Background(), "/users/usr_test?x=one%20two", tokens, vrchat.Request{Method: "PUT", Body: map[string]string{"status": "active"}, Header: header})
	if err != nil {
		t.Fatal(err)
	}
	if data.(map[string]any)["number"] != json.Number("9007199254740993") {
		t.Errorf("JSON number lost precision: %v", data)
	}
	if updates != (vrchat.Tokens{Auth: "rotated"}) {
		t.Errorf("updates = %+v", updates)
	}
	tokens.Apply(updates)
	if tokens.Auth != "rotated" || tokens.TwoFactorAuth != "tfa" {
		t.Errorf("merged tokens = %+v", tokens)
	}
	if !reflect.DeepEqual(header, original) {
		t.Error("caller headers were mutated")
	}
}

func TestAuthenticationFlow(t *testing.T) {
	var step int
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
		step++
		switch step {
		case 1:
			want := "Basic " + base64.StdEncoding.EncodeToString([]byte("a%3Ab%20%C3%A9:p%2B%2F%3F"))
			if r.Method != "GET" || r.URL.Path != "/api/1/auth/user" || r.Header.Get("Authorization") != want || r.Header.Get("Cookie") != "" {
				t.Errorf("login = %s %s %v", r.Method, r.URL, r.Header)
			}
			return response(200, `{"requiresTwoFactorAuth":["totp"]}`, http.Header{"Set-Cookie": {"auth=pending; Path=/"}}), nil
		case 2:
			body, _ := io.ReadAll(r.Body)
			if r.Method != "POST" || r.URL.Path != "/api/1/auth/twofactorauth/totp/verify" || string(body) != `{"code":"123456"}` || r.Header.Get("Cookie") != "auth=pending" {
				t.Errorf("verify = %s %s %s", r.Method, r.URL, body)
			}
			return response(200, `{"verified":true}`, http.Header{"Set-Cookie": {"twoFactorAuth=verified; Path=/"}}), nil
		case 3:
			if r.Method != "GET" || r.URL.Path != "/api/1/auth/user" || r.Header.Get("Cookie") != "auth=pending; twoFactorAuth=verified" {
				t.Errorf("current user = %s %s %v", r.Method, r.URL, r.Header)
			}
			return response(200, `{"id":"usr_test"}`, nil), nil
		case 4:
			if r.Method != "PUT" || r.URL.Path != "/api/1/logout" {
				t.Errorf("logout = %s %s", r.Method, r.URL)
			}
			return response(204, "", nil), nil
		default:
			t.Fatal("unexpected upstream call")
			return nil, nil
		}
	})
	ctx := context.Background()
	_, tokens, err := c.Login(ctx, "a:b é", "p+/?")
	if err != nil {
		t.Fatal(err)
	}
	_, updates, err := c.VerifyTwoFactor(ctx, tokens, "totp", "123456")
	if err != nil {
		t.Fatal(err)
	}
	tokens.Apply(updates)
	if _, _, err := c.CurrentUser(ctx, tokens); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Logout(ctx, tokens); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.VerifyTwoFactor(ctx, tokens, "../other", "code"); err == nil {
		t.Error("unsupported 2FA method accepted")
	}
	if step != 4 {
		t.Errorf("upstream calls = %d", step)
	}
}

func TestErrorsAndResponseBounds(t *testing.T) {
	for _, status := range []int{301, 400, 401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c := client(t, vrchat.Config{}, func(*http.Request) (*http.Response, error) {
				calls++
				return response(status, "private upstream diagnostic", http.Header{"Retry-After": {"120"}, "Location": {"https://other.example"}, "Set-Cookie": {"auth=rejected"}}), nil
			})
			_, updates, err := c.CurrentUser(context.Background(), vrchat.Tokens{})
			var apiErr *vrchat.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.RetryDelay(time.Now(), 10*time.Minute) != 2*time.Minute {
				t.Fatalf("error = %v", err)
			}
			if calls != 1 || updates != (vrchat.Tokens{}) || strings.Contains(err.Error(), "private") {
				t.Errorf("calls=%d updates=%+v error=%v", calls, updates, err)
			}
		})
	}
	for _, body := range []string{`{"too":"large"}`, "{", ""} {
		c := client(t, vrchat.Config{MaxResponseBytes: 8}, func(*http.Request) (*http.Response, error) {
			return response(200, body, http.Header{"Set-Cookie": {"auth=rotated"}}), nil
		})
		_, updates, err := c.CurrentUser(context.Background(), vrchat.Tokens{})
		if err == nil || updates.Auth != "rotated" {
			t.Errorf("body %q: updates=%+v error=%v", body, updates, err)
		}
		if len(body) > 8 && !errors.Is(err, vrchat.ErrResponseTooLarge) {
			t.Errorf("size error = %v", err)
		}
	}
}

func TestClientIsolation(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(vrchat.DefaultBaseURL)
	jar.SetCookies(u, []*http.Cookie{{Name: "auth", Value: "from_jar"}})
	shared := &http.Client{Jar: jar, Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" {
			t.Errorf("implicit cookies leaked: %s", r.Header.Get("Cookie"))
		}
		return response(200, `{}`, http.Header{"Set-Cookie": {"auth=not_saved; Path=/"}}), nil
	})}
	c, err := vrchat.New(vrchat.Config{UserAgent: "Test/1.0", HTTPClient: shared})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.CurrentUser(context.Background(), vrchat.Tokens{}); err != nil {
		t.Fatal(err)
	}
	if shared.Jar != jar || shared.CheckRedirect != nil || jar.Cookies(u)[0].Value != "from_jar" {
		t.Error("caller HTTP client was mutated")
	}
}

func TestValidationAndCancellation(t *testing.T) {
	for _, cfg := range []vrchat.Config{
		{}, {UserAgent: "x\r\ny"}, {UserAgent: "x", BaseURL: "https://api.example?query=1"},
		{UserAgent: "x", BaseURL: "ftp://api.example"}, {UserAgent: "x", PipelineURL: "https://pipeline.example"},
		{UserAgent: "x", Timeout: -1}, {UserAgent: "x", MaxResponseBytes: -1},
		{UserAgent: "x", MaxResponseBytes: math.MaxInt64},
		{UserAgent: "x", BaseURL: "https://api.example?"},
		{UserAgent: "x", PipelineURL: "wss://pipeline.example?"},
	} {
		if _, err := vrchat.New(cfg); err == nil {
			t.Errorf("invalid configuration accepted: %+v", cfg)
		}
	}
	var calls int
	c := client(t, vrchat.Config{Timeout: 5 * time.Millisecond}, func(r *http.Request) (*http.Response, error) {
		calls++
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	for _, path := range []string{"https://other.example/path", "//other.example/path", "relative", "/path#fragment"} {
		if _, _, err := c.Do(context.Background(), path, vrchat.Tokens{}, vrchat.Request{}); err == nil {
			t.Errorf("invalid path accepted: %q", path)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid paths reached the upstream transport %d times", calls)
	}
	if _, _, err := c.CurrentUser(context.Background(), vrchat.Tokens{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.CurrentUser(ctx, vrchat.Tokens{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation = %v", err)
	}
}

func TestContactTokensAndRetryAfter(t *testing.T) {
	for value, valid := range map[string]bool{
		"operator@example.com": true, "https://example.com/contact": true,
		"": false, "name@example.com)": false, "operator\n@example.com": false,
		"ftp://example.com": false, "https://user:password@example.com": false,
	} {
		if vrchat.ValidContact(value) != valid {
			t.Errorf("ValidContact(%q) != %v", value, valid)
		}
	}
	if got := (vrchat.Tokens{Auth: "safe", TwoFactorAuth: "bad; cookie=injected"}).Cookie(); got != "auth=safe" {
		t.Errorf("Cookie = %q", got)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for value, want := range map[string]time.Duration{
		"": time.Minute, "soon": time.Minute, "30": 30 * time.Second, "0": time.Second,
		"999999": 10 * time.Minute, "Thu, 01 Jan 2026 00:02:00 GMT": 2 * time.Minute,
		"Wed, 31 Dec 2025 23:59:00 GMT": time.Second,
	} {
		if got := vrchat.RetryDelay(value, now, 10*time.Minute); got != want {
			t.Errorf("RetryDelay(%q) = %v, want %v", value, got, want)
		}
	}
	if got := vrchat.RetryDelay("3600", now, 24*time.Hour); got != time.Hour {
		t.Errorf("desktop RetryDelay = %v", got)
	}
}
