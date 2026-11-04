//go:build js

package vrchat

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// pipelinePing keeps a quiet pipeline open through relays that close idle
// connections.
const pipelinePing = time.Minute

// In the browser, coder/websocket wraps the browser's WebSocket, which cannot
// use the client's transport or set a User-Agent. This build speaks WebSocket
// itself over the connection from Config.PipelineDial.
type jsPipeline struct {
	conn *websocket.Conn
	stop chan struct{}
	once sync.Once
}

func (p *jsPipeline) Read(ctx context.Context) ([]byte, error) {
	// gorilla reads ignore contexts, so a cancelled ctx closes the connection.
	cancel := context.AfterFunc(ctx, func() { p.conn.Close() })
	defer cancel()
	_, data, err := p.conn.ReadMessage()
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return data, err
}

func (p *jsPipeline) Close() error {
	p.once.Do(func() { close(p.stop) })
	return p.conn.Close()
}

func (c *Client) dialPipeline(ctx context.Context, auth string) (Pipeline, error) {
	if c.pipelineDial == nil {
		return nil, errors.New("VRChat live connection needs Config.PipelineDial in the browser")
	}
	dialer := websocket.Dialer{
		NetDialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return c.pipelineDial(ctx) },
		HandshakeTimeout:  20 * time.Second,
	}
	conn, resp, err := dialer.DialContext(ctx, c.pipelineURL+"?authToken="+url.QueryEscape(auth), http.Header{"User-Agent": {c.userAgent}})
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			return nil, &APIError{StatusCode: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("VRChat live connection failed")
	}
	conn.SetReadLimit(1 << 20)
	p := &jsPipeline{conn: conn, stop: make(chan struct{})}
	go func() {
		t := time.NewTicker(pipelinePing)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)) != nil {
					return
				}
			}
		}
	}()
	return p, nil
}
