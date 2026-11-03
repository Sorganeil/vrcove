package vrchat_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Sorganeil/vrcove/vrchat"
)

// These fixtures specify the wire contract independently of the generator.
func TestNamedOperationContracts(t *testing.T) {
	cases := []struct {
		name                      string
		operation                 vrchat.Call
		method, path, query, body string
	}{
		{"user", vrchat.GetUser("usr_one"), "GET", "/users/usr_one", "", ""},
		{"profile", vrchat.GetPublicProfile("usr_one", vrchat.GetPublicProfileParams{}), "GET", "/profile/usr_one", "", ""},
		{"clear profile", vrchat.UpdateProfile("usr_one", &vrchat.UpdateProfileRequest{Bio: vrchat.Ptr(""), BioLinks: vrchat.Ptr([]string{})}), "PUT", "/profile/usr_one", "", `{"bio":"","bioLinks":[]}`},
		{"explicit false", vrchat.UpdateUser("usr_one", &vrchat.UpdateUserRequest{AllowAvatarCopying: vrchat.Ptr(false)}), "PUT", "/users/usr_one", "", `{"allowAvatarCopying":false}`},
		{"user search", vrchat.SearchUsers(vrchat.SearchUsersParams{Search: vrchat.Ptr("one & two"), N: vrchat.Ptr(50), Offset: vrchat.Ptr(0)}), "GET", "/users", "n=50&offset=0&search=one+%26+two", ""},
		{"username", vrchat.CheckUserExists(vrchat.CheckUserExistsParams{Username: vrchat.Ptr("one & two")}), "GET", "/auth/exists", "username=one+%26+two", ""},
		{"friends", vrchat.GetFriends(vrchat.GetFriendsParams{Offline: vrchat.Ptr(false), N: vrchat.Ptr(100), Offset: vrchat.Ptr(0)}), "GET", "/auth/user/friends", "n=100&offset=0&offline=false", ""},
		{"friend request", vrchat.Friend("usr_one"), "POST", "/user/usr_one/friendRequest", "", ""},
		{"cancel friend", vrchat.DeleteFriendRequest("usr_one"), "DELETE", "/user/usr_one/friendRequest", "", ""},
		{"friend status", vrchat.GetFriendStatus("usr_one"), "GET", "/user/usr_one/friendStatus", "", ""},
		{"unfriend", vrchat.Unfriend("usr_one"), "DELETE", "/auth/user/friends/usr_one", "", ""},
		{"avatar", vrchat.GetAvatar("avtr_one"), "GET", "/avatars/avtr_one", "", ""},
		{"select", vrchat.SelectAvatar("avtr_one"), "PUT", "/avatars/avtr_one/select", "", ""},
		{"fallback casing", vrchat.SelectFallbackAvatar("avtr_one"), "PUT", "/avatars/avtr_one/selectFallback", "", ""},
		{"world", vrchat.GetWorld("wrld_one"), "GET", "/worlds/wrld_one", "", ""},
		{"world search", vrchat.SearchWorlds(vrchat.SearchWorldsParams{Search: vrchat.Ptr("one & two"), N: vrchat.Ptr(48), Platform: vrchat.Ptr("android")}), "GET", "/worlds", "n=48&platform=android&search=one+%26+two", ""},
		{"world instance", vrchat.GetWorldInstance("wrld_one", "123~region(eu)"), "GET", "/worlds/wrld_one/123~region(eu)", "", ""},
		{"create instance", vrchat.CreateInstance(vrchat.CreateInstanceRequest{WorldID: "wrld_one", Region: "eu", Type: "public"}), "POST", "/instances", "", `{"region":"eu","type":"public","worldId":"wrld_one"}`},
		{"favorites", vrchat.GetFavorites(vrchat.GetFavoritesParams{Type: vrchat.Ptr("friend"), N: vrchat.Ptr(100)}), "GET", "/favorites", "n=100&type=friend", ""},
		{"add favorite", vrchat.AddFavorite(&vrchat.AddFavoriteRequest{FavoriteID: "avtr_one", Tags: []vrchat.Tag{"avatars1"}, Type: "avatar"}), "POST", "/favorites", "", `{"favoriteId":"avtr_one","tags":["avatars1"],"type":"avatar"}`},
		{"remove favorite", vrchat.RemoveFavorite("fvrt_one"), "DELETE", "/favorites/fvrt_one", "", ""},
		{"favorite groups", vrchat.GetFavoriteGroups(vrchat.GetFavoriteGroupsParams{Type: vrchat.Ptr(vrchat.FavoriteType("avatar"))}), "GET", "/favorite/groups", "type=avatar", ""},
		{"group", vrchat.GetGroup("grp_one", vrchat.GetGroupParams{}), "GET", "/groups/grp_one", "", ""},
		{"group posts", vrchat.GetGroupPosts("grp_one", vrchat.GetGroupPostsParams{N: vrchat.Ptr(20), Offset: vrchat.Ptr(0)}), "GET", "/groups/grp_one/posts", "n=20&offset=0", ""},
		{"group members", vrchat.GetGroupMembers("grp_one", vrchat.GetGroupMembersParams{N: vrchat.Ptr(50)}), "GET", "/groups/grp_one/members", "n=50", ""},
		{"group instances", vrchat.GetGroupInstances("grp_one"), "GET", "/groups/grp_one/instances", "", ""},
		{"user groups", vrchat.GetUserGroups("usr_one"), "GET", "/users/usr_one/groups", "", ""},
		{"group search", vrchat.SearchGroups(vrchat.SearchGroupsParams{Query: vrchat.Ptr("a&b")}), "GET", "/groups", "query=a%26b", ""},
		{"join without optional body", vrchat.JoinGroup("grp_one", vrchat.JoinGroupParams{}, nil), "POST", "/groups/grp_one/join", "", ""},
		{"leave group", vrchat.LeaveGroup("grp_one"), "POST", "/groups/grp_one/leave", "", ""},
		{"notifications", vrchat.GetNotifications(vrchat.GetNotificationsParams{Hidden: vrchat.Ptr(false), N: vrchat.Ptr(100)}), "GET", "/auth/user/notifications", "hidden=false&n=100", ""},
		{"accept", vrchat.AcceptFriendRequest("not_one"), "PUT", "/auth/user/notifications/not_one/accept", "", ""},
		{"hide", vrchat.DeleteNotification("not_one"), "PUT", "/auth/user/notifications/not_one/hide", "", ""},
		{"seen", vrchat.MarkNotificationAsRead("not_one"), "PUT", "/auth/user/notifications/not_one/see", "", ""},
		{"request invite", vrchat.RequestInvite("usr_one", &vrchat.RequestInviteRequest{RequestSlot: vrchat.Ptr(0)}), "POST", "/requestInvite/usr_one", "", `{"requestSlot":0}`},
		{"self invite", vrchat.InviteMyselfTo("wrld_one", "123~region(eu)"), "POST", "/invite/myself/to/wrld_one:123~region(eu)", "", ""},
		{"player moderation", vrchat.GetPlayerModerations(vrchat.GetPlayerModerationsParams{Type: vrchat.Ptr(vrchat.PlayerModerationType("block"))}), "GET", "/auth/user/playermoderations", "type=block", ""},
		{"file metadata", vrchat.GetFile("file_one"), "GET", "/file/file_one", "", ""},
		{"file version", vrchat.GetFileDataUploadStatus("file_one", 2, "file"), "GET", "/file/file_one/2/file/status", "", ""},
		{"notification v2", vrchat.GetNotificationV2("not_one"), "GET", "/notifications/not_one", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
				if r.Method != tc.method || r.URL.Path != "/api/1"+tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				wantQuery, err := url.ParseQuery(tc.query)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(r.URL.Query(), wantQuery) {
					t.Errorf("query = %v, want %v", r.URL.Query(), wantQuery)
				}
				var body []byte
				if r.Body != nil {
					body, err = io.ReadAll(r.Body)
					if err != nil {
						t.Fatal(err)
					}
				}
				if tc.body == "" {
					if len(body) != 0 {
						t.Errorf("unexpected body %s", body)
					}
				} else {
					var got, want any
					if err := json.Unmarshal(body, &got); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("body = %s, want %s", body, tc.body)
					}
				}
				if r.Header.Get("Cookie") != "auth=original" {
					t.Errorf("cookie = %s", r.Header.Get("Cookie"))
				}
				return response(200, `{"unknown":{"number":9007199254740993}}`, http.Header{"Set-Cookie": {"auth=rotated"}}), nil
			})
			raw, updates, err := c.Call(context.Background(), vrchat.Tokens{Auth: "original"}, tc.operation)
			if err != nil {
				t.Fatal(err)
			}
			if updates.Auth != "rotated" || raw.(map[string]any)["unknown"].(map[string]any)["number"] != json.Number("9007199254740993") {
				t.Errorf("response=%v updates=%+v", raw, updates)
			}
		})
	}
}

