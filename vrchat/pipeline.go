package vrchat

import (
	"context"
	"errors"
)

// Pipeline is one connection to VRChat's live event stream. Messages are raw
// envelopes; interpreting events and reconnecting are the caller's responsibility.
type Pipeline interface {
	Read(context.Context) ([]byte, error)
	Close() error
}

// DialPipeline opens the live feed using the auth token and configured User-Agent.
// HTTP handshake failures return APIError; other dial failures omit the URL so
// its authToken query parameter cannot appear in error logs.
func (c *Client) DialPipeline(ctx context.Context, auth string) (Pipeline, error) {
	if !tokenValue.MatchString(auth) {
		return nil, errors.New("invalid VRChat auth token")
	}
	return c.dialPipeline(ctx, auth)
}
