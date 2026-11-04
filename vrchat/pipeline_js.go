//go:build js

package vrchat

import (
	"context"
	"errors"
)

// In the browser, coder/websocket wraps the browser's WebSocket, which cannot
// use the client's HTTP transport or set a User-Agent. A dialer that runs over
// the client's own connection replaces this.
func (c *Client) dialPipeline(context.Context, string) (Pipeline, error) {
	return nil, errors.New("VRChat live connection is not available in the browser yet")
}
