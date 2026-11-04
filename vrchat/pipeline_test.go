package vrchat_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
	"github.com/coder/websocket"
)

func TestPipeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("authToken") != "synthetic_token" || r.UserAgent() != "Test/1.0" {
			t.Errorf("pipeline request = %s %v", r.URL.Path, r.Header)
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"notification","content":"{}"}`)); err != nil {
			t.Error(err)
			return
		}
		conn.Read(ctx)
	}))
	defer server.Close()
	c, err := vrchat.New(vrchat.Config{UserAgent: "Test/1.0", PipelineURL: "ws" + strings.TrimPrefix(server.URL, "http")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pipe, err := c.DialPipeline(ctx, "synthetic_token")
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	message, err := pipe.Read(ctx)
	if err != nil || string(message) != `{"type":"notification","content":"{}"}` {
		t.Errorf("Read = %s, %v", message, err)
	}
}

func TestPipelineErrors(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		c := client(t, vrchat.Config{}, func(*http.Request) (*http.Response, error) {
			return response(status, "private diagnostic", http.Header{"Retry-After": {"30"}}), nil
		})
		_, err := c.DialPipeline(context.Background(), "synthetic_secret")
		var apiErr *vrchat.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.RetryAfter != "30" {
			t.Fatalf("pipeline error = %v", err)
		}
		if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "synthetic_secret") {
			t.Errorf("pipeline error exposed credentials: %v", err)
		}
	}
	c := client(t, vrchat.Config{}, func(*http.Request) (*http.Response, error) { return nil, errors.New("private transport diagnostic") })
	if _, err := c.DialPipeline(context.Background(), "synthetic_secret"); err == nil || strings.Contains(err.Error(), "synthetic_secret") || strings.Contains(err.Error(), "private") {
		t.Errorf("transport error = %v", err)
	}
	if _, err := c.DialPipeline(context.Background(), "invalid token"); err == nil {
		t.Error("invalid token accepted")
	}
}
