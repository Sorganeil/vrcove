package sensors

import (
	"context"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestParseStat(t *testing.T) {
	busy, idle, ok := parseStat("cpu  100 10 50 800 40 5 5 0 30 0")
	if !ok || busy != 170 || idle != 840 {
		t.Fatalf("busy=%d idle=%d ok=%v", busy, idle, ok)
	}
	for _, bad := range []string{"", "cpu 1 2", "cpu0 1 2 3 4 5 6 7", "cpu a b c d e f"} {
		if _, _, ok := parseStat(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseMeminfo(t *testing.T) {
	if value, ok := parseMeminfo("MemTotal: 1000 kB\nMemFree: 100 kB\nMemAvailable: 250 kB\n"); !ok || value != 75 {
		t.Fatalf("value=%d ok=%v", value, ok)
	}
	for _, bad := range []string{"", "MemTotal: 0 kB", "MemTotal: 10 kB\nMemAvailable: 20 kB"} {
		if _, ok := parseMeminfo(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseMetadata(t *testing.T) {
	media := parseMetadata(map[string]dbus.Variant{"xesam:title": dbus.MakeVariant(" Song "), "xesam:artist": dbus.MakeVariant([]string{"One", "Two"})})
	if media != (Media{"Song", "One, Two"}) {
		t.Fatalf("media=%+v", media)
	}
	if media := parseMetadata(map[string]dbus.Variant{"xesam:title": dbus.MakeVariant(5)}); media != (Media{}) {
		t.Fatalf("wrong type=%+v", media)
	}
}

func TestSamplerReadsThisComputer(t *testing.T) {
	var s Sampler
	if cpu, ram, ok := s.Read(); !ok || cpu != 0 || ram <= 0 || ram > 100 {
		t.Fatalf("first read cpu=%d ram=%d ok=%v", cpu, ram, ok)
	}
	if cpu, _, ok := s.Read(); !ok || cpu < 0 || cpu > 100 {
		t.Fatalf("second read cpu=%d ok=%v", cpu, ok)
	}
}

func TestNowPlayingNeverFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if media := NowPlaying(ctx); media != (Media{}) {
		t.Fatalf("cancelled read=%+v", media)
	}
}
