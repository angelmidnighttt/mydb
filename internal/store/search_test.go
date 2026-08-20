package store

import (
	"bytes"
	"fmt"
	"testing"
)

func keys(ss ...string) [][]byte {
	out := make([][]byte, len(ss))
	for i, s := range ss {
		out[i] = []byte(s)
	}
	return out
}

func TestSearch(t *testing.T) {
	tests := []struct {
		name    string
		keys    [][]byte
		target  string
		wantPos int
		wantOk  bool
	}{
		{"empty", nil, "a", 0, false},
		{"one, found", keys("b"), "b", 0, true},
		{"one, before it", keys("b"), "a", 0, false},
		{"one, after it", keys("b"), "c", 1, false},

		{"first of many", keys("a", "c", "e"), "a", 0, true},
		{"middle of many", keys("a", "c", "e"), "c", 1, true},
		{"last of many", keys("a", "c", "e"), "e", 2, true},

		{"before everything", keys("b", "c", "e"), "a", 0, false},
		{"between the first two", keys("a", "c", "e"), "b", 1, false},
		{"between the last two", keys("a", "c", "e"), "d", 2, false},
		{"after everything", keys("a", "c", "e"), "f", 3, false},

		// An even length is where an off-by-one hides: the halves are not the
		// same size, so which way mid rounds starts to matter.
		{"even length, found left", keys("a", "b", "c", "d"), "b", 1, true},
		{"even length, found right", keys("a", "b", "c", "d"), "c", 2, true},
		{"even length, gap", keys("a", "b", "d", "e"), "c", 2, false},

		// A key that is a prefix of another sorts before it.
		{"prefix comes first", keys("ab", "abc"), "ab", 0, true},
		{"the empty key sorts first", keys("", "a"), "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, ok := search(tt.keys, []byte(tt.target))

			if pos != tt.wantPos || ok != tt.wantOk {
				t.Errorf("search(%q) = %d, %v; want %d, %v", tt.target, pos, ok, tt.wantPos, tt.wantOk)
			}
		})
	}
}

// Every subset of a small alphabet, searched for every letter of it. Small
// enough to run through completely, which beats hoping the handful of cases
// above happened to include the one that breaks.
func TestSearchExhaustively(t *testing.T) {
	const universe = "abcdefgh"

	for subset := 0; subset < 1<<len(universe); subset++ {
		var sorted [][]byte
		for i := 0; i < len(universe); i++ {
			if subset&(1<<i) != 0 {
				sorted = append(sorted, []byte{universe[i]})
			}
		}

		for i := 0; i < len(universe); i++ {
			target := []byte{universe[i]}
			pos, ok := search(sorted, target)

			wantPos, wantOk := searchSlowly(sorted, target)
			if pos != wantPos || ok != wantOk {
				t.Fatalf("search(%q, %q) = %d, %v; want %d, %v",
					sorted, target, pos, ok, wantPos, wantOk)
			}

			// Whether or not it was found, pos has to be a position that keeps
			// the order — that is what an insert relies on.
			if pos > 0 && bytes.Compare(sorted[pos-1], target) >= 0 {
				t.Fatalf("search(%q, %q) = %d, which is not past %q", sorted, target, pos, sorted[pos-1])
			}
			if !ok && pos < len(sorted) && bytes.Compare(sorted[pos], target) <= 0 {
				t.Fatalf("search(%q, %q) = %d, which is not before %q", sorted, target, pos, sorted[pos])
			}
		}
	}
}

// searchSlowly is the obvious version: walk from the front. It is the answer the
// fast one has to agree with.
func searchSlowly(sorted [][]byte, target []byte) (int, bool) {
	for i, key := range sorted {
		switch cmp := bytes.Compare(key, target); {
		case cmp == 0:
			return i, true
		case cmp > 0:
			return i, false
		}
	}
	return len(sorted), false
}

// Every length up to a few hundred, so the loop is exercised at every shape a
// range can halve into.
func TestSearchAtEveryLength(t *testing.T) {
	for n := 0; n <= 300; n++ {
		sorted := make([][]byte, n)
		for i := range sorted {
			// Even numbers only, so every odd number in between is a gap.
			sorted[i] = []byte(fmt.Sprintf("k%05d", 2*i))
		}

		for i := 0; i <= n; i++ {
			if i < n {
				target := []byte(fmt.Sprintf("k%05d", 2*i))
				if pos, ok := search(sorted, target); pos != i || !ok {
					t.Fatalf("n=%d: search(%q) = %d, %v; want %d, true", n, target, pos, ok, i)
				}
			}

			gap := []byte(fmt.Sprintf("k%05d", 2*i-1))
			if pos, ok := search(sorted, gap); pos != i || ok {
				t.Fatalf("n=%d: search(%q) = %d, %v; want %d, false", n, gap, pos, ok, i)
			}
		}
	}
}
