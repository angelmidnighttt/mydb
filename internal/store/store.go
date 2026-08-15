// Package store implements an in-memory key-value store.
package store

import "sync"

// Store is a concurrency-safe key-value store.
type Store struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// New returns an empty Store ready for use.
func New() *Store {
	return &Store{data: make(map[string][]byte)}
}

// Get returns the value stored under key. ok is false if the key is absent.
// The returned slice is a copy, so callers cannot mutate the stored value.
func (s *Store) Get(key string) (value []byte, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), v...), true
}

// Set stores value under key, overwriting any previous value.
// The value is copied, so later mutations by the caller are not observed.
func (s *Store) Set(key string, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = append([]byte(nil), value...)
}

// Delete removes key. It reports whether the key was present.
func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.data[key]
	delete(s.data, key)
	return ok
}

// Len returns the number of keys currently stored.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.data)
}

// Keys returns every key in the store, in unspecified order.
func (s *Store) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys
}
