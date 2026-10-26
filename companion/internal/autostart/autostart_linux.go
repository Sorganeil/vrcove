//go:build linux

package autostart

import (
	"errors"
	"os"
	"path/filepath"
)

func (m *Manager) desktopFilePath() (string, error) {
	dir, err := m.configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", AppID+".desktop"), nil
}

// Enabled reports whether the XDG autostart entry is present.
func (m *Manager) Enabled() (bool, error) {
	path, err := m.desktopFilePath()
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

// SetEnabled creates or removes the user's XDG autostart desktop entry.
func (m *Manager) SetEnabled(enabled bool) error {
	path, err := m.desktopFilePath()
	if err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("Could not remove the startup entry.")
		}
		return nil
	}
	exe, err := m.executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("Could not create the startup folder.")
	}
	tmp, err := os.CreateTemp(dir, ".vrcove-autostart-*.desktop")
	if err != nil {
		return errors.New("Could not write the startup entry.")
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(desktopEntryContent(exe)); err != nil {
		_ = tmp.Close()
		return errors.New("Could not write the startup entry.")
	}
	if err := tmp.Close(); err != nil {
		return errors.New("Could not save the startup entry.")
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return errors.New("Could not set permissions on the startup entry.")
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errors.New("Could not save the startup entry.")
	}
	return nil
}
