package server

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

const shell = `<!doctype html><html><head><title>Old</title><script type="application/ld+json">{"@graph":[{"@type":"WebApplication"}]}</script></head><body><div id="root"></div></body></html>`

func contentHarness(t *testing.T) (*harness, *sql.DB) {
	h := newHarness(t)
	h.srv.static = fstest.MapFS{"index.html": {Data: []byte(shell)}}
	var path string
	if err := h.srv.db.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	rw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rw.Close() })
	return h, rw
}

func addAvatar(t *testing.T, rw *sql.DB, id, name, author, authorID, updated string, tags []string) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"id": id, "name": name, "author": author, "authorId": authorID, "description": name,
		"thumbnail": "https://api.vrchat.cloud/api/1/image/file_00000000-0000-0000-0000-000000000001/1", "tags": tags, "created": "2024-01-01T00:00:00Z"})
	res, err := rw.Exec(`INSERT INTO catalog VALUES (?,?,?,?,1,1,'Good',?)`, id, name, author, updated, string(data))
	if err != nil {
		t.Fatal(err)
	}
	rowid, _ := res.LastInsertId()
	if _, err := rw.Exec(`INSERT INTO catalog_fts(rowid,name,author,description,tags) VALUES (?,?,?,'',?)`, rowid, name, author, strings.Join(tags, " ")); err != nil {
		t.Fatal(err)
	}
}

func structured(t *testing.T, body string) map[string]any {
	t.Helper()
	m := regexp.MustCompile(`<script type="application/ld\+json">(.*?)</script>`).FindAllStringSubmatch(body, -1)
	if len(m) != 1 {
		t.Fatalf("want one structured data block, got %d", len(m))
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(m[0][1]), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAvatarPageContent(t *testing.T) {
	h, rw := contentHarness(t)
	const creator = "usr_00000000-0000-0000-0000-0000000000c1"
	main := "avtr_00000000-0000-0000-0000-000000000201"
	addAvatar(t, rw, main, `Fox <script>alert(1)</script>`, "Kit Maker", creator, "2026-01-05T00:00:00Z", []string{"author_tag_face tracking"})
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000202", "Fox Scout", "Kit Maker", creator, "2026-01-04T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000203", "Club Outfit", "Kit Maker", creator, "2026-01-03T00:00:00Z", []string{"content_sex"})

	body := h.do(httptest.NewRequest("GET", "/avatar/"+main, nil)).Body.String()
	if strings.Contains(body, "<script>alert") {
		t.Fatal("avatar name was not escaped")
	}
	for _, want := range []string{
		`<title>Fox &lt;script&gt;alert(1)&lt;/script&gt; by Kit Maker | VRChat Avatar | VRCove</title>`,
		`name="robots" content="index, follow`,
		`<h1>Fox &lt;script&gt;alert(1)&lt;/script&gt;</h1>`,
		`<dt>Platforms</dt><dd>PC and Quest</dd>`,
		`<dt>Performance rank</dt><dd>Good</dd>`,
		`href="/avatars?tag=face+tracking">face tracking</a>`,
		`More VRChat avatars by Kit Maker`,
		`href="/avatar/avtr_00000000-0000-0000-0000-000000000202">Fox Scout</a>`,
		`href="/avatars?author=Kit+Maker&amp;authorId=` + creator + `">All avatars by Kit Maker</a>`,
		`/api/image?url=`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	graph := structured(t, body)["@graph"].([]any)
	work := graph[2].(map[string]any)
	if work["@type"] != "CreativeWork" || work["author"].(map[string]any)["name"] != "Kit Maker" || work["keywords"] != "face tracking" {
		t.Errorf("structured data = %v", work)
	}
	if strings.Contains(body, "WebApplication") {
		t.Error("the shell's app description stayed on an avatar page")
	}

	// Adult avatars get a page but stay out of the index.
	adult := h.do(httptest.NewRequest("GET", "/avatar/avtr_00000000-0000-0000-0000-000000000203", nil)).Body.String()
	if !strings.Contains(adult, `name="robots" content="noindex`) {
		t.Error("adult avatar is indexable")
	}
	// Avatars missing from the catalog are not indexed either.
	missing := h.do(httptest.NewRequest("GET", "/avatar/avtr_00000000-0000-0000-0000-000000000299", nil)).Body.String()
	if !strings.Contains(missing, `name="robots" content="noindex`) || strings.Contains(missing, "<h1>") {
		t.Error("missing avatar page is indexable or has content")
	}
}

func TestAuthorPage(t *testing.T) {
	h, rw := contentHarness(t)
	const creator = "usr_00000000-0000-0000-0000-0000000000c2"
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000301", "Moth", "Wing Smith", creator, "2026-02-02T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000302", "Beetle", "Wing Smith", creator, "2026-02-01T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000303", "Lone", "Solo Maker", "usr_00000000-0000-0000-0000-0000000000c3", "2026-02-01T00:00:00Z", nil)

	body := h.do(httptest.NewRequest("GET", "/avatars?author=Wing+Smith&authorId="+creator, nil)).Body.String()
	for _, want := range []string{
		`<title>VRChat Avatars by Wing Smith | VRCove</title>`,
		`name="robots" content="index, follow`,
		`rel="canonical" href="` + origin + `/avatars?author=Wing+Smith&amp;authorId=` + creator + `"`,
		`<h1>VRChat avatars by Wing Smith</h1>`,
		`2 public avatars by Wing Smith`,
		`>Moth</a>`, `>Beetle</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Index(body, ">Moth<") > strings.Index(body, ">Beetle<") {
		t.Error("newest avatar is not first")
	}
	for path, why := range map[string]string{
		"/avatars?author=Solo+Maker&authorId=usr_00000000-0000-0000-0000-0000000000c3": "one avatar",
		"/avatars?author=Wing+Smith":                                   "no creator ID",
		"/avatars?author=Wing+Smith&authorId=" + creator + "&tag=moth": "extra filter",
		"/avatars?author=Nobody&authorId=" + creator:                   "no avatars",
	} {
		if b := h.do(httptest.NewRequest("GET", path, nil)).Body.String(); !strings.Contains(b, `name="robots" content="noindex`) {
			t.Errorf("%s (%s) is indexable", path, why)
		}
	}
}

func TestSectionPagesHaveContent(t *testing.T) {
	h, rw := contentHarness(t)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000401", "Fresh Avatar", "New Maker", "usr_00000000-0000-0000-0000-0000000000c4", "2026-03-01T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000402", "Spicy Avatar", "New Maker", "usr_00000000-0000-0000-0000-0000000000c4", "2026-03-02T00:00:00Z", []string{"content_adult"})
	home := h.do(httptest.NewRequest("GET", "/", nil)).Body.String()
	if !strings.Contains(home, "<h1>VRChat avatar search and web client</h1>") || !strings.Contains(home, ">Fresh Avatar</a> by New Maker") {
		t.Errorf("home content missing: %s", home)
	}
	if strings.Contains(home, "Spicy Avatar") {
		t.Error("adult avatar listed on the home page")
	}
	if !strings.Contains(home, "WebApplication") {
		t.Error("home lost its structured data")
	}
	if b := h.do(httptest.NewRequest("GET", "/avatars", nil)).Body.String(); !strings.Contains(b, "<h1>VRChat avatar search</h1>") || !strings.Contains(b, "paste an avatar ID") {
		t.Errorf("avatar search content missing")
	}
	// Personal pages get no server-rendered text.
	if b := h.do(httptest.NewRequest("GET", "/friends", nil)).Body.String(); !strings.Contains(b, `<div id="root"></div>`) {
		t.Error("personal page got content")
	}
}
