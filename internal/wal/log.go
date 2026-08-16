package wal

import (
	"errors"
	"io"
	"os"
)

// Log is an append-only file of encoded entries.
//
// Open leaves the read cursor at the start, so the caller replays with Read
// until eof. That same cursor is what Write appends at, so a full replay must
// finish before the first Write.
type Log struct {
	FileName string
	fp       *os.File

	// offset is the end of the last intact record, which is where a torn tail
	// gets cut back to.
	offset int64
}

// Open opens the log file, creating it if it does not exist.
func (l *Log) Open() (err error) {
	l.fp, err = createFileSync(l.FileName)
	return err
}

// createFileSync opens file for reading and writing, creating it if needed, and
// makes sure the directory entry itself is on disk before returning.
func createFileSync(file string) (*os.File, error) {
	fp, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}

	// syncDir takes the full path and flushes the directory containing it.
	// Passing only the base name here would flush the process's working
	// directory instead — which is usually some other directory entirely, so the
	// new file's name could still be lost on power failure.
	if err := syncDir(file); err != nil {
		fp.Close()
		return nil, err
	}

	return fp, nil
}

// Close closes the log file.
func (l *Log) Close() error {
	return l.fp.Close()
}

// Write appends one entry to the end of the log and flushes it to the physical
// disk before returning.
//
// The fsync is what makes a successful return mean something: once Write
// returns nil, the entry survives a power cut. It is also by far the slowest
// part of a write — see BenchmarkWrite against BenchmarkWriteNoSync.
func (l *Log) Write(ent *Entry) error {
	if _, err := l.fp.Write(ent.Encode()); err != nil {
		return err
	}
	if err := l.fp.Sync(); err != nil {
		return err
	}

	l.offset += int64(ent.Size())
	return nil
}

// Read decodes the next entry. eof is true once the log is exhausted, which is
// the normal way to end a replay loop.
//
// A record left half-written by a crash also ends the log: it was never
// acknowledged to anyone, so dropping it loses nothing that was promised. Read
// cuts the file back to the last intact record before reporting eof, so the
// next Write appends to good data instead of to garbage.
func (l *Log) Read(ent *Entry) (eof bool, err error) {
	err = ent.Decode(l.fp)

	switch {
	case err == nil:
		l.offset += int64(ent.Size())
		return false, nil

	case errors.Is(err, io.EOF):
		return true, nil

	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, ErrBadSum):
		if err := l.truncateTail(); err != nil {
			return false, err
		}
		return true, nil

	default:
		// ErrCorruptEntry and anything else: the bytes are intact but make no
		// sense, so this is not a torn write and must not be papered over.
		return false, err
	}
}

// truncateTail drops everything after the last intact record and puts the write
// cursor there.
//
// Without the seek, the cursor would still sit wherever the failed read stopped,
// and the next Write would land past the garbage. The next replay would then hit
// that garbage, stop early, and silently lose every record written after the
// crash.
func (l *Log) truncateTail() error {
	if err := l.fp.Truncate(l.offset); err != nil {
		return err
	}
	if _, err := l.fp.Seek(l.offset, io.SeekStart); err != nil {
		return err
	}
	return l.fp.Sync()
}

// Sync flushes the operating system's buffers to the physical disk. Write does
// this already; the method stays for callers that write through fp directly, or
// for a future batched mode where Write stops syncing every record.
func (l *Log) Sync() error {
	return l.fp.Sync()
}
