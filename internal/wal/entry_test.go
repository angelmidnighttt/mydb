package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"testing"
	"testing/iotest"
)

// The byte layout is the file format, so it is pinned literally: crc32 of
// everything that follows it, then the sizes, the flag, and the data.
func TestEncodeLayout(t *testing.T) {
	ent := Entry{Key: []byte("a"), Val: []byte("bb")}

	got := ent.Encode()
	want := []byte{
		59, 37, 55, 31, // crc32 of the bytes below, little-endian
		1, 0, 0, 0, // key size
		2, 0, 0, 0, // val size
		0,             // not deleted
		'a', 'b', 'b', // key, then val
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("Encode() = %v, want %v", got, want)
	}
	if ent.Size() != len(want) {
		t.Fatalf("Size() = %d, want %d", ent.Size(), len(want))
	}
}

func TestEncodeDeleted(t *testing.T) {
	ent := Entry{Key: []byte("a"), Deleted: true}

	got := ent.Encode()
	want := []byte{199, 99, 230, 47, 1, 0, 0, 0, 0, 0, 0, 0, 1, 'a'}

	if !bytes.Equal(got, want) {
		t.Fatalf("Encode() = %v, want %v", got, want)
	}
}

// No single flipped bit anywhere in a record may pass as valid data.
//
// Which error comes back depends on where the bit was: damage inside a size
// field makes the record claim a length the data no longer matches, so the read
// runs out first and reports a truncated record. Either way the record is
// rejected, and Log.Read treats both the same.
func TestDecodeDetectsFlippedBit(t *testing.T) {
	data := (&Entry{Key: []byte("key"), Val: []byte("value")}).Encode()

	for i := range data {
		t.Run(fmt.Sprintf("byte %d", i), func(t *testing.T) {
			damaged := bytes.Clone(data)
			damaged[i] ^= 1 << 3

			var ent Entry
			err := ent.Decode(bytes.NewReader(damaged))
			if !errors.Is(err, ErrBadSum) && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("Decode() with byte %d flipped = %v, want ErrBadSum or io.ErrUnexpectedEOF", i, err)
			}
		})
	}
}

// A record can be complete and match its checksum yet still hold a field this
// package would never write. That is not damage, so it is not ErrBadSum.
func TestDecodeValidSumInvalidFlag(t *testing.T) {
	body := []byte{1, 0, 0, 0, 1, 0, 0, 0, 7, 'k', 'v'} // flag = 7

	data := make([]byte, 0, sumSize+len(body))
	data = binary.LittleEndian.AppendUint32(data, crc32.ChecksumIEEE(body))
	data = append(data, body...)

	var ent Entry
	err := ent.Decode(bytes.NewReader(data))
	if !errors.Is(err, ErrCorruptEntry) {
		t.Fatalf("Decode() = %v, want ErrCorruptEntry", err)
	}
	if errors.Is(err, ErrBadSum) {
		t.Fatal("a valid checksum was reported as a bad one")
	}
}

// A header claiming far more data than the file holds must not be allocated on
// faith — the checksum has not been verified at that point.
func TestDecodeHugeSizeIsNotAllocated(t *testing.T) {
	data := make([]byte, headerSize+4)
	binary.LittleEndian.PutUint32(data[sumSize:], 0xFFFFFFFF) // key size: 4 GiB

	var ent Entry
	if err := ent.Decode(bytes.NewReader(data)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Decode() = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		key     []byte
		val     []byte
		deleted bool
	}{
		{"simple", []byte("a"), []byte("bb"), false},
		{"empty key", []byte(""), []byte("value"), false},
		{"empty val", []byte("key"), []byte(""), false},
		{"both empty", []byte(""), []byte(""), false},
		{"binary", []byte{0, 1, 2, 255}, []byte{255, 0, 128}, false},
		{"long", bytes.Repeat([]byte("k"), 5000), bytes.Repeat([]byte("v"), 9000), false},
		{"tombstone", []byte("gone"), nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Entry{Key: tc.key, Val: tc.val, Deleted: tc.deleted}

			var out Entry
			if err := out.Decode(bytes.NewReader(in.Encode())); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}

			if !bytes.Equal(out.Key, tc.key) {
				t.Errorf("Key = %q, want %q", out.Key, tc.key)
			}
			if !bytes.Equal(out.Val, tc.val) {
				t.Errorf("Val = %q, want %q", out.Val, tc.val)
			}
			if out.Deleted != tc.deleted {
				t.Errorf("Deleted = %v, want %v", out.Deleted, tc.deleted)
			}
		})
	}
}

// A stale entry must not leak into the next Decode.
func TestDecodeResetsDeleted(t *testing.T) {
	ent := Entry{Key: []byte("old"), Deleted: true}

	live := (&Entry{Key: []byte("new"), Val: []byte("v")}).Encode()
	if err := ent.Decode(bytes.NewReader(live)); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if ent.Deleted {
		t.Fatal("Deleted stayed true after decoding a live entry")
	}
}

