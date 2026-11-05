// Package chatbox sends OSC chatbox messages to VRChat on this computer.
package chatbox

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"vrcove/companion/internal/osc"
)

const (
	MinInterval = 5 * time.Second
	// MaxLength and MaxLines are the limits of VRChat's chatbox. It counts
	// UTF-16 code units.
	MaxLength = 144
	MaxLines  = 9
)

var (
	// ErrTooSoon and ErrEmpty are routine for automatic messages: one is
	// retried on the next tick and the other waits for something to show.
	ErrTooSoon = errors.New("Wait 5 seconds between chatbox messages.")
	ErrEmpty   = errors.New("Chatbox messages must contain 1 to 144 characters.")
)

type Sender struct {
	mu   sync.Mutex
	last time.Time
	// Write and Now are optional test hooks. The default target is always loopback.
	Write func(int, []byte) error
	Now   func() time.Time
}

// Send shows text in the chatbox straight away.
func (s *Sender) Send(text string, port int, sound bool) error {
	return s.deliver(text, port, true, sound)
}

// Fill opens the in-game keyboard with text, for the player to edit and send.
func (s *Sender) Fill(text string, port int) error {
	return s.deliver(text, port, false, false)
}

func (s *Sender) deliver(text string, port int, direct, sound bool) error {
	if err := checkPort(port); err != nil {
		return err
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || strings.TrimSpace(text) == "" || length(text) > MaxLength {
		return ErrEmpty
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if !s.last.IsZero() && now.Sub(s.last) < MinInterval {
		return ErrTooSoon
	}
	if err := s.write(port, osc.Message{Address: "/chatbox/input", Args: []any{text, direct, sound}}); err != nil {
		return err
	}
	s.last = now
	return nil
}

// Clear removes the current message. It skips MinInterval, so stopping always
// takes the text down.
func (s *Sender) Clear(port int) error {
	if err := checkPort(port); err != nil {
		return err
	}
	return s.write(port, osc.Message{Address: "/chatbox/input", Args: []any{"", true, false}})
}

// Typing turns the typing indicator over the player's head on or off.
func (s *Sender) Typing(typing bool, port int) error {
	if err := checkPort(port); err != nil {
		return err
	}
	return s.write(port, osc.Message{Address: "/chatbox/typing", Args: []any{typing}})
}

func checkPort(port int) error {
	if port < 1024 || port > 65535 {
		return errors.New("Choose an OSC port between 1024 and 65535.")
	}
	return nil
}

func (s *Sender) write(port int, m osc.Message) error {
	packet, err := osc.Encode(m)
	if err != nil {
		return err
	}
	write := s.Write
	if write == nil {
		write = osc.Send
	}
	if err := write(port, packet); err != nil {
		return errors.New("Could not send to VRChat. Check that VRChat and OSC are running.")
	}
	return nil
}

func length(text string) int { return len(utf16.Encode([]rune(text))) }

// Values fills a template's placeholders. An empty value means there's nothing
// to show right now, and the line that needed it adapts.
type Values struct {
	Status, Rotate, Media, CPU, GPU, RAM, World, Players, Playtime, AFK string
}

// placeholder names a template placeholder and how to fill it.
type placeholder struct {
	name  string
	value func(Values, time.Time) string
}

// Placeholders lists every supported placeholder, longest name first where one
// could be read as another.
var placeholders = []placeholder{
	{"{time12}", func(_ Values, now time.Time) string { return now.Format("3:04 PM") }},
	{"{time}", func(_ Values, now time.Time) string { return now.Format("15:04") }},
	{"{date}", func(_ Values, now time.Time) string { return now.Format("2006-01-02") }},
	{"{status}", func(v Values, _ time.Time) string { return v.Status }},
	{"{rotate}", func(v Values, _ time.Time) string { return v.Rotate }},
	{"{media}", func(v Values, _ time.Time) string { return v.Media }},
	{"{cpu}", func(v Values, _ time.Time) string { return v.CPU }},
	{"{gpu}", func(v Values, _ time.Time) string { return v.GPU }},
	{"{ram}", func(v Values, _ time.Time) string { return v.RAM }},
	{"{world}", func(v Values, _ time.Time) string { return v.World }},
	{"{players}", func(v Values, _ time.Time) string { return v.Players }},
	{"{playtime}", func(v Values, _ time.Time) string { return v.Playtime }},
	{"{afktime}", func(v Values, _ time.Time) string { return v.AFK }},
}

// Render expands only supported placeholders, with the clock updated at send
// time, and fits the result to the chatbox. A line whose placeholders are all
// empty is dropped, and one with some empty loses the separators around them,
// so "{time} | {media}" reads "14:30" when nothing is playing.
func Render(template string, values Values, now time.Time) string {
	lines := []string{}
	for _, line := range strings.Split(template, "\n") {
		text, filled, empty := expand(line, values, now)
		if filled == 0 && empty > 0 {
			continue
		}
		if empty > 0 {
			text = closeGaps(text)
		}
		lines = append(lines, text)
	}
	return Fit(strings.Join(lines, "\n"))
}

// expand fills a line, counting the placeholders with and without a value.
func expand(line string, values Values, now time.Time) (text string, filled, empty int) {
	pairs := make([]string, 0, 2*len(placeholders))
	for _, p := range placeholders {
		if !strings.Contains(line, p.name) {
			continue
		}
		value := p.value(values, now)
		if value == "" {
			empty++
		} else {
			filled++
		}
		pairs = append(pairs, p.name, value)
	}
	return strings.NewReplacer(pairs...).Replace(line), filled, empty
}

// closeGaps removes the separators an empty placeholder left behind.
func closeGaps(line string) string {
	for _, gap := range []string{" | ", " · ", " • ", " - ", " / "} {
		for strings.Contains(line, gap+gap) {
			line = strings.ReplaceAll(line, gap+gap, gap)
		}
	}
	const marks = " |·•-/,:"
	return strings.Trim(line, marks)
}

// Fit shortens text to VRChat's limits. It cuts at a space when one is close,
// so a long song title doesn't end mid-word or stop automatic messages.
func Fit(text string) string {
	if lines := strings.Split(text, "\n"); len(lines) > MaxLines {
		text = strings.Join(lines[:MaxLines], "\n")
	}
	if length(text) <= MaxLength {
		return text
	}
	cut := 0
	used := 0
	for i, r := range text {
		size := 1
		if r > 0xFFFF {
			size = 2
		}
		if used+size > MaxLength-1 {
			cut = i
			break
		}
		used += size
	}
	shortened := text[:cut]
	if space := strings.LastIndexAny(shortened, " \n"); space > len(shortened)-24 && space > 0 {
		shortened = shortened[:space]
	}
	return strings.TrimRight(shortened, " \n") + "…"
}

// Duration writes a span the way the chatbox shows it, such as "1h 05m".
func Duration(span time.Duration) string {
	minutes := int(span.Minutes())
	if minutes < 60 {
		return strconv.Itoa(minutes) + "m"
	}
	return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
}
