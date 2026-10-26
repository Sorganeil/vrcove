package chatbox

import (
	"bytes"
	"net"
	"testing"
	"time"
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
	if got := Render("{time} · {date} · {status}", "AFK", now); got != "14:30 · 2026-10-25 · AFK" {
		t.Fatal(got)
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
