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
	"vrcove/companion/internal/retry"
)

var avatar = regexp.MustCompile(`\bavtr_[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)

const (
	maxPending = 10000
	batchSize  = 5
	// pace spaces ingestion requests: the server accepts forty per ten minutes
	// (one per fifteen seconds), and a small margin avoids hitting the limit.
	pace = 16 * time.Second
	// checkGap spaces this PC's VRChat lookups within a batch.
	checkGap = time.Second
)

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

// Verifier checks avatars with the desktop's own VRChat login. Check reports
// whether id is a public avatar; errors leave the avatar queued.
type Verifier interface {
	Ready() bool
	Check(ctx context.Context, id string) (public bool, err error)
}

type State struct {
	// Verifier is the desktop's VRChat login. It is never saved here.
	Verifier Verifier          `json:"-"`
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
	// RetryReason says why uploads wait until RetryAt, such as which service
	// rate limited them.
	RetryReason string `json:"retryReason,omitempty"`
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

// Submit processes one batch of up to five queued IDs:
//
//  1. Ask VRCove which IDs it already has (a catalog-only check that does not
//     use ingestion quota). Those are marked existing without asking VRChat.
//  2. Check the rest with this PC's VRChat login. Private, missing or invalid
//     avatars are rejected locally and never sent.
//  3. Send only IDs that passed. The server verifies each one again with its
//     own session before adding it.
//
// Rate limits, connection problems and expired logins leave unfinished IDs
// queued and set RetryAt; collection continues meanwhile.
func (s *State) Submit(ctx context.Context, client *http.Client) error {
	if len(s.Pending) == 0 {
		return nil
	}
	if time.Now().Before(s.RetryAt) {
		s.Status = s.WaitingStatus()
		return nil
	}
	if s.Config.Token == "" {
		s.Status = "Collecting locally · add an API key in Settings to upload"
		return nil
	}
	if s.Verifier == nil || !s.Verifier.Ready() {
		s.Status = "Collecting locally · sign in to VRChat in Settings to upload"
		return nil
	}
	origin, err := ValidateServer(s.Config.Server)
	if err != nil {
		return err
	}
	batch := append([]string(nil), s.Pending[:min(batchSize, len(s.Pending))]...)
	existing, err := s.known(ctx, client, origin, batch)
	if err != nil {
		return s.retry(err)
	}
	var upload []string
	var checkErr error
	checked := 0
	for _, id := range batch {
		switch {
		case slices.Contains(existing, id):
			s.settle(id, "existing")
		case !avatar.MatchString(id) || len(id) != 41:
			s.settle(id, "rejected")
		default:
			if checked > 0 && !sleep(ctx, checkGap) {
				checkErr = context.Canceled
			}
			if checkErr == nil {
				checked++
				var public bool
				if public, checkErr = s.Verifier.Check(ctx, id); checkErr == nil {
					if public {
						upload = append(upload, id)
					} else {
						s.settle(id, "rejected")
					}
				}
			}
		}
		if checkErr != nil {
			break
		}
	}
	// IDs that already passed are sent even if a later check failed, so the
	// check isn't repeated. The failed and unchecked IDs stay queued.
	if len(upload) > 0 {
		if err := s.ingest(ctx, client, origin, upload); err != nil {
			return s.retry(err)
		}
	}
	if checkErr != nil {
		return s.retry(checkErr)
	}
	if len(upload) == 0 {
		s.Failures, s.RetryAt, s.RetryReason = 0, time.Time{}, ""
	}
	return nil
}

// ingest sends IDs that passed local checks and records the server's results.
func (s *State) ingest(ctx context.Context, client *http.Client, origin string, ids []string) error {
	resp, err := s.post(ctx, client, origin+"/api/logger/avatars", ids)
	if err != nil {
		return errors.New("VRCove connection unavailable · uploads will retry")
	}
	defer resp.Body.Close()
	if err := responseError(resp, "Upload"); err != nil {
		return err
	}
	var out struct {
		Results []Result `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&out); err != nil {
		return errors.New("Invalid VRCove response · uploads will retry")
	}
	acked := map[string]bool{}
	for _, result := range out.Results {
		if !slices.Contains(ids, result.ID) || acked[result.ID] {
			continue
		}
		switch result.Status {
		case "added", "existing", "rejected":
			acked[result.ID] = true
			s.settle(result.ID, result.Status)
		}
	}
	// The server stops early when VRChat limits its verification; the rest of
	// the batch waits for the time it gives.
	if resp.Header.Get("Retry-After") != "" {
		return limit(resp)
	}
	if len(acked) != len(ids) {
		return errors.New("Some avatars are awaiting verification · will retry")
	}
	s.Failures = 0
	s.RetryAt, s.RetryReason = time.Now().Add(pace), "Pacing uploads"
	return nil
}

