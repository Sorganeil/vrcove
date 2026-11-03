package server

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCatalogTagsCountAvatarsPerTag(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(t.TempDir(), "tags.sqlite")
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
	for i, tags := range [][]string{
		{"author_tag_Furry", "author_tag_furry", "author_tag_Face_Tracking"},
		{"author_tag_furry", "author_tag_face-tracking"},
		{"author_tag_FURRY", "author_tag_face tracking", "author_tag_robot"},
		{"author_tag_robot", "author_tag_rare", "content_horror"},
		{"author_tag_Robot"},
		{"author_tag_robot", "author_tag_rare"},
		// Adult avatars are left out, so "lewd" never reaches three.
		{"author_tag_lewd", "author_tag_robot", "content_sex"},
		{"author_tag_lewd", "content_adult"},
		{"author_tag_lewd", "author_tag_" + strings.Repeat("x", maxTagRunes+1)},
	} {
		id := "avtr_00000000-0000-0000-0000-00000000020" + string(rune('0'+i))
		data, _ := json.Marshal(map[string]any{"id": id, "name": "Avatar", "tags": tags})
		res, err := rw.Exec(`INSERT INTO catalog VALUES (?,'Avatar','Maker','2026-01-01',1,1,'Good',?)`, id, string(data))
		if err != nil {
			t.Fatal(err)
		}
		rowid, _ := res.LastInsertId()
		if _, err := rw.Exec(`INSERT INTO catalog_fts(rowid,name,author,description,tags) VALUES (?,'Avatar','Maker','',?)`, rowid, strings.Join(tags, " ")); err != nil {
			t.Fatal(err)
		}
	}
	readOnly, err := OpenCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { readOnly.Close() })
	h.srv.db = readOnly

	var body struct{ Tags []catalogTag }
	w := h.do(get("/catalog/tags"))
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []catalogTag{{"robot", 4}, {"face tracking", 3}, {"furry", 3}}
	if !reflect.DeepEqual(body.Tags, want) {
		t.Errorf("tags = %v, want %v", body.Tags, want)
	}
}
