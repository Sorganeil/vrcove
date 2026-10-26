package history

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	mia = "usr_00000000-0000-0000-0000-000000000002"
	zoe = "usr_00000000-0000-0000-0000-000000000003"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func ms(t *testing.T, value string) int64 {
	at, err := time.ParseInLocation("2006.01.02 15:04:05", value, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return at.UnixMilli()
}

func TestGameLogHistory(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "output_log_2026-10-25_18-00-00.txt")
	first := "2026.10.25 18:00:01 Log        -  [Behaviour] Entering Room: Cozy Room\n" +
		"2026.10.25 18:00:01 Log        -  [Behaviour] Joining wrld_00000000-0000-0000-0000-000000000009:12345~private(usr_x)\n" +
		"2026.10.25 18:00:02 Log        -  [Behaviour] Joining or Creating Room: Cozy Room\n" +
		"2026.10.25 18:00:03 Log        -  [Behaviour] OnPlayerJoined Mia (" + mia + ")\n" +
		"2026.10.25 18:00:04 Debug      -  unrelated noise\n" +
		"2026.10.25 18:00:05 Log        -  [Behaviour] Switching Mia to avatar Robot Fox\n" +
		"2026.10.25 18:00:06 Log        -  [Behaviour] OnPlayerJoined Zoe (" + zoe + ")\n" +
		"2026.10.25 18:00:07 Log        -  [Behaviour] OnPlayerLeft Zoe (" + zoe + ")\n" +
		"2026.10.25 18:00:08 Log        -  [Behaviour] OnPlayerJo" // still being written
	os.WriteFile(log, []byte(first), 0o600)
	if err := s.ReadLogs(ctx, dir); err != nil {
		t.Fatal(err)
	}
	visits, err := s.Visits(ctx, 0, "", 10)
	if err != nil || len(visits) != 1 {
		t.Fatalf("visits = %v %v", visits, err)
	}
	v := visits[0]
	if v.WorldName != "Cozy Room" || v.WorldID != "wrld_00000000-0000-0000-0000-000000000009" || v.Left != 0 || len(v.People) != 2 {
		t.Fatalf("visit = %+v", v)
	}
	if v.People[0].UserID != mia || v.People[0].Avatar != "Robot Fox" || v.People[1].Left != ms(t, "2026.10.25 18:00:07") {
		t.Fatalf("people = %+v", v.People)
	}

	// The rest of the file arrives later; a second pass reads only new lines.
	os.WriteFile(log, []byte(first+"ined Zoe ("+zoe+")\n2026.10.25 18:10:00 Log        -  [Behaviour] OnLeftRoom\n"), 0o600)
	if err := s.ReadLogs(ctx, dir); err != nil {
		t.Fatal(err)
	}
	visits, _ = s.Visits(ctx, 0, "", 10)
	if len(visits) != 1 || len(visits[0].People) != 3 || visits[0].Left != ms(t, "2026.10.25 18:10:00") {
		t.Fatalf("after second pass = %+v", visits)
	}
	if found, _ := s.Visits(ctx, 0, "zoe", 10); len(found) != 1 {
		t.Fatal("search by person found nothing")
	}
	if found, _ := s.Visits(ctx, 0, "nowhere", 10); len(found) != 0 {
		t.Fatal("search matched an unrelated visit")
	}

	// A screenshot taken during the visit shows the world and who was there.
	pictures := t.TempDir()
	os.MkdirAll(filepath.Join(pictures, "2026-10"), 0o700)
	os.WriteFile(filepath.Join(pictures, "2026-10", "VRChat_2026-10-25_18-00-06.512_1920x1080.png"), []byte("png"), 0o600)
	os.WriteFile(filepath.Join(pictures, "2026-10", "notes.txt"), []byte("x"), 0o600)
	shots, err := s.Screenshots(ctx, pictures, "2026-10")
	if err != nil || len(shots) != 1 || shots[0].WorldName != "Cozy Room" || shots[0].Width != 1920 {
		t.Fatalf("screenshots = %+v %v", shots, err)
	}
	if names := []string{shots[0].People[0].DisplayName, shots[0].People[1].DisplayName}; names[0] != "Mia" || names[1] != "Zoe" {
		t.Fatalf("people in screenshot = %v", names)
	}
	if got := Months(pictures); len(got) != 1 || got[0] != "2026-10" {
		t.Fatalf("months = %v", got)
	}
	for _, bad := range []string{"../secret.png", "2026-10/../../x.png", "2026-10/notes.txt", "2026-10/.hidden.png"} {
		if _, ok := ScreenshotPath(pictures, bad); ok {
			t.Errorf("ScreenshotPath accepted %q", bad)
		}
	}
}

func TestFolders(t *testing.T) {
	analytics := filepath.Join("/home", "me", "pfx", "drive_c", "users", "steamuser", "AppData", "Local", "Temp", "VRChat", "VRChat")
	if got := LogFolder(analytics); got != filepath.Join("/home", "me", "pfx", "drive_c", "users", "steamuser", "AppData", "LocalLow", "VRChat", "VRChat") {
		t.Errorf("LogFolder = %s", got)
	}
	if got := ScreenshotFolder(analytics); got != filepath.Join("/home", "me", "pfx", "drive_c", "users", "steamuser", "Pictures", "VRChat") {
		t.Errorf("ScreenshotFolder = %s", got)
	}
}

