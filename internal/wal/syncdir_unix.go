//go:build unix

package wal

import (
	"os"
	"path/filepath"
)

// syncDir flushes the directory that holds name.
//
// On Unix, fsync on a file persists its contents but not the fact that the file
// exists: the name lives in the parent directory, which is a separate object
// with its own dirty pages. Lose power after creating a file but before the
// directory reaches disk and the data is on disk with no name pointing at it.
// Creating, renaming and deleting a file all need this.
//
// Opening the directory read-only and calling Sync is the standard library
// spelling of open(2) plus fsync(2) on a directory file descriptor. A directory
// fd cannot be read or written like a file, but it can be fsynced.
func syncDir(name string) error {
	dir, err := os.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer dir.Close()

	return dir.Sync()
}
