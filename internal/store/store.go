// Package store implements an in-memory key-value store, held in order.
//
// The keys live in one slice and the values in another, side by side: entry i of
// one belongs with entry i of the other. Keeping them apart rather than as one
// slice of pairs puts every key next to the next key in memory, which is what a
// binary search walks over — and a search that touches only keys should not be
// dragging values through the cache with it.
//
// Order is the whole point of the change from a map. A map answers "what is
// under this key" and nothing else; sorted keys also answer "what comes next",
// which is what a table scan, a range query and an ORDER BY are all made of.
// None of those exist yet — this is the shape they need underneath.
package store

import (
	"bytes"
	"slices"
	"sync"
)

// Store is a concurrency-safe key-value store, sorted by key.
type Store struct {
	mu sync.RWMutex

	// keys is sorted and holds no duplicates; vals is the same length, and
	// vals[i] belongs to keys[i]. Every write has to move both or neither.
	keys [][]byte
	vals [][]byte
}

// New returns an empty Store ready for use.
func New() *Store {
	return &Store{}
}

// Get returns the value stored under key. ok is false if the key is absent.
// The returned slice is a copy, so callers cannot mutate the stored value.
func (s *Store) Get(key []byte) (value []byte, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pos, found := search(s.keys, key)
	if !found {
		return nil, false
	}
	return bytes.Clone(s.vals[pos]), true
}

// Has reports whether key is present, without copying its value.
func (s *Store) Has(key []byte) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, found := search(s.keys, key)
	return found
}

// Set stores value under key, overwriting any previous value.
// The key and value are copied, so later mutations by the caller are not
// observed.
func (s *Store) Set(key []byte, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pos, found := search(s.keys, key)
	if found {
		// The key is already in place, so only the value moves. This is the
		// cheap case, and the only one that touches no other entry.
		s.vals[pos] = bytes.Clone(value)
		return
	}

	// A new key has to be opened a gap at pos, in both slices at once, or the
	// two stop lining up. Everything after it shifts along: this is what a
	// sorted array costs, and what a tree will later avoid.
	s.keys = slices.Insert(s.keys, pos, bytes.Clone(key))
	s.vals = slices.Insert(s.vals, pos, bytes.Clone(value))
}

// Delete removes key. It reports whether the key was present.
func (s *Store) Delete(key []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	pos, found := search(s.keys, key)
	if !found {
		return false
	}

	s.keys = slices.Delete(s.keys, pos, pos+1)
	s.vals = slices.Delete(s.vals, pos, pos+1)
	return true
}

// Len returns the number of keys currently stored.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.keys)
}

// Keys returns every key, in order. They come back sorted now, where the map
// this replaced gave them back in whatever order it felt like.
func (s *Store) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, len(s.keys))
	for i, key := range s.keys {
		keys[i] = string(key)
	}
	return keys
}
