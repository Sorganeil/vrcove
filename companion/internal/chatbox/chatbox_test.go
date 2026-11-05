package chatbox

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPacketAndRateLimit(t *testing.T) {
	now := time.Date(2026, 10, 25, 14, 30, 0, 0, time.UTC)
	var packets [][]byte
	s := Sender{Now: func() time.Time { return now }, Write: func(port int, b []byte) error {
		if port != 9000 {
			t.Errorf("port=%d", port)
		}
		packets = append(packets, bytes.Clone(b))
		return nil
	}}
	if err := s.Send("Hello", 9000, false); err != nil {
		t.Fatal(err)
	}
	want := []byte("/chatbox/input\x00\x00,sTF\x00\x00\x00\x00Hello\x00\x00\x00")
	if !bytes.Equal(packets[0], want) {
		t.Fatalf("packet=%q want=%q", packets[0], want)
	}
	if err := s.Send("too soon", 9000, false); err == nil {
		t.Fatal("rate limit missing")
	}
	now = now.Add(MinInterval)
	if err := s.Send("你好 👋", 9000, true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(packets[1], []byte(",sTT")) {
		t.Fatal("sound flag missing")
	}
	if got := Render("{time} · {date} · {status}", Values{Status: "AFK"}, now); got != "14:30 · 2026-10-25 · AFK" {
		t.Fatal(got)
	}
}

func TestRender(t *testing.T) {
	now := time.Date(2026, 10, 25, 14, 30, 0, 0, time.UTC)
	values := Values{Media: "Song - Artist", CPU: "CPU 12%", RAM: "RAM 40%", World: "The Black Cat", Players: "12", Rotate: "Hello"}
	for template, want := range map[string]string{
		"{time12}":               "2:30 PM",
		"{cpu} | {ram}":          "CPU 12% | RAM 40%",
		"{world} ({players})":    "The Black Cat (12)",
		"Now: {media}\n{rotate}": "Now: Song - Artist\nHello",
		"Plain {unknown} text":   "Plain {unknown} text",
		"First\n\nThird":         "First\n\nThird",
		"{time} | {gpu}":         "14:30",
		"{gpu} | {time}":         "14:30",
		"{time} | {gpu} | {cpu}": "14:30 | CPU 12%",
		"{gpu}\n{time}":          "14:30",
		"{gpu}":                  "",
		"{gpu} | {playtime}":     "",
		"Playing: {gpu}":         "",
		"{cpu} - ":               "CPU 12% - ",
		"{status}\nKept":         "Kept",
	} {
		if got := Render(template, values, now); got != want {
			t.Errorf("Render(%q) = %q, want %q", template, got, want)
		}
	}
}

func TestFit(t *testing.T) {
	short := "A short message"
	if Fit(short) != short {
		t.Fatal("changed a short message")
	}
	words := strings.Repeat("word ", 40)
	fitted := Fit(words)
	if length(fitted) > MaxLength || !strings.HasSuffix(fitted, "word…") {
		t.Fatalf("words: %d %q", length(fitted), fitted)
	}
	// Emoji count double, and a cut never splits one.
	emoji := Fit(strings.Repeat("😀", 100))
	if length(emoji) > MaxLength || !utf8.ValidString(emoji) {
		t.Fatalf("emoji: %d %q", length(emoji), emoji)
	}
	// One long word is cut where it has to be.
	if long := Fit(strings.Repeat("x", 300)); length(long) != MaxLength {
		t.Fatalf("long word: %d", length(long))
	}
	if lines := strings.Count(Fit(strings.Repeat("a\n", 20)), "\n"); lines > MaxLines-1 {
		t.Fatalf("lines: %d", lines)
	}
}

func TestDuration(t *testing.T) {
	for span, want := range map[time.Duration]string{0: "0m", 59 * time.Minute: "59m", 65 * time.Minute: "1h 05m", 25 * time.Hour: "25h 00m"} {
		if got := Duration(span); got != want {
			t.Errorf("Duration(%v) = %q", span, got)
		}
	}
}

func TestOtherChatboxMessages(t *testing.T) {
	var packets [][]byte
	now := time.Date(2026, 10, 25, 14, 30, 0, 0, time.UTC)
	s := Sender{Now: func() time.Time { return now }, Write: func(_ int, b []byte) error {
		packets = append(packets, bytes.Clone(b))
		return nil
	}}
	if err := s.Fill("Draft", 9000); err != nil {
		t.Fatal(err)
	}
	if err := s.Fill("Too soon", 9000); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("rate limit: %v", err)
	}
	// Neither the indicator nor clearing counts as a message.
	if err := s.Typing(true, 9000); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(9000); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"/chatbox/input\x00\x00,sFF\x00\x00\x00\x00Draft\x00\x00\x00", "/chatbox/typing\x00,T\x00\x00", "/chatbox/input\x00\x00,sTF\x00\x00\x00\x00\x00\x00\x00\x00"} {
		if string(packets[i]) != want {
			t.Errorf("packet %d = %q", i, packets[i])
		}
	}
	if err := s.Typing(true, 80); err == nil {
		t.Error("accepted port 80")
	}
}

func TestValidationDoesNotSend(t *testing.T) {
	s := Sender{Write: func(int, []byte) error { t.Fatal("sent invalid input"); return nil }}
	for _, text := range []string{"", "  ", "a\x00b", string([]byte{0xff}), string(bytes.Repeat([]byte("😀"), 73))} {
		if err := s.Send(text, 9000, false); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
	if err := s.Send("hi", 0, false); err == nil {
		t.Error("accepted port 0")
	}
}
func TestSendsUDPToLoopback(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s := Sender{}
	if err := s.Send("Local message", conn.LocalAddr().(*net.UDPAddr).Port, false); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(b)
	if err != nil || !bytes.Contains(b[:n], []byte("Local message")) {
		t.Fatalf("receive: %q %v", b[:n], err)
	}
}
