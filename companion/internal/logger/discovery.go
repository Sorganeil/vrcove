package logger

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DetectFolder checks Steam's configured libraries rather than assuming VRChat
// lives on the same drive as Steam. It never reads general log or telemetry data.
func DetectFolder() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		candidates := []string{filepath.Join(os.TempDir(), "VRChat", "VRChat")}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates, filepath.Join(local, "Temp", "VRChat", "VRChat"))
		}
		if found := newestAnalyticsFolder(candidates); found != "" {
			return found
		}
		return candidates[0]
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	roots := []string{
		filepath.Join(dataHome, "Steam"),
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".steam", "root"),
		filepath.Join(home, ".steam", "debian-installation"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
	}
	var prefixes []string
	if compat := os.Getenv("STEAM_COMPAT_DATA_PATH"); compat != "" {
		prefixes = append(prefixes, filepath.Join(compat, "pfx"))
	}
	if prefix := os.Getenv("WINEPREFIX"); prefix != "" {
		prefixes = append(prefixes, prefix)
	}
	return discoverSteamFolder(roots, prefixes)
}

// Both modern nested library entries and old numeric path entries are accepted.
var libraryPath = regexp.MustCompile(`(?i)"(?:path|[0-9]+)"\s*"((?:\\.|[^"\\])*)"`)

func discoverSteamFolder(roots, prefixes []string) string {
	seenLibraries := map[string]bool{}
	for _, root := range roots {
		libraries := []string{root}
		for _, relative := range []string{"steamapps/libraryfolders.vdf", "config/libraryfolders.vdf"} {
			f, err := os.Open(filepath.Join(root, filepath.FromSlash(relative)))
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(f, 1<<20))
			f.Close()
			if err != nil {
				continue
			}
			for _, match := range libraryPath.FindAllSubmatch(data, -1) {
				path, err := strconv.Unquote(`"` + string(match[1]) + `"`)
				if err == nil && filepath.IsAbs(path) {
					libraries = append(libraries, path)
				}
			}
		}
		for _, library := range libraries {
			key := canonicalPath(library)
			if seenLibraries[key] {
				continue
			}
			seenLibraries[key] = true
			prefixes = append(prefixes, filepath.Join(library, "steamapps", "compatdata", "438100", "pfx"))
		}
	}
	var candidates []string
	for _, prefix := range prefixes {
		// Proton versions and migrated prefixes can have different user names.
		users, _ := os.ReadDir(filepath.Join(prefix, "drive_c", "users"))
		for _, user := range users {
			if strings.EqualFold(user.Name(), "Public") {
				continue
			}
			candidates = append(candidates, filepath.Join(prefix, "drive_c", "users", user.Name(), "AppData", "Local", "Temp", "VRChat", "VRChat"))
		}
	}
	return newestAnalyticsFolder(candidates)
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func newestAnalyticsFolder(candidates []string) string {
	seen := map[string]bool{}
	var found, fallback string
	var newest time.Time
	for _, folder := range candidates {
		folder = canonicalPath(folder)
		if seen[folder] {
			continue
		}
		seen[folder] = true
		if dir, err := os.Stat(folder); err != nil || !dir.IsDir() {
			continue
		}
		if fallback == "" {
			fallback = folder
		}
		info, err := os.Stat(filepath.Join(folder, "amplitude.cache"))
		if err == nil && info.Mode().IsRegular() && (found == "" || info.ModTime().After(newest)) {
			found, newest = folder, info.ModTime()
		}
	}
	if found != "" {
		return found
	}
	return fallback
}
