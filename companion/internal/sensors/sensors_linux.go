package sensors

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	mprisPrefix = "org.mpris.MediaPlayer2."
	mprisPlayer = "org.mpris.MediaPlayer2.Player"
	mprisObject = dbus.ObjectPath("/org/mpris/MediaPlayer2")
)

func helper(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// NowPlaying asks media players on the session bus. Players speak MPRIS, and
// any that is playing will do.
func NowPlaying(ctx context.Context) Media {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return Media{}
	}
	defer conn.Close()
	var names []string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return Media{}
	}
	for _, name := range names {
		if !strings.HasPrefix(name, mprisPrefix) {
			continue
		}
		player := conn.Object(name, mprisObject)
		var status dbus.Variant
		if player.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, mprisPlayer, "PlaybackStatus").Store(&status) != nil || status.Value() != "Playing" {
			continue
		}
		var metadata map[string]dbus.Variant
		if player.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, mprisPlayer, "Metadata").Store(&metadata) != nil {
			continue
		}
		if media := parseMetadata(metadata); media.Title != "" {
			return media
		}
	}
	return Media{}
}

func parseMetadata(metadata map[string]dbus.Variant) Media {
	var media Media
	if title, ok := metadata["xesam:title"].Value().(string); ok {
		media.Title = strings.TrimSpace(title)
	}
	if artists, ok := metadata["xesam:artist"].Value().([]string); ok {
		media.Artist = strings.TrimSpace(join(artists))
	}
	return media
}

// readUsage reads processor time from /proc/stat and memory from /proc/meminfo.
func readUsage() (busy, idle uint64, ram int, ok bool) {
	stat, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, 0, false
	}
	defer stat.Close()
	scanner := bufio.NewScanner(stat)
	if !scanner.Scan() {
		return 0, 0, 0, false
	}
	busy, idle, ok = parseStat(scanner.Text())
	if !ok {
		return 0, 0, 0, false
	}
	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, false
	}
	ram, ok = parseMeminfo(string(meminfo))
	return busy, idle, ram, ok
}

// parseStat reads /proc/stat's first line. Fields four and five are idle and
// iowait.
func parseStat(line string) (busy, idle uint64, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 6 || fields[0] != "cpu" {
		return 0, 0, false
	}
	for i, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		// guest and guest_nice are already counted in user and nice.
		switch {
		case i == 3 || i == 4:
			idle += value
		case i < 8:
			busy += value
		}
	}
	return busy, idle, true
}

func parseMeminfo(text string) (int, bool) {
	var total, available uint64
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = value
		case "MemAvailable:":
			available = value
		}
	}
	if total == 0 || available > total {
		return 0, false
	}
	return percent(total-available, total), true
}