func TestTypedEndpointResponses(t *testing.T) {
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/1/avatars/avtr_one":
			return response(200, `{"id":"avtr_one","name":"Fox","authorId":"usr_one","version":7}`, http.Header{"Set-Cookie": {"twoFactorAuth=rotated"}}), nil
		case "/api/1/worlds":
			return response(200, `[{"id":"wrld_one","name":"Cafe","capacity":32}]`, nil), nil
		case "/api/1/groups/grp_one/posts":
			return response(200, `{"posts":[{"id":"post_one","title":"News"}],"total":1}`, nil), nil
		case "/api/1/user/usr_one/friendStatus":
			return response(200, `{"isFriend":true,"incomingRequest":false,"outgoingRequest":false}`, nil), nil
		default:
			t.Fatalf("unexpected request %s", r.URL)
			return nil, nil
		}
	})
	ctx := context.Background()
	avatar, updates, err := c.GetAvatar(ctx, vrchat.Tokens{}, "avtr_one")
	if err != nil || avatar.ID != "avtr_one" || avatar.Name != "Fox" || avatar.Version != 7 || updates.TwoFactorAuth != "rotated" {
		t.Fatalf("avatar=%+v updates=%+v error=%v", avatar, updates, err)
	}
	worlds, _, err := c.SearchWorlds(ctx, vrchat.Tokens{}, vrchat.SearchWorldsParams{})
	if err != nil || len(worlds) != 1 || worlds[0].Name != "Cafe" {
		t.Fatalf("worlds=%+v error=%v", worlds, err)
	}
	posts, _, err := c.GetGroupPosts(ctx, vrchat.Tokens{}, "grp_one", vrchat.GetGroupPostsParams{})
	if err != nil || posts.Total == nil || *posts.Total != 1 || posts.Posts == nil || len(*posts.Posts) != 1 {
		t.Fatalf("posts=%+v error=%v", posts, err)
	}
	status, _, err := c.GetFriendStatus(ctx, vrchat.Tokens{}, "usr_one")
	if err != nil || !status.IsFriend {
		t.Fatalf("friend status=%+v error=%v", status, err)
	}
}

