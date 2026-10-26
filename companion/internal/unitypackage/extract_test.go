package unitypackage

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, entries [][3]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "test.unitypackage")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := byte(tar.TypeReg)
		if e[2] == "link" {
			typ = tar.TypeSymlink
		}
		if err := tw.WriteHeader(&tar.Header{Name: e[0], Size: int64(len(e[1])), Mode: 0600, Typeflag: typ, Linkname: "/etc/passwd"}); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e[1])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestExtractRestoresNamesAndMetadata(t *testing.T) {
	source := fixture(t, [][3]string{{id + "/asset", "fake fbx data", ""}, {id + "/asset.meta", "guid: abc", ""}, {id + "/pathname", "Assets/My Avatar/body.fbx\n", ""}, {id + "/preview.png", "not an asset", ""}})
	for _, metadata := range []bool{false, true} {
		dest := t.TempDir()
		got, err := Extract(context.Background(), source, dest, metadata)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filepath.Join(got.Folder, "Assets/My Avatar/body.fbx"))
		if err != nil || string(content) != "fake fbx data" {
			t.Fatalf("asset=%s, %v", content, err)
		}
		want := 1
		if metadata {
			want = 2
		}
		if got.Files != want {
			t.Errorf("files=%d", got.Files)
		}
		_, err = os.Stat(filepath.Join(got.Folder, "Assets/My Avatar/body.fbx.meta"))
		if metadata && err != nil || !metadata && !os.IsNotExist(err) {
			t.Errorf("metadata=%v err=%v", metadata, err)
		}
		again, err := Extract(context.Background(), source, dest, metadata)
		if err != nil || again.Folder == got.Folder {
			t.Fatalf("repeat: %+v %v", again, err)
		}
	}
}
func TestRejectsUnsafePathsAndRollsBack(t *testing.T) {
	for _, name := range []string{"../outside", "/tmp/escape", "Assets/../../escape", `C:\escape`, "Assets/NUL.txt", "Assets/test./file", "Assets/a\x00b", "Assets/a:b", "Assets/CON/file"} {
		t.Run(name, func(t *testing.T) {
			source := fixture(t, [][3]string{{id + "/pathname", name, ""}, {id + "/asset", "x", ""}})
			dest := t.TempDir()
			if _, err := Extract(context.Background(), source, dest, true); err == nil {
				t.Fatal("unsafe package accepted")
			}
			entries, _ := os.ReadDir(dest)
			if len(entries) > 0 {
				t.Fatal("partial files left")
			}
		})
	}
	source := fixture(t, [][3]string{{id + "/pathname", "Assets/a", ""}, {id + "/asset", "", "link"}})
	if _, err := Extract(context.Background(), source, t.TempDir(), true); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestRejectsDuplicateAssets(t *testing.T) {
	source := fixture(t, [][3]string{{id + "/pathname", "Assets/a", ""}, {id + "/asset", "first", ""}, {id + "/asset", "second", ""}})
	dest := t.TempDir()
	if _, err := Extract(context.Background(), source, dest, true); err == nil {
		t.Fatal("duplicate accepted")
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Fatal("partial output left")
	}
}
func TestRejectsCorruptAndCancelledPackage(t *testing.T) {
	source := fixture(t, [][3]string{{id + "/pathname", "Assets/a", ""}, {id + "/asset", strings.Repeat("x", 1000), ""}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Extract(ctx, source, t.TempDir(), true); err == nil {
		t.Fatal("cancelled extraction succeeded")
	}
	b, _ := os.ReadFile(source)
	os.WriteFile(source, b[:len(b)-5], 0600)
	if _, err := Extract(context.Background(), source, t.TempDir(), true); err == nil {
		t.Fatal("truncated package accepted")
	}
}
