// Package wal implements the write-ahead log: the record format plus the file
// it is appended to.
//
// An entry is serialized as a length-prefixed pair of byte strings, tagged with
// a flag that says whether it records a write or a delete:
//
//	| key size | val size | deleted | key data | val data |
//	| 4 bytes  | 4 bytes  | 1 byte  |   ...    |   ...    |
//
// Sizes are little-endian uint32 and come first, so a reader knows how many
// bytes to pull before it has seen them.
package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// headerSize is the fixed prefix: two uint32 lengths and the delete flag.
const headerSize = 9

// Values of the delete flag. Anything else means the record is corrupt.
const (
	flagLive    byte = 0
	flagDeleted byte = 1
)

// ErrCorruptEntry reports a record that cannot be a value this package wrote.
var ErrCorruptEntry = errors.New("wal: corrupt entry")

// Entry is a single log record: a key-value write, or a delete when Deleted is
// set. A delete carries no value.
type Entry struct {
	Key     []byte
	Val     []byte
	Deleted bool
}

// Encode serializes the entry into a newly allocated slice.
func (ent *Entry) Encode() []byte {
	data := make([]byte, headerSize+len(ent.Key)+len(ent.Val))

	binary.LittleEndian.PutUint32(data[0:4], uint32(len(ent.Key)))
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(ent.Val)))
	if ent.Deleted {
		data[8] = flagDeleted
	}
	copy(data[headerSize:], ent.Key)
	copy(data[headerSize+len(ent.Key):], ent.Val)

	return data
}

// Size reports how many bytes Encode will produce.
func (ent *Entry) Size() int {
	return headerSize + len(ent.Key) + len(ent.Val)
}

// Decode reads one entry from r and overwrites ent with it. Key and Val are
// freshly allocated, so they never alias r's buffers or ent's previous values.
//
// It returns io.EOF when r is exhausted at a record boundary — the normal way
// to end a replay loop — and io.ErrUnexpectedEOF when a record is cut short.
func (ent *Entry) Decode(r io.Reader) error {
	var header [headerSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}

	keySize := binary.LittleEndian.Uint32(header[0:4])
	valSize := binary.LittleEndian.Uint32(header[4:8])

	var deleted bool
	switch header[8] {
	case flagLive:
	case flagDeleted:
		deleted = true
	default:
		// Not a flag this package ever writes, so the bytes are not the record
		// we think they are. Reading on would misinterpret whatever follows.
		return fmt.Errorf("%w: delete flag = %d", ErrCorruptEntry, header[8])
	}

	key := make([]byte, keySize)
	if err := readBody(r, key); err != nil {
		return err
	}

	val := make([]byte, valSize)
	if err := readBody(r, val); err != nil {
		return err
	}

	ent.Key = key
	ent.Val = val
	ent.Deleted = deleted
	return nil
}

// readBody fills buf completely. The header already promised these bytes, so a
// stream that ends here is a truncated record rather than a clean end of file;
// io.EOF is promoted to io.ErrUnexpectedEOF to say so.
func readBody(r io.Reader, buf []byte) error {
	if _, err := io.ReadFull(r, buf); err != nil {
		if errors.Is(err, io.EOF) {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	return nil
}
