package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vrcove/companion/internal/chatbox"
	"vrcove/companion/internal/osc"
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

func TestChatboxActions(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	var packets []string
	d.tools.sender.Write = func(_ int, b []byte) error { packets = append(packets, string(b)); return nil }
	if w := do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"typing","typing":true}`); w.Code != 200 || !strings.Contains(packets[0], "/chatbox/typing") {
		t.Fatalf("typing=%d %q", w.Code, packets)
	}
	if w := do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"say","text":"Hello there","open":true}`); w.Code != 200 || !strings.Contains(packets[1], ",sFF") {
		t.Fatalf("keyboard=%d %q", w.Code, packets)
	}
	if w := do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"say","text":"Too soon"}`); w.Code != 400 {
		t.Fatalf("rate limit=%d", w.Code)
	}
	if w := do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"clear"}`); w.Code != 200 {
		t.Fatalf("clear=%d", w.Code)
	}
	w := do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"phrases","phrases":["brb","", "  ","hello"]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"phrases":["brb","hello"]`) || len(d.store.data.Chatbox.Phrases) != 2 {
		t.Fatalf("phrases=%d %s", w.Code, w.Body)
	}
	tooMany := `["` + strings.Repeat(`a","`, maxChatList) + `a"]`
	if w := do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"phrases","phrases":`+tooMany+`}`); w.Code != 400 {
		t.Fatalf("too many phrases=%d", w.Code)
	}
	if w := do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"save","port":9000,"interval":5,"rotation":`+tooMany+`}`); w.Code != 400 {
		t.Fatalf("too many rotating messages=%d", w.Code)
	}
}

func TestChatboxRotatesAndUsesSavedPhrases(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	now := time.Now()
	d.tools.sender.Now = func() time.Time { return now }
	var packets []string
	d.tools.sender.Write = func(_ int, b []byte) error { packets = append(packets, string(b)); return nil }
	body := `{"action":"send","template":"{rotate}","port":9000,"interval":5,"rotation":["First {date}","Second",""]}`
	for range 3 {
		if w := do(d, "POST", "/api/desktop/tools/chatbox", body); w.Code != 200 {
			t.Fatalf("send=%d %s", w.Code, w.Body)
		}
		now = now.Add(chatbox.MinInterval)
	}
	if !strings.Contains(packets[0], "First "+now.Format("2006")) || !strings.Contains(packets[1], "Second") || !strings.Contains(packets[2], "First") {
		t.Fatalf("packets=%q", packets)
	}
}

func TestRotateLineDisappearsWithoutMessages(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	d.tools.mu.Lock()
	defer d.tools.mu.Unlock()
	if got := d.renderChat(context.Background(), chatPrefs{Template: "Kept\n{rotate}"}, false); got != "Kept" {
		t.Fatalf("message=%q", got)
	}
}

func TestAutomaticMessagesSurviveRoutineGaps(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	var packets atomic.Int32
	d.tools.sender.Write = func(int, []byte) error { packets.Add(1); return nil }
	// Nothing is playing, so there's nothing to send.
	d.tools.prefs = chatPrefs{Template: "{media}", Port: 9000, Interval: 5}
	d.tools.automatic = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.runChatbox(ctx); close(done) }()
	time.Sleep(1200 * time.Millisecond)
	cancel()
	<-done
	if packets.Load() != 0 || !d.tools.automatic || d.tools.lastError != "" {
		t.Fatalf("packets=%d automatic=%v error=%q", packets.Load(), d.tools.automatic, d.tools.lastError)
	}
	// Stopping takes the last message down.
	if w := do(d, "POST", "/api/desktop/tools/chatbox", `{"action":"stop"}`); w.Code != 200 || packets.Load() != 1 {
		t.Fatalf("stop=%d packets=%d", w.Code, packets.Load())
	}
}

func TestChatboxPreviewShowsSensorReadings(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	do(d, "GET", "/api/desktop/tools/chatbox?want=cpu,ram,gpu,bogus", "")
	wanted := d.sensorsWanted()
	if !slices.Equal(wanted, []string{"cpu", "ram", "gpu"}) {
		t.Fatalf("wanted=%q", wanted)
	}
	d.readSensors(context.Background(), []string{"ram"})
	w := do(d, "GET", "/api/desktop/tools/chatbox", "")
	if !strings.Contains(w.Body.String(), `"ram":"RAM `) {
		t.Fatalf("preview=%s", w.Body)
	}
	// Nothing is read for a closed page.
	d.tools.viewed = time.Now().Add(-time.Hour)
	if wanted := d.sensorsWanted(); len(wanted) != 0 {
		t.Fatalf("wanted=%q", wanted)
	}
	d.tools.automatic = true
	d.tools.prefs.Template = "{gpu}"
	if wanted := d.sensorsWanted(); !slices.Equal(wanted, []string{"gpu"}) {
		t.Fatalf("automatic wanted=%q", wanted)
	}
}

