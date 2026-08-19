package table

import (
	"sync"

	"github.com/angelmidnighttt/mydb/internal/kv"
)

// DB is a relational database: rows addressed by primary key, kept in the
// key-value store underneath. One row is one KV pair — the key columns encoded
// into the key, everything else into the value.
//
// The zero value is not usable; set KV.Path and call Open.
//
// The row operations take the schema they work against, rather than looking it
// up. The database does know its tables — CreateTable writes them down and
// GetSchema reads them back — but keeping the two apart means a row can be read
// or written by anything holding a schema, whether the catalog has heard of it
// or not, which is what the tests of those operations rely on.
type DB struct {
	KV kv.KV

	// mu guards tables, which is a cache in front of the catalog rather than
	// state of its own: everything in it is also in the store.
	mu     sync.RWMutex
	tables map[string]*Schema
}

// Open opens the underlying store, replaying its log.
func (db *DB) Open() error {
	if err := db.KV.Open(); err != nil {
		return err
	}
	db.tables = make(map[string]*Schema)
	return nil
}

// Close closes the underlying store.
func (db *DB) Close() error { return db.KV.Close() }

// Select reads the row addressed by the primary key cells of row and fills in
// the rest of its cells. ok is false when there is no such row, and row is left
// as it was.
//
// row is as long as the table is wide: the key cells are input, the others are
// output.
func (db *DB) Select(schema *Schema, row Row) (ok bool, err error) {
	if err := schema.checkKey(row); err != nil {
		return false, err
	}

	val, ok := db.KV.Get(row.EncodeKey(schema))
	if !ok {
		return false, nil
	}
	if err := row.DecodeVal(schema, val); err != nil {
		return false, err
	}
	return true, nil
}

// Insert adds a row that is not there yet — SQL's INSERT. updated is false when
// the primary key is taken, and the row already stored is left untouched.
//
// row must be complete: every column, key or not, holds its value.
func (db *DB) Insert(schema *Schema, row Row) (updated bool, err error) {
	return db.write(schema, row, kv.ModeInsert)
}

// Update overwrites a row that is already there — SQL's UPDATE. updated is false
// when the primary key is absent, and no row is created.
//
// There is no partial update: row carries every column, so changing one field
// means reading the row with Select first. Nothing stops another writer from
// slipping in between the two calls — that gap closes when transactions arrive.
func (db *DB) Update(schema *Schema, row Row) (updated bool, err error) {
	return db.write(schema, row, kv.ModeUpdate)
}

// Upsert writes row either way, inserting it or overwriting what was there.
// updated reports which of the two happened: true means it overwrote.
func (db *DB) Upsert(schema *Schema, row Row) (updated bool, err error) {
	return db.write(schema, row, kv.ModeUpsert)
}

// Delete removes the row addressed by the primary key cells of row. deleted is
// false when there was no such row. The cells outside the key are not read.
func (db *DB) Delete(schema *Schema, row Row) (deleted bool, err error) {
	if err := schema.checkKey(row); err != nil {
		return false, err
	}
	return db.KV.Del(row.EncodeKey(schema))
}

// write is the one write path. The three modes differ only in what KV does about
// a key that is, or is not, already there, so they share everything up to it:
// the same checks, the same key, the same value.
func (db *DB) write(schema *Schema, row Row, mode kv.UpdateMode) (bool, error) {
	if err := schema.checkRow(row); err != nil {
		return false, err
	}
	return db.KV.SetEx(row.EncodeKey(schema), row.EncodeVal(schema), mode)
}
