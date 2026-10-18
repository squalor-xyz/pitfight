// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// names returns the names of the entries of dir.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// TestWriteFileAtomic: a write that works replaces the file; a write that fails
// partway leaves no partial file at the target (none if there was none, the old file
// if there was one) and no temporary file in the directory.
func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "match.jsonl")
	boom := errors.New("disk full")
	partial := func(w io.Writer) error {
		if _, err := w.Write([]byte("partial")); err != nil {
			return err
		}
		return boom
	}

	// A failed write to a new path leaves nothing.
	if err := writeFileAtomic(target, 0o644, partial); !errors.Is(err, boom) {
		t.Fatalf("error %v, want %v", err, boom)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Errorf("after a failed write the directory holds %q, want nothing", got)
	}

	// A good write creates the file with the content, mode 0644 less the umask.
	if err := writeFileAtomic(target, 0o644, func(w io.Writer) error { _, err := w.Write([]byte("old")); return err }); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("read %q, %v; want old", got, err)
	}
	if got := names(t, dir); len(got) != 1 || got[0] != "match.jsonl" {
		t.Errorf("directory holds %q, want only match.jsonl", got)
	}
	if fi, err := os.Stat(target); err != nil || fi.Mode().Perm()&^0o644 != 0 || fi.Mode().Perm()&0o400 == 0 {
		t.Errorf("mode %v, err %v; want at most 0644 and readable by the owner", fi.Mode().Perm(), err)
	}

	// A failed write over an existing file leaves that file as it was.
	if err := writeFileAtomic(target, 0o644, partial); !errors.Is(err, boom) {
		t.Fatalf("error %v, want %v", err, boom)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Errorf("after a failed overwrite the file holds %q, %v; want old", got, err)
	}
	if got := names(t, dir); len(got) != 1 {
		t.Errorf("directory holds %q, want only match.jsonl", got)
	}

	// A good write over an existing file replaces it.
	if err := writeFileAtomic(target, 0o644, func(w io.Writer) error { _, err := w.Write([]byte("new")); return err }); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Errorf("read %q, %v; want new", got, err)
	}

	// A directory that does not exist is an error, and so is a directory as target.
	if err := writeFileAtomic(filepath.Join(dir, "missing", "x"), 0o644, partial); err == nil {
		t.Error("no error for a missing directory")
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "d"), 0o644, func(w io.Writer) error { return nil }); err == nil {
		t.Error("no error for a directory as target")
	}
	if got := names(t, dir); len(got) != 2 {
		t.Errorf("directory holds %q, want match.jsonl and d", got)
	}
}

// TestWriteFileAtomicSpecialTarget: a target that is not a regular file, such as a
// symbolic link, is written through, not replaced.
func TestWriteFileAtomicSpecialTarget(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", link); err != nil {
		t.Skip("no symbolic links:", err)
	}
	if err := writeFileAtomic(link, 0o644, func(w io.Writer) error { _, err := w.Write([]byte("y")); return err }); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", fi, err)
	}
	if got, _ := os.ReadFile(real); string(got) != "y" {
		t.Errorf("real holds %q, want y", got)
	}
}

// TestWriteFileAtomicKeepsMode: replacing an existing regular file keeps its mode,
// as writing into it would; perm applies only to a file that is new.
func TestWriteFileAtomicKeepsMode(t *testing.T) {
	dir := t.TempDir()
	write := func(w io.Writer) error { _, err := w.Write([]byte("new")); return err }
	for _, mode := range []os.FileMode{0o600, 0o640} {
		target := filepath.Join(dir, mode.String())
		if err := os.WriteFile(target, []byte("old"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, mode); err != nil { // not changed by the umask
			t.Fatal(err)
		}
		if err := writeFileAtomic(target, 0o644, write); err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Stat(target); err != nil || fi.Mode().Perm() != mode {
			t.Errorf("mode %v after the overwrite (%v), want %v", fi.Mode().Perm(), err, mode)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
			t.Errorf("read %q, %v; want new", got, err)
		}
	}
}
