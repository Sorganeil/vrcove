package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakePipeline struct{ messages chan string }

func (p fakePipeline) Read(ctx context.Context) ([]byte, error) {
	select {
	case m, ok := <-p.messages:
		if !ok {
			return nil, errors.New("closed")
		}
		return []byte(m), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (p fakePipeline) Close() error { return nil }

func TestLiveNotifications(t *testing.T) {
	h := newConnectedHarness(t)
	session := &Session{Auth: "live-token", Expires: time.Now().Add(time.Hour).UnixMilli()}
	var dialed string
	messages := make(chan string, 4)
	h.srv.dialPipeline = func(_ context.Context, auth string) (pipelineConn, error) {
		dialed = auth
		return fakePipeline{messages}, nil
	}
	messages <- `{"type":"friend-online","content":{"userId":"usr_00000000-0000-0000-0000-000000000009"}}`
	messages <- `{"type":"notification","content":"{\"id\":\"not_0123456789abcdef\",\"type\":\"invite\",\"senderUserId\":\"usr_00000000-0000-0000-0000-000000000002\",\"senderUsername\":\"Mia\",\"receiverUserId\":\"usr_private\",\"message\":\"Join me\",\"details\":\"{\\\"worldId\\\":\\\"wrld_x\\\",\\\"worldName\\\":\\\"Cozy room\\\"}\"}"}`
	close(messages)
	w := h.do(withSession(t, get("/account/live"), session))
	body := w.Body.String()
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/event-stream" || dialed != "live-token" {
		t.Fatalf("live = %d %q dialed %q", w.Code, body, dialed)
	}
	want := `data: {"created_at":"","details":{"worldName":"Cozy room"},"id":"not_0123456789abcdef","message":"Join me","senderUserId":"usr_00000000-0000-0000-0000-000000000002","senderUsername":"Mia","type":"invite"}`
	if !strings.Contains(body, want) || strings.Contains(body, "friend-online") || strings.Contains(body, "usr_private") || strings.Contains(body, "live-token") {
		t.Fatalf("live body = %q", body)
	}

	// A pipeline error, such as a session VRChat rejected, ends the stream.
	messages = make(chan string, 1)
	messages <- `{"err":"authToken doesn't correspond with an active session"}`
	w = h.do(withSession(t, get("/account/live"), session))
	if !strings.Contains(w.Body.String(), "event: ended") {
		t.Fatalf("ended = %q", w.Body)
	}
	if h.srv.live.total != 0 {
		t.Fatal("live stream was not released")
	}
	if got := h.do(get("/account/live")); got.Code != 401 {
		t.Fatalf("live without a VRChat session = %d", got.Code)
	}
}

func TestLiveStreamLimits(t *testing.T) {
	var l liveStreams
	if !l.acquire(1) || !l.acquire(1) || l.acquire(1) {
		t.Fatal("per-account limit not applied")
	}
	if !l.acquire(2) {
		t.Fatal("another account was refused")
	}
	l.release(1)
	if !l.acquire(1) {
		t.Fatal("released stream not counted down")
	}
}