const oscTestAvatar = "avtr_00000000-0000-0000-0000-000000000001"

func oscDesktop(t *testing.T) (*desktop, *[]string) {
	t.Helper()
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	folder := t.TempDir()
	dir := filepath.Join(folder, "usr_test", "Avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"id":"` + oscTestAvatar + `","name":"Test","parameters":[
		{"name":"Glasses","input":{"address":"/avatar/parameters/Glasses","type":"Bool"},"output":{"address":"/avatar/parameters/Glasses","type":"Bool"}},
		{"name":"Outfit","input":{"address":"/avatar/parameters/Outfit","type":"Int"},"output":{"address":"/avatar/parameters/Outfit","type":"Int"}},
		{"name":"Hue","input":{"address":"/avatar/parameters/Hue","type":"Float"},"output":{"address":"/avatar/parameters/Hue","type":"Float"}},
		{"name":"Velocity","output":{"address":"/avatar/parameters/Velocity","type":"Float"}}]}`
	if err := os.WriteFile(filepath.Join(dir, oscTestAvatar+".json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	d.osc.folder = func() string { return folder }
	sent := &[]string{}
	d.tools.sender.Write = func(_ int, b []byte) error { *sent = append(*sent, string(b)); return nil }
	t.Cleanup(d.stopOSC)
	return d, sent
}

func TestOSCParameters(t *testing.T) {
	d, sent := oscDesktop(t)
	if w := do(d, "GET", "/api/desktop/tools/osc", ""); !strings.Contains(w.Body.String(), `"listening":false`) {
		t.Fatalf("starts off: %s", w.Body)
	}
	if w := do(d, "POST", "/api/desktop/tools/osc", `{"action":"listen","port":80}`); w.Code != 400 {
		t.Fatalf("low port=%d", w.Code)
	}
	// Port 0 picks the default, which a real VRChat may hold, so use a free port.
	port := freeUDPPort(t)
	w := do(d, "POST", "/api/desktop/tools/osc", fmt.Sprintf(`{"action":"listen","port":%d}`, port))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"avatarName":"Test"`) || !strings.Contains(w.Body.String(), `"writable":false`) {
		t.Fatalf("listen=%d %s", w.Code, w.Body)
	}
	for body, wanted := range map[string]string{
		`{"action":"set","name":"Glasses","value":true}`: "/avatar/parameters/Glasses\x00\x00,T",
		`{"action":"set","name":"Outfit","value":3}`:     "/avatar/parameters/Outfit\x00\x00\x00,i",
		`{"action":"set","name":"Hue","value":-0.5}`:     "/avatar/parameters/Hue\x00\x00,f",
	} {
		*sent = nil
		if w := do(d, "POST", "/api/desktop/tools/osc", body); w.Code != 200 || len(*sent) != 1 || !strings.HasPrefix((*sent)[0], wanted) {
			t.Errorf("%s = %d %q", body, w.Code, *sent)
		}
	}
	*sent = nil
	for _, body := range []string{
		`{"action":"set","name":"Velocity","value":0.5}`, `{"action":"set","name":"Missing","value":1}`,
		`{"action":"set","name":"Outfit","value":256}`, `{"action":"set","name":"Outfit","value":1.5}`, `{"action":"set","name":"Outfit","value":-1}`,
		`{"action":"set","name":"Hue","value":2}`, `{"action":"set","name":"Glasses","value":"yes"}`, `{"action":"nonsense"}`,
	} {
		if w := do(d, "POST", "/api/desktop/tools/osc", body); w.Code != 400 {
			t.Errorf("%s = %d", body, w.Code)
		}
	}
	if len(*sent) != 0 {
		t.Fatalf("invalid input was sent: %q", *sent)
	}
	if w := do(d, "POST", "/api/desktop/tools/osc", `{"action":"stop"}`); !strings.Contains(w.Body.String(), `"listening":false`) {
		t.Fatalf("stop=%s", w.Body)
	}
}

