package osc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// maxAvatarFile bounds a parameter file, which VRChat writes for each avatar.
const maxAvatarFile = 4 << 20

var avatarID = regexp.MustCompile(`^avtr_[0-9a-f-]{36}$`)

// Parameter is one avatar parameter from VRChat's OSC config. An empty Input
// means the avatar only reports it. Output is empty when VRChat never sends it.
type Parameter struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Input  string `json:"-"`
	Output string `json:"-"`
}

// Avatar is VRChat's OSC config for one avatar.
type Avatar struct {
	ID         string
	Name       string
	Parameters []Parameter
}

// Folder is VRChat's OSC config folder, inside its LocalLow folder.
func Folder(vrchat string) string { return filepath.Join(vrchat, "OSC") }

// LoadAvatar reads the config for id. VRChat writes one per account, and the
// newest is used when several accounts have worn the avatar.
func LoadAvatar(folder, id string) (*Avatar, error) {
	if !avatarID.MatchString(id) {
		return nil, errors.New("osc: invalid avatar ID")
	}
	files, err := filepath.Glob(filepath.Join(folder, "usr_*", "Avatars", id+".json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fs.ErrNotExist
	}
	newestFirst(files)
	return readAvatar(files[0])
}

// LatestAvatar reads the config VRChat wrote most recently, for when the app
// starts listening after the avatar changed.
func LatestAvatar(folder string) (*Avatar, error) {
	files, err := filepath.Glob(filepath.Join(folder, "usr_*", "Avatars", "avtr_*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fs.ErrNotExist
	}
	newestFirst(files)
	return readAvatar(files[0])
}

func newestFirst(files []string) {
	modified := make(map[string]int64, len(files))
	for _, file := range files {
		if info, err := os.Stat(file); err == nil {
			modified[file] = info.ModTime().UnixNano()
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return modified[files[i]] > modified[files[j]] })
}

func readAvatar(file string) (*Avatar, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxAvatarFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAvatarFile {
		return nil, errors.New("osc: avatar config is too large")
	}
	// VRChat writes these files with a UTF-8 byte order mark.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var raw struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Parameters []struct {
			Name  string `json:"name"`
			Input *struct {
				Address string `json:"address"`
				Type    string `json:"type"`
			} `json:"input"`
			Output *struct {
				Address string `json:"address"`
				Type    string `json:"type"`
			} `json:"output"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	avatar := &Avatar{ID: raw.ID, Name: raw.Name}
	for _, p := range raw.Parameters {
		parameter := Parameter{Name: p.Name}
		if p.Input != nil {
			parameter.Input, parameter.Type = p.Input.Address, p.Input.Type
		}
		if p.Output != nil {
			parameter.Output, parameter.Type = p.Output.Address, p.Output.Type
		}
		switch parameter.Type {
		case "Bool", "Int", "Float":
			avatar.Parameters = append(avatar.Parameters, parameter)
		}
	}
	return avatar, nil
}
