package history

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestFriendFeedBelongsToSelectedAccount(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	message := []byte(`{"type":"friend-online","content":{"userId":"usr_00000000-0000-0000-0000-000000000010","user":{"displayName":"Friend"},"location":"private"}}`)
	for _, owner := range []string{"alice", "bob"} {
		if _, err := s.RecordFor(ctx, owner, message, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []string{"alice", "bob"} {
		entries, err := s.FeedFor(ctx, owner, 0, "", "", 100)
		if err != nil || len(entries) != 1 {
			t.Fatalf("%s=%+v %v", owner, entries, err)
		}
	}
	if entries, err := s.FeedFor(ctx, "carol", 0, "", "", 100); err != nil || len(entries) != 0 {
		t.Fatal("another account can see the feed")
	}
}

func TestLegacyFriendFeedMigrationPreservesRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE feed (id INTEGER PRIMARY KEY,at INTEGER NOT NULL,user_id TEXT NOT NULL,display_name TEXT NOT NULL,kind TEXT NOT NULL,detail TEXT NOT NULL,previous TEXT NOT NULL DEFAULT '',world_name TEXT NOT NULL DEFAULT '');
	CREATE TABLE friends (user_id TEXT PRIMARY KEY,display_name TEXT NOT NULL,status TEXT NOT NULL,status_description TEXT NOT NULL,location TEXT NOT NULL,online INTEGER NOT NULL);
	INSERT INTO feed(at,user_id,display_name,kind,detail) VALUES(1,'friend','Old Friend','online','private');
	INSERT INTO friends VALUES('friend','Old Friend','active','','private',1);`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimLegacyFeed(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.FeedFor(context.Background(), "alice", 0, "", "", 100)
	if err != nil || len(entries) != 1 || entries[0].DisplayName != "Old Friend" {
		t.Fatalf("migration=%v %v", entries, err)
	}
	s.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.ClaimLegacyFeed(context.Background(), "bob"); err != nil {
		t.Fatal(err)
	}
	entries, err = reopened.FeedFor(context.Background(), "bob", 0, "", "", 100)
	if err != nil || len(entries) != 0 {
		t.Fatal("migration reassigned another account's feed")
	}
}
