package wal

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func openLog(t *testing.T, path string) *Log {
	t.Helper()

	l := &Log{FileName: path}
	if err := l.Open(); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

// readAll replays the log from the current cursor to the end.
func readAll(t *testing.T, l *Log) []Entry {
	t.Helper()

	var entries []Entry
	for {
		var ent Entry
		eof, err := l.Read(&ent)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if eof {
			return entries
		}
		entries = append(entries, ent)
	}
}

func TestLogWriteThenReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")

	want := []Entry{
		{Key: []byte("name"), Val: []byte("mydb")},
		{Key: []byte("lang"), Val: []byte("go")},
		{Key: []byte("name"), Deleted: true},
	}

	l := openLog(t, path)
	for _, ent := range want {
		if err := l.Write(&ent); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	got := readAll(t, openLog(t, path))
	if len(got) != len(want) {
		t.Fatalf("replayed %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i].Key, want[i].Key) || got[i].Deleted != want[i].Deleted {
			t.Errorf("entry %d = (%q, deleted=%v), want (%q, deleted=%v)",
				i, got[i].Key, got[i].Deleted, want[i].Key, want[i].Deleted)
		}
	}
}

func TestLogReadEmpty(t *testing.T) {
	l := openLog(t, filepath.Join(t.TempDir(), "empty.log"))

	var ent Entry
	eof, err := l.Read(&ent)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !eof {
		t.Fatal("Read() on an empty log = not eof, want eof")
	}
}

// The file is opened for both reading and writing with one cursor. Replaying to
// the end is what leaves that cursor at the tail, so later writes append instead
// of overwriting the records already there.
func TestLogWriteAppendsAfterReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "append.log")

	first := openLog(t, path)
	ent := Entry{Key: []byte("k1"), Val: []byte("v1")}
	if err := first.Write(&ent); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	first.Close()

	second := openLog(t, path)
	if got := readAll(t, second); len(got) != 1 {
		t.Fatalf("replayed %d entries, want 1", len(got))
	}
	ent = Entry{Key: []byte("k2"), Val: []byte("v2")}
	if err := second.Write(&ent); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	second.Close()

	got := readAll(t, openLog(t, path))
	if len(got) != 2 {
		t.Fatalf("replayed %d entries, want 2", len(got))
	}
	if !bytes.Equal(got[0].Key, []byte("k1")) || !bytes.Equal(got[1].Key, []byte("k2")) {
		t.Fatalf("keys = %q, %q; want k1, k2", got[0].Key, got[1].Key)
	}
}

// A log cut off mid-record is damaged, not finished. Read must say so instead of
// reporting a clean end of file, which would silently drop the tail.
func TestLogReadTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "torn.log")

	l := openLog(t, path)
	ent := Entry{Key: []byte("key"), Val: []byte("value")}
	if err := l.Write(&ent); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	l.Close()

	if err := os.Truncate(path, int64(ent.Size())-3); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}

	var got Entry
	eof, err := openLog(t, path).Read(&got)
	if eof {
		t.Fatal("Read() on a truncated record = eof, want an error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Read() = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestCreateFileSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "created.log")

	fp, err := createFileSync(path)
	if err != nil {
		t.Fatalf("createFileSync() error = %v", err)
	}
	defer fp.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if _, err := fp.Write([]byte("writable")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

func TestCreateFileSyncMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "x.log")

	fp, err := createFileSync(path)
	if err == nil {
		fp.Close()
		t.Fatal("createFileSync() in a missing directory = nil, want an error")
	}
}

// A write that returned nil must be readable after reopening the file, with no
// flush of our own in between.
func TestLogWriteIsDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "durable.log")

	l := openLog(t, path)
	ent := Entry{Key: []byte("k"), Val: []byte("v")}
	if err := l.Write(&ent); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Deliberately no Close: the entry has to be on disk already.
	got := readAll(t, openLog(t, path))
	if len(got) != 1 || !bytes.Equal(got[0].Val, []byte("v")) {
		t.Fatalf("replayed %v, want one entry with value \"v\"", got)
	}
}

func TestLogSync(t *testing.T) {
	l := openLog(t, filepath.Join(t.TempDir(), "sync.log"))

	ent := Entry{Key: []byte("k"), Val: []byte("v")}
	if err := l.Write(&ent); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := l.Sync(); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
}

// The two benchmarks together price durability: the only difference between
// them is the fsync.
func BenchmarkWrite(b *testing.B) {
	l := &Log{FileName: filepath.Join(b.TempDir(), "bench.log")}
	if err := l.Open(); err != nil {
		b.Fatalf("Open() error = %v", err)
	}
	defer l.Close()

	ent := Entry{Key: []byte("some-key"), Val: bytes.Repeat([]byte("v"), 256)}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := l.Write(&ent); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteNoSync(b *testing.B) {
	l := &Log{FileName: filepath.Join(b.TempDir(), "bench.log")}
	if err := l.Open(); err != nil {
		b.Fatalf("Open() error = %v", err)
	}
	defer l.Close()

	ent := Entry{Key: []byte("some-key"), Val: bytes.Repeat([]byte("v"), 256)}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := l.fp.Write(ent.Encode()); err != nil {
			b.Fatal(err)
		}
	}
}
