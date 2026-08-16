// Package wal implements the write-ahead log: the record format plus the file
// it is appended to.
//
// An entry is serialized as a length-prefixed pair of byte strings, tagged with
// a flag that says whether it records a write or a delete, and covered by a
// checksum:
//
//	|  crc32  | key size | val size | deleted | key data | val data |
//	| 4 bytes | 4 bytes  | 4 bytes  | 1 byte  |   ...    |   ...    |
//
// Sizes are little-endian uint32 and come first, so a reader knows how many
// bytes to pull before it has seen them. The checksum covers everything after
// itself, which is what lets a reader tell a record that was fully written from
// one that was interrupted by a crash.
package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

const (
	// sumSize is the crc32 prefix.
	sumSize = 4
	// metaSize is the part of the header the checksum covers: two uint32
	// lengths and the delete flag.
	metaSize = 9

	headerSize = sumSize + metaSize
)

// Values of the delete flag. Anything else means the record is corrupt.
const (
	flagLive    byte = 0
	flagDeleted byte = 1
)

var (
	// ErrBadSum reports a record whose checksum does not match its contents.
	// The usual cause is a write that a crash cut short.
	ErrBadSum = errors.New("wal: bad checksum")

	// ErrCorruptEntry reports a record that survived the checksum but holds a
	// value this package never writes — a bug or a format from another version,
	// not damage in transit.
	ErrCorruptEntry = errors.New("wal: corrupt entry")
)

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

	// Everything past the checksum field, which is what the checksum covers.
	body := data[sumSize:]

	binary.LittleEndian.PutUint32(body[0:4], uint32(len(ent.Key)))
	binary.LittleEndian.PutUint32(body[4:8], uint32(len(ent.Val)))
	if ent.Deleted {
		body[8] = flagDeleted
	}
	copy(body[metaSize:], ent.Key)
	copy(body[metaSize+len(ent.Key):], ent.Val)

	binary.LittleEndian.PutUint32(data[0:sumSize], crc32.ChecksumIEEE(body))

	return data
}

// Size reports how many bytes Encode will produce.
func (ent *Entry) Size() int {
	return headerSize + len(ent.Key) + len(ent.Val)
}

// Decode reads one entry from r and overwrites ent with it. Key and Val are
// freshly allocated, so they never alias r's buffers or ent's previous values.
//
// The errors say what kind of end this is, which is what recovery turns on:
//
//	io.EOF               r ended at a record boundary; every record was intact
//	io.ErrUnexpectedEOF  r ended inside a record; the write never finished
//	ErrBadSum            the record is all there but does not match its checksum
//	ErrCorruptEntry      the checksum passed but a field holds an impossible value
func (ent *Entry) Decode(r io.Reader) error {
	var header [headerSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}

	want := binary.LittleEndian.Uint32(header[0:sumSize])
	meta := header[sumSize:]
	keySize := binary.LittleEndian.Uint32(meta[0:4])
	valSize := binary.LittleEndian.Uint32(meta[4:8])

	key, err := readBody(r, keySize)
	if err != nil {
		return err
	}
	val, err := readBody(r, valSize)
	if err != nil {
		return err
	}

	// Checksum first: until it passes, nothing in the header is worth believing,
	// including the delete flag checked below.
	sum := crc32.ChecksumIEEE(meta)
	sum = crc32.Update(sum, crc32.IEEETable, key)
	sum = crc32.Update(sum, crc32.IEEETable, val)
	if sum != want {
		return fmt.Errorf("%w: record says %#08x, contents give %#08x", ErrBadSum, want, sum)
	}

	var deleted bool
	switch meta[8] {
	case flagLive:
	case flagDeleted:
		deleted = true
	default:
		return fmt.Errorf("%w: delete flag = %d", ErrCorruptEntry, meta[8])
	}

	ent.Key = key
	ent.Val = val
	ent.Deleted = deleted
	return nil
}

// readBody reads exactly size bytes. The header already promised them, so a
// stream that ends here is a truncated record rather than a clean end of file;
// io.EOF is promoted to io.ErrUnexpectedEOF to say so.
//
// size comes from a header that has not been checksummed yet, so it cannot be
// trusted: a torn write can leave anything in those four bytes. Small sizes are
// allocated up front, but past a threshold the buffer grows as the bytes
// actually arrive, so a damaged record claiming 4 GiB allocates only what the
// file really holds instead of exhausting memory before the checksum can reject it.
func readBody(r io.Reader, size uint32) ([]byte, error) {
	const maxPrealloc = 1 << 20

	if size <= maxPrealloc {
		buf := make([]byte, size)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, endOfRecord(err)
		}
		return buf, nil
	}

	var buf bytes.Buffer
	if _, err := io.CopyN(&buf, r, int64(size)); err != nil {
		return nil, endOfRecord(err)
	}
	return buf.Bytes(), nil
}

// endOfRecord reports a stream that ran out mid-record as a truncated record.
func endOfRecord(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
