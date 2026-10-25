package main

import (
	"os"
	"strings"

	"vrcove/companion/internal/logger"
)

func displayFolder(path string) string {
	if path == "" {
		return "Choose your VRChat analytics folder"
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		path = strings.Replace(path, home, "~", 1)
	}
	return path
}

func detectFolder() string {
	return logger.DetectFolder()
}
