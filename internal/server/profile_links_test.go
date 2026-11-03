package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSaveSocialLinksUsesProfileAPI(t *testing.T) {
	for _, tc := range []struct {
		name  string
		patch string
		want  map[string]any
	}{
		{"links only", `{"bioLinks":[" https://example.com/social ","http://example.org"]}`, map[string]any{"bioLinks": []any{"https://example.com/social", "http://example.org"}}},
		{"clear", `{"bioLinks":[]}`, map[string]any{"bioLinks": []any{}}},
		{"bio and status", `{"bio":"New bio","status":"busy","bioLinks":["https://example.com/social"]}`, map[string]any{"bio": "New bio", "bioLinks": []any{"https://example.com/social"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newConnectedHarness(t)
			id := "usr_00000000-0000-0000-0000-000000000003"
			profileWrites, userWrites := 0, 0
			h.upstream = func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/1/auth/user" && r.Method == http.MethodGet {
					return respond(200, "application/json", []byte(`{"id":"`+id+`","displayName":"Test","bio":"Unchanged bio"}`)), nil
				}
				if r.Method != http.MethodPut {
					t.Fatalf("unexpected method %s", r.Method)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				switch r.URL.Path {
				case "/api/1/users/" + id:
					userWrites++
					if !reflect.DeepEqual(body, map[string]any{"status": "busy"}) {
						t.Fatalf("user patch = %v", body)
					}
					return respond(200, "application/json", []byte(`{"id":"`+id+`","status":"busy"}`)), nil
				case "/api/1/profile/" + id:
					profileWrites++
					if !reflect.DeepEqual(body, tc.want) {
						t.Fatalf("profile patch = %v, want %v", body, tc.want)
					}
					body["email"] = "hidden"
					raw, _ := json.Marshal(body)
					return respond(200, "application/json", raw), nil
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
					return nil, nil
				}
			}
			w := h.do(withSession(t, post("/account/profile", strings.NewReader(tc.patch)), &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}))
			user := decode(t, w).(map[string]any)
			if user["id"] != id || user["email"] != nil || !reflect.DeepEqual(user["bioLinks"], tc.want["bioLinks"]) || profileWrites != 1 {
				t.Fatalf("saved profile = %s; writes = %d", w.Body, profileWrites)
			}
			if tc.name == "bio and status" {
				if user["bio"] != "New bio" || user["status"] != "busy" || userWrites != 1 {
					t.Fatalf("combined profile = %s; user writes = %d", w.Body, userWrites)
				}
			} else if user["bio"] != "Unchanged bio" || userWrites != 0 {
				t.Fatalf("links patch changed other fields: %s; user writes = %d", w.Body, userWrites)
			}
		})
	}
}

func TestInvalidSocialLinksDoNotWriteProfile(t *testing.T) {
	for _, links := range []string{`null`, `"https://example.com"`, `{}`, `[42]`, `[null]`, `[""]`, `["example.com"]`, `["javascript:alert(1)"]`, `["ftp://example.com"]`, `["https://"]`, `["https://example.com","bad"]`} {
		t.Run(links, func(t *testing.T) {
			h := newConnectedHarness(t)
			h.upstream = func(r *http.Request) (*http.Response, error) {
				t.Fatalf("invalid links contacted upstream: %s", r.URL.Path)
				return nil, nil
			}
			w := h.do(withSession(t, post("/account/profile", strings.NewReader(`{"bio":"Must not save","status":"busy","bioLinks":`+links+`}`)), &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body)
			}
		})
	}
}
