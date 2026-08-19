package table

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/angelmidnighttt/mydb/internal/kv"
)

// schemaPrefix is what a table definition is stored under: the prefix, then the
// table name.
//
// The catalog shares one keyspace with every row of every table, and the @ is
// what keeps it out of their way — a row key opens with the four bytes of its
// table name length, so it would take a table named by more than a billion
// characters to begin like this one. Cheap, and worth knowing it rests on that
// rather than on anything enforced.
const schemaPrefix = "@schema_"

var (
	// ErrNoTable reports a table the catalog has never been told about.
	ErrNoTable = errors.New("table: no such table")

	// ErrTableExists reports a table being created a second time.
	ErrTableExists = errors.New("table: table already exists")
)

// schemaKey is where the definition of the named table lives.
func schemaKey(name string) []byte {
	return []byte(schemaPrefix + name)
}

// CreateTable writes schema into the catalog, which is what makes the table
// findable by name afterwards — and after a restart, since the catalog goes
// through the same log as everything else.
//
// A table that already exists is refused rather than overwritten: ErrTableExists
// comes straight from ModeInsert, the update mode that exists for exactly this.
// Overwriting would leave every row already written under the old definition to
// be decoded under the new one, which does not fail — it just reads nonsense.
//
// The schema is stored as JSON. It is the one thing in this database not written
// in a format of its own, and it can afford to be: there is one of them per
// table, they are read once per process, and a definition that has to survive
// versions is exactly where a self-describing format earns its size.
func (db *DB) CreateTable(schema *Schema) error {
	if err := schema.check(); err != nil {
		return err
	}

	val, err := json.Marshal(schema)
	if err != nil {
		return err
	}

	added, err := db.KV.SetEx(schemaKey(schema.Name), val, kv.ModeInsert)
	if err != nil {
		return err
	}
	if !added {
		return fmt.Errorf("%w: %s", ErrTableExists, schema.Name)
	}
	return nil
}

// GetSchema returns the definition of the named table, reading it out of the
// catalog the first time and out of a cache after that.
//
// The returned schema belongs to the database and is shared with every other
// caller: read it, do not write to it.
//
// Nothing is cached by CreateTable, even though it has the schema in its hands.
// Letting the first read decode it out of the store instead costs a JSON parse
// of a small value that is already in memory, and buys a cache that holds only
// what the store holds — never a struct the caller still has a pointer to and
// might yet change.
func (db *DB) GetSchema(name string) (*Schema, error) {
	db.mu.RLock()
	schema, cached := db.tables[name]
	db.mu.RUnlock()
	if cached {
		return schema, nil
	}

	val, stored := db.KV.Get(schemaKey(name))
	if !stored {
		return nil, fmt.Errorf("%w: %s", ErrNoTable, name)
	}

	schema = &Schema{}
	if err := json.Unmarshal(val, schema); err != nil {
		return nil, fmt.Errorf("table: definition of %s: %w", name, err)
	}
	// What comes back out of the store has not been checked by anything since
	// it went in, and the row operations index Cols and Types by the positions
	// in PK. A definition damaged or written by another version is caught here
	// rather than by a panic further down.
	if err := schema.check(); err != nil {
		return nil, err
	}

	db.mu.Lock()
	db.tables[name] = schema
	db.mu.Unlock()

	return schema, nil
}
