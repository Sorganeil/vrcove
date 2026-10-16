package logger

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// VRC-LOG documents amplitude.cache in the Windows temporary directory and
// the equivalent Proton prefix. Read it as a snapshot: VRChat rewrites it, and
// its last record need not end in a newline. Never modify the source file.
func amplitudePaths(folder string) []string {
	paths := []string{filepath.Join(folder, "amplitude.cache")}
	low := filepath.Dir(filepath.Dir(filepath.Clean(folder)))
	if strings.EqualFold(filepath.Base(low), "LocalLow") {
		paths = append(paths, filepath.Join(filepath.Dir(low), "Local", "Temp", "VRChat", "VRChat", "amplitude.cache"))
	}
	if runtime.GOOS == "windows" {
		paths = append(paths, filepath.Join(os.TempDir(), "VRChat", "VRChat", "amplitude.cache"))
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func (s *State) scanAmplitude(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("Unable to read VRChat analytics file")
	}
	defer f.Close()
	// Bound memory and processing even if a different file occupies this path.
	const limit = 32 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return true, errors.New("Unable to read VRChat analytics file")
	}
	if len(data) > limit {
		return true, errors.New("VRChat analytics file exceeds the 32 MiB scan limit")
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(data))
	if s.Files[path].First == fingerprint {
		return true, nil
	}
	for _, id := range IDs(string(data)) {
		if _, exists := s.Seen[id]; exists {
			continue
		}
		if len(s.Pending) >= maxPending {
			// Revisit the snapshot after uploads free queue space.
			return true, nil
		}
		s.Seen[id] = "queued"
		s.Pending = append(s.Pending, id)
		s.event(id, "Queued")
	}
	s.Files[path] = Cursor{First: fingerprint}
	return true, nil
}

// ClearAmplitudeCache is only called after an explicit user action and after
// discovered IDs have been durably saved. Preserve the file and its permissions.
func ClearAmplitudeCache(folder string) error {
	path := filepath.Join(folder, "amplitude.cache")
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("No amplitude.cache file in the selected folder")
	}
	if err != nil {
		return errors.New("Unable to open amplitude.cache for clearing")
	}
	defer f.Close()
	if err := f.Truncate(0); err != nil {
		return errors.New("Unable to clear amplitude.cache")
	}
	// An empty JSON array matches VRChat's cleared analytics format.
	if _, err := f.Write([]byte("[]")); err != nil {
		return errors.New("Unable to reset amplitude.cache")
	}
	return f.Sync()
}
