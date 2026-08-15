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
	l.fp, err = os.OpenFile(l.FileName, os.O_RDWR|os.O_CREATE, 0o644)
	return err
}

// Close closes the log file.
func (l *Log) Close() error {
	return l.fp.Close()
}

// Write appends one entry to the end of the log.
//
// The bytes reach the operating system but not necessarily the physical disk;
// see Sync.
func (l *Log) Write(ent *Entry) error {
	_, err := l.fp.Write(ent.Encode())
	return err
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

// Sync flushes the operating system's buffers to the physical disk. Without it
// a write survives a process crash but not a power loss.
func (l *Log) Sync() error {
	return l.fp.Sync()
}
