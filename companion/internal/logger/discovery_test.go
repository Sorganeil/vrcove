package logger

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestDiscoverExternalSteamLibrary(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(strconv.FormatBool(legacy), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "Steam")
			library := filepath.Join(t.TempDir(), "Another drive", "SteamLibrary")
			folder := filepath.Join(library, "steamapps", "compatdata", "438100", "pfx", "drive_c", "users", "custom-user", "AppData", "Local", "Temp", "VRChat", "VRChat")
			if err := os.MkdirAll(filepath.Join(root, "steamapps"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(folder, 0700); err != nil {
				t.Fatal(err)
			}
			key := `"path"`
			if legacy {
				key = `"1"`
			}
			vdf := `"libraryfolders" { "0" { ` + key + ` ` + strconv.Quote(library) + ` } }`
			if err := os.WriteFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, "amplitude.cache"), []byte(`{"avatar":"`+id+`"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if got := discoverSteamFolder([]string{root}, nil); got != canonicalPath(folder) {
				t.Fatalf("detected %q, want %q", got, folder)
			}
		})
	}
}

func TestDiscoveryPrefersNewestCacheOverEmptyFolder(t *testing.T) {
	var folders []string
	for i := range 3 {
		folder := filepath.Join(t.TempDir(), "VRChat")
		if err := os.Mkdir(folder, 0700); err != nil {
			t.Fatal(err)
		}
		folders = append(folders, folder)
		if i == 0 {
			continue
		}
		path := filepath.Join(folder, "amplitude.cache")
		if err := os.WriteFile(path, []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(i), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if got := newestAnalyticsFolder(folders); got != folders[2] {
		t.Fatalf("did not prefer newest cache: %q", got)
	}
}
