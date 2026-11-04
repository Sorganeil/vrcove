package vrchat

import (
	"bytes"
	"encoding/json"
)

// PipelineMessage is VRChat's live-event envelope. Content is retained so callers
// can handle event types beyond the typed notification and friend payloads below.
type PipelineMessage struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
	Err     string          `json:"err"`
}

// DecodePipelineMessage parses one WebSocket message without filtering event types.
func DecodePipelineMessage(message []byte) (PipelineMessage, error) {
	var envelope PipelineMessage
	err := json.Unmarshal(message, &envelope)
	return envelope, err
}

// DecodePipelineContent handles both JSON objects and JSON encoded inside strings,
// which VRChat uses for event content and notification details.
func DecodePipelineContent(raw json.RawMessage, into any) error {
	var inner string
	if json.Unmarshal(raw, &inner) == nil {
		raw = json.RawMessage(inner)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(into)
}

func (message PipelineMessage) DecodeContent(into any) error {
	return DecodePipelineContent(message.Content, into)
}

// NotificationEvent is the notification payload consumed by VRCove's live feed.
// Details keeps its wire form. DecodePipelineContent reads either form VRChat
// sends.
type NotificationEvent struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	SenderUserID   string          `json:"senderUserId"`
	SenderUsername string          `json:"senderUsername"`
	Message        string          `json:"message"`
	Details        json.RawMessage `json:"details"`
	CreatedAt      string          `json:"created_at"`
}

// FriendEventUser contains the profile fields used in friend activity events.
type FriendEventUser struct {
	DisplayName       string `json:"displayName"`
	Status            string `json:"status"`
	StatusDescription string `json:"statusDescription"`
}

// FriendEvent is the content of friend online, offline, location, update, active,
// add and delete events. Applications decide which changes to store or display.
type FriendEvent struct {
	UserID   string          `json:"userId"`
	User     FriendEventUser `json:"user"`
	Location string          `json:"location"`
	World    struct {
		Name string `json:"name"`
	} `json:"world"`
}
