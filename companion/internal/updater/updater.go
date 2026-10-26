// Package updater checks GitHub Releases for newer VRCove Desktop builds,
// verifies archives against SHA256SUMS, and replaces the local executable.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultRepo    = "Sorganeil/vrcove"
	DefaultAPIBase = "https://api.github.com"
	maxSumsBytes   = 64 << 10
	maxAssetBytes  = 250 << 20
)

var semverPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)

// Status describes the desktop updater state for the settings UI.
type Status struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	ReleaseURL     string `json:"releaseUrl,omitempty"`
	AssetName      string `json:"assetName,omitempty"`
	Available      bool   `json:"available"`
	Checking       bool   `json:"checking"`
	Installing     bool   `json:"installing"`
	Installed      bool   `json:"installed"`
	CheckedAt      int64  `json:"checkedAt,omitempty"`
	Error          string `json:"error,omitempty"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName    string         `json:"tag_name"`
	HTMLURL    string         `json:"html_url"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

// Updater manages checking and installing updates from GitHub Releases.
type Updater struct {
	CurrentVersion string
	Repo           string
	APIBase        string
	GOOS           string
	GOARCH         string
	Client         *http.Client
	ExecPath       func() (string, error)

	mu            sync.Mutex
	busy          bool
	status        Status
	assetURL      string
	sumsURL       string
	installedVers string
}

// New creates an Updater for the running build.
func New(currentVersion string) *Updater {
	if currentVersion == "" {
		currentVersion = "dev"
	}
	repo := strings.TrimSpace(os.Getenv("VRCOVE_UPDATE_REPO"))
	if repo == "" {
		repo = DefaultRepo
	}
	apiBase := strings.TrimSpace(os.Getenv("VRCOVE_UPDATE_API"))
	if apiBase == "" {
		apiBase = DefaultAPIBase
	}
	return &Updater{
		CurrentVersion: currentVersion,
		Repo:           repo,
		APIBase:        strings.TrimRight(apiBase, "/"),
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		ExecPath:       os.Executable,
		status:         Status{CurrentVersion: currentVersion},
	}
}

// CleanupOldExecutable removes leftover staging files from a previous update.
func CleanupOldExecutable() {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	_ = os.Remove(exe + ".old")
	_ = os.Remove(exe + ".new")
}

// Status returns a snapshot of the updater state.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status.CurrentVersion = u.CurrentVersion
	return u.status
}

// Check queries GitHub Releases for the latest version.
func (u *Updater) Check(ctx context.Context) (st Status, err error) {
	u.mu.Lock()
	if u.busy {
		st = u.status
		u.mu.Unlock()
		return st, errors.New("An update check or installation is already in progress.")
	}
	u.busy = true
	u.status.CurrentVersion = u.CurrentVersion
	u.status.Checking = true
	u.status.Error = ""
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.busy = false
		u.status.Checking = false
		st = u.status
		u.mu.Unlock()
	}()

	return u.performCheck(ctx)
}

func (u *Updater) performCheck(ctx context.Context) (Status, error) {
	fail := func(msg string) (Status, error) {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.status.Error = msg
		u.status.CheckedAt = time.Now().UnixMilli()
		return u.status, errors.New(msg)
	}

	endpoint := strings.TrimRight(u.APIBase, "/") + "/repos/" + u.Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fail("Could not check for updates.")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "VRCoveDesktop/"+u.CurrentVersion)

	resp, err := u.client().Do(req)
	if err != nil {
		return fail("Could not reach GitHub to check for updates.")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.status.LatestVersion = u.CurrentVersion
		u.status.Available = false
		u.status.CheckedAt = time.Now().UnixMilli()
		u.status.Error = ""
		return u.status, nil
	}
	if resp.StatusCode != http.StatusOK {
		return fail("GitHub update check returned status " + strconv.Itoa(resp.StatusCode) + ".")
	}

	var rel githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return fail("Could not read the release information from GitHub.")
	}
	if rel.Draft || !strings.HasPrefix(rel.TagName, "v") || !semverPattern.MatchString(rel.TagName) {
		return fail("Latest GitHub release does not have a valid version tag.")
	}
	if rel.HTMLURL != "" && !strings.HasPrefix(rel.HTMLURL, "https://") {
		rel.HTMLURL = ""
	}

	archiveName, _, ok := AssetNameFor(rel.TagName, u.goos(), u.goarch())
	var assetURL, sumsURL string
	if ok {
		for _, a := range rel.Assets {
			if a.Name == archiveName && u.allowedDownloadURL(a.BrowserDownloadURL) {
				assetURL = a.BrowserDownloadURL
			}
			if a.Name == "SHA256SUMS" && u.allowedDownloadURL(a.BrowserDownloadURL) {
				sumsURL = a.BrowserDownloadURL
			}
		}
	}

	cmp, validCurrent := CompareVersions(rel.TagName, u.CurrentVersion)
	newer := validCurrent && cmp > 0

	u.mu.Lock()
	defer u.mu.Unlock()
	u.status.CurrentVersion = u.CurrentVersion
	u.status.LatestVersion = rel.TagName
	u.status.ReleaseURL = rel.HTMLURL
	u.status.CheckedAt = time.Now().UnixMilli()
	u.status.Error = ""
	u.assetURL = assetURL
	u.sumsURL = sumsURL
	if u.installedVers == rel.TagName {
		u.status.Installed = true
		u.status.Available = false
		u.status.AssetName = archiveName
		return u.status, nil
	}
	if newer && assetURL != "" && sumsURL != "" {
		u.status.Available = true
		u.status.AssetName = archiveName
	} else {
		u.status.Available = false
		u.status.AssetName = ""
		if newer && (assetURL == "" || sumsURL == "") {
			u.status.Error = "A newer release exists, but no verified package was found for this platform."
			return u.status, errors.New(u.status.Error)
		}
	}
	return u.status, nil
}

