package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func covePreviewPage(h *harness, path string) string {
	h.srv.static = fstest.MapFS{"index.html": {Data: []byte("<!doctype html><html><head><title>VRCove</title></head><body>app</body></html>")}}
	return h.do(httptest.NewRequest("GET", "http://localhost"+path, nil)).Body.String()
}

func TestProfilePreviews(t *testing.T) {
	h, _ := newAccountHarness(t)
	owner := h.signup("embed_owner", "a synthetic long passphrase")
	h.do(as(post("/cove/profile", jsonBody(`{"bio":"Collects <cozy> worlds."}`)), owner))

	private := covePreviewPage(h, "/cove/embed_owner")
	// Only the requested URL may echo the name, as it would for a missing profile.
	if strings.Contains(private, "embed_owner ·") || strings.Contains(private, "embed_owner |") || strings.Contains(private, "cozy") || !strings.Contains(private, `content="VRCove profile"`) {
		t.Fatalf("private profile leaked: %s", private)
	}
	if missing := covePreviewPage(h, "/cove/nobody_here"); !strings.Contains(missing, `content="VRCove profile"`) {
		t.Fatalf("missing profile: %s", missing)
	}

	h.do(as(post("/cove/profile", jsonBody(`{"public":true}`)), owner))
	public := covePreviewPage(h, "/cove/EMBED_OWNER")
	for _, want := range []string{`property="og:title" content="embed_owner · VRCove"`, `Collects &lt;cozy&gt; worlds.`,
		`<title>embed_owner | VRCove</title>`, `content="noindex`, `rel="canonical" href="` + origin + `/cove/embed_owner"`} {
		if !strings.Contains(public, want) {
			t.Errorf("public profile missing %q: %s", want, public)
		}
	}
}

func TestCollectionPreviews(t *testing.T) {
	h, _ := newAccountHarness(t)
	owner := h.linkedSignup("embed_curator", "a synthetic long passphrase")
	id := decode(t, h.do(as(post("/cove/collections/create", jsonBody(`{"kind":"world","name":"Late night worlds"}`)), owner))).(map[string]any)["id"].(string)
	if code := h.do(as(post("/cove/collections/add", jsonBody(`{"id":"`+id+`","target":"`+testWorld+`","name":"Cabin","image":"`+testImage+`"}`)), owner)).Code; code != 200 {
		t.Fatalf("add = %d", code)
	}

	if private := covePreviewPage(h, "/collection/"+id); strings.Contains(private, "Late night") || !strings.Contains(private, `content="VRCove collection"`) {
		t.Fatalf("private collection leaked: %s", private)
	}

	h.do(as(post("/cove/collections/update", jsonBody(`{"id":"`+id+`","name":"Late night worlds","public":true}`)), owner))
	shared := covePreviewPage(h, "/collection/"+id)
	for _, want := range []string{`property="og:title" content="Late night worlds · VRCove"`, "A collection of 1 world by a VRCove user on VRCove.",
		`/api/image?url=`, `content="noindex`} {
		if !strings.Contains(shared, want) {
			t.Errorf("shared collection missing %q: %s", want, shared)
		}
	}
	if strings.Contains(shared, "embed_curator") {
		t.Fatal("private owner named in preview")
	}

	h.do(as(post("/cove/profile", jsonBody(`{"public":true}`)), owner))
	if named := covePreviewPage(h, "/collection/"+id); !strings.Contains(named, "by embed_curator on VRCove.") {
		t.Fatalf("public owner not named: %s", named)
	}
}
