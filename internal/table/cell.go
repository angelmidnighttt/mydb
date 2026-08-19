// Package table starts the relational layer on top of the key-value store. A
// database holds tables, a table holds rows, and a row holds cells. Unlike KV,
// where a value is just bytes, every cell has a data type — for now int64 or
// []byte.
//
// A cell is serialized as its value alone:
//
//	int64   | value   |
//	        | 8 bytes |
//
//	[]byte  | length  | data |
//	        | 4 bytes | ...  |
//
// Numbers are little-endian, matching the rest of the on-disk formats in this
// project and the byte order of the CPUs that run it, so encoding is a copy
// rather than a byte swap.
//
// The type is not written down. Which column holds which type is fixed by the
// table schema, so both writer and reader already know it; a tag on every cell
// of every row would repeat, once per row, what the schema states once. Decode
// therefore takes the type from the cell it is filling, not from the data.
package table

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Widths of the fixed-size parts of the encoding.
const (
	i64Size    = 8
	strLenSize = 4
)

// CellType says which of the two data types a cell holds. The zero value is
// deliberately not a valid type, so a Cell that was never given one is rejected
// instead of quietly passing as an int64.
type CellType uint8

const (
	TypeI64 CellType = 1
	TypeStr CellType = 2
)

// String renders the type for error messages.
func (t CellType) String() string {
	switch t {
	case TypeI64:
		return "int64"
	case TypeStr:
		return "bytes"
	default:
		return fmt.Sprintf("CellType(%d)", uint8(t))
	}
}

// ErrBadType reports a cell whose Type is neither TypeI64 nor TypeStr. It means
// the schema and the data disagree, or a Cell was built without a type — not
// damage in transit.
var ErrBadType = errors.New("table: unknown cell type")

// Cell is one typed value. Type selects which of the remaining fields carries
// the value: I64 for TypeI64, Str for TypeStr. Go has no union type, so both
// fields exist in every cell and one of them is always wasted space — 8 bytes
// per cell, paid in memory only. Nothing of the unused field reaches the disk.
type Cell struct {
	Type CellType
	I64  int64
	Str  []byte
}

// Encode appends the serialized cell to dst and returns the extended slice, the
// way append does. A row is many cells written back to back, so the caller
// threads one buffer through all of them instead of allocating per value, and
// can hand back the same buffer on the next row once it has been flushed.
//
// It panics if Type is invalid: the signature has nowhere to put an error, and
// an unknown type is a bug in the caller rather than bad input.
func (cell *Cell) Encode(dst []byte) []byte {
	switch cell.Type {
	case TypeI64:
		// Casting int64 to uint64 moves no bits, it only changes how they are
		// read, so negative values survive the unsigned API untouched.
		return binary.LittleEndian.AppendUint64(dst, uint64(cell.I64))
	case TypeStr:
		dst = binary.LittleEndian.AppendUint32(dst, uint32(len(cell.Str)))
		return append(dst, cell.Str...)
	default:
		panic(fmt.Sprintf("%v: %v", ErrBadType, cell.Type))
	}
}

// Size reports how many bytes Encode will append. It panics on an invalid type,
// as Encode does.
func (cell *Cell) Size() int {
	switch cell.Type {
	case TypeI64:
		return i64Size
	case TypeStr:
		return strLenSize + len(cell.Str)
	default:
		panic(fmt.Sprintf("%v: %v", ErrBadType, cell.Type))
	}
}

// Decode fills the cell from the front of data and returns what follows it, so
// a row is read by threading rest through one cell after another until it is
// empty.
//
// cell.Type must already hold the column's type before the call — it says what
// to read, and it is not in the data. Str is a fresh copy, so a decoded cell
// never aliases data and stays valid after the caller reuses that buffer; an
// empty string decodes to an empty non-nil slice.
//
// Errors:
//
//	io.ErrUnexpectedEOF  data ends inside the cell — it was cut short
//	ErrBadType           cell.Type is not one of the two data types
func (cell *Cell) Decode(data []byte) (rest []byte, err error) {
	switch cell.Type {
	case TypeI64:
		if len(data) < i64Size {
			return nil, fmt.Errorf("%w: int64 needs %d bytes, got %d", io.ErrUnexpectedEOF, i64Size, len(data))
		}
		cell.I64 = int64(binary.LittleEndian.Uint64(data[:i64Size]))
		cell.Str = nil
		return data[i64Size:], nil

	case TypeStr:
		if len(data) < strLenSize {
			return nil, fmt.Errorf("%w: string header needs %d bytes, got %d", io.ErrUnexpectedEOF, strLenSize, len(data))
		}
		size := binary.LittleEndian.Uint32(data[:strLenSize])
		data = data[strLenSize:]
		// Compared as uint64 so a huge length cannot overflow int on a 32-bit
		// build and slip through as a small one.
		if uint64(size) > uint64(len(data)) {
			return nil, fmt.Errorf("%w: string says %d bytes, got %d", io.ErrUnexpectedEOF, size, len(data))
		}
		cell.I64 = 0
		cell.Str = bytes.Clone(data[:size])
		return data[size:], nil

	default:
		return nil, fmt.Errorf("%w: %v", ErrBadType, cell.Type)
	}
}
