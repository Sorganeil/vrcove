//go:build !linux && !windows

package autostart

import "errors"

func (m *Manager) Enabled() (bool, error) {
	return false, nil
}

func (m *Manager) SetEnabled(enabled bool) error {
	if !enabled {
		return nil
	}
	return errors.New("Startup on sign-in is not supported on this platform.")
}