func TestAvatarSearchVariants(t *testing.T) {
	for _, body := range []string{`[{"id":"avtr_one","name":"Fox"}]`, `{"results":[{"id":"avtr_one","name":"Fox"}],"totalCount":1,"hasNext":false}`} {
		c := client(t, vrchat.Config{}, func(*http.Request) (*http.Response, error) { return response(200, body, nil), nil })
		result, _, err := c.SearchAvatars(context.Background(), vrchat.Tokens{}, vrchat.SearchAvatarsParams{})
		if err != nil || len(result.Avatars) != 1 || result.Avatars[0].Name != "Fox" {
			t.Errorf("result=%+v error=%v", result, err)
		}
		if strings.HasPrefix(body, "{") && (result.TotalCount == nil || *result.TotalCount != 1 || result.HasNext == nil || *result.HasNext) {
			t.Errorf("metadata=%+v", result)
		}
	}
	for _, body := range []string{`null`, `{}`, `"bad"`} {
		var result vrchat.SearchAvatarsResult
		if err := json.Unmarshal([]byte(body), &result); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestOperationValidationAndEscaping(t *testing.T) {
	calls := 0
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.EscapedPath() != "/api/1/users/usr_a%2Fb%3Fc%23d" {
			t.Errorf("escaped path=%s", r.URL.EscapedPath())
		}
		return response(200, `{}`, nil), nil
	})
	ctx := context.Background()
	var zero vrchat.Operation[vrchat.User]
	var missing *vrchat.Operation[vrchat.User]
	for _, op := range []vrchat.Call{nil, missing, zero, vrchat.GetUser(""), vrchat.GetUser(".."), vrchat.GetUser("a\nb"), vrchat.VerifyTwoFactor("../other", "123456")} {
		if _, _, err := c.Call(ctx, vrchat.Tokens{}, op); err == nil {
			t.Errorf("accepted invalid operation %T", op)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid operations caused %d requests", calls)
	}
	if _, _, err := c.Call(ctx, vrchat.Tokens{}, vrchat.GetUser("usr_a/b?c#d")); err != nil {
		t.Fatal(err)
	}
}

func TestTypedDecodeFailureKeepsRotatedTokens(t *testing.T) {
	c := client(t, vrchat.Config{}, func(*http.Request) (*http.Response, error) {
		return response(200, `{"version":"wrong type"}`, http.Header{"Set-Cookie": {"auth=rotated"}}), nil
	})
	_, updates, err := c.GetAvatar(context.Background(), vrchat.Tokens{}, "avtr_one")
	var typeError *json.UnmarshalTypeError
	if !errors.As(err, &typeError) || updates.Auth != "rotated" {
		t.Errorf("updates=%+v error=%v", updates, err)
	}
}

func TestArrayQueriesAndHeaderParameters(t *testing.T) {
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/1/instances/discover":
			if got := r.URL.Query()["worldId"]; !reflect.DeepEqual(got, []string{"wrld_one", "wrld_two"}) {
				t.Errorf("worldId=%v", got)
			}
			if got := r.URL.Query().Get("groupHosted"); got != "false" {
				t.Errorf("groupHosted=%s", got)
			}
		case "/api/1/users/usr_one/tutorial":
			if r.Header.Get("X-Platform") != "android" || r.Header.Get("X-Store") != "oculus" {
				t.Errorf("headers=%v", r.Header)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return response(200, `{}`, nil), nil
	})
	ctx := context.Background()
	if _, _, err := c.Call(ctx, vrchat.Tokens{}, vrchat.DiscoverInstances(vrchat.DiscoverInstancesParams{WorldID: []vrchat.WorldID{"wrld_one", "wrld_two"}, GroupHosted: vrchat.Ptr(false)})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Call(ctx, vrchat.Tokens{}, vrchat.GetUserTutorialStatus("usr_one", vrchat.GetUserTutorialStatusParams{XPlatform: vrchat.Ptr("android"), XStore: vrchat.Ptr("oculus")})); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredQueryAndEmptySuccess(t *testing.T) {
	calls := 0
	c := client(t, vrchat.Config{}, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/api/1/groups/grp_one/leave" {
			t.Errorf("path=%s", r.URL.Path)
		}
		return response(200, "", nil), nil
	})
	ctx := context.Background()
	if _, _, err := c.Call(ctx, vrchat.Tokens{}, vrchat.ConfirmEmail(vrchat.ConfirmEmailParams{})); err == nil || calls != 0 {
		t.Fatalf("missing required query: error=%v calls=%d", err, calls)
	}
	if _, _, err := c.LeaveGroup(ctx, vrchat.Tokens{}, "grp_one"); err != nil {
		t.Errorf("empty success=%v", err)
	}
	if _, _, err := c.Call(ctx, vrchat.Tokens{}, vrchat.LeaveGroup("grp_one")); err != nil {
		t.Errorf("raw empty success=%v", err)
	}
}

