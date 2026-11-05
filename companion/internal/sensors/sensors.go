// Package sensors reads what this computer is doing for chatbox messages:
// the song playing and how busy the hardware is. Nothing leaves the computer
// until the player puts it in a message.
package sensors

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// commandTimeout bounds a helper program, so a stuck one can't stall readings.
const commandTimeout = 4 * time.Second

// Media is the song or video playing right now.
type Media struct{ Title, Artist string }

// String is how the chatbox shows it, empty when nothing is playing.
func (m Media) String() string {
	switch {
	case m.Title == "":
		return ""
	case m.Artist == "":
		return m.Title
	}
	return m.Title + " - " + m.Artist
}

// Sampler reports processor and memory use. Processor use is measured between
// two reads, so the first read has only memory to go on and reports 0.
type Sampler struct {
	mu         sync.Mutex
	busy, idle uint64
}

// Read returns the percentages, or ok=false where the system can't tell.
func (s *Sampler) Read() (cpu, ram int, ok bool) {
	busy, idle, ram, ok := readUsage()
	if !ok {
		return 0, 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cpu = percent(busy-s.busy, busy-s.busy+idle-s.idle)
	if s.busy == 0 && s.idle == 0 {
		cpu = 0
	}
	s.busy, s.idle = busy, idle
	return cpu, ram, true
}

func percent(part, total uint64) int {
	if total == 0 {
		return 0
	}
	return int((part*100 + total/2) / total)
}

// GPU reads the busiest NVIDIA GPU with nvidia-smi, which comes with the
// driver. Other vendors have no equivalent tool on both systems.
func GPU(ctx context.Context) (int, bool) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	output, err := helper(ctx, "nvidia-smi", "--query-gpu=utilization.gpu", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, false
	}
	return parseGPU(string(output))
}

func parseGPU(output string) (int, bool) {
	best, ok := 0, false
	for _, line := range strings.Split(output, "\n") {
		if value, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && value >= 0 && value <= 100 {
			best, ok = max(best, value), true
		}
	}
	return best, ok
}

// join writes a list of artists the way players show them.
func join(artists []string) string { return strings.Join(artists, ", ") }
