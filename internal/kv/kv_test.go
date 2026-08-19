package kv

import (
	"bytes"
	"errors"
	"fmt"
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

func mustSetEx(t *testing.T, db *KV, key, val string, mode UpdateMode) bool {
	t.Helper()

	ok, err := db.SetEx([]byte(key), []byte(val), mode)
	if err != nil {
		t.Fatalf("SetEx(%q, %v) error = %v", key, mode, err)
	}
	return ok
}

// logSize reports how large the log file has grown, for the tests that check a
// call wrote nothing.
func logSize(t *testing.T, path string) int64 {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	return info.Size()
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

// The whole truth table of SetEx: what each mode does to a key that is already
// there, and to one that is not. wantVal is empty when the key must stay
// absent; the log may only grow when the value actually changed to "new".
func TestSetExModes(t *testing.T) {
	tests := []struct {
		mode    UpdateMode
		exists  bool
		want    bool
		wantVal string
	}{
		{ModeUpsert, false, false, "new"}, // inserted, nothing was overwritten
		{ModeUpsert, true, true, "new"},   // overwrote
		{ModeInsert, false, true, "new"},  // inserted
		{ModeInsert, true, false, "old"},  // refused: the key was taken
		{ModeUpdate, false, false, ""},    // refused: nothing to update
		{ModeUpdate, true, true, "new"},   // updated
	}

	for _, tt := range tests {
		state := "absent"
		if tt.exists {
			state = "existing"
		}
		t.Run(fmt.Sprintf("%v/%s", tt.mode, state), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.log")
			db := openKV(t, path)
			if tt.exists {
				mustSet(t, db, "k", "old")
			}
			before := logSize(t, path)

			if got := mustSetEx(t, db, "k", "new", tt.mode); got != tt.want {
				t.Errorf("SetEx() = %v, want %v", got, tt.want)
			}

			got, ok := mustGet(t, db, "k")
			switch {
			case tt.wantVal == "":
				if ok {
					t.Errorf("Get(k) = %q, want absent", got)
				}
			case !ok || got != tt.wantVal:
				t.Errorf("Get(k) = %q, %v; want %q, true", got, ok, tt.wantVal)
			}

			// A refused write changed nothing, so there is nothing to record —
			// the same rule that keeps a no-op delete out of the log.
			wrote, wantWrote := logSize(t, path) > before, tt.wantVal == "new"
			if wrote != wantWrote {
				t.Errorf("log grew = %v, want %v", wrote, wantWrote)
			}
		})
	}
}

// The refusal has to hold after a restart too: if a refused write had reached
// the log, replay would carry it out.
func TestSetExRefusalsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")

	db := openKV(t, path)
	mustSet(t, db, "k", "v1")
	mustSetEx(t, db, "k", "v2", ModeInsert)     // refused: k is taken
	mustSetEx(t, db, "absent", "x", ModeUpdate) // refused: absent is not there
	db.Close()

	reopened := openKV(t, path)
	if got, _ := mustGet(t, reopened, "k"); got != "v1" {
		t.Errorf("Get(k) after restart = %q, want \"v1\" — a refused insert reached the log", got)
	}
	if _, ok := mustGet(t, reopened, "absent"); ok {
		t.Error("a refused update created the key, visible after restart")
	}
}

// An unknown mode is a bug in the caller, not bad data, so it is reported and
// nothing is written.
func TestSetExRejectsUnknownMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	db := openKV(t, path)

	ok, err := db.SetEx([]byte("k"), []byte("v"), UpdateMode(7))
	if !errors.Is(err, ErrBadMode) {
		t.Fatalf("SetEx(mode 7) error = %v, want ErrBadMode", err)
	}
	if ok {
		t.Error("SetEx(mode 7) = true, want false")
	}
	if _, found := mustGet(t, db, "k"); found {
		t.Error("an invalid mode wrote the key anyway")
	}
	if size := logSize(t, path); size != 0 {
		t.Errorf("log grew to %d bytes on an invalid mode", size)
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

// Losing power in the middle of a write leaves a half-record at the end of the
// log. That write never returned success, so nothing was promised about it:
// recovery drops it and keeps every write that did return.
func TestOpenRecoversFromTornWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "torn.log")

	db := openKV(t, path)
	mustSet(t, db, "committed", "yes")
	mustSet(t, db, "torn", "no")
	db.Close()

	// Cut the file so the last record is incomplete.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := os.WriteFile(path, data[:len(data)-4], 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	recovered := openKV(t, path)
	if got, ok := mustGet(t, recovered, "committed"); !ok || got != "yes" {
		t.Fatalf("Get(committed) = %q, %v; want \"yes\", true — an acknowledged write was lost", got, ok)
	}
	if _, ok := mustGet(t, recovered, "torn"); ok {
		t.Fatal("Get(torn) = ok; a half-written record was applied")
	}

	// Recovery has to leave the log writable, not just readable.
	mustSet(t, recovered, "after", "3")
	recovered.Close()

	final := openKV(t, path)
	if got, ok := mustGet(t, final, "after"); !ok || got != "3" {
		t.Fatalf("Get(after) = %q, %v; want \"3\", true — writes after recovery were lost", got, ok)
	}
	if final.Len() != 2 {
		t.Fatalf("Len() = %d, want 2; keys = %v", final.Len(), final.Keys())
	}
}

// Damage anywhere in the log stops the replay there, so a bad record in the
// middle costs every record after it. Nothing in the format can distinguish that
// from a torn tail — the size header of a damaged record cannot be trusted, so
// there is no way to find where the next record would start.
func TestOpenStopsAtDamageInTheMiddle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "damaged.log")

	db := openKV(t, path)
	mustSet(t, db, "first", "1")
	mustSet(t, db, "second", "2")
	mustSet(t, db, "third", "3")
	db.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	data[len(data)/2] ^= 1 << 3
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	recovered := openKV(t, path)
	if _, ok := mustGet(t, recovered, "first"); !ok {
		t.Fatal("Get(first) lost; records before the damage must survive")
	}
	if _, ok := mustGet(t, recovered, "third"); ok {
		t.Fatal("Get(third) = ok; records past the damage are not reachable")
	}
}
