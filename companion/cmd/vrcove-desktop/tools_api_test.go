package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDesktopChatboxLifecycle(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	var packets atomic.Int32
	d.tools.sender.Write = func(int, []byte) error { packets.Add(1); return nil }
	body := `{"template":"{time} · {status}","status":"AFK","port":9000,"interval":5,"action":"start"}`
	if w := do(d, "POST", "/api/desktop/tools/chatbox", body); w.Code != 200 || !strings.Contains(w.Body.String(), `"automatic":true`) {
		t.Fatalf("start=%d %s", w.Code, w.Body)
	}
	if packets.Load() != 1 {
		t.Fatalf("sent=%d", packets.Load())
	}
	// Opening the app again restores settings but never resumes broadcasting.
	reopened := newTestDesktop(t, site)
	reopened.store.data.Chatbox = d.store.data.Chatbox
	fresh, err := newDesktop(reopened.store, site, reopened.assets)
	if err != nil {
		t.Fatal(err)
	}
	w := do(fresh, "GET", "/api/desktop/tools/chatbox", "")
	if !strings.Contains(w.Body.String(), `"automatic":false`) || !strings.Contains(w.Body.String(), `"status":"AFK"`) {
		t.Fatalf("reopen=%s", w.Body)
	}
	// A second send cannot bypass the 5-second limit through the HTTP route.
	if w := do(d, "POST", "/api/desktop/tools/chatbox", body); w.Code != 400 || packets.Load() != 1 {
		t.Errorf("rate limit=%d %s", w.Code, w.Body)
	}
	if w := do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"stop"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"automatic":false`) {
		t.Errorf("stop=%s", w.Body)
	}
	for _, bad := range []string{`{"action":"send","port":80,"interval":5}`, `{"action":"start","port":9000,"interval":0}`, `{"action":"bogus","port":9000,"interval":5}`} {
		if w := do(d, "POST", "/api/desktop/tools/chatbox", bad); w.Code != 400 {
			t.Errorf("bad settings=%d", w.Code)
		}
	}
	// The background loop sends while the page is closed, and stops on quit.
	d.tools.mu.Lock()
	d.tools.automatic = true
	d.tools.lastAttempt = time.Now().Add(-time.Hour)
	d.tools.sender.Now = func() time.Time { return time.Now().Add(time.Hour) }
	d.tools.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.runChatbox(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(3 * time.Second)
	for packets.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if packets.Load() != 2 {
		t.Fatal("background sender did not send")
	}
	do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"stop"}`)
}

func TestDesktopExtractionUsesOnlyNativeSelections(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	d.choosePackage = func() (string, error) { return "", nil }
	d.chooseFolder = func() (string, error) { t.Fatal("folder picker after cancellation"); return "", nil }
	if w := do(d, "POST", "/api/desktop/tools/extract", `{"metadata":true}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"cancelled":true`) {
		t.Fatalf("cancel=%d %s", w.Code, w.Body)
	}
	source := filepath.Join(t.TempDir(), "example.unitypackage")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, data := range map[string]string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/pathname": "Assets/file.txt", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/asset": "original content"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0600}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(data))
	}
	tw.Close()
	gz.Close()
	f.Close()
	destination := t.TempDir()
	d.choosePackage = func() (string, error) { return source, nil }
	d.chooseFolder = func() (string, error) { return destination, nil }
	// Paths in the request cannot override the dialog selections.
	w := do(d, "POST", "/api/desktop/tools/extract", `{"metadata":true,"source":"/etc/passwd","destination":"/"}`)
	if w.Code != 200 {
		t.Fatalf("extract=%d %s", w.Code, w.Body)
	}
	var got struct {
		Folder string
		Files  int
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	b, err := os.ReadFile(filepath.Join(got.Folder, "Assets/file.txt"))
	if err != nil || string(b) != "original content" || got.Files != 1 {
		t.Fatalf("output=%s %v %+v", b, err, got)
	}
	if w := do(d, "GET", "/api/desktop/tools/extract", ""); w.Code != 405 {
		t.Error("GET opened dialogs")
	}
}
