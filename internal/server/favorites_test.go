package server

import (
	"net/http"
	"testing"
)

func TestFavoriteTypes(t *testing.T) {
	for _, tc := range []struct{ query, kind string }{{"", "avatar"}, {"?type=avatar&offset=100", "avatar"}, {"?type=world&offset=100", "world"}, {"?type=friend", "friend"}} {
		t.Run(tc.query, func(t *testing.T) {
			h := newConnectedHarness(t)
			offset := "0"
			if tc.query != "" && tc.kind != "friend" {
				offset = "100"
			}
			w := h.signedIn(get("/account/favorites"+tc.query), "/favorites?type="+tc.kind+"&n=100&offset="+offset, `[]`)
			decode(t, w)
		})
	}
	h := newConnectedHarness(t)
	w := h.signedIn(get("/account/favorites?type=user"), "", `[]`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	for query, kind := range map[string]string{"": "avatar", "?type=friend": "friend", "?type=other": "avatar"} {
		h := newConnectedHarness(t)
		decode(t, h.signedIn(get("/account/favorite-groups"+query), "/favorite/groups?type="+kind+"&n=100", `[]`))
	}
}
