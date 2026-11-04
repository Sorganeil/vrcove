//go:build !js

package server

// The SQLite driver is only needed where VRCove keeps databases. Browser
// builds run local mode, which keeps none, so they leave it out.
import _ "modernc.org/sqlite"