// Entries are written back to back with no separator, so Decode must stop
// exactly at the end of its own record and leave the rest for the next call.
func TestDecodeSequential(t *testing.T) {
	var buf bytes.Buffer
	want := []Entry{
		{Key: []byte("one"), Val: []byte("1")},
		{Key: []byte("two"), Val: []byte("22")},
		{Key: []byte("three"), Val: []byte("333")},
	}
	for _, ent := range want {
		buf.Write(ent.Encode())
	}

	for i, w := range want {
		var got Entry
		if err := got.Decode(&buf); err != nil {
			t.Fatalf("entry %d: Decode() error = %v", i, err)
		}
		if !bytes.Equal(got.Key, w.Key) || !bytes.Equal(got.Val, w.Val) {
			t.Fatalf("entry %d = (%q, %q), want (%q, %q)", i, got.Key, got.Val, w.Key, w.Val)
		}
	}

	var extra Entry
	if err := extra.Decode(&buf); !errors.Is(err, io.EOF) {
		t.Fatalf("Decode() past the end = %v, want io.EOF", err)
	}
}

// A clean end of stream is io.EOF; anything cut short mid-record is not.
func TestDecodeEOF(t *testing.T) {
	var ent Entry
	if err := ent.Decode(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("Decode(empty) = %v, want io.EOF", err)
	}
}

func TestDecodeTruncated(t *testing.T) {
	full := (&Entry{Key: []byte("key"), Val: []byte("value")}).Encode()

	cases := map[string]int{
		"partial header": 5,
		"header only":    headerSize,
		"partial key":    headerSize + 2,
		"partial val":    headerSize + 3 + 2,
		"flag missing":   headerSize - 1,
	}

	for name, n := range cases {
		t.Run(name, func(t *testing.T) {
			var ent Entry
			err := ent.Decode(bytes.NewReader(full[:n]))
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("Decode(%d bytes) = %v, want io.ErrUnexpectedEOF", n, err)
			}
		})
	}
}

// An io.Reader may return fewer bytes than asked for without erroring. Decode
// must keep reading until the record is complete, not trust a single Read.
func TestDecodeShortReads(t *testing.T) {
	in := Entry{Key: []byte("hello"), Val: []byte("world")}

	var out Entry
	if err := out.Decode(iotest.OneByteReader(bytes.NewReader(in.Encode()))); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !bytes.Equal(out.Key, in.Key) || !bytes.Equal(out.Val, in.Val) {
		t.Fatalf("got (%q, %q), want (%q, %q)", out.Key, out.Val, in.Key, in.Val)
	}
}

func TestDecodeReaderError(t *testing.T) {
	want := errors.New("disk on fire")
	full := (&Entry{Key: []byte("k"), Val: []byte("v")}).Encode()

	var ent Entry
	err := ent.Decode(iotest.ErrReader(want))
	if !errors.Is(err, want) {
		t.Fatalf("Decode() on failing header read = %v, want %v", err, want)
	}

	// The same error surfaced partway through the body must also reach the caller.
	r := io.MultiReader(bytes.NewReader(full[:headerSize]), iotest.ErrReader(want))
	if err := ent.Decode(r); !errors.Is(err, want) {
		t.Fatalf("Decode() on failing body read = %v, want %v", err, want)
	}
}

// Decode allocates its own slices; it must not hand back memory that the next
// Decode, or the caller's own buffer, can overwrite.
func TestDecodeDoesNotAlias(t *testing.T) {
	first := (&Entry{Key: []byte("aaa"), Val: []byte("111")}).Encode()
	second := (&Entry{Key: []byte("bbb"), Val: []byte("222")}).Encode()
	r := bytes.NewReader(append(first, second...))

	var ent Entry
	if err := ent.Decode(r); err != nil {
		t.Fatalf("first Decode() error = %v", err)
	}
	keptKey, keptVal := ent.Key, ent.Val

	if err := ent.Decode(r); err != nil {
		t.Fatalf("second Decode() error = %v", err)
	}

	if !bytes.Equal(keptKey, []byte("aaa")) || !bytes.Equal(keptVal, []byte("111")) {
		t.Fatalf("first entry mutated by second Decode: (%q, %q)", keptKey, keptVal)
	}
}

func BenchmarkEncode(b *testing.B) {
	ent := Entry{Key: []byte("some-key"), Val: bytes.Repeat([]byte("v"), 256)}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ent.Encode()
	}
}

func BenchmarkDecode(b *testing.B) {
	data := (&Entry{Key: []byte("some-key"), Val: bytes.Repeat([]byte("v"), 256)}).Encode()
	r := bytes.NewReader(nil)
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r.Reset(data)
		var ent Entry
		if err := ent.Decode(r); err != nil {
			b.Fatal(err)
		}
	}
}
