package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testWorld = "wrld_00000000-0000-0000-0000-000000000001"
	testGroup = "grp_00000000-0000-0000-0000-000000000002"
)

// signedIn returns a request carrying a valid session and an upstream stub
// that serves body for the expected VRChat path.
func (h *harness) signedIn(r *http.Request, wantPath, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	h.upstream = func(up *http.Request) (*http.Response, error) {
		if strings.HasPrefix(wantPath, "/users/") && up.URL.Path == "/api/1/profile/"+strings.TrimPrefix(wantPath, "/users/") {
			return respond(200, "application/json", []byte(`{}`)), nil
		}
		if got := strings.TrimPrefix(up.URL.RequestURI(), "/api/1"); got != wantPath {
			h.t.Errorf("upstream path = %s, want %s", got, wantPath)
		}
		return respond(200, "application/json", []byte(body)), nil
	}
	return h.do(withSession(h.t, r, &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}))
}

func decode(t *testing.T, w *httptest.ResponseRecorder) any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var v any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWorldDetails(t *testing.T) {
	h := newConnectedHarness(t)
	w := h.signedIn(get("/account/world?id="+testWorld), "/worlds/"+testWorld, `{
		"id":"`+testWorld+`","name":"Test","created_at":"2024-01-01","secret":"x",
		"unityPackages":[{"platform":"standalonewindows","assetUrl":"https://files/asset"},{"platform":"android"},{"platform":"standalonewindows"}],
		"instances":[["12345~private(usr_x)",3],["bad/../id",2],[7,1]]}`)
	world := decode(t, w).(map[string]any)
	if world["created"] != "2024-01-01" || world["secret"] != nil || strings.Contains(w.Body.String(), "assetUrl") {
		t.Errorf("world = %s", w.Body)
	}
	if got, _ := json.Marshal(world["platforms"]); string(got) != `["standalonewindows","android"]` {
		t.Errorf("platforms = %s", got)
	}
	if got, _ := json.Marshal(world["instances"]); string(got) != `[{"id":"12345~private(usr_x)","users":3}]` {
		t.Errorf("instances = %s", got)
	}
}

func TestUserProfile(t *testing.T) {
	h := newConnectedHarness(t)
	// Legacy 10-character IDs are accepted.
	w := h.signedIn(get("/account/user?id=AbCdE12345"), "/users/AbCdE12345", `{
		"id":"AbCdE12345","displayName":"Old","location":"somewhere/else","email":"x","isFriend":1,"date_joined":"2018-01-01",
		"badges":[{"badgeId":"b1","badgeName":"Shown","badgeImageUrl":"i"},{"badgeId":"b2","badgeName":"Hidden","showcased":false},{"badgeId":"b3"}]}`)
	user := decode(t, w).(map[string]any)
	if _, has := user["location"]; has || user["email"] != nil || user["isFriend"] != true || user["dateJoined"] != "2018-01-01" {
		t.Errorf("profile = %s", w.Body)
	}
	if got, _ := json.Marshal(user["badges"]); string(got) != `[{"id":"b1","imageUrl":"i","name":"Shown"}]` {
		t.Errorf("badges = %s", got)
	}
	w = h.signedIn(get("/account/user?id=usr_00000000-0000-0000-0000-000000000003"), "/users/usr_00000000-0000-0000-0000-000000000003", `{"location":"traveling"}`)
	if decode(t, w).(map[string]any)["location"] != "traveling" {
		t.Errorf("profile = %s", w.Body)
	}
}

func TestProfileBioFromProfileAPI(t *testing.T) {
	for _, id := range []string{"AbCdE12345", "usr_00000000-0000-0000-0000-000000000003"} {
		t.Run(id, func(t *testing.T) {
			h := newConnectedHarness(t)
			h.upstream = func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/1/users/" + id:
					return respond(200, "application/json", []byte(`{"id":"`+id+`","bio":"stale","isFriend":true,"location":"private","date_joined":"2020-01-01"}`)), nil
				case "/api/1/profile/" + id:
					return respond(200, "application/json", []byte(`{"bio":"Actual biography\nSecond line","bioLinks":["https://example.com"],"email":"hidden","isFriend":false}`), "Set-Cookie", "auth=rotated_profile; Path=/"), nil
				default:
					t.Fatalf("unexpected upstream path %s", r.URL.Path)
					return nil, nil
				}
			}
			w := h.do(withSession(t, get("/account/user?id="+id), &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}))
			user := decode(t, w).(map[string]any)
			if user["bio"] != "Actual biography\nSecond line" || user["isFriend"] != true || user["location"] != "private" || user["dateJoined"] != "2020-01-01" || user["email"] != nil {
				t.Errorf("profile = %s", w.Body)
			}
			if len(user["bioLinks"].([]any)) != 1 || w.Header().Get("Set-Cookie") == "" {
				t.Errorf("missing links or rotated session: %s", w.Body)
			}
		})
	}
}

