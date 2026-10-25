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

func TestCatalogRelevanceRatingAndAddedSorts(t *testing.T) {
	h, rw := contentHarness(t)
	const creator = "usr_00000000-0000-0000-0000-0000000000a9"
	for _, a := range []struct{ id, name, author, updated string }{
		{"avtr_00000000-0000-0000-0000-000000000901", "Fox", "Maker", "2026-01-01T00:00:00Z"},
		{"avtr_00000000-0000-0000-0000-000000000902", "Arctic Fox", "Maker", "2026-01-05T00:00:00Z"},
		{"avtr_00000000-0000-0000-0000-000000000903", "Foxglove Witch", "Maker", "2026-01-04T00:00:00Z"},
		{"avtr_00000000-0000-0000-0000-000000000904", "Kit", "Fox", "2026-01-06T00:00:00Z"},
		{"avtr_00000000-0000-0000-0000-000000000905", "Snow Fox Kit", "Maker", "2026-01-03T00:00:00Z"},
		{"avtr_00000000-0000-0000-0000-000000000906", "Girl Protogen", "Maker", "2025-01-01T00:00:00Z"},
	} {
		addAvatar(t, rw, a.id, a.name, a.author, creator, a.updated, nil)
	}
	// Matches only through its tags, so it ranks below names with every word.
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000907", "Protogen", "Maker", creator, "2026-02-01T00:00:00Z", []string{"author_tag_girl"})
	for id, rating := range map[string]string{"avtr_00000000-0000-0000-0000-000000000902": "Excellent", "avtr_00000000-0000-0000-0000-000000000905": "VeryPoor", "avtr_00000000-0000-0000-0000-000000000903": "Unknown"} {
		if _, err := rw.Exec("UPDATE catalog SET rating = ? WHERE id = ?", rating, id); err != nil {
			t.Fatal(err)
		}
	}
	names := func(query string) []string {
		body := decode(t, h.do(get("/catalog?"+query))).(map[string]any)
		var out []string
		for _, a := range body["avatars"].([]any) {
			out = append(out, a.(map[string]any)["name"].(string))
		}
		return out
	}
	if got := names("q=fox&sort=relevance"); !slices.Equal(got, []string{"Fox", "Foxglove Witch", "Arctic Fox", "Snow Fox Kit", "Kit"}) {
		t.Errorf("best match = %v", got)
	}
	if got := names("q=fox"); !slices.Equal(got, []string{"Kit", "Arctic Fox", "Foxglove Witch", "Snow Fox Kit", "Fox"}) {
		t.Errorf("newest = %v", got)
	}
	if got := names("q=protogen+girl&sort=relevance"); !slices.Equal(got, []string{"Girl Protogen", "Protogen"}) {
		t.Errorf("best match with two words = %v", got)
	}
	// LIKE wildcards in a search are literal text.
	if got := names("q=fox%25&sort=relevance"); len(got) != 5 {
		t.Errorf("search with %% = %v", got)
	}
	// Best match without a search falls back to the newest first.
	if got := names("sort=relevance"); got[0] != "Protogen" {
		t.Errorf("best match without search = %v", got)
	}
	if got := names("sort=rating"); got[0] != "Arctic Fox" || got[len(got)-2] != "Snow Fox Kit" || got[len(got)-1] != "Foxglove Witch" {
		t.Errorf("best performance = %v", got)
	}
	if got := names("sort=added"); got[0] != "Protogen" || got[len(got)-1] != "Fox" {
		t.Errorf("recently added = %v", got)
	}
}

func TestCatalogByCreatorIDAlone(t *testing.T) {
	h, rw := contentHarness(t)
	const creator = "usr_00000000-0000-0000-0000-0000000000b7"
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000a01", "First", "Old Name", creator, "2026-01-01T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000a02", "Second", "New Name", creator, "2026-01-02T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000a03", "Other", "Old Name", "usr_00000000-0000-0000-0000-0000000000b8", "2026-01-03T00:00:00Z", nil)
	body := decode(t, h.do(get("/catalog?authorId="+creator))).(map[string]any)
	var names []string
	for _, a := range body["avatars"].([]any) {
		names = append(names, a.(map[string]any)["name"].(string))
	}
	// Both upload names count; another creator with the same name does not.
	if !slices.Equal(names, []string{"Second", "First"}) || body["total"] != 2.0 {
		t.Errorf("by creator ID = %v (%v)", names, body["total"])
	}
	if total := decode(t, h.do(get("/catalog?authorId=bogus"))).(map[string]any)["total"]; total != 3.0 {
		t.Errorf("invalid creator ID should be ignored, total = %v", total)
	}
}

func TestCatalogByImageFile(t *testing.T) {
	h, rw := contentHarness(t)
	const creator = "usr_00000000-0000-0000-0000-0000000000c9"
	// addAvatar gives every avatar the same picture, so change two of them.
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000b01", "Worn", "Maker", creator, "2026-01-01T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000b02", "Copy", "Copier", creator, "2026-01-02T00:00:00Z", nil)
	addAvatar(t, rw, "avtr_00000000-0000-0000-0000-000000000b03", "Other", "Maker", creator, "2026-01-03T00:00:00Z", nil)
	const file = "file_00000000-0000-0000-0000-0000000000f1"
	for _, id := range []string{"avtr_00000000-0000-0000-0000-000000000b01", "avtr_00000000-0000-0000-0000-000000000b02"} {
		if _, err := rw.Exec(`UPDATE catalog SET data = json_set(data, '$.image', ?) WHERE id = ?`,
			"https://api.vrchat.cloud/api/1/file/"+file+"/3/file", id); err != nil {
			t.Fatal(err)
		}
	}
	body := decode(t, h.do(get("/catalog?imageFile="+file))).(map[string]any)
	var names []string
	for _, a := range body["avatars"].([]any) {
		names = append(names, a.(map[string]any)["name"].(string))
	}
	if !slices.Equal(names, []string{"Copy", "Worn"}) {
		t.Errorf("by picture = %v", names)
	}
	if total := decode(t, h.do(get("/catalog?imageFile=file_bogus"))).(map[string]any)["total"]; total != 3.0 {
		t.Errorf("invalid file ID should be ignored, total = %v", total)
	}
}
