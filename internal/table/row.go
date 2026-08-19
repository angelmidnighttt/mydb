package table

import (
	"encoding/binary"
	"fmt"
)

// Row is one row of a table: one cell per column of its schema, in the schema's
// column order, so a cell's position is its column.
//
// A row is always as long as the table is wide, even for operations that only
// need part of it. Select is handed a row with the key cells filled and fills in
// the rest; Delete reads the key cells and ignores the others.
type Row []Cell

// EncodeKey builds the KV key for row: the table name, then the key cells in the
// order Schema.PK lists them.
//
//	| name size | name | key cell | key cell | ... |
//	|  4 bytes  | ...  |   ...    |   ...    |     |
//
// The name is framed the same way a []byte cell is, and for the same reason:
// what follows it has to start at a known offset. It comes first because every
// table shares one KV keyspace — without it, row 1 of one table and row 1 of
// another are the same key, and the second insert quietly overwrites the first.
//
// It panics if row is shorter than the schema or a key cell has an invalid type.
// The DB methods check both before they get here.
func (row Row) EncodeKey(schema *Schema) []byte {
	size := strLenSize + len(schema.Name)
	for _, col := range schema.PK {
		size += row[col].Size()
	}

	out := make([]byte, 0, size)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(schema.Name)))
	out = append(out, schema.Name...)
	for _, col := range schema.PK {
		out = row[col].Encode(out)
	}
	return out
}

// EncodeVal builds the KV value for row: every column outside the primary key,
// in column order, one after another.
//
// The key columns are left out on purpose. They are already in the key, and a
// second copy is a second thing to keep in step — one that can end up disagreeing
// with the first.
//
// It panics under the same conditions as EncodeKey.
func (row Row) EncodeVal(schema *Schema) []byte {
	size := 0
	for i := range row {
		if !schema.isPK(i) {
			size += row[i].Size()
		}
	}

	out := make([]byte, 0, size)
	for i := range row {
		if !schema.isPK(i) {
			out = row[i].Encode(out)
		}
	}
	return out
}

// DecodeVal fills the non-key cells of row from data, which is what EncodeVal
// wrote. The key cells are left alone: the caller supplied them, and they are
// not in the value.
//
// Each cell is given its column's type before it is read, because the data does
// not say. That is also why the leftover check at the end matters — with no tags
// in the stream, a schema that has drifted from the data decodes into plausible
// nonsense, and bytes left over is the one sign of it that shows up on its own.
func (row Row) DecodeVal(schema *Schema, data []byte) error {
	rest := data
	for i := range row {
		if schema.isPK(i) {
			continue
		}
		row[i].Type = schema.Types[i]

		var err error
		if rest, err = row[i].Decode(rest); err != nil {
			return fmt.Errorf("%w: column %q of %q: %w", ErrBadRow, schema.Cols[i], schema.Name, err)
		}
	}
	if len(rest) != 0 {
		return fmt.Errorf("%w: %d bytes left over after the last column of %q",
			ErrBadRow, len(rest), schema.Name)
	}
	return nil
}