// Install downloads, verifies and installs the latest release binary in place.
func (u *Updater) Install(ctx context.Context) (st Status, err error) {
	u.mu.Lock()
	if u.busy {
		st = u.status
		u.mu.Unlock()
		return st, errors.New("An update check or installation is already in progress.")
	}
	u.busy = true
	u.status.CurrentVersion = u.CurrentVersion
	u.status.Installing = true
	u.status.Error = ""
	needCheck := !u.status.Available || u.assetURL == "" || u.sumsURL == ""
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.busy = false
		u.status.Installing = false
		st = u.status
		u.mu.Unlock()
	}()

	if needCheck {
		if st, err := u.performCheck(ctx); err != nil {
			return st, err
		}
	}

	fail := func(msg string) (Status, error) {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.status.Error = msg
		return u.status, errors.New(msg)
	}

	u.mu.Lock()
	available := u.status.Available
	tag := u.status.LatestVersion
	assetName := u.status.AssetName
	assetURL := u.assetURL
	sumsURL := u.sumsURL
	u.mu.Unlock()

	if !available || assetURL == "" || sumsURL == "" {
		return fail("VRCove is already up to date.")
	}

	_, binaryName, ok := AssetNameFor(tag, u.goos(), u.goarch())
	if !ok {
		return fail("Updates are not supported on this platform.")
	}

	sumsBytes, err := u.download(ctx, sumsURL, maxSumsBytes)
	if err != nil {
		return fail("Could not download the release checksums.")
	}
	expectedSum, err := parseSHA256Sums(string(sumsBytes), assetName)
	if err != nil {
		return fail("Release checksum for this platform was missing or invalid.")
	}

	archiveBytes, err := u.download(ctx, assetURL, maxAssetBytes)
	if err != nil {
		return fail("Could not download the update package.")
	}
	actualSum := sha256.Sum256(archiveBytes)
	if subtle.ConstantTimeCompare(actualSum[:], expectedSum) != 1 {
		return fail("Update package failed SHA-256 verification and was rejected.")
	}

	binaryBytes, err := extractBinary(archiveBytes, assetName, binaryName)
	if err != nil {
		return fail("Could not extract the verified update package.")
	}

	execFn := u.ExecPath
	if execFn == nil {
		execFn = os.Executable
	}
	exe, err := execFn()
	if err != nil || exe == "" {
		return fail("Could not locate the running VRCove executable.")
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	if err := replaceExecutable(exe, binaryBytes); err != nil {
		return fail("Could not replace the application binary. Check folder permissions or download from GitHub.")
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.installedVers = tag
	u.status.Installed = true
	u.status.Available = false
	u.status.Error = ""
	return u.status, nil
}

func (u *Updater) client() *http.Client {
	if u.Client != nil {
		return u.Client
	}
	// GitHub redirects downloads to its file host; every hop must stay on GitHub.
	return &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !u.allowedDownloadURL(r.URL.String()) {
			return errors.New("update download left GitHub")
		}
		return nil
	}}
}

func (u *Updater) goos() string {
	if u.GOOS != "" {
		return u.GOOS
	}
	return runtime.GOOS
}

func (u *Updater) goarch() string {
	if u.GOARCH != "" {
		return u.GOARCH
	}
	return runtime.GOARCH
}

func (u *Updater) allowedDownloadURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	// Release files come only from GitHub. Loopback is allowed only when the
	// updater itself points at a local test server.
	if parsed.Scheme == "https" && (host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com")) {
		return true
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && isLoopback(host) && u.localAPI()
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// localAPI reports whether APIBase is a local test server.
func (u *Updater) localAPI() bool {
	base, err := url.Parse(u.APIBase)
	return err == nil && isLoopback(strings.ToLower(base.Hostname()))
}

func (u *Updater) download(ctx context.Context, targetURL string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "VRCoveDesktop/"+u.CurrentVersion)
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("response exceeded size limit")
	}
	return data, nil
}