func TestHeterogeneousSchemaValuesKeepRawJSON(t *testing.T) {
	for _, body := range []string{`false`, `"2026-11-02T12:00:00Z"`, `{"number":9007199254740993}`} {
		var value vrchat.UserActivityTimestamp
		if err := json.Unmarshal([]byte(body), &value); err != nil || string(value) != body {
			t.Errorf("value=%s error=%v", value, err)
		}
	}
}

func TestCoverageListsImplementedClientMethods(t *testing.T) {
	data, err := os.ReadFile("coverage.json")
	if err != nil {
		t.Fatal(err)
	}
	var coverage struct {
		Operations []struct{ Operation, Category string }
		Excluded   []struct{ Operation string }
	}
	if err := json.Unmarshal(data, &coverage); err != nil {
		t.Fatal(err)
	}
	methods := reflect.TypeOf(&vrchat.Client{})
	categories := map[string]bool{}
	for _, operation := range coverage.Operations {
		method, ok := methods.MethodByName(operation.Operation)
		if !ok {
			t.Errorf("documented operation %s has no client method", operation.Operation)
			continue
		}
		if method.Type.NumOut() != 3 || method.Type.Out(1) != reflect.TypeOf(vrchat.Tokens{}) || !method.Type.Out(2).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			t.Errorf("operation %s does not return result, tokens, error", operation.Operation)
		}
		categories[operation.Category] = true
	}
	if len(coverage.Operations) != 328 || len(categories) != 19 || len(coverage.Excluded) != 11 {
		t.Errorf("coverage=%d operations, %d categories, %d exclusions", len(coverage.Operations), len(categories), len(coverage.Excluded))
	}
}

func TestNullableOwnerModel(t *testing.T) {
	var owner vrchat.InstanceOwnerID
	if err := json.Unmarshal([]byte(`"usr_one"`), &owner); err != nil || owner == nil || *owner != "usr_one" {
		t.Fatalf("owner=%v error=%v", owner, err)
	}
	if err := json.Unmarshal([]byte(`null`), &owner); err != nil || owner != nil {
		t.Errorf("null owner=%v error=%v", owner, err)
	}
}

func TestTypedNotifications(t *testing.T) {
	c := client(t, vrchat.Config{}, func(*http.Request) (*http.Response, error) {
		return response(200, `[{"id":"not_one","type":"friendRequest","senderUserId":"usr_one","message":"hello","created_at":"2026-11-02T12:00:00Z"}]`, nil), nil
	})
	notifications, _, err := c.GetNotifications(context.Background(), vrchat.Tokens{}, vrchat.GetNotificationsParams{})
	if err != nil || len(notifications) != 1 || notifications[0].Type == nil || *notifications[0].Type != "friendRequest" {
		t.Fatalf("notifications=%+v error=%v", notifications, err)
	}
}
