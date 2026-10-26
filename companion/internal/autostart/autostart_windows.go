//go:build windows

package autostart

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "VRCove Desktop"
)

// Enabled reports whether VRCove Desktop is registered in the user's Run key.
func (m *Manager) Enabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("Could not read Windows startup settings.")
	}
	defer k.Close()
	val, _, err := k.GetStringValue(runValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("Could not read Windows startup settings.")
	}
	return strings.TrimSpace(val) != "", nil
}

// SetEnabled sets or removes the user's Windows Run registry value.
func (m *Manager) SetEnabled(enabled bool) error {
	if !enabled {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		if err != nil {
			return errors.New("Could not update Windows startup settings.")
		}
		defer k.Close()
		if err := k.DeleteValue(runValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return errors.New("Could not remove VRCove from Windows startup.")
		}
		return nil
	}
	exe, err := m.executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return errors.New("Could not update Windows startup settings.")
	}
	defer k.Close()
	if err := k.SetStringValue(runValueName, `"`+exe+`"`); err != nil {
		return errors.New("Could not add VRCove to Windows startup.")
	}
	return nil
}
