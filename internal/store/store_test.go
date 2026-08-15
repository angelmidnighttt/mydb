package store

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

func TestSetGet(t *testing.T) {
	s := New()
	s.Set("name", []byte("mydb"))

	got, ok := s.Get("name")
	if !ok {
		t.Fatal("Get(name) missing after Set")
	}
	if !bytes.Equal(got, []byte("mydb")) {
		t.Fatalf("Get(name) = %q, want %q", got, "mydb")
	}
}

func TestGetMissing(t *testing.T) {
	s := New()

	if _, ok := s.Get("nope"); ok {
		t.Fatal("Get(nope) = ok, want missing")
	}
}

func TestSetOverwrites(t *testing.T) {
	s := New()
	s.Set("k", []byte("old"))
	s.Set("k", []byte("new"))

	got, _ := s.Get("k")
	if !bytes.Equal(got, []byte("new")) {
		t.Fatalf("Get(k) = %q, want %q", got, "new")
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", s.Len())
	}
}

func TestDelete(t *testing.T) {
	s := New()
	s.Set("k", []byte("v"))

	if !s.Delete("k") {
		t.Fatal("Delete(k) = false, want true")
	}
	if _, ok := s.Get("k"); ok {
		t.Fatal("Get(k) = ok after Delete")
	}
	if s.Delete("k") {
		t.Fatal("second Delete(k) = true, want false")
	}
}

func TestValueIsCopied(t *testing.T) {
	s := New()
	v := []byte("value")
	s.Set("k", v)
	v[0] = 'X'

	got, _ := s.Get("k")
	if !bytes.Equal(got, []byte("value")) {
		t.Fatalf("stored value aliased caller slice: %q", got)
	}

	got[0] = 'Y'
	again, _ := s.Get("k")
	if !bytes.Equal(again, []byte("value")) {
		t.Fatalf("returned value aliased stored slice: %q", again)
	}
}

func TestKeys(t *testing.T) {
	s := New()
	s.Set("a", []byte("1"))
	s.Set("b", []byte("2"))

	keys := s.Keys()
	if len(keys) != 2 {
		t.Fatalf("Keys() = %v, want 2 keys", keys)
	}
}

// Run with -race to check the locking.
func TestConcurrentAccess(t *testing.T) {
	s := New()
	const workers = 8
	const ops = 200

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				key := fmt.Sprintf("w%d-k%d", w, i)
				s.Set(key, []byte(key))
				if _, ok := s.Get(key); !ok {
					t.Errorf("Get(%s) missing right after Set", key)
					return
				}
				s.Delete(key)
			}
		}(w)
	}
	wg.Wait()

	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", s.Len())
	}
}
