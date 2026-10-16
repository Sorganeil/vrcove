package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListenRefusesPublicAddresses(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "[::]:0", "localhost:0"} {
		if ln, err := listen(addr, true); err == nil {
			ln.Close()
			t.Errorf("tunnel-only listen accepted %s", addr)
		}
	}
	ln, err := listen("127.0.0.1:0", true)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

func TestListenUnixSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vrcove.sock")
	ln, err := listen("unix:"+path, true)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o660 {
		t.Errorf("socket mode = %v, %v", info.Mode(), err)
	}
	ln.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("socket not removed on close")
	}
}
