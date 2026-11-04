package vrchat

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/coder/websocket"
)

// Pipeline is one connection to VRChat's live event stream. Messages are raw
// envelopes; interpreting events and reconnecting are the caller's responsibility.
type Pipeline interface {
	Read(context.Context) ([]byte, error)
	Close() error
}

type wsPipeline struct{ conn *websocket.Conn }

func (p *wsPipeline) Read(ctx context.Context) ([]byte, error) {
	_, data, err := p.conn.Read(ctx)
	return data, err
}
func (p *wsPipeline) Close() error { return p.conn.CloseNow() }

// DialPipeline opens the live feed using the auth token and configured User-Agent.
// HTTP handshake failures return APIError; other dial failures omit the URL so
// its authToken query parameter cannot appear in error logs.
func (c *Client) DialPipeline(ctx context.Context, auth string) (Pipeline, error) {
	if !tokenValue.MatchString(auth) {
		return nil, errors.New("invalid VRChat auth token")
	}
	conn, resp, err := websocket.Dial(ctx, c.pipelineURL+"?authToken="+url.QueryEscape(auth), &websocket.DialOptions{
		HTTPClient: c.httpClient,
		HTTPHeader: http.Header{"User-Agent": {c.userAgent}},
	})
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
	return &wsPipeline{conn: conn}, nil
}