func TestSaveBioUsesProfileAPI(t *testing.T) {
	h := newConnectedHarness(t)
	id := "usr_00000000-0000-0000-0000-000000000003"
	paths := []string{}
	h.upstream = func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/1/auth/user" {
			return respond(200, "application/json", []byte(`{"id":"`+id+`","displayName":"Test","bio":"stale"}`)), nil
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		switch r.URL.Path {
		case "/api/1/users/" + id:
			if len(body) != 1 || body["status"] != "busy" {
				t.Errorf("status body = %v", body)
			}
			return respond(200, "application/json", []byte(`{"id":"`+id+`","displayName":"Test","status":"busy","bio":"stale"}`)), nil
		case "/api/1/profile/" + id:
			if len(body) != 1 || body["bio"] != "New bio" {
				t.Errorf("bio body = %v", body)
			}
			return respond(200, "application/json", []byte(`{"bio":"New bio","email":"hidden"}`)), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
			return nil, nil
		}
	}
	w := h.do(withSession(t, post("/account/profile", strings.NewReader(`{"bio":"New bio","status":"busy"}`)), &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}))
	user := decode(t, w).(map[string]any)
	if user["bio"] != "New bio" || user["status"] != "busy" || user["id"] != id || user["email"] != nil || len(paths) != 3 {
		t.Errorf("saved profile = %s; paths = %v", w.Body, paths)
	}
}

func TestProfileBioEmptyAndUnavailable(t *testing.T) {
	for _, status := range []int{200, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			h := newConnectedHarness(t)
			h.upstream = func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/1/users/AbCdE12345" {
					return respond(200, "application/json", []byte(`{"id":"AbCdE12345","bio":"stale"}`)), nil
				}
				return respond(status, "application/json", []byte(`{"bio":"","bioLinks":[]}`)), nil
			}
			w := h.do(withSession(t, get("/account/user?id=AbCdE12345"), &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}))
			if w.Code != status {
				t.Fatalf("profile status = %d, want %d", w.Code, status)
			}
			if status == 200 && decode(t, w).(map[string]any)["bio"] != "" {
				t.Errorf("cleared bio replaced by stale value: %s", w.Body)
			}
		})
	}
}

func TestGroupDetails(t *testing.T) {
	h := newConnectedHarness(t)
	w := h.signedIn(get("/account/group-instances?id="+testGroup), "/groups/"+testGroup+"/instances", `[
		{"instanceId":"111~group(grp_x)","world":{"id":"`+testWorld+`","name":"W","capacity":16},"memberCount":4},
		{"location":"`+testWorld+`:222","world":{"name":"W"},"n_users":2},
		{"location":"javascript:alert(1)","world":{}}]`)
	if got := w.Body.String(); !strings.Contains(got, `"location":"`+testWorld+`:111~group(grp_x)"`) || !strings.Contains(got, `"users":2`) || strings.Contains(got, "javascript") {
		t.Errorf("instances = %s", got)
	}
	w = h.signedIn(get("/account/group-members?id="+testGroup), "/groups/"+testGroup+"/members?n=50&offset=0",
		`[{"userId":"usr_1","joinedAt":"t","user":{"displayName":"M","iconUrl":"","userIcon":"icon","thumbnailUrl":"thumb"}}]`)
	if got := strings.TrimSpace(w.Body.String()); got != `[{"currentAvatarThumbnailImageUrl":"thumb","displayName":"M","id":"usr_1","joinedAt":"t","userIcon":"icon"}]` {
		t.Errorf("members = %s", got)
	}
	w = h.signedIn(get("/account/group-posts?id="+testGroup), "/groups/"+testGroup+"/posts?n=20&offset=0", `{"posts":[{"id":"p","title":"T","editorId":"x"}]}`)
	if got := strings.TrimSpace(w.Body.String()); got != `[{"id":"p","title":"T"}]` {
		t.Errorf("posts = %s", got)
	}
	h.upstream = func(*http.Request) (*http.Response, error) { return respond(403, "text/plain", nil), nil }
	if w := h.do(withSession(t, get("/account/group-instances?id="+testGroup), &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()})); w.Code != 403 {
		t.Errorf("non-member = %d", w.Code)
	}
}

