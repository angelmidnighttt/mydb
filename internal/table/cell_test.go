package table

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"testing"
)

// The byte layout is the on-disk format, so it is pinned literally: an int64 is
// eight bytes low-order first, a string is its length then its bytes.
func TestEncodeLayout(t *testing.T) {
	tests := []struct {
		name string
		cell Cell
		want []byte
	}{
		{
			name: "i64",
			cell: Cell{Type: TypeI64, I64: 0x1122334455667788},
			want: []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11},
		},
		{
			// Two's complement: -1 is every bit set, the same bits uint64 reads
			// as its largest value.
			name: "i64 negative",
			cell: Cell{Type: TypeI64, I64: -1},
			want: []byte{255, 255, 255, 255, 255, 255, 255, 255},
		},
		{
			// The lowest int64 is the top half of uint64 starting over: only the
			// sign bit is set, and little-endian puts it in the last byte.
			name: "i64 min",
			cell: Cell{Type: TypeI64, I64: math.MinInt64},
			want: []byte{0, 0, 0, 0, 0, 0, 0, 128},
		},
		{
			name: "str",
			cell: Cell{Type: TypeStr, Str: []byte("bb")},
			want: []byte{2, 0, 0, 0, 98, 98},
		},
		{
			name: "str empty",
			cell: Cell{Type: TypeStr},
			want: []byte{0, 0, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cell.Encode(nil)
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("Encode(nil) = %v, want %v", got, tt.want)
			}
			if tt.cell.Size() != len(tt.want) {
				t.Fatalf("Size() = %d, want %d", tt.cell.Size(), len(tt.want))
			}
		})
	}
}

// Every value a cell can hold has to come back out unchanged — the edges of the
// int64 range included, since that is where a wrong signed conversion shows.
func TestRoundTrip(t *testing.T) {
	cells := []Cell{
		{Type: TypeI64, I64: 0},
		{Type: TypeI64, I64: 1},
		{Type: TypeI64, I64: -1},
		{Type: TypeI64, I64: math.MaxInt64},
		{Type: TypeI64, I64: math.MinInt64},
		{Type: TypeStr, Str: nil},
		{Type: TypeStr, Str: []byte{}},
		{Type: TypeStr, Str: []byte("hello")},
		{Type: TypeStr, Str: bytes.Repeat([]byte{0}, 1000)},
	}

	for _, want := range cells {
		t.Run(fmt.Sprint(want.Type, " ", want.I64, " ", len(want.Str)), func(t *testing.T) {
			got := Cell{Type: want.Type}
			rest, err := got.Decode(want.Encode(nil))
			if err != nil {
				t.Fatalf("Decode() = %v", err)
			}
			if len(rest) != 0 {
				t.Fatalf("Decode() left %d bytes over", len(rest))
			}
			if got.I64 != want.I64 || !bytes.Equal(got.Str, want.Str) {
				t.Fatalf("round trip = %+v, want %+v", got, want)
			}
		})
	}
}

// Encode appends, so a row is built by passing one buffer through every cell,
// and Decode hands back the rest so the same row is read back the same way.
func TestCellsJoinIntoOneBuffer(t *testing.T) {
	row := []Cell{
		{Type: TypeI64, I64: 7},
		{Type: TypeStr, Str: []byte("alice")},
		{Type: TypeI64, I64: -7},
	}

	data := []byte("header")
	for i := range row {
		data = row[i].Encode(data)
	}
	if !bytes.HasPrefix(data, []byte("header")) {
		t.Fatalf("Encode() overwrote what the buffer already held: %q", data)
	}

	rest := data[len("header"):]
	for i, want := range row {
		got := Cell{Type: want.Type}
		var err error
		if rest, err = got.Decode(rest); err != nil {
			t.Fatalf("Decode() cell %d = %v", i, err)
		}
		if got.I64 != want.I64 || !bytes.Equal(got.Str, want.Str) {
			t.Fatalf("cell %d = %+v, want %+v", i, got, want)
		}
	}
	if len(rest) != 0 {
		t.Fatalf("%d bytes left after the last cell", len(rest))
	}
}

// Any prefix of a cell is a cell that was cut short, never a shorter valid one.
func TestDecodeTruncated(t *testing.T) {
	for _, cell := range []Cell{
		{Type: TypeI64, I64: -1},
		{Type: TypeStr, Str: []byte("hello")},
	} {
		data := cell.Encode(nil)
		for i := range data {
			t.Run(fmt.Sprintf("%v/%d bytes", cell.Type, i), func(t *testing.T) {
				got := Cell{Type: cell.Type}
				if _, err := got.Decode(data[:i]); !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("Decode() = %v, want io.ErrUnexpectedEOF", err)
				}
			})
		}
	}
}

// A length field claiming more than the buffer holds must be rejected, not used
// to slice past the end.
func TestDecodeStrLengthOverruns(t *testing.T) {
	data := []byte{255, 255, 255, 255, 97} // says 4 GiB, carries one byte

	cell := Cell{Type: TypeStr}
	if _, err := cell.Decode(data); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Decode() = %v, want io.ErrUnexpectedEOF", err)
	}
}

// The type comes from the schema, so a cell without a valid one has nothing to
// say how the bytes should be read.
func TestDecodeBadType(t *testing.T) {
	for _, typ := range []CellType{0, 3, 255} {
		t.Run(typ.String(), func(t *testing.T) {
			cell := Cell{Type: typ}
			if _, err := cell.Decode([]byte{1, 0, 0, 0, 0, 0, 0, 0}); !errors.Is(err, ErrBadType) {
				t.Fatalf("Decode() = %v, want ErrBadType", err)
			}
		})
	}
}

// Writing an untyped cell would put bytes on disk that nothing can read back,
// so it fails loudly at the call instead.
func TestEncodeBadTypePanics(t *testing.T) {
	for name, call := range map[string]func(*Cell){
		"Encode": func(c *Cell) { c.Encode(nil) },
		"Size":   func(c *Cell) { c.Size() },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s() on an untyped cell did not panic", name)
				}
			}()
			cell := Cell{}
			call(&cell)
		})
	}
}

// A decoded string outlives the buffer it came from: callers reuse those, and a
// cell that aliased one would change under them.
func TestDecodeCopiesInput(t *testing.T) {
	data := (&Cell{Type: TypeStr, Str: []byte("alice")}).Encode(nil)

	cell := Cell{Type: TypeStr}
	if _, err := cell.Decode(data); err != nil {
		t.Fatalf("Decode() = %v", err)
	}
	for i := range data {
		data[i] = 120
	}

	if string(cell.Str) != "alice" {
		t.Fatalf("Str = %q after the source buffer was overwritten, want %q", cell.Str, "alice")
	}
}

// Decoding overwrites the whole cell, so nothing of an earlier value is left in
// the field the new type does not use.
func TestDecodeClearsTheUnusedField(t *testing.T) {
	cell := Cell{Type: TypeStr, Str: []byte("alice"), I64: 42}

	cell.Type = TypeI64
	if _, err := cell.Decode((&Cell{Type: TypeI64, I64: 1}).Encode(nil)); err != nil {
		t.Fatalf("Decode() = %v", err)
	}
	if cell.Str != nil {
		t.Fatalf("Str = %q after decoding an int64, want nil", cell.Str)
	}

	cell.Type = TypeStr
	if _, err := cell.Decode((&Cell{Type: TypeStr, Str: []byte("bob")}).Encode(nil)); err != nil {
		t.Fatalf("Decode() = %v", err)
	}
	if cell.I64 != 0 {
		t.Fatalf("I64 = %d after decoding a string, want 0", cell.I64)
	}
}
