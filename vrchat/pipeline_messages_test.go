package vrchat_test

import (
	"encoding/json"
	"testing"

	"github.com/Sorganeil/vrcove/vrchat"
)

func TestPipelineContentWireForms(t *testing.T) {
	content := `{"id":"not_one","type":"invite","details":"{\"worldName\":\"Cafe\"}","senderUserId":"usr_one"}`
	for _, quoted := range []bool{false, true} {
		var payload any = json.RawMessage(content)
		if quoted {
			payload = content
		}
		raw, _ := json.Marshal(map[string]any{"type": "notification", "content": payload})
		message, err := vrchat.DecodePipelineMessage(raw)
		if err != nil || message.Type != "notification" {
			t.Fatalf("message=%+v error=%v", message, err)
		}
		var notification vrchat.NotificationEvent
		if err := message.DecodeContent(&notification); err != nil || notification.ID != "not_one" || notification.Type != "invite" {
			t.Fatalf("notification=%+v error=%v", notification, err)
		}
		var details struct {
			WorldName string `json:"worldName"`
		}
		if err := vrchat.DecodePipelineContent(notification.Details, &details); err != nil || details.WorldName != "Cafe" {
			t.Errorf("details=%+v error=%v", details, err)
		}
	}
}

func TestFriendAndUnknownPipelineEvents(t *testing.T) {
	message, err := vrchat.DecodePipelineMessage([]byte(`{"type":"friend-location","content":{"userId":"usr_one","location":"wrld_one:123","user":{"displayName":"Mia","status":"active"},"world":{"name":"Cafe"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var friend vrchat.FriendEvent
	if err := message.DecodeContent(&friend); err != nil || friend.UserID != "usr_one" || friend.User.DisplayName != "Mia" || friend.World.Name != "Cafe" {
		t.Fatalf("friend=%+v error=%v", friend, err)
	}
	unknown, err := vrchat.DecodePipelineMessage([]byte(`{"type":"new-event","content":"{\"large\":9007199254740993}"}`))
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := unknown.DecodeContent(&data); err != nil || data["large"] != json.Number("9007199254740993") {
		t.Errorf("content=%v error=%v", data, err)
	}
	failure, err := vrchat.DecodePipelineMessage([]byte(`{"err":"session invalid"}`))
	if err != nil || failure.Err != "session invalid" {
		t.Errorf("failure=%+v error=%v", failure, err)
	}
}

func TestMalformedPipelineData(t *testing.T) {
	if _, err := vrchat.DecodePipelineMessage([]byte("{")); err == nil {
		t.Error("accepted malformed envelope")
	}
	message, err := vrchat.DecodePipelineMessage([]byte(`{"type":"notification","content":"not json"}`))
	if err != nil {
		t.Fatal(err)
	}
	var notification vrchat.NotificationEvent
	if err := message.DecodeContent(&notification); err == nil {
		t.Error("accepted malformed content")
	}
}
