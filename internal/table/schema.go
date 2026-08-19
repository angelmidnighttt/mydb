package table

import (
	"errors"
	"fmt"
)

var (
	// ErrBadSchema reports a schema that does not describe a usable table. It is
	// a bug where the table was defined, not where the row came from.
	ErrBadSchema = errors.New("table: invalid schema")

	// ErrBadRow reports a row that does not fit the schema it was handed to:
	// the wrong number of cells, or a cell holding a type its column was not
	// declared with.
	ErrBadRow = errors.New("table: row does not fit the schema")
)

// Schema describes one table: what its columns are called, what each one holds,
// and which of them make up the primary key.
//
// Columns keep the order they were declared in, and a Row follows that same
// order — position i of a row is column i of the schema. The primary key is a
// list of positions rather than "the first n columns", so
//
//	create table t (a int64, b int64, primary key (b))
//
// is Schema{Cols: {"a", "b"}, PK: {1}} and needs no reordering of the table to
// put the key in front.
type Schema struct {
	// Name identifies the table, and is the prefix of every key the table
	// writes. Two tables share one KV keyspace, so without it row 1 of one
	// table and row 1 of another would be the same key.
	Name string

	// Cols are the column names, in declared order.
	Cols []string

	// Types are the column types, one for each entry in Cols. This is the only
	// record of which column holds which type: the encoded row carries no tags.
	Types []CellType

	// PK lists the positions in Cols that form the primary key, in key order.
	// Every table needs one — it is the only way a row can be addressed.
	PK []int
}

// check reports whether the schema describes a table at all. The row checks run
// it first because they index Cols and Types by the positions in PK, which is
// only safe once those positions are known to be in range.
func (schema *Schema) check() error {
	if schema.Name == "" {
		return fmt.Errorf("%w: table has no name", ErrBadSchema)
	}
	if len(schema.Cols) == 0 {
		return fmt.Errorf("%w: table %q has no columns", ErrBadSchema, schema.Name)
	}
	if len(schema.Types) != len(schema.Cols) {
		return fmt.Errorf("%w: table %q has %d columns but %d types",
			ErrBadSchema, schema.Name, len(schema.Cols), len(schema.Types))
	}
	for i, typ := range schema.Types {
		if typ != TypeI64 && typ != TypeStr {
			return fmt.Errorf("%w: column %q has type %v", ErrBadSchema, schema.Cols[i], typ)
		}
		// Two columns of one name cannot both be addressed: a statement naming
		// that column would always mean the first, and the second could only
		// ever be written by position.
		for _, earlier := range schema.Cols[:i] {
			if earlier == schema.Cols[i] {
				return fmt.Errorf("%w: table %q has two columns named %q",
					ErrBadSchema, schema.Name, schema.Cols[i])
			}
		}
	}

	if len(schema.PK) == 0 {
		return fmt.Errorf("%w: table %q has no primary key", ErrBadSchema, schema.Name)
	}
	for n, col := range schema.PK {
		if col < 0 || col >= len(schema.Cols) {
			return fmt.Errorf("%w: primary key column %d is outside the %d columns of %q",
				ErrBadSchema, col, len(schema.Cols), schema.Name)
		}
		// A column twice in the key would be encoded twice and compared twice,
		// which says nothing the single copy did not already say.
		for _, earlier := range schema.PK[:n] {
			if earlier == col {
				return fmt.Errorf("%w: column %q appears twice in the primary key of %q",
					ErrBadSchema, schema.Cols[col], schema.Name)
			}
		}
	}
	return nil
}

// isPK reports whether column i belongs to the primary key. Keys are a handful
// of columns at most, so the scan beats keeping a set alongside them.
func (schema *Schema) isPK(i int) bool {
	for _, col := range schema.PK {
		if col == i {
			return true
		}
	}
	return false
}

// checkKey reports whether row can be addressed under schema: it is as long as
// the table is wide, and its key cells hold the types those columns were
// declared with. The other cells are not looked at — Select overwrites them and
// Delete never reads them.
func (schema *Schema) checkKey(row Row) error {
	if err := schema.check(); err != nil {
		return err
	}
	if len(row) != len(schema.Cols) {
		return fmt.Errorf("%w: table %q has %d columns, row has %d cells",
			ErrBadRow, schema.Name, len(schema.Cols), len(row))
	}
	for _, col := range schema.PK {
		if err := schema.checkCell(row, col); err != nil {
			return err
		}
	}
	return nil
}

// checkRow is checkKey plus the columns outside the key, for the operations
// that write a whole row.
func (schema *Schema) checkRow(row Row) error {
	if err := schema.checkKey(row); err != nil {
		return err
	}
	for i := range row {
		if err := schema.checkCell(row, i); err != nil {
			return err
		}
	}
	return nil
}

// checkCell reports whether cell i of row holds the type column i was declared
// with. A mismatch has to be caught here: nothing downstream can catch it, since
// the encoding writes no type and Decode believes whatever the schema says.
func (schema *Schema) checkCell(row Row, i int) error {
	if row[i].Type != schema.Types[i] {
		return fmt.Errorf("%w: column %q of %q is %v, cell is %v",
			ErrBadRow, schema.Cols[i], schema.Name, schema.Types[i], row[i].Type)
	}
	return nil
}
