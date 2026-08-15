package kv

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// openKV opens a database at path and closes it when the test ends.
func openKV(t *testing.T, path string) *KV {
	t.Helper()

	db := &KV{Path: path}
	if err := db.Open(); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustSet(t *testing.T, db *KV, key, val string) bool {
	t.Helper()

	updated, err := db.Set([]byte(key), []byte(val))
	if err != nil {
		t.Fatalf("Set(%q) error = %v", key, err)
	}
	return updated
}

func mustGet(t *testing.T, db *KV, key string) (string, bool) {
	t.Helper()

	val, ok := db.Get([]byte(key))
	return string(val), ok
}

func TestOpenCreatesEmptyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.log")
	db := openKV(t, path)

	if db.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", db.Len())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}

func TestSetGet(t *testing.T) {
	db := openKV(t, filepath.Join(t.TempDir(), "test.log"))

	mustSet(t, db, "hello", "world")

	got, ok := mustGet(t, db, "hello")
	if !ok || got != "world" {
		t.Fatalf("Get(hello) = %q, %v; want \"world\", true", got, ok)
	}
	if _, ok := mustGet(t, db, "missing"); ok {
		t.Fatal("Get(missing) = ok, want absent")
	}
}

func TestSetReportsUpdated(t *testing.T) {
	db := openKV(t, filepath.Join(t.TempDir(), "test.log"))

	if updated := mustSet(t, db, "k", "v1"); updated {
		t.Fatal("first Set() = updated, want inserted")
	}
	if updated := mustSet(t, db, "k", "v2"); !updated {
		t.Fatal("second Set() = inserted, want updated")
	}
}

func TestDelReportsDeleted(t *testing.T) {
	db := openKV(t, filepath.Join(t.TempDir(), "test.log"))
	mustSet(t, db, "k", "v")

	deleted, err := db.Del([]byte("k"))
	if err != nil {
		t.Fatalf("Del() error = %v", err)
	}
	if !deleted {
		t.Fatal("Del(existing) = false, want true")
	}
	if _, ok := mustGet(t, db, "k"); ok {
		t.Fatal("key still readable after Del")
	}

	deleted, err = db.Del([]byte("k"))
	if err != nil {
		t.Fatalf("second Del() error = %v", err)
	}
	if deleted {
		t.Fatal("Del(absent) = true, want false")
	}
}

// Deleting a key that was never set writes nothing, so the log stays empty.
func TestDelAbsentWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	db := openKV(t, path)

	if _, err := db.Del([]byte("never-set")); err != nil {
		t.Fatalf("Del() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("log grew to %d bytes on a no-op delete", info.Size())
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")

	db := openKV(t, path)
	mustSet(t, db, "name", "mydb")
	mustSet(t, db, "lang", "go")
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened := openKV(t, path)
	if reopened.Len() != 2 {
		t.Fatalf("Len() after restart = %d, want 2", reopened.Len())
	}
	for key, want := range map[string]string{"name": "mydb", "lang": "go"} {
		if got, ok := mustGet(t, reopened, key); !ok || got != want {
			t.Errorf("Get(%q) after restart = %q, %v; want %q, true", key, got, ok, want)
		}
	}
}

// The log keeps every version of a key; replay must end on the newest one.
func TestLaterEntriesWin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")

	db := openKV(t, path)
	mustSet(t, db, "k", "first")
	mustSet(t, db, "k", "second")
	mustSet(t, db, "k", "third")
	db.Close()

	reopened := openKV(t, path)
	if got, _ := mustGet(t, reopened, "k"); got != "third" {
		t.Fatalf("Get(k) after restart = %q, want \"third\"", got)
	}
	if reopened.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", reopened.Len())
	}
}

// A delete is itself a record. Replay has to apply it, or the key comes back
// from the dead on the next start.
func TestDeleteSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")

	db := openKV(t, path)
	mustSet(t, db, "k", "v")
	if _, err := db.Del([]byte("k")); err != nil {
		t.Fatalf("Del() error = %v", err)
	}
	db.Close()

	reopened := openKV(t, path)
	if _, ok := mustGet(t, reopened, "k"); ok {
		t.Fatal("deleted key came back after restart")
	}
	if reopened.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", reopened.Len())
	}
}

// Writes made after a restart must land at the end of the log, not on top of
// the records that were replayed.
func TestWritesAfterRestartAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")

	db := openKV(t, path)
	mustSet(t, db, "first", "1")
	db.Close()

	db = openKV(t, path)
	mustSet(t, db, "second", "2")
	db.Close()

	final := openKV(t, path)
	if final.Len() != 2 {
		t.Fatalf("Len() = %d, want 2; keys = %v", final.Len(), final.Keys())
	}
	if got, ok := mustGet(t, final, "first"); !ok || got != "1" {
		t.Fatalf("Get(first) = %q, %v; want \"1\", true", got, ok)
	}
}

// Values must not alias the caller's slice, in memory or through the log.
func TestSetCopiesValue(t *testing.T) {
	db := openKV(t, filepath.Join(t.TempDir(), "test.log"))

	val := []byte("original")
	if _, err := db.Set([]byte("k"), val); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	val[0] = 'X'

	if got, _ := db.Get([]byte("k")); !bytes.Equal(got, []byte("original")) {
		t.Fatalf("stored value = %q, want \"original\"", got)
	}
}

// A damaged log must stop Open rather than come up with partial data and let
// the next write append to a file that is already broken.
func TestOpenRejectsCorruptLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.log")

	db := openKV(t, path)
	mustSet(t, db, "k", "v")
	db.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := os.WriteFile(path, data[:len(data)-2], 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	broken := &KV{Path: path}
	if err := broken.Open(); err == nil {
		broken.Close()
		t.Fatal("Open() on a truncated log = nil, want an error")
	}
}
