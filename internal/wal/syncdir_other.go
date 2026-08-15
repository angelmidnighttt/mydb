//go:build !unix

package wal

// syncDir does nothing outside Unix.
//
// Windows has no equivalent operation to ask for: NTFS journals the metadata of
// a file creation itself, so a file that was successfully created does not need
// its directory flushed separately. Opening a directory as a handle and syncing
// it is not permitted there either.
func syncDir(name string) error {
	return nil
}
