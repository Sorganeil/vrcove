package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func buildTarGz(t *testing.T, entryName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: entryName, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, entryName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v0.2.0", "v0.1.5", 1, true},
		{"v0.2.0", "v0.2.0", 0, true},
		{"v0.1.5", "v0.2.0", -1, true},
		{"v0.2.0", "v0.2.0-rc.1", 1, true},
		{"v0.2.0-rc.2", "v0.2.0-rc.1", 1, true},
		{"dev", "v0.2.0", 0, false},
	}
	for _, tc := range cases {
		got, ok := CompareVersions(tc.a, tc.b)
		if got != tc.want || ok != tc.ok {
			t.Errorf("CompareVersions(%q, %q) = (%d, %v), want (%d, %v)", tc.a, tc.b, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCheckAndInstallLinuxAndWindows(t *testing.T) {
	for _, platform := range []struct {
		goos, archive, entry, binary string
		makeArchive                  func(*testing.T, string, []byte) []byte
	}{
		{"linux", "vrcove-desktop-v0.3.0-linux-amd64.tar.gz", "vrcove-desktop-v0.3.0-linux-amd64/vrcove-desktop", "vrcove-desktop", buildTarGz},
		{"windows", "vrcove-desktop-v0.3.0-windows-amd64.zip", "vrcove-desktop-v0.3.0-windows-amd64/vrcove-desktop.exe", "vrcove-desktop.exe", buildZip},
	} {
		t.Run(platform.goos, func(t *testing.T) {
			newBinary := []byte("updated-binary-" + platform.goos)
			archiveBytes := platform.makeArchive(t, platform.entry, newBinary)
			sum := sha256.Sum256(archiveBytes)
			sumsContent := hex.EncodeToString(sum[:]) + "  " + platform.archive + "\n"

			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/Sorganeil/vrcove/releases/latest":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"tag_name": "v0.3.0",
						"html_url": "https://github.com/Sorganeil/vrcove/releases/tag/v0.3.0",
						"assets": []map[string]any{
							{"name": platform.archive, "browser_download_url": srv.URL + "/download/" + platform.archive},
							{"name": "SHA256SUMS", "browser_download_url": srv.URL + "/download/SHA256SUMS"},
						},
					})
				case "/download/" + platform.archive:
					_, _ = w.Write(archiveBytes)
				case "/download/SHA256SUMS":
					_, _ = io.WriteString(w, sumsContent)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			exePath := filepath.Join(t.TempDir(), platform.binary)
			if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
				t.Fatal(err)
			}

			u := New("v0.2.0")
			u.APIBase = srv.URL
			u.GOOS = platform.goos
			u.GOARCH = "amd64"
			u.ExecPath = func() (string, error) { return exePath, nil }

			st, err := u.Check(context.Background())
			if err != nil {
				t.Fatalf("Check error = %v", err)
			}
			if !st.Available || st.Checking || st.Installing || st.LatestVersion != "v0.3.0" || st.AssetName != platform.archive {
				t.Fatalf("Check status = %+v", st)
			}

			st, err = u.Install(context.Background())
			if err != nil {
				t.Fatalf("Install error = %v", err)
			}
			if !st.Installed || st.Available || st.Checking || st.Installing {
				t.Fatalf("Install status = %+v", st)
			}
			got, err := os.ReadFile(exePath)
			if err != nil || !bytes.Equal(got, newBinary) {
				t.Fatalf("installed binary = %q, err = %v", got, err)
			}
		})
	}
}

func TestTamperedArchiveIsRejected(t *testing.T) {
	archiveBytes := buildTarGz(t, "vrcove-desktop-v0.3.0-linux-amd64/vrcove-desktop", []byte("legit"))
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Sorganeil/vrcove/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.3.0",
				"html_url": "https://github.com/Sorganeil/vrcove/releases/tag/v0.3.0",
				"assets": []map[string]any{
					{"name": "vrcove-desktop-v0.3.0-linux-amd64.tar.gz", "browser_download_url": srv.URL + "/download/archive"},
					{"name": "SHA256SUMS", "browser_download_url": srv.URL + "/download/SHA256SUMS"},
				},
			})
		case "/download/archive":
			_, _ = w.Write(archiveBytes)
		case "/download/SHA256SUMS":
			_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  vrcove-desktop-v0.3.0-linux-amd64.tar.gz\n"))
		}
	}))
	defer srv.Close()

	exePath := filepath.Join(t.TempDir(), "vrcove-desktop")
	_ = os.WriteFile(exePath, []byte("original"), 0o755)

	u := New("v0.2.0")
	u.APIBase = srv.URL
	u.GOOS = "linux"
	u.GOARCH = "amd64"
	u.ExecPath = func() (string, error) { return exePath, nil }

	if _, err := u.Install(context.Background()); err == nil {
		t.Fatal("expected tampered archive to be rejected")
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "original" {
		t.Fatalf("executable was modified despite failed verification: %q", got)
	}
}