func TestUserGroupImages(t *testing.T) {
	h := newConnectedHarness(t)
	w := h.signedIn(get("/account/user-groups?id=AbCdE12345"), "/users/AbCdE12345/groups", `[
		{"groupId":"`+testGroup+`","id":"gmem_membership","name":"Banner group","iconUrl":null,"bannerUrl":"https://assets.vrchat.com/banner.png","isRepresenting":true,"secret":"hidden"},
		{"groupId":"grp_other","name":"Icon group","iconUrl":"https://assets.vrchat.com/icon.png","bannerUrl":""}]`)
	groups := decode(t, w).([]any)
	first := groups[0].(map[string]any)
	if first["id"] != testGroup || first["iconUrl"] != nil || first["bannerUrl"] != "https://assets.vrchat.com/banner.png" || first["secret"] != nil {
		t.Errorf("banner group = %s", w.Body)
	}
	if groups[1].(map[string]any)["iconUrl"] != "https://assets.vrchat.com/icon.png" {
		t.Errorf("icon group = %s", w.Body)
	}
}

func TestDetailValidation(t *testing.T) {
	h := newConnectedHarness(t)
	session := &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}
	for _, path := range []string{
		"/account/world?id=wrld_x", "/account/user?id=../auth/user", "/account/user?id=usr_x",
		"/account/user-worlds?id=abc", "/account/group-posts?id=grp_../x",
		"/account/raw?kind=file&id=" + testWorld, "/account/raw?kind=avatar&id=" + testWorld,
	} {
		if w := h.do(withSession(t, get(path), session)); w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", path, w.Code)
		}
	}
	for _, body := range []string{
		`{"action":"selfInvite","id":"wrld_x:1"}`,
		`{"action":"newInstance","id":"` + testWorld + `","region":"mars"}`,
		`{"action":"friend","id":"usr_../x"}`,
	} {
		if w := h.do(withSession(t, post("/account/action", strings.NewReader(body)), session)); w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, w.Code)
		}
	}
	if n := h.fetches.Load(); n != 0 {
		t.Errorf("%d upstream requests", n)
	}
	// Unauthenticated callers get 401 before any validation.
	if w := h.do(get("/account/raw?kind=world&id=" + testWorld)); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous raw = %d", w.Code)
	}
}

func TestRawAndUploads(t *testing.T) {
	h := newConnectedHarness(t)
	w := h.signedIn(get("/account/raw?kind=avatar&id=avtr_00000000-0000-0000-0000-000000000004"), "/avatars/avtr_00000000-0000-0000-0000-000000000004",
		`{"id":"avtr_00000000-0000-0000-0000-000000000004","unityPackages":[{"assetUrl":"https://files/a"}]}`)
	if !strings.Contains(w.Body.String(), "assetUrl") {
		t.Errorf("raw should be unfiltered: %s", w.Body)
	}
	w = h.signedIn(get("/account/my-worlds?offset=99999"), "/worlds?user=me&releaseStatus=all&sort=updated&order=descending&n=100&offset=10000", `[{"id":"w"}]`)
	decode(t, w)
}

func TestNewInstance(t *testing.T) {
	h := newConnectedHarness(t)
	session := &Session{Auth: "a", Expires: time.Now().Add(time.Hour).UnixMilli()}
	create := func(reply string) *httptest.ResponseRecorder {
		h.upstream = func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || r.URL.Path != "/api/1/instances" || !strings.Contains(string(body), `"region":"eu"`) {
				t.Errorf("upstream %s %s %s", r.Method, r.URL.Path, body)
			}
			return respond(200, "application/json", []byte(reply)), nil
		}
		return h.do(withSession(t, post("/account/action", strings.NewReader(`{"action":"newInstance","id":"`+testWorld+`","region":"eu"}`)), session))
	}
	if w := create(`{"instanceId":"55555~region(eu)"}`); !strings.Contains(w.Body.String(), `"location":"`+testWorld+`:55555~region(eu)"`) {
		t.Errorf("new instance = %d %s", w.Code, w.Body)
	}
	if w := create(`{"location":"wrld_evil:<script>"}`); w.Code != http.StatusBadGateway {
		t.Errorf("bad location = %d", w.Code)
	}
}
