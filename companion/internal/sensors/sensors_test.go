package sensors

import "testing"

func TestMediaString(t *testing.T) {
	for media, want := range map[Media]string{{}: "", {Artist: "Artist"}: "", {Title: "Song"}: "Song", {Title: "Song", Artist: "Artist"}: "Song - Artist"} {
		if got := media.String(); got != want {
			t.Errorf("%+v = %q", media, got)
		}
	}
}

func TestParseGPU(t *testing.T) {
	if value, ok := parseGPU("12\n47\n"); !ok || value != 47 {
		t.Fatalf("two GPUs = %d %v", value, ok)
	}
	for _, bad := range []string{"", "[N/A]", "NVIDIA-SMI has failed", "140"} {
		if value, ok := parseGPU(bad); ok {
			t.Errorf("%q = %d", bad, value)
		}
	}
}

func TestPercent(t *testing.T) {
	if percent(1, 3) != 33 || percent(2, 3) != 67 || percent(5, 0) != 0 {
		t.Fatal(percent(1, 3), percent(2, 3), percent(5, 0))
	}
}
