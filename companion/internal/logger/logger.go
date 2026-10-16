// Package logger extracts avatar IDs from the local VRChat analytics file.
// It has no GUI dependencies; all state is owned by the caller's worker goroutine.
package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"vrcove/companion/internal/buildinfo"
)

var avatar = regexp.MustCompile(`\bavtr_[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)

const maxPending = 10000

type Config struct {
	Folder string `json:"folder"`
	Server string `json:"server"`
	Token  string `json:"token,omitempty"`
}

type Cursor struct {
	// Only a fingerprint of the analytics snapshot is retained.
	First string `json:"first"`
}

type Event struct{ Time, ID, Result string }
type State struct {
	Config   Config            `json:"config"`
	Files    map[string]Cursor `json:"files"`
	Seen     map[string]string `json:"seen"`
	Pending  []string          `json:"pending"`
	Events   []Event           `json:"events"`
	Added    int               `json:"added"`
	Existing int               `json:"existing"`
	Rejected int               `json:"rejected"`
	Status   string            `json:"-"`
	RetryAt  time.Time         `json:"retryAt,omitempty"`
	Failures int               `json:"failures,omitempty"`
}

func New() *State {
	return &State{Config: Config{Server: "https://vrcove.st"}, Files: map[string]Cursor{}, Seen: map[string]string{}}
}

func (s *State) QueueFull() bool { return len(s.Pending) >= maxPending }

func Load(path string) (*State, error) {
	s := New()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("read saved logger state: %w", err)
	}
	if s.Files == nil {
		s.Files = map[string]Cursor{}
	}
	if s.Seen == nil {
		s.Seen = map[string]string{}
	}
	return s, nil
}

// Save replaces the state only after a complete write. A failed save must stop
// collection, so a restart cannot silently skip IDs that were not persisted.
func (s *State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".logger-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

func IDs(line string) []string {
	ids := avatar.FindAllString(line, -1)
	for i := range ids {
		ids[i] = strings.ToLower(ids[i])
	}
	return ids
}

func (s *State) event(id, result string) {
	s.Events = append([]Event{{time.Now().Format("15:04"), id, result}}, s.Events...)
	if len(s.Events) > 8 {
		s.Events = s.Events[:8]
	}
}

// Scan reads analytics snapshots without retaining or uploading telemetry.
func (s *State) Scan() error {
	info, err := os.Stat(s.Config.Folder)
	if err != nil || !info.IsDir() {
		return errors.New("Choose the VRChat analytics folder")
	}
	live := map[string]bool{}
	analytics := 0
	for _, path := range amplitudePaths(s.Config.Folder) {
		present, err := s.scanAmplitude(path)
		if err != nil {
			return err
		}
		if present {
			analytics++
			live[path] = true
		}
	}
	for path := range s.Files {
		if !live[path] {
			delete(s.Files, path)
		}
	}
	if analytics == 0 {
		s.Status = "Waiting for VRChat activity"
	} else {
		s.Status = "Watching VRChat analytics"
	}
	if len(s.Pending) >= maxPending {
		s.Status = "Queue full · connect to upload before collecting more"
	}
	return nil
}

func ValidateServer(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("Enter a server origin, such as https://vrcove.st")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return "", errors.New("Use HTTPS, or HTTP on localhost for development")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

type Result struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Submit uses one bounded batch. The client must not follow redirects with a
// bearer token. A partial/invalid response leaves unacknowledged IDs queued.
func (s *State) Submit(ctx context.Context, client *http.Client) error {
	if len(s.Pending) == 0 {
		return nil
	}
	if time.Now().Before(s.RetryAt) {
		s.Status = "Uploads waiting to retry · collecting locally"
		return nil
	}
	if s.Config.Token == "" {
		s.Status = "Collecting locally · uploads off"
		return nil
	}
	origin, err := ValidateServer(s.Config.Server)
	if err != nil {
		return err
	}
	n := min(5, len(s.Pending))
	batch := append([]string(nil), s.Pending[:n]...)
	body, _ := json.Marshal(map[string]any{"ids": batch})
	req, err := http.NewRequestWithContext(ctx, "POST", origin+"/api/logger/avatars", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return s.retry(errors.New("Connection unavailable · uploads will retry"))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return s.retry(errors.New("API key expired or revoked · generate a new key on VRCove"))
	}
	if resp.StatusCode != http.StatusOK {
		return s.retry(fmt.Errorf("Server returned %d · check connection settings", resp.StatusCode))
	}
	var out struct {
		Results []Result `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&out); err != nil {
		return s.retry(errors.New("Invalid server response · uploads will retry"))
	}
	acked := map[string]bool{}
	for _, result := range out.Results {
		if !slices.Contains(batch, result.ID) || acked[result.ID] {
			continue
		}
		switch result.Status {
		case "added":
			s.Added++
		case "existing":
			s.Existing++
		case "rejected":
			s.Rejected++
		default:
			continue
		}
		acked[result.ID] = true
		s.Seen[result.ID] = result.Status
		s.event(result.ID, strings.ToUpper(result.Status[:1])+result.Status[1:])
	}
	s.Pending = slices.DeleteFunc(s.Pending, func(id string) bool { return acked[id] })
	if len(acked) != len(batch) {
		return s.retry(errors.New("Some avatars are awaiting verification · will retry"))
	}
	s.Failures = 0
	s.RetryAt = time.Time{}
	return nil
}

func (s *State) retry(err error) error {
	s.Failures = min(s.Failures+1, 6)
	s.RetryAt = time.Now().Add(time.Duration(1<<s.Failures) * 5 * time.Second)
	return err
}