func TestOSCReceivesFromVRChat(t *testing.T) {
	d, _ := oscDesktop(t)
	port := freeUDPPort(t)
	if err := d.startOSC(port); err != nil {
		t.Fatal(err)
	}
	send := func(address string, value any) {
		t.Helper()
		packet, err := osc.Encode(osc.Message{Address: address, Args: []any{value}})
		if err != nil {
			t.Fatal(err)
		}
		if err := osc.Send(port, packet); err != nil {
			t.Fatal(err)
		}
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal(what)
			}
		}
	}
	send("/avatar/parameters/Outfit", int32(7))
	send("/avatar/parameters/AFK", true)
	waitFor("AFK not seen", func() bool { _, afk := d.osc.afkSince(); return afk })
	if status := do(d, "GET", "/api/desktop/tools/osc", "").Body.String(); !strings.Contains(status, `"value":7`) {
		t.Fatalf("status=%s", status)
	}
	// The AFK text replaces the template, then the template comes back.
	d.tools.mu.Lock()
	prefs := chatPrefs{Template: "Normal", AFKText: "Away for {afktime}", Port: 9000, Interval: 5}
	afkText := d.renderChat(context.Background(), prefs, false)
	d.tools.mu.Unlock()
	if afkText != "Away for 0m" {
		t.Fatalf("afk text=%q", afkText)
	}
	send("/avatar/parameters/AFK", false)
	waitFor("AFK not cleared", func() bool { _, afk := d.osc.afkSince(); return !afk })
	d.tools.mu.Lock()
	normal := d.renderChat(context.Background(), prefs, false)
	d.tools.mu.Unlock()
	if normal != "Normal" {
		t.Fatalf("normal text=%q", normal)
	}
	// Changing avatar forgets the old values, and an unknown avatar has no list.
	send("/avatar/change", "avtr_00000000-0000-0000-0000-000000000009")
	waitFor("avatar change not seen", func() bool {
		return !strings.Contains(do(d, "GET", "/api/desktop/tools/osc", "").Body.String(), "Glasses")
	})
	send("/avatar/change", "../../outside")
	send("/avatar/change", oscTestAvatar)
	waitFor("avatar not loaded", func() bool {
		return strings.Contains(do(d, "GET", "/api/desktop/tools/osc", "").Body.String(), "Glasses")
	})
}

func TestOSCPacketAfterStopIsIgnored(t *testing.T) {
	d, _ := oscDesktop(t)
	d.receive(osc.Message{Address: "/avatar/parameters/Outfit", Args: []any{int32(1)}})
	if err := d.startOSC(freeUDPPort(t)); err != nil {
		t.Fatal(err)
	}
	d.stopOSC()
	d.receive(osc.Message{Address: "/avatar/parameters/Outfit", Args: []any{int32(1)}})
	d.receive(osc.Message{Address: "/avatar/change", Args: []any{oscTestAvatar}})
}

func TestOSCListenerKeepsToItsLimit(t *testing.T) {
	d, _ := oscDesktop(t)
	if err := d.startOSC(freeUDPPort(t)); err != nil {
		t.Fatal(err)
	}
	for i := range maxOSCValues + 10 {
		d.receive(osc.Message{Address: fmt.Sprintf("/avatar/parameters/P%d", i), Args: []any{true}})
	}
	if n := len(d.osc.values); n != maxOSCValues {
		t.Fatalf("remembered %d values", n)
	}
}

func TestOSCPortInUse(t *testing.T) {
	d, _ := oscDesktop(t)
	holder, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	port := holder.LocalAddr().(*net.UDPAddr).Port
	w := do(d, "POST", "/api/desktop/tools/osc", fmt.Sprintf(`{"action":"listen","port":%d}`, port))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Another app") {
		t.Fatalf("port in use=%d %s", w.Code, w.Body)
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	return probe.LocalAddr().(*net.UDPAddr).Port
}

func TestDesktopRestartAction(t *testing.T) {
	site, _ := url.Parse("https://vrcove.example")
	d := newTestDesktop(t, site)
	if w := do(d, "POST", "/api/desktop/update", `{"action":"restart"}`); w.Code != 503 {
		t.Fatalf("without a restarter=%d %s", w.Code, w.Body)
	}
	restarts := 0
	d.restart = func() error { restarts++; return nil }
	if w := do(d, "POST", "/api/desktop/update", `{"action":"restart"}`); w.Code != 200 || restarts != 1 {
		t.Fatalf("restart=%d %s, restarts=%d", w.Code, w.Body, restarts)
	}
	d.restart = func() error { return errors.New("missing executable") }
	if w := do(d, "POST", "/api/desktop/update", `{"action":"restart"}`); w.Code != 500 || strings.Contains(w.Body.String(), "missing executable") {
		t.Fatalf("failed restart=%d %s", w.Code, w.Body)
	}
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
