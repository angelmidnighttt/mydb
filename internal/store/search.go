package store

import "bytes"

// search looks for key among keys, which must already be sorted.
//
// found says whether it is there. pos is where it is — or, when it is not, where
// it would have to go to keep the order, which is exactly what an insert needs.
// One search therefore answers both questions a write asks: is it here, and if
// not, where does it belong.
//
// The loop holds one invariant: if key is anywhere, it is in keys[lo:hi].
// Everything before lo has been shown to be smaller, everything from hi on has
// been shown to be larger. When lo meets hi the range is empty, key is not
// there, and lo is the seam between the smaller and the larger — the insert
// position, without a single extra comparison.
//
// The half-open range is what keeps this from going wrong. hi is one past the
// last candidate rather than the last candidate, so hi = len(keys) is a legal
// starting point, and an empty slice or a key past the end need no special case:
// the loop simply does not run, and pos comes back as len(keys).
func search(keys [][]byte, key []byte) (pos int, found bool) {
	lo, hi := 0, len(keys)

	for lo < hi {
		// lo + (hi-lo)/2 rather than (lo+hi)/2. With 64-bit ints the sum could
		// not overflow on any slice that fits in memory, but the version that
		// can is the one that sat broken in several standard libraries for
		// years, and this one costs nothing.
		mid := lo + (hi-lo)/2

		switch cmp := bytes.Compare(keys[mid], key); {
		case cmp < 0:
			lo = mid + 1 // mid is too small, and so is everything before it
		case cmp > 0:
			hi = mid // mid is too large, and it is not the answer either
		default:
			return mid, true
		}
	}

	return lo, false
}