func TestFriendFeed(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	now := time.Now().Add(-time.Hour)
	record := func(message string) *Note {
		t.Helper()
		now = now.Add(time.Second)
		note, err := s.Record(ctx, []byte(message), now)
		if err != nil {
			t.Fatal(err)
		}
		return note
	}
	record(`{"type":"friend-online","content":"{\"userId\":\"` + mia + `\",\"location\":\"private\",\"user\":{\"displayName\":\"Mia\",\"status\":\"active\",\"statusDescription\":\"\"}}"}`)
	record(`{"type":"friend-location","content":{"userId":"` + mia + `","location":"traveling","user":{"displayName":"Mia","status":"active"}}}`)
	record(`{"type":"friend-location","content":{"userId":"` + mia + `","location":"wrld_00000000-0000-0000-0000-000000000009:1","world":{"name":"Cozy Room"},"user":{"displayName":"Mia","status":"active"}}}`)
	record(`{"type":"friend-update","content":{"userId":"` + mia + `","user":{"displayName":"Mia","status":"busy","statusDescription":"working"}}}`)
	record(`{"type":"friend-offline","content":{"userId":"` + mia + `"}}`)
	record(`{"type":"friend-offline","content":{"userId":"` + mia + `"}}`) // repeated: no new entry
	record(`{"type":"friend-online","content":{"userId":"not-a-user"}}`)
	entries, err := s.Feed(ctx, 0, "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range entries {
		kinds = append(kinds, e.Kind)
	}
	if want := []string{"offline", "status", "location", "online"}; len(kinds) != len(want) || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] || kinds[3] != want[3] {
		t.Fatalf("feed kinds = %v", kinds)
	}
	if loc := entries[2]; loc.WorldName != "Cozy Room" || loc.Previous != "private" || loc.DisplayName != "Mia" {
		t.Fatalf("location entry = %+v", loc)
	}
	if st := entries[1]; st.Detail != "busy working" || st.Previous != "active" {
		t.Fatalf("status entry = %+v", st)
	}
	if only, _ := s.Feed(ctx, 0, "location", "", 50); len(only) != 1 {
		t.Fatalf("location filter = %v", only)
	}

	note := record(`{"type":"notification","content":"{\"id\":\"not_0123456789abcdef\",\"type\":\"invite\",\"senderUsername\":\"Mia\",\"details\":\"{\\\"worldName\\\":\\\"Cozy Room\\\"}\"}"}`)
	if note == nil || note.WorldName != "Cozy Room" || note.Type != "invite" {
		t.Fatalf("note = %+v", note)
	}

	// An offline event for a friend whose name was not yet in the friends table
	// is backfilled as soon as RememberUsers or a later event supplies the name.
	record(`{"type":"friend-offline","content":{"userId":"` + zoe + `"}}`)
	if err := s.RememberUsers(ctx, "", map[string]string{zoe: "Zoe"}); err != nil {
		t.Fatal(err)
	}
	entries, err = s.Feed(ctx, 0, "offline", "", 50)
	if err != nil || len(entries) < 1 || entries[0].DisplayName != "Zoe" {
		t.Fatalf("resolved offline entry = %+v %v", entries, err)
	}
}

func TestGameLogTimeline(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "output_log_2026-10-25_18-00-00.txt"), []byte(
		"2026.10.25 18:00:01 Log        -  [Behaviour] Joining wrld_00000000-0000-0000-0000-000000000009:1~private(usr_x)\n"+
			"2026.10.25 18:00:02 Log        -  [Behaviour] Joining or Creating Room: Cozy Room\n"+
			"2026.10.25 18:00:03 Log        -  [Behaviour] OnPlayerJoined Mia ("+mia+")\n"+
			"2026.10.25 18:00:04 Log        -  [Behaviour] Switching Mia to avatar Robot Fox\n"+
			"2026.10.25 18:00:05 Log        -  [Video Playback] Attempting to resolve URL 'https://example.com/v'\n"+
			"2026.10.25 18:00:06 Log        -  [Behaviour] OnPlayerLeft Mia ("+mia+")\n"+
			"2026.10.25 18:00:09 Log        -  [Behaviour] OnPlayerJoined Zoe ("+zoe+")\n"+
			"2026.10.25 18:00:10 Log        -  [Behaviour] OnLeftRoom\n"), 0o600)
	if err := s.ReadLogs(ctx, dir); err != nil {
		t.Fatal(err)
	}
	all, err := s.GameLog(ctx, 0, "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range all {
		kinds = append(kinds, e.Kind)
	}
	// Zoe was still there when you left, so her leave is not a separate event.
	if got, want := strings.Join(kinds, ","), "join,leave,video,avatar,join,location"; got != want {
		t.Fatalf("timeline = %s, want %s", got, want)
	}
	if all[2].Detail != "https://example.com/v" || all[3].Detail != "Robot Fox" || all[5].WorldName != "Cozy Room" {
		t.Fatalf("entries = %+v", all)
	}
	if only, _ := s.GameLog(ctx, 0, "join, leave", "", 50); len(only) != 3 {
		t.Fatalf("join and leave filter = %+v", only)
	}
	if found, _ := s.GameLog(ctx, 0, "", "robot", 50); len(found) != 1 || found[0].Kind != "avatar" {
		t.Fatalf("search = %+v", found)
	}
	if older, _ := s.GameLog(ctx, all[2].At, "", "", 50); len(older) != 3 {
		t.Fatalf("paging = %+v", older)
	}
}

func TestDatabaseIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps AppData private per user instead of using file modes.")
	}
	path := filepath.Join(t.TempDir(), "history.sqlite")
	// Earlier versions left the database readable by other users.
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		os.WriteFile(name, nil, 0o644)
	}
	os.Remove(path)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`INSERT INTO log_files (name, offset) VALUES ('x', 1)`)
	s.Close()
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if info, err := os.Stat(name); err == nil && info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v", filepath.Base(name), info.Mode().Perm())
		}
	}
	if info, _ := os.Stat(path); info == nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %v", info)
	}
}
