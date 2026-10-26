// Package chatbox sends OSC chatbox messages to VRChat on this computer.
package chatbox

import (
	"bytes"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const MinInterval = 5 * time.Second

type Sender struct {
	mu   sync.Mutex
	last time.Time
	// Write and Now are optional test hooks. The default target is always loopback.
	Write func(int, []byte) error
	Now   func() time.Time
}

func (s *Sender) Send(text string, port int, sound bool) error {
	if port < 1024 || port > 65535 {
		return errors.New("Choose an OSC port between 1024 and 65535.")
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || strings.TrimSpace(text) == "" || len(utf16.Encode([]rune(text))) > 144 {
		return errors.New("Chatbox messages must contain 1 to 144 characters.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if !s.last.IsZero() && now.Sub(s.last) < MinInterval {
		return errors.New("Wait 5 seconds between chatbox messages.")
	}
	var b bytes.Buffer
	oscString(&b, "/chatbox/input")
	tags := ",sTF"
	if sound {
		tags = ",sTT"
	}
	oscString(&b, tags)
	oscString(&b, text)
	write := s.Write
	if write == nil {
		write = sendUDP
	}
	if err := write(port, b.Bytes()); err != nil {
		return errors.New("Could not send to VRChat. Check that VRChat and OSC are running.")
	}
	s.last = now
	return nil
}

func oscString(b *bytes.Buffer, s string) {
	b.WriteString(s)
	b.WriteByte(0)
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

func sendUDP(port int, packet []byte) error {
	c, err := net.DialTimeout("udp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetWriteDeadline(time.Now().Add(time.Second))
	_, err = c.Write(packet)
	return err
}

// Render expands only supported placeholders, with the clock updated at send time.
func Render(template, status string, now time.Time) string {
	return strings.NewReplacer("{time}", now.Format("15:04"), "{date}", now.Format("2006-01-02"), "{status}", status).Replace(template)
}
