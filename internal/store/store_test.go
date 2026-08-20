package store

import (
	"bytes"
	"fmt"
	"math/rand"
	"slices"
	"sync"
	"testing"
)

// key spells a key out as bytes, which is what the store speaks now.
func key(s string) []byte { return []byte(s) }

func TestSetGet(t *testing.T) {
	s := New()
	s.Set(key("name"), []byte("mydb"))

	got, ok := s.Get(key("name"))
	if !ok {
		t.Fatal("Get(name) missing after Set")
	}
	if !bytes.Equal(got, []byte("mydb")) {
		t.Fatalf("Get(name) = %q, want %q", got, "mydb")
	}
}

func TestGetMissing(t *testing.T) {
	s := New()

	if _, ok := s.Get(key("nope")); ok {
		t.Fatal("Get(nope) = ok, want missing")
	}
}

func TestHas(t *testing.T) {
	s := New()
	s.Set(key("here"), nil)

	if !s.Has(key("here")) {
		t.Error("Has(here) = false, want true")
	}
	if s.Has(key("gone")) {
		t.Error("Has(gone) = true, want false")
	}
}

func TestSetOverwrites(t *testing.T) {
	s := New()
	s.Set(key("k"), []byte("old"))
	s.Set(key("k"), []byte("new"))

	got, _ := s.Get(key("k"))
	if !bytes.Equal(got, []byte("new")) {
		t.Fatalf("Get(k) = %q, want %q", got, "new")
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", s.Len())
	}
}

func TestDelete(t *testing.T) {
	s := New()
	s.Set(key("k"), []byte("v"))

	if !s.Delete(key("k")) {
		t.Fatal("Delete(k) = false, want true")
	}
	if _, ok := s.Get(key("k")); ok {
		t.Fatal("Get(k) = ok after Delete")
	}
	if s.Delete(key("k")) {
		t.Fatal("second Delete(k) = true, want false")
	}
}

func TestValueIsCopied(t *testing.T) {
	s := New()
	v := []byte("value")
	s.Set(key("k"), v)
	v[0] = 'X'

	got, _ := s.Get(key("k"))
	if !bytes.Equal(got, []byte("value")) {
		t.Fatalf("stored value aliased caller slice: %q", got)
	}

	got[0] = 'Y'
	again, _ := s.Get(key("k"))
	if !bytes.Equal(again, []byte("value")) {
		t.Fatalf("returned value aliased stored slice: %q", again)
	}
}

// The key is copied too. A caller reusing one buffer for key after key would
// otherwise rewrite the keys already stored — and the order they are held in
// would stop being an order.
func TestKeyIsCopied(t *testing.T) {
	s := New()
	buf := []byte("k1")
	s.Set(buf, []byte("v1"))
	buf[1] = '2'
	s.Set(buf, []byte("v2"))

	if _, ok := s.Get(key("k1")); !ok {
		t.Fatal("k1 is gone; the stored key aliased the caller buffer")
	}
	if s.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", s.Len())
	}
}

// The store is sorted now, so this is no longer "in unspecified order".
func TestKeysComeBackSorted(t *testing.T) {
	s := New()
	for _, k := range []string{"c", "a", "b10", "b2", ""} {
		s.Set(key(k), []byte(k))
	}

	got := s.Keys()
	want := []string{"", "a", "b10", "b2", "c"}
	if !slices.Equal(got, want) {
		t.Fatalf("Keys() = %v, want %v", got, want)
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
				k := key(fmt.Sprintf("w%d-k%d", w, i))
				s.Set(k, k)
				if _, ok := s.Get(k); !ok {
					t.Errorf("Get(%s) missing right after Set", k)
					return
				}
				s.Delete(k)
			}
		}(w)
	}
	wg.Wait()

	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", s.Len())
	}
}

// Keys arrive in whatever order the writes came in; what is stored has to end up
// in order regardless.
func TestOrderSurvivesAnyWriteOrder(t *testing.T) {
	rng := rand.New(rand.NewSource(1))

	for round := 0; round < 50; round++ {
		s := New()
		var written []string

		for i := 0; i < 40; i++ {
			k := fmt.Sprintf("k%03d", rng.Intn(100))
			s.Set(key(k), []byte(k))
			if !slices.Contains(written, k) {
				written = append(written, k)
			}
		}
		slices.Sort(written)

		if got := s.Keys(); !slices.Equal(got, written) {
			t.Fatalf("round %d: Keys() = %v, want %v", round, got, written)
		}
	}
}
