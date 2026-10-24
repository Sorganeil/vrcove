package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestVRCXProvider(t *testing.T) {
	h, rw := contentHarness(t)
	const creator = "usr_00000000-0000-0000-0000-0000000000d1"
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000501", "Snow Fox", "Kit Maker", creator, "2026-04-02T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000502", "Fox Scout", "Kit Maker", creator, "2026-04-01T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000503", "Robot", "Other", "usr_00000000-0000-0000-0000-0000000000d2", "2026-04-03T00:00:00Z", nil)
	fetch := func(query string) (int, []map[string]any) {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/vrcx/avatars"+query, nil)
		r.Header.Set("Referer", "https://vrcx.app")
		w := h.do(r)
		var list []map[string]any
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
				t.Fatalf("%s: %v %s", query, err, w.Body)
			}
		}
		return w.Code, list
	}

	code, list := fetch("?search=fox&n=5000")
	if code != 200 || len(list) != 2 || list[0]["name"] != "Snow Fox" || list[1]["name"] != "Fox Scout" {
		t.Fatalf("search = %d %v", code, list)
	}
	a := list[0]
	for key, want := range map[string]any{"id": "avtr_00000000-0000-0000-0000-000000000501", "authorId": creator, "authorName": "Kit Maker",
		"releaseStatus": "public", "updated_at": "2026-04-02T00:00:00Z", "created_at": "2024-01-01T00:00:00Z"} {
		if a[key] != want {
			t.Errorf("%s = %v, want %v", key, a[key], want)
		}
	}
	if a["thumbnailImageUrl"] == "" || a["imageUrl"] == "" {
		t.Errorf("images missing: %v", a)
	}
	if _, list := fetch("?search=fox&n=1"); len(list) != 1 {
		t.Errorf("n=1 returned %d", len(list))
	}
	if _, list := fetch("?search=avtr_00000000-0000-0000-0000-000000000503"); len(list) != 1 || list[0]["name"] != "Robot" {
		t.Errorf("ID search = %v", list)
	}
	if code, list := fetch("?search=%21%21"); code != 200 || list == nil || len(list) != 0 {
		t.Errorf("empty search = %d %v", code, list)
	}
	if _, list := fetch("?authorId=" + creator); len(list) != 2 {
		t.Errorf("author lookup = %v", list)
	}
	for query, want := range map[string]int{"?authorId=bob": 400, "?fileId=file_00000000-0000-0000-0000-000000000001": 404, "": 404} {
		if code, _ := fetch(query); code != want {
			t.Errorf("%q = %d, want %d", query, code, want)
		}
	}
}
