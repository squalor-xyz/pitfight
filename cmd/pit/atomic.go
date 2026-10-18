// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeFileAtomic makes the file path hold what fill writes. fill writes to a new
// temporary file in the same directory, and only if that and the close succeed is the
// file renamed over path, so a failure at any point leaves path as it was (absent, or
// the old file) and no temporary file behind. A new file has mode perm less the
// umask; a regular file that is replaced keeps its mode. The data is synced to disk
// before the rename.
//
// If path exists and is not a regular file (a symbolic link, /dev/null, a named
// pipe) it is written in place instead, because renaming over it would replace the
// link or the device with a regular file.
func writeFileAtomic(path string, perm os.FileMode, fill func(w io.Writer) error) (err error) {
	fi, statErr := os.Lstat(path)
	if statErr == nil && !fi.Mode().IsRegular() {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
		if err != nil {
			return err
		}
		if err := fill(f); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}

	dir, base := filepath.Split(path)
	var f *os.File
	var tmp string
	for i := 0; ; i++ {
		tmp = filepath.Join(dir, fmt.Sprintf(".%s.tmp%d.%d", base, os.Getpid(), i))
		f, err = os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if !errors.Is(err, os.ErrExist) || i >= 100 {
			break
		}
	}
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(tmp)
		}
	}()
	if err = fill(f); err != nil {
		f.Close()
		return err
	}
	if statErr == nil {
		if err = f.Chmod(fi.Mode().Perm()); err != nil {
			f.Close()
			return err
		}
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
