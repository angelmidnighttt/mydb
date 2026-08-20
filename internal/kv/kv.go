// Package kv joins the in-memory store to the write-ahead log: every change is
// appended to the log on disk first, then applied to memory, so restarting the
// process rebuilds the same state by replaying the log.
package kv

import (
	"errors"
	"fmt"
	"sync"

	"github.com/angelmidnighttt/mydb/internal/store"
	"github.com/angelmidnighttt/mydb/internal/wal"
)

// KV is a persistent key-value database. The zero value is not usable; set
// Path and call Open.
type KV struct {
	// Path is the log file. It is created if it does not exist.
	Path string

	// mu serializes writers so that the order of records in the log matches the
	// order the changes were applied to mem. Readers go straight to mem, which
	// does its own locking.
	mu  sync.Mutex
	log wal.Log
	mem *store.Store
}

// Open opens the log and replays it into memory.
func (kv *KV) Open() error {
	kv.log = wal.Log{FileName: kv.Path}
	if err := kv.log.Open(); err != nil {
		return err
	}
	kv.mem = store.New()

	if err := kv.replay(); err != nil {
		kv.log.Close()
		kv.mem = nil
		return err
	}
	return nil
}

// Close closes the log file. The in-memory state is dropped with the KV.
func (kv *KV) Close() error {
	return kv.log.Close()
}

// replay rebuilds memory from the log, oldest record first, so a later record
// for a key overwrites an earlier one. It runs to the end of the file, which
// also leaves the cursor where Write appends.
func (kv *KV) replay() error {
	for {
		var ent wal.Entry
		eof, err := kv.log.Read(&ent)
		if err != nil {
			return err
		}
		if eof {
			return nil
		}
		kv.apply(&ent)
	}
}

// apply performs one record against memory. It is the single place that turns a
// record into a change, so replay and live writes cannot drift apart.
func (kv *KV) apply(ent *wal.Entry) {
	if ent.Deleted {
		kv.mem.Delete(ent.Key)
		return
	}
	kv.mem.Set(ent.Key, ent.Val)
}

// Get returns the value stored under key. ok is false if the key is absent.
func (kv *KV) Get(key []byte) (val []byte, ok bool) {
	return kv.mem.Get(key)
}

// UpdateMode says what SetEx does about a key that is — or is not — already
// there. The three modes are SQL's upsert, INSERT and UPDATE, all landing on
// the same write path.
//
// The zero value is ModeUpsert: a caller that leaves the mode out gets the
// permissive behaviour of Set rather than an error.
type UpdateMode int

const (
	// ModeUpsert writes either way: insert a new key, overwrite an old one.
	ModeUpsert UpdateMode = 0
	// ModeInsert writes only a new key; an existing one is left as it was.
	ModeInsert UpdateMode = 1
	// ModeUpdate writes only over an existing key; an absent one is not created.
	ModeUpdate UpdateMode = 2
)

// String renders the mode for error messages.
func (mode UpdateMode) String() string {
	switch mode {
	case ModeUpsert:
		return "upsert"
	case ModeInsert:
		return "insert"
	case ModeUpdate:
		return "update"
	default:
		return fmt.Sprintf("UpdateMode(%d)", int(mode))
	}
}

// ErrBadMode reports a mode that is none of the three. It is a bug in the
// caller, not something wrong with the key, the value or the log.
var ErrBadMode = errors.New("kv: unknown update mode")

// SetEx stores val under key, subject to mode.
//
// The bool answers the question the mode asks:
//
//	ModeInsert   inserted — false means the key was already there
//	ModeUpdate   updated  — false means the key was absent
//	ModeUpsert   updated  — true means the write overwrote a value
//
// The two restricted modes can refuse to write, and their bool reports whether
// the write went through. ModeUpsert never refuses, so it has no failure to
// report and its bool keeps the older meaning instead.
//
// A refused write touches neither the log nor memory, for the same reason
// deleting an absent key writes nothing: it changes no state, so a record of it
// would only grow the log.
//
// The record reaches the log before memory changes: if the write fails, the
// database is left exactly as it was.
func (kv *KV) SetEx(key []byte, val []byte, mode UpdateMode) (bool, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	exists := kv.mem.Has(key)

	// What the bool reports depends on the mode, so it is settled here, beside
	// the check that decided it, rather than after the write.
	var ok bool
	switch mode {
	case ModeUpsert:
		ok = exists
	case ModeInsert:
		if exists {
			return false, nil
		}
		ok = true
	case ModeUpdate:
		if !exists {
			return false, nil
		}
		ok = true
	default:
		return false, fmt.Errorf("%w: %v", ErrBadMode, mode)
	}

	ent := wal.Entry{Key: key, Val: val}
	if err := kv.log.Write(&ent); err != nil {
		return false, err
	}
	kv.apply(&ent)

	return ok, nil
}

// Set stores val under key, inserting it or overwriting what was there. updated
// reports whether the key already had a value, that is, whether this overwrote
// something rather than inserting.
func (kv *KV) Set(key []byte, val []byte) (updated bool, err error) {
	return kv.SetEx(key, val, ModeUpsert)
}

// Del removes key. deleted reports whether the key was there to begin with.
//
// Deleting an absent key writes nothing: a tombstone for a key that was never
// set carries no information and would only grow the log.
func (kv *KV) Del(key []byte) (deleted bool, err error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	if !kv.mem.Has(key) {
		return false, nil
	}

	ent := wal.Entry{Key: key, Deleted: true}
	if err := kv.log.Write(&ent); err != nil {
		return false, err
	}
	kv.apply(&ent)

	return true, nil
}

// Len returns the number of keys currently stored.
func (kv *KV) Len() int {
	return kv.mem.Len()
}

// Keys returns every key, in unspecified order.
func (kv *KV) Keys() []string {
	return kv.mem.Keys()
}

// Sync flushes the log to the physical disk. Writes are durable against a
// process crash without it, but not against a power loss.
func (kv *KV) Sync() error {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	return kv.log.Sync()
}