// known asks which IDs VRCove already has. Only the catalog is read.
func (s *State) known(ctx context.Context, client *http.Client, origin string, ids []string) ([]string, error) {
	resp, err := s.post(ctx, client, origin+"/api/logger/known", ids)
	if err != nil {
		return nil, errors.New("VRCove connection unavailable · will retry")
	}
	defer resp.Body.Close()
	if err := responseError(resp, "Catalog check"); err != nil {
		return nil, err
	}
	var out struct {
		Existing *[]string `json:"existing"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&out) != nil || out.Existing == nil {
		return nil, errors.New("Invalid VRCove response · will retry")
	}
	var existing []string
	for _, id := range *out.Existing {
		if slices.Contains(ids, id) && !slices.Contains(existing, id) {
			existing = append(existing, id)
		}
	}
	return existing, nil
}

// post sends IDs with the API key. VRChat credentials never go to VRCove.
func (s *State) post(ctx context.Context, client *http.Client, url string, ids []string) (*http.Response, error) {
	body, _ := json.Marshal(map[string]any{"ids": ids})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	origin, _ := ValidateServer(s.Config.Server)
	req.Header.Set("Authorization", "Bearer "+s.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	return client.Do(req)
}

// responseError explains a non-success VRCove response.
func responseError(resp *http.Response, action string) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusTooManyRequests:
		return limit(resp)
	case http.StatusUnauthorized:
		return errors.New("API key expired or revoked · generate a new key on VRCove")
	default:
		return fmt.Errorf("%s returned %d · will retry", action, resp.StatusCode)
	}
}

// limit reads a VRCove rate limit and which service caused it.
func limit(resp *http.Response) *retry.Limited {
	source := "VRCove"
	if resp.Header.Get("X-RateLimit-Source") == "vrchat" {
		source = "VRChat (server)"
	}
	return &retry.Limited{Source: source, Until: retry.After(resp.Header.Get("Retry-After"), time.Now())}
}

// retry keeps unfinished IDs queued. Rate limits wait exactly as long as the
// service asked; other failures back off exponentially up to about five minutes.
func (s *State) retry(err error) error {
	until, reason := time.Time{}, ""
	var limited *retry.Limited
	if errors.As(err, &limited) {
		until, reason = limited.Until, limited.Source+" rate limit"
	} else {
		s.Failures = min(s.Failures+1, 6)
		until = time.Now().Add(time.Duration(1<<s.Failures) * 5 * time.Second)
		reason = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(err.Error(), " · collecting locally"), " · will retry"), " · uploads will retry")
	}
	// Never shorten a wait already set in this batch, such as upload pacing.
	if until.After(s.RetryAt) {
		s.RetryAt, s.RetryReason = until, reason
	}
	return err
}

// WaitingStatus says why uploads are waiting and until when.
func (s *State) WaitingStatus() string {
	reason := s.RetryReason
	if reason == "" {
		reason = "Uploads waiting to retry"
	}
	return fmt.Sprintf("%s · retry at %s · collecting locally", reason, s.RetryAt.Local().Format("15:04:05"))
}

// settle records a final result for id and removes it from the queue.
func (s *State) settle(id, status string) {
	switch status {
	case "added":
		s.Added++
	case "existing":
		s.Existing++
	case "rejected":
		s.Rejected++
	}
	s.Seen[id] = status
	s.Pending = slices.DeleteFunc(s.Pending, func(p string) bool { return p == id })
	s.event(id, strings.ToUpper(status[:1])+status[1:])
}

// sleep waits d unless ctx ends first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
