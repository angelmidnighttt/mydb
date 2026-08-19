package table

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// The running example throughout: create table t (a int64, b int64, primary key (b)).
// Column b is the key even though it is declared second, so the key columns are
// not the leading ones.
func testSchema() *Schema {
	return &Schema{
		Name:  "t",
		Cols:  []string{"a", "b"},
		Types: []CellType{TypeI64, TypeI64},
		PK:    []int{1},
	}
}

func testRow(a, b int64) Row {
	return Row{
		{Type: TypeI64, I64: a},
		{Type: TypeI64, I64: b},
	}
}

func TestEncodeKeyLayout(t *testing.T) {
	schema := testSchema()

	got := testRow(7, 42).EncodeKey(schema)
	want := []byte{
		1, 0, 0, 0, // length of the table name
		't',
		42, 0, 0, 0, 0, 0, 0, 0, // column b, the key
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeKey() = %v, want %v", got, want)
	}
}

// Column a is not in the key, so it is what the value holds — and column b is
// not repeated there.
func TestEncodeValHoldsTheOtherColumns(t *testing.T) {
	schema := testSchema()

	got := testRow(7, 42).EncodeVal(schema)
	want := []byte{7, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeVal() = %v, want %v", got, want)
	}
}

// Two tables share one keyspace. The same primary key value in each must not
// produce the same KV key, or one table's rows land on the other's.
func TestEncodeKeySeparatesTables(t *testing.T) {
	first := testSchema()
	second := testSchema()
	second.Name = "u"

	row := testRow(7, 42)
	if bytes.Equal(row.EncodeKey(first), row.EncodeKey(second)) {
		t.Fatal("rows of two tables encode to the same key")
	}
}

// The key columns come out in the order PK lists them, not in column order.
func TestEncodeKeyFollowsPKOrder(t *testing.T) {
	schema := &Schema{
		Name:  "t",
		Cols:  []string{"a", "b"},
		Types: []CellType{TypeI64, TypeI64},
		PK:    []int{1, 0},
	}

	got := testRow(7, 42).EncodeKey(schema)
	want := []byte{
		1, 0, 0, 0,
		't',
		42, 0, 0, 0, 0, 0, 0, 0, // b first, as PK says
		7, 0, 0, 0, 0, 0, 0, 0, // then a
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeKey() = %v, want %v", got, want)
	}
}

func TestValRoundTrip(t *testing.T) {
	schema := &Schema{
		Name:  "users",
		Cols:  []string{"id", "name", "age"},
		Types: []CellType{TypeI64, TypeStr, TypeI64},
		PK:    []int{0},
	}
	row := Row{
		{Type: TypeI64, I64: 7},
		{Type: TypeStr, Str: []byte("alice")},
		{Type: TypeI64, I64: 30},
	}

	// Only the key cell is known up front; the rest is what decoding fills in.
	decoded := Row{{Type: TypeI64, I64: 7}, {}, {}}
	if err := decoded.DecodeVal(schema, row.EncodeVal(schema)); err != nil {
		t.Fatalf("DecodeVal() error = %v", err)
	}

	if got := string(decoded[1].Str); got != "alice" {
		t.Errorf("name = %q, want \"alice\"", got)
	}
	if decoded[2].I64 != 30 {
		t.Errorf("age = %d, want 30", decoded[2].I64)
	}
	// The key cell was not in the value and must be left as the caller set it.
	if decoded[0].I64 != 7 {
		t.Errorf("id = %d, want 7 — the key cell was overwritten", decoded[0].I64)
	}
}

// Nothing in the value says where it ends, so bytes left over are the only
// self-evident sign that the schema and the data have drifted apart.
func TestDecodeValRejectsTrailingBytes(t *testing.T) {
	schema := testSchema()
	data := append(testRow(7, 42).EncodeVal(schema), 0xff)

	err := testRow(0, 42).DecodeVal(schema, data)
	if !errors.Is(err, ErrBadRow) {
		t.Fatalf("DecodeVal() error = %v, want ErrBadRow", err)
	}
}

func TestDecodeValRejectsTruncated(t *testing.T) {
	schema := testSchema()
	full := testRow(7, 42).EncodeVal(schema)

	for cut := 0; cut < len(full); cut++ {
		err := testRow(0, 42).DecodeVal(schema, full[:cut])
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("DecodeVal(%d of %d bytes) error = %v, want io.ErrUnexpectedEOF", cut, len(full), err)
		}
	}
}
