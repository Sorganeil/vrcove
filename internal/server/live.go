package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Live notifications: while a browser keeps /account/live open, the server
// connects to VRChat's pipeline with that browser's session and forwards new
// notifications as server-sent events. The VRChat token stays in memory for
// the length of the connection only; nothing is written to disk. The relay
// ends when the browser leaves, the session is ended or expires, or after an
// hour, when the browser reconnects with its cookie checked again.

const (
	vrchatPipeline    = "wss://pipeline.vrchat.cloud/"
	liveHeartbeat     = 25 * time.Second
	liveRecheck       = 5 * time.Minute
	liveLifetime      = time.Hour
	maxLivePerAccount = 2
	maxLiveStreams    = 500
	// liveConnects caps how often one account may open the pipeline in the
	// limiter's window, so a reconnecting browser cannot hammer VRChat.
	liveConnects = 30
)

// Pipeline is one open connection to VRChat's live feed.
type Pipeline interface {
	Read(ctx context.Context) ([]byte, error)
	Close() error
}

// liveStreams counts open relays, in total and per VRCove account.
type liveStreams struct {
	mu         sync.Mutex
	total      int
	perAccount map[int64]int
}

func (l *liveStreams) acquire(account int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.perAccount == nil {
		l.perAccount = map[int64]int{}
	}
	if l.total >= maxLiveStreams || l.perAccount[account] >= maxLivePerAccount {
		return false
	}
	l.total++
	l.perAccount[account]++
	return true
}

func (l *liveStreams) release(account int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total--
	if l.perAccount[account]--; l.perAccount[account] <= 0 {
		delete(l.perAccount, account)
	}
}

type wsPipeline struct{ c *websocket.Conn }

func (p wsPipeline) Read(ctx context.Context) ([]byte, error) {
	_, b, err := p.c.Read(ctx)
	return b, err
}
func (p wsPipeline) Close() error { return p.c.CloseNow() }

func (s *Server) dialVRChatPipeline(ctx context.Context, auth string) (Pipeline, error) {
	c, resp, err := websocket.Dial(ctx, vrchatPipeline+"?authToken="+url.QueryEscape(auth), &websocket.DialOptions{
		HTTPClient: s.Client,
		HTTPHeader: http.Header{"User-Agent": {s.userAgent()}},
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			return nil, rateLimitedError()
		}
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return nil, invalidVRChatSession()
		}
		return nil, errStatus(http.StatusBadGateway, "VRChat live updates are unavailable right now.")
	}
	c.SetReadLimit(1 << 20)
	return wsPipeline{c}, nil
}

// liveNotification picks the fields VRCove shows from one pipeline message,
// or returns nil for messages that are not new notifications. ended reports a
// pipeline error, such as a session VRChat no longer accepts.
func liveNotification(message []byte) (out map[string]any, ended bool) {
	var envelope struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
		Err     string          `json:"err"`
	}
	if json.Unmarshal(message, &envelope) != nil {
		return nil, false
	}
	if envelope.Err != "" {
		return nil, true
	}
	if envelope.Type != "notification" {
		return nil, false
	}
	content := []byte(envelope.Content)
	// VRChat sends the notification as a JSON string holding JSON.
	var inner string
	if json.Unmarshal(content, &inner) == nil {
		content = []byte(inner)
	}
	var n struct {
		ID             string          `json:"id"`
		Type           string          `json:"type"`
		SenderUserID   string          `json:"senderUserId"`
		SenderUsername string          `json:"senderUsername"`
		Message        string          `json:"message"`
		Details        json.RawMessage `json:"details"`
		Created        string          `json:"created_at"`
	}
	if json.Unmarshal(content, &n) != nil || !notificationID.MatchString(n.ID) {
		return nil, false
	}
	out = map[string]any{"id": n.ID, "type": n.Type, "senderUsername": n.SenderUsername, "message": n.Message, "created_at": n.Created}
	if userID.MatchString(n.SenderUserID) {
		out["senderUserId"] = n.SenderUserID
	}
	details := []byte(n.Details)
	if json.Unmarshal(details, &inner) == nil {
		details = []byte(inner)
	}
	var d struct {
		WorldName string `json:"worldName"`
	}
	if json.Unmarshal(details, &d) == nil && d.WorldName != "" {
		out["details"] = map[string]string{"worldName": d.WorldName}
	}
	return out, false
}

// liveNotifications streams new VRChat notifications until the browser leaves.
func (s *Server) liveNotifications(w http.ResponseWriter, r *http.Request, session *Session) error {
	now := s.now()
	key := backoffKey(session)
	if s.backoff.coolingDown(key, now) {
		return rateLimitedError()
	}
	if wait := s.limiter.allowN("live:"+strconv.FormatInt(session.AccountID, 10), liveConnects, now); wait > 0 {
		return rateLimitedError()
	}
	if !s.live.acquire(session.AccountID) {
		return errStatus(http.StatusTooManyRequests, "Live updates are already open in other VRCove windows.")
	}
	defer s.live.release(session.AccountID)
	end := now.Add(liveLifetime)
	if expires := time.UnixMilli(session.Expires); expires.Before(end) {
		end = expires
	}
	ctx, cancel := context.WithDeadline(r.Context(), end)
	defer cancel()
	dialCtx, stop := context.WithTimeout(ctx, 15*time.Second)
	pipe, err := s.dialPipeline(dialCtx, session.Auth)
	stop()
	if err != nil {
		return err
	}
	defer pipe.Close()

	rc := http.NewResponseController(w)
	// The server's read and write timeouts suit ordinary requests; this one stays open.
	rc.SetReadDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(chunk string) bool {
		rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if _, err := io.WriteString(w, chunk); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("retry: 10000\n\n") {
		return nil
	}

	messages := make(chan []byte)
	go func() {
		defer close(messages)
		for {
			b, err := pipe.Read(ctx)
			if err != nil {
				return
			}
			select {
			case messages <- b:
			case <-ctx.Done():
				return
			}
		}
	}()
	beat, recheck := time.NewTicker(liveHeartbeat), time.NewTicker(liveRecheck)
	defer beat.Stop()
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case b, ok := <-messages:
			if !ok {
				return nil
			}
			n, ended := liveNotification(b)
			if ended {
				send("event: ended\ndata: {}\n\n")
				return nil
			}
			if n == nil {
				continue
			}
			encoded, err := json.Marshal(n)
			if err != nil || !send("data: "+string(encoded)+"\n\n") {
				return nil
			}
		case <-beat.C:
			if !send(": keep-alive\n\n") {
				return nil
			}
		case <-recheck.C:
			if !s.sessionLive(ctx, session) {
				send("event: ended\ndata: {}\n\n")
				return nil
			}
		}
	}
}
