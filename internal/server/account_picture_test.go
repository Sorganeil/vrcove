package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

func pictureFixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 48, 24))
	for y := range 24 {
		for x := range 48 {
			img.Set(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 8), B: 90, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAccountPictures(t *testing.T) {
	h, _ := newAccountHarness(t)
	a := h.signup("picture_owner", "synthetic passphrase")
	other := h.signup("other_person", "synthetic passphrase")
	raw := pictureFixture(t)
	if w := h.do(post("/veil/picture", bytes.NewReader(raw))); w.Code != 401 {
		t.Errorf("anonymous upload = %d", w.Code)
	}
	w := h.do(as(post("/veil/picture", bytes.NewReader(raw)), a))
	me := decode(t, w).(map[string]any)
	picture := me["picture"].(string)
	if !strings.HasPrefix(picture, "/api/veil/picture?username=picture_owner&v=") {
		t.Fatalf("picture URL = %q", picture)
	}
	path := strings.TrimPrefix(picture, "/api")
	for _, r := range []*http.Request{get(path), as(get(path), other)} {
		if got := h.do(r); got.Code != 404 {
			t.Errorf("private picture = %d", got.Code)
		}
	}
	got := h.do(as(get(path), a))
	if got.Code != 200 || got.Header().Get("Content-Type") != "image/png" || got.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("owner picture = %d %v", got.Code, got.Header())
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(got.Body.Bytes()))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 {
		t.Fatalf("normalized image = %+v %v", cfg, err)
	}
	profile := decode(t, h.do(as(get("/veil/profile?username=picture_owner"), a))).(map[string]any)
	if profile["picture"] != picture {
		t.Fatal("profile missing picture")
	}
	h.do(as(post("/veil/profile", jsonBody(`{"public":true}`)), a))
	if got := h.do(get(path)); got.Code != 200 {
		t.Errorf("public picture = %d", got.Code)
	}
	h.do(as(post("/veil/profile", jsonBody(`{"public":false}`)), a))
	if got := h.do(get(path)); got.Code != 404 {
		t.Errorf("picture visible after making profile private: %d", got.Code)
	}
	removed := decode(t, h.do(as(post("/veil/picture/remove", jsonBody(`{}`)), a))).(map[string]any)
	if removed["picture"] != "" {
		t.Error("removed picture still returned")
	}
	if got := h.do(as(get(path), a)); got.Code != 404 {
		t.Errorf("removed picture = %d", got.Code)
	}
	h.do(as(post("/veil/picture", bytes.NewReader(raw)), a))
	if got := h.do(as(post("/veil/delete", jsonBody(`{"password":"synthetic passphrase"}`)), a)); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	var count int
	if err := h.srv.accounts.QueryRow("SELECT count(*) FROM account_pictures").Scan(&count); err != nil || count != 0 {
		t.Fatalf("pictures did not cascade on deletion: %d %v", count, err)
	}
	if h.fetches.Load() != 0 {
		t.Fatal("account pictures contacted VRChat")
	}
}

func TestAccountPictureValidation(t *testing.T) {
	h, _ := newAccountHarness(t)
	a := h.signup("picture_owner", "synthetic passphrase")
	for _, data := range [][]byte{nil, []byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>"), []byte("not an image")} {
		if w := h.do(as(post("/veil/picture", bytes.NewReader(data)), a)); w.Code != 400 {
			t.Errorf("invalid picture = %d", w.Code)
		}
	}
	r := as(post("/veil/picture", strings.NewReader(strings.Repeat("x", int(maxAccountPictureBytes)+1))), a)
	r.ContentLength = -1
	if w := h.do(r); w.Code != 413 {
		t.Errorf("oversized streamed picture = %d", w.Code)
	}
	r = as(post("/veil/picture", bytes.NewReader(pictureFixture(t)), "Origin", "https://other.example"), a)
	if w := h.do(r); w.Code != 403 {
		t.Errorf("cross-origin picture = %d", w.Code)
	}
	// A syntactically valid but excessive image header is rejected before full decode.
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, 4097, 1)))
	if w := h.do(as(post("/veil/picture", bytes.NewReader(b.Bytes())), a)); w.Code != 400 {
		t.Errorf("oversized dimensions = %d", w.Code)
	}
}
