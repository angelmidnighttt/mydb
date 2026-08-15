// Package kv joins the in-memory store to the write-ahead log: every change is
// appended to the log on disk first, then applied to memory, so restarting the
// process rebuilds the same state by replaying the log.
package kv

import (
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
		kv.mem.Delete(string(ent.Key))
		return
	}
	kv.mem.Set(string(ent.Key), ent.Val)
}

// Get returns the value stored under key. ok is false if the key is absent.
func (kv *KV) Get(key []byte) (val []byte, ok bool) {
	return kv.mem.Get(string(key))
}

// Set stores val under key. updated reports whether the key already had a
// value, that is, whether this overwrote something rather than inserting.
//
// The record reaches the log before memory changes: if the write fails, the
// database is left exactly as it was.
func (kv *KV) Set(key []byte, val []byte) (updated bool, err error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	updated = kv.mem.Has(string(key))

	ent := wal.Entry{Key: key, Val: val}
	if err := kv.log.Write(&ent); err != nil {
		return false, err
	}
	kv.apply(&ent)

	return updated, nil
}

// Del removes key. deleted reports whether the key was there to begin with.
//
// Deleting an absent key writes nothing: a tombstone for a key that was never
// set carries no information and would only grow the log.
func (kv *KV) Del(key []byte) (deleted bool, err error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	if !kv.mem.Has(string(key)) {
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
