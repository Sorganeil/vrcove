package history

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	monthFolder    = regexp.MustCompile(`^\d{4}-\d{2}$`)
	screenshotName = regexp.MustCompile(`^VRChat_(\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2})(?:\.(\d{3}))?_(\d+)x(\d+)`)
	// VRChat stores the world in the picture's XMP metadata.
	xmpWorldID   = regexp.MustCompile(`<vrc:WorldID>(wrld_[0-9a-f-]+)</vrc:WorldID>`)
	xmpWorldName = regexp.MustCompile(`<vrc:WorldDisplayName>([^<]{1,200})</vrc:WorldDisplayName>`)
)

// ScreenshotFolder finds VRChat's screenshot folder (Pictures\VRChat in the
// same user profile) from its analytics folder.
func ScreenshotFolder(analytics string) string {
	profile := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(analytics)))))
	return filepath.Join(profile, "Pictures", "VRChat")
}

// Screenshot is one picture with what VRCove knows about when it was taken.
type Screenshot struct {
	// File is the path inside the screenshot folder, with forward slashes.
	File      string      `json:"file"`
	Taken     int64       `json:"taken"`
	Width     int         `json:"width,omitempty"`
	Height    int         `json:"height,omitempty"`
	WorldID   string      `json:"worldId,omitempty"`
	WorldName string      `json:"worldName,omitempty"`
	People    []Encounter `json:"people"`
}

// Months lists the month folders that hold screenshots, newest first.
func Months(folder string) []string {
	entries, _ := os.ReadDir(folder)
	months := []string{}
	for _, e := range entries {
		if e.IsDir() && monthFolder.MatchString(e.Name()) {
			months = append(months, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(months)))
	return months
}

// Screenshots lists one month's screenshots, newest first, with the world
// and the people who were there from the game log history.
func (s *Store) Screenshots(ctx context.Context, folder, month string) ([]Screenshot, error) {
	shots := []Screenshot{}
	if !monthFolder.MatchString(month) {
		return shots, nil
	}
	entries, err := os.ReadDir(filepath.Join(folder, month))
	if err != nil {
		return shots, nil
	}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || (ext != ".png" && ext != ".jpg" && ext != ".jpeg") {
			continue
		}
		shot := Screenshot{File: month + "/" + e.Name(), People: []Encounter{}}
		if m := screenshotName.FindStringSubmatch(e.Name()); m != nil {
			if t, err := time.ParseInLocation("2006-01-02_15-04-05", m[1], time.Local); err == nil {
				shot.Taken = t.UnixMilli()
			}
			shot.Width, shot.Height = atoi(m[3]), atoi(m[4])
		}
		if shot.Taken == 0 {
			if info, err := e.Info(); err == nil {
				shot.Taken = info.ModTime().UnixMilli()
			}
		}
		shot.WorldID, shot.WorldName = pictureWorld(filepath.Join(folder, month, e.Name()))
		shots = append(shots, shot)
	}
	sort.Slice(shots, func(i, j int) bool { return shots[i].Taken > shots[j].Taken })
	for i := range shots {
		v, err := s.VisitAt(ctx, shots[i].Taken)
		if err != nil {
			return nil, err
		}
		if v == nil {
			continue
		}
		if shots[i].WorldName == "" {
			shots[i].WorldID, shots[i].WorldName = v.WorldID, v.WorldName
		}
		shots[i].People = v.People
	}
	return shots, nil
}

// pictureWorld reads the world VRChat wrote into a screenshot's metadata.
func pictureWorld(path string) (id, name string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	// The metadata sits near the start of the file.
	head, _ := io.ReadAll(io.LimitReader(f, 64<<10))
	if m := xmpWorldID.FindSubmatch(head); m != nil {
		id = string(m[1])
	}
	if m := xmpWorldName.FindSubmatch(head); m != nil {
		name = string(m[1])
	}
	return id, name
}

// ScreenshotPath resolves a File from Screenshots inside folder, refusing
// anything outside it.
func ScreenshotPath(folder, file string) (string, bool) {
	parts := strings.Split(file, "/")
	if len(parts) != 2 || !monthFolder.MatchString(parts[0]) || parts[1] == "" || strings.ContainsAny(parts[1], `/\`) || parts[1][0] == '.' {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(parts[1]))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return "", false
	}
	return filepath.Join(folder, parts[0], parts[1]), true
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' || n > 1e6 {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
