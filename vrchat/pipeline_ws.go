//go:build !js

package vrchat

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/coder/websocket"
)

type wsPipeline struct{ conn *websocket.Conn }

func (p *wsPipeline) Read(ctx context.Context) ([]byte, error) {
	_, data, err := p.conn.Read(ctx)
	return data, err
}
func (p *wsPipeline) Close() error { return p.conn.CloseNow() }

func (c *Client) dialPipeline(ctx context.Context, auth string) (Pipeline, error) {
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
