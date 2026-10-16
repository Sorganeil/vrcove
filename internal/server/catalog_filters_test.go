package server

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"path/filepath"
	"slices"
	"testing"
)

const (
	creatorA = "usr_00000000-0000-0000-0000-00000000000a"
	creatorB = "usr_00000000-0000-0000-0000-00000000000b"
)

// seedTaggedCatalog loads avatars with creators, tags and ratings.
func seedTaggedCatalog(t *testing.T, h *harness) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tagged.sqlite")
	db, err := OpenCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	rw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	for i, a := range []struct {
		name, author, authorID, rating string
		tags                           []string
	}{
		{"Fox Scout", "Kit Maker", creatorA, "Excellent", []string{"author_tag_furry", "author_tag_face tracking"}},
		{"Night Terror", "Kit Maker", creatorA, "Good", []string{"content_horror", "content_gore"}},
		{"Club Outfit", "Other Kit Maker", creatorB, "Medium", []string{"content_sex"}},
		{"Quest Backup", "Kit Maker", creatorB, "Poor", []string{"author_quest_fallback"}},
		{"Plain Robot", "Zed", creatorB, "VeryPoor", nil},
	} {
		id := "avtr_00000000-0000-0000-0000-00000000010" + string(rune('0'+i))
		data, _ := json.Marshal(map[string]any{"id": id, "name": a.name, "author": a.author, "authorId": a.authorID, "tags": a.tags})
		res, err := rw.Exec(`INSERT INTO catalog VALUES (?,?,?,?,1,1,?,?)`, id, a.name, a.author, "2026-01-0"+string(rune('1'+i)), a.rating, string(data))
		if err != nil {
			t.Fatal(err)
		}
		rowid, _ := res.LastInsertId()
		tags := ""
		for _, tag := range a.tags {
			tags += tag + " "
		}
		if _, err := rw.Exec(`INSERT INTO catalog_fts(rowid,name,author,description,tags) VALUES (?,?,?,'',?)`, rowid, a.name, a.author, tags); err != nil {
			t.Fatal(err)
		}
	}
	readOnly, err := OpenCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { readOnly.Close() })
	h.srv.db = readOnly
}

func catalogNames(t *testing.T, h *harness, params url.Values) []string {
	t.Helper()
	body := decode(t, h.do(get("/catalog?"+params.Encode()))).(map[string]any)
	var names []string
	for _, a := range body["avatars"].([]any) {
		names = append(names, a.(map[string]any)["name"].(string))
	}
	if total := int(body["total"].(float64)); total != len(names) {
		t.Errorf("%v: total %d for %d results", params, total, len(names))
	}
	slices.Sort(names)
	return names
}

func TestCatalogFilters(t *testing.T) {
	h := newHarness(t)
	seedTaggedCatalog(t, h)
	for _, tc := range []struct {
		params url.Values
		want   []string
	}{
		// Exact creator, not creators whose name merely contains it.
		{url.Values{"author": {"kit maker"}}, []string{"Fox Scout", "Night Terror", "Quest Backup"}},
		{url.Values{"author": {"Kit Maker"}, "authorId": {creatorA}}, []string{"Fox Scout", "Night Terror"}},
		{url.Values{"author": {"Nobody"}}, nil},
		{url.Values{"tag": {"face track"}}, []string{"Fox Scout"}},
		{url.Values{"tag": {"furry"}}, []string{"Fox Scout"}},
		{url.Values{"hide": {"adult"}}, []string{"Fox Scout", "Night Terror", "Plain Robot", "Quest Backup"}},
		{url.Values{"hide": {"adult,gore,horror"}}, []string{"Fox Scout", "Plain Robot", "Quest Backup"}},
		{url.Values{"fallback": {"1"}}, []string{"Quest Backup"}},
		{url.Values{"rating": {"Good+"}}, []string{"Fox Scout", "Night Terror"}},
		{url.Values{"rating": {"Poor+"}}, []string{"Club Outfit", "Fox Scout", "Night Terror", "Quest Backup"}},
		{url.Values{"rating": {"Medium"}}, []string{"Club Outfit"}},
		{url.Values{"author": {"Kit Maker"}, "hide": {"horror"}, "rating": {"Excellent+"}}, []string{"Fox Scout"}},
		// Query syntax in user input is treated as words, never as FTS operators.
		{url.Values{"tag": {`furry" OR "content`}}, nil},
		{url.Values{"author": {`*`}}, nil},
		{url.Values{"rating": {"Bogus+"}}, []string{"Club Outfit", "Fox Scout", "Night Terror", "Plain Robot", "Quest Backup"}},
	} {
		if got := catalogNames(t, h, tc.params); !slices.Equal(got, tc.want) {
			t.Errorf("%v = %v, want %v", tc.params, got, tc.want)
		}
	}
}

func TestCatalogSorts(t *testing.T) {
	h := newHarness(t)
	seedTaggedCatalog(t, h)
	order := func(sort string) []string {
		body := decode(t, h.do(get("/catalog?sort="+sort))).(map[string]any)
		var names []string
		for _, a := range body["avatars"].([]any) {
			names = append(names, a.(map[string]any)["name"].(string))
		}
		return names
	}
	if got := order("name-desc"); got[0] != "Quest Backup" || got[len(got)-1] != "Club Outfit" {
		t.Errorf("name-desc = %v", got)
	}
	// Creator A–Z, then name within a creator.
	if got := order("author"); !slices.Equal(got, []string{"Fox Scout", "Night Terror", "Quest Backup", "Club Outfit", "Plain Robot"}) {
		t.Errorf("author = %v", got)
	}
}
