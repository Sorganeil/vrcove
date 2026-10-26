// Package autostart manages launching VRCove Desktop when the user signs in
// to their computer. Autostart is off by default and only changes when the
// user toggles it in Local Settings.
package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const AppID = "vrcove-desktop"

// Manager reads and updates the user's login autostart registration.
type Manager struct {
	ExecPath  func() (string, error)
	ConfigDir func() (string, error)
}

// New returns a Manager for the current process and user config directory.
func New() *Manager {
	return &Manager{
		ExecPath:  os.Executable,
		ConfigDir: os.UserConfigDir,
	}
}

func (m *Manager) executable() (string, error) {
	fn := m.ExecPath
	if fn == nil {
		fn = os.Executable
	}
	exe, err := fn()
	if err != nil || strings.TrimSpace(exe) == "" {
		return "", errors.New("Could not determine the application path.")
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", errors.New("Could not resolve the application path.")
	}
	if strings.ContainsAny(abs, "\r\n\x00") {
		return "", errors.New("Application path contains invalid characters.")
	}
	return abs, nil
}

func (m *Manager) configDir() (string, error) {
	fn := m.ConfigDir
	if fn == nil {
		fn = os.UserConfigDir
	}
	dir, err := fn()
	if err != nil || strings.TrimSpace(dir) == "" {
		return "", errors.New("Could not find the user configuration folder.")
	}
	return dir, nil
}

func desktopEntryContent(exe string) string {
	escaped := strings.ReplaceAll(exe, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=VRCove\n" +
		"Comment=VRCove Desktop\n" +
		"Exec=\"" + escaped + "\"\n" +
		"Terminal=false\n" +
		"X-GNOME-Autostart-enabled=true\n"
}
