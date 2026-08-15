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
	return l.fp.Sync()
}

// Read decodes the next entry. eof is true once the log is exhausted at a
// record boundary, which is the normal way to end a replay loop.
//
// A record that stops halfway is reported as an error, not as eof: the log is
// damaged and pretending otherwise would silently drop data.
func (l *Log) Read(ent *Entry) (eof bool, err error) {
	err = ent.Decode(l.fp)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

// Sync flushes the operating system's buffers to the physical disk. Write does
// this already; the method stays for callers that write through fp directly, or
// for a future batched mode where Write stops syncing every record.
func (l *Log) Sync() error {
	return l.fp.Sync()
}
