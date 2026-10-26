// Package unitypackage restores files from Unity's GUID-based package layout.
package unitypackage

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxExpanded = 4 << 30
const maxEntries = 200000

var guid = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

type Result struct {
	Folder string `json:"folder"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// Extract uses a fresh directory inside destination. Partial output is removed
// on failure. Packages are read as data; scripts and editor hooks never run.
func Extract(ctx context.Context, source, destination string, metadata bool) (result Result, err error) {
	f, err := os.Open(source)
	if err != nil {
		return result, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<30 {
		return result, errors.New("Choose a Unity package smaller than 1 GB.")
	}
	paths := map[string]string{}
	used := map[string]bool{}
	err = walk(ctx, f, func(h *tar.Header, r io.Reader) error {
		id, member, ok := part(h.Name)
		if !ok || member != "pathname" {
			return nil
		}
		if h.Size > 4096 {
			return errors.New("A package pathname is too long.")
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		name := strings.TrimRight(string(data), "\x00\r\n")
		if !safePath(name) {
			return fmt.Errorf("Unsafe package pathname: %q", name)
		}
		key := strings.ToLower(name)
		if _, exists := paths[id]; exists || used[key] {
			return errors.New("The package contains duplicate paths.")
		}
		paths[id], used[key] = name, true
		return nil
	})
	if err != nil {
		return result, err
	}
	if len(paths) == 0 {
		return result, errors.New("This file contains no Unity package assets.")
	}
	if metadata {
		for _, name := range paths {
			if used[strings.ToLower(name+".meta")] {
				return result, errors.New("An asset conflicts with a metadata file.")
			}
		}
	}
	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	// The package filename is only a label, never an output path.
	base = regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(base, "_")
	if len(base) > 60 {
		base = base[:60]
	}
	folder, err := os.MkdirTemp(destination, base+"-extracted-")
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(folder)
		}
	}()
	root, err := os.OpenRoot(folder)
	if err != nil {
		return result, err
	}
	defer root.Close()
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return result, err
	}
	written := map[string]bool{}
	result.Folder = folder
	err = walk(ctx, f, func(h *tar.Header, r io.Reader) error {
		id, member, ok := part(h.Name)
		name, exists := paths[id]
		if !ok || !exists || (member != "asset" && member != "asset.meta") {
			return nil
		}
		if member == "asset.meta" {
			if !metadata {
				return nil
			}
			name += ".meta"
		}
		key := strings.ToLower(name)
		if written[key] {
			return errors.New("The package contains a duplicate asset.")
		}
		written[key] = true
		if err := root.MkdirAll(path.Dir(name), 0700); err != nil {
			return err
		}
		out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(out, r)
		closeErr := out.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		result.Files++
		result.Bytes += n
		return nil
	})
	if err == nil && result.Files == 0 {
		err = errors.New("This package contains no files to extract.")
	}
	return result, err
}

func part(name string) (string, string, bool) {
	p := strings.Split(strings.TrimPrefix(name, "./"), "/")
	if len(p) != 2 || !guid.MatchString(p[0]) {
		return "", "", false
	}
	return p[0], p[1], true
}

// Use rules safe on Windows and Linux, even when extracting on Linux now and
// copying the output to Windows later.
func safePath(name string) bool {
	if !utf8.ValidString(name) || name == "" || path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") {
		return false
	}
	for _, p := range strings.Split(name, "/") {
		if p == "." || p == ".." || strings.HasSuffix(p, ".") || strings.HasSuffix(p, " ") || strings.ContainsAny(p, `<>"|?*`) {
			return false
		}
		for _, r := range p {
			if r < 32 {
				return false
			}
		}
		stem := strings.ToUpper(strings.SplitN(p, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9') {
			return false
		}
	}
	return true
}

func walk(ctx context.Context, f io.Reader, visit func(*tar.Header, io.Reader) error) error {
	gz, err := gzip.NewReader(f)
	if err != nil {
		return errors.New("This is not a readable Unity package.")
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxExpanded + 1}
	tr := tar.NewReader(limited)
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count > maxEntries {
			return errors.New("This package has too many entries.")
		}
		h, err := tr.Next()
		if err == io.EOF {
			// Read the gzip trailer, so truncated/corrupt archives are not accepted.
			_, err = io.Copy(io.Discard, limited)
			if limited.N <= 0 {
				return errors.New("This package expands beyond 4 GB.")
			}
			return err
		}
		if err != nil {
			return err
		}
		if h.Size < 0 || h.Size > limited.N || limited.N <= 0 {
			return errors.New("This package expands beyond 4 GB.")
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return errors.New("Packages containing links or special files are not supported.")
		}
		if err := visit(h, tr); err != nil {
			return err
		}
	}
}
