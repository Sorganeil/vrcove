package autostart

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDesktopEntryContentQuotesExecutable(t *testing.T) {
	got := desktopEntryContent(`/opt/VRCove Desktop/vrcove-desktop`)
	if !strings.Contains(got, `Exec="/opt/VRCove Desktop/vrcove-desktop"`) {
		t.Fatalf("unexpected desktop entry:\n%s", got)
	}
}

func TestLinuxAutostartDefaultOffAndToggle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only XDG autostart test")
	}
	cfgDir := t.TempDir()
	exePath := filepath.Join(t.TempDir(), "vrcove-desktop")
	if err := os.WriteFile(exePath, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{
		ExecPath:  func() (string, error) { return exePath, nil },
		ConfigDir: func() (string, error) { return cfgDir, nil },
	}
	enabled, err := m.Enabled()
	if err != nil || enabled {
		t.Fatalf("initial Enabled() = (%v, %v), want (false, nil)", enabled, err)
	}
	if err := m.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true) = %v", err)
	}
	enabled, err = m.Enabled()
	if err != nil || !enabled {
		t.Fatalf("after enable Enabled() = (%v, %v), want (true, nil)", enabled, err)
	}
	if err := m.SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled(false) = %v", err)
	}
	enabled, err = m.Enabled()
	if err != nil || enabled {
		t.Fatalf("after disable Enabled() = (%v, %v), want (false, nil)", enabled, err)
	}
}