// AssetNameFor returns the archive filename and inner executable name for a release tag.
func AssetNameFor(tag, goos, goarch string) (archiveName, binaryName string, ok bool) {
	if goarch == "" {
		goarch = "amd64"
	}
	switch goos {
	case "linux":
		return fmt.Sprintf("vrcove-desktop-%s-linux-%s.tar.gz", tag, goarch), "vrcove-desktop", true
	case "windows":
		return fmt.Sprintf("vrcove-desktop-%s-windows-%s.zip", tag, goarch), "vrcove-desktop.exe", true
	default:
		return "", "", false
	}
}

func parseSHA256Sums(content, targetName string) ([]byte, error) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(fields[1], "*"), "./")
		if name != targetName {
			continue
		}
		if len(fields[0]) != sha256.Size*2 {
			return nil, errors.New("invalid sha256 length")
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil {
			return nil, err
		}
		return digest, nil
	}
	return nil, errors.New("checksum not found")
}

func extractBinary(archiveBytes []byte, archiveName, binaryName string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".tar.gz") {
		gz, err := gzip.NewReader(bytes.NewReader(archiveBytes))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			if hdr.Typeflag != tar.TypeReg {
				continue
			}
			if path.Base(hdr.Name) != binaryName {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(tr, maxAssetBytes+1))
			if err != nil {
				return nil, err
			}
			if len(data) == 0 || int64(len(data)) > maxAssetBytes {
				return nil, errors.New("invalid binary size")
			}
			return data, nil
		}
		return nil, errors.New("binary not found in tar.gz")
	}
	if strings.HasSuffix(archiveName, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || path.Base(strings.ReplaceAll(f.Name, `\`, `/`)) != binaryName {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(io.LimitReader(rc, maxAssetBytes+1))
			rc.Close()
			if readErr != nil {
				return nil, readErr
			}
			if len(data) == 0 || int64(len(data)) > maxAssetBytes {
				return nil, errors.New("invalid binary size")
			}
			return data, nil
		}
		return nil, errors.New("binary not found in zip")
	}
	return nil, errors.New("unsupported archive format")
}

func replaceExecutable(exePath string, binaryBytes []byte) error {
	newPath := exePath + ".new"
	oldPath := exePath + ".old"
	_ = os.Remove(newPath)
	_ = os.Remove(oldPath)

	f, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := f.Write(binaryBytes); err != nil {
		f.Close()
		_ = os.Remove(newPath)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(newPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(newPath)
		return err
	}

	// On Windows, a running .exe cannot be overwritten directly, so rename the
	// current binary to .old first and then move .new into place.
	if runtime.GOOS != "windows" {
		if err := os.Rename(newPath, exePath); err == nil {
			return nil
		}
	}
	if err := os.Rename(exePath, oldPath); err != nil {
		_ = os.Remove(newPath)
		return err
	}
	if err := os.Rename(newPath, exePath); err != nil {
		_ = os.Rename(oldPath, exePath)
		_ = os.Remove(newPath)
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Remove(oldPath)
	}
	return nil
}

type parsedSemver struct {
	major, minor, patch int
	pre                 string
}

func parseSemver(v string) (parsedSemver, bool) {
	m := semverPattern.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return parsedSemver{}, false
	}
	maj, err1 := strconv.Atoi(m[1])
	min, err2 := strconv.Atoi(m[2])
	pat, err3 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return parsedSemver{}, false
	}
	return parsedSemver{major: maj, minor: min, patch: pat, pre: m[4]}, true
}

// CompareVersions compares two semantic version strings (with or without a leading "v").
// It returns (-1, true) if a < b, (0, true) if a == b, (1, true) if a > b, or (0, false) if either is not semver.
func CompareVersions(a, b string) (int, bool) {
	va, okA := parseSemver(a)
	vb, okB := parseSemver(b)
	if !okA || !okB {
		return 0, false
	}
	if va.major != vb.major {
		if va.major < vb.major {
			return -1, true
		}
		return 1, true
	}
	if va.minor != vb.minor {
		if va.minor < vb.minor {
			return -1, true
		}
		return 1, true
	}
	if va.patch != vb.patch {
		if va.patch < vb.patch {
			return -1, true
		}
		return 1, true
	}
	if va.pre == vb.pre {
		return 0, true
	}
	if va.pre == "" {
		return 1, true
	}
	if vb.pre == "" {
		return -1, true
	}
	partsA := strings.Split(va.pre, ".")
	partsB := strings.Split(vb.pre, ".")
	for i := 0; i < len(partsA) && i < len(partsB); i++ {
		if partsA[i] == partsB[i] {
			continue
		}
		numA, errA := strconv.Atoi(partsA[i])
		numB, errB := strconv.Atoi(partsB[i])
		if errA == nil && errB == nil {
			if numA < numB {
				return -1, true
			}
			return 1, true
		}
		if errA == nil {
			return -1, true
		}
		if errB == nil {
			return 1, true
		}
		if partsA[i] < partsB[i] {
			return -1, true
		}
		return 1, true
	}
	if len(partsA) < len(partsB) {
		return -1, true
	}
	return 1, true
}
