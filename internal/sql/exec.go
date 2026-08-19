package sql

import (
	"errors"
	"fmt"

	"github.com/angelmidnighttt/mydb/internal/table"
)

// ErrBadStatement reports a statement that reads fine but does not fit the
// database it was run against: a column no table has, a WHERE that is not the
// primary key, an INSERT with the wrong number of values.
//
// It is the other half of ErrSyntax. One says the text is not SQL; this one says
// the SQL does not match what is there. A caller telling a user what went wrong
// wants to keep those apart, because only one of them can be answered by reading
// the statement again.
var ErrBadStatement = errors.New("sql: statement does not fit the database")

// Result is what running one statement produced.
//
// A SELECT fills Header and Values; the other statements fill Updated with the
// number of rows they changed. Values is a list although only one row can come
// back today — the shape is what a scan or a range query will return, and the
// callers written against it now will not have to change then.
type Result struct {
	Updated int
	Header  []string
	Values  []table.Row
}

// Exec runs one parsed statement against db.
//
// This is where the two halves of the database meet. Everything above it is
// text: names, words, positions in a string. Everything below it is storage:
// schemas, rows, keys. The whole job of this file is turning the first into the
// second — mostly by turning column names into column positions, which is the
// one thing the parser was not allowed to do.
//
// A statement of an unknown type is an error rather than a panic. Exec takes
// any, so an unknown type is something a caller passed in, and code that is
// handed bad input reports it.
func Exec(db *table.DB, stmt any) (Result, error) {
	var (
		res Result
		err error
	)

	switch stmt := stmt.(type) {
	case *StmtCreateTable:
		err = execCreateTable(db, stmt)
	case *StmtSelect:
		res.Header = stmt.cols
		res.Values, err = execSelect(db, stmt)
	case *StmtInsert:
		res.Updated, err = execInsert(db, stmt)
	case *StmtUpdate:
		res.Updated, err = execUpdate(db, stmt)
	case *StmtDelete:
		res.Updated, err = execDelete(db, stmt)
	default:
		return Result{}, fmt.Errorf("%w: %T is not a statement", ErrBadStatement, stmt)
	}

	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// execCreateTable turns a CREATE TABLE into a Schema and writes it to the
// catalog.
//
// The key columns are named in the statement and numbered in the schema, so this
// is where the names are looked up — and where a key naming a column that was
// never declared is finally caught. The parser could have caught it, since both
// halves are in the one statement, and deliberately did not: names become
// positions here, once, for every statement.
func execCreateTable(db *table.DB, stmt *StmtCreateTable) error {
	schema := table.Schema{
		Name:  stmt.table,
		Cols:  make([]string, len(stmt.cols)),
		Types: make([]table.CellType, len(stmt.cols)),
		PK:    make([]int, 0, len(stmt.pkey)),
	}
	for i, col := range stmt.cols {
		schema.Cols[i] = col.name
		schema.Types[i] = col.typ
	}

	for _, name := range stmt.pkey {
		col := columnIndex(&schema, name)
		if col < 0 {
			return fmt.Errorf("%w: the primary key of %s names %s, which is not one of its columns",
				ErrBadStatement, schema.Name, name)
		}
		schema.PK = append(schema.PK, col)
	}

	return db.CreateTable(&schema)
}

// execSelect reads one row and hands back the columns that were asked for.
func execSelect(db *table.DB, stmt *StmtSelect) ([]table.Row, error) {
	schema, err := db.GetSchema(stmt.table)
	if err != nil {
		return nil, err
	}

	indices, err := lookupColumns(schema, stmt.cols)
	if err != nil {
		return nil, err
	}
	row, err := makePKey(schema, stmt.keys)
	if err != nil {
		return nil, err
	}

	found, err := db.Select(schema, row)
	if err != nil {
		return nil, err
	}
	if !found {
		// No row is not an error. A SELECT that matches nothing has done its
		// job and found nothing, which is what an empty list says.
		return nil, nil
	}

	return []table.Row{subsetRow(row, indices)}, nil
}

// execInsert writes a new row. The values are positional, so the only thing to
// resolve is how many there should be; the types are checked by the table layer
// against the schema, which is the one place that knows them.
func execInsert(db *table.DB, stmt *StmtInsert) (int, error) {
	schema, err := db.GetSchema(stmt.table)
	if err != nil {
		return 0, err
	}
	if len(stmt.value) != len(schema.Cols) {
		return 0, fmt.Errorf("%w: table %s has %d columns, the insert has %d values",
			ErrBadStatement, schema.Name, len(schema.Cols), len(stmt.value))
	}

	inserted, err := db.Insert(schema, table.Row(stmt.value))
	if err != nil {
		return 0, err
	}
	return affected(inserted), nil
}

// execUpdate reads the row, writes the SET columns over it, and puts it back.
//
// The read is needed because the table layer writes a whole row at a time: the
// columns the statement says nothing about still have to be in what is written,
// and the only place to get them is the row that is there. A read and a write
// with no lock between them is not atomic, so two updates to one row can lose
// each other. That gap closes with transactions, not before.
//
// Key columns cannot be set. The key is what the row is stored under, so writing
// to it does not change a row, it moves one — a delete and an insert wearing an
// UPDATE for a hat. Refusing is honest until that is what it actually does.
func execUpdate(db *table.DB, stmt *StmtUpdate) (int, error) {
	schema, err := db.GetSchema(stmt.table)
	if err != nil {
		return 0, err
	}

	// Resolve the SET list before touching the store, so a statement naming a
	// column that does not exist fails the same way whether the row is there or
	// not.
	sets := make([]int, len(stmt.value))
	for i, set := range stmt.value {
		col := columnIndex(schema, set.column)
		if col < 0 {
			return 0, fmt.Errorf("%w: table %s has no column %s", ErrBadStatement, schema.Name, set.column)
		}
		if isKeyColumn(schema, col) {
			return 0, fmt.Errorf("%w: %s is part of the primary key of %s and cannot be set",
				ErrBadStatement, set.column, schema.Name)
		}
		sets[i] = col
	}

	row, err := makePKey(schema, stmt.keys)
	if err != nil {
		return 0, err
	}
	found, err := db.Select(schema, row)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}

	for i, set := range stmt.value {
		row[sets[i]] = set.value
	}

	changed, err := db.Update(schema, row)
	if err != nil {
		return 0, err
	}
	return affected(changed), nil
}

// execDelete removes one row by its primary key.
func execDelete(db *table.DB, stmt *StmtDelete) (int, error) {
	schema, err := db.GetSchema(stmt.table)
	if err != nil {
		return 0, err
	}

	row, err := makePKey(schema, stmt.keys)
	if err != nil {
		return 0, err
	}

	deleted, err := db.Delete(schema, row)
	if err != nil {
		return 0, err
	}
	return affected(deleted), nil
}

// lookupColumns turns the column names of a statement into their positions in
// the schema, in the order they were written — which is the order the answer
// comes back in, so select b,a is not select a,b.
func lookupColumns(schema *table.Schema, names []string) ([]int, error) {
	indices := make([]int, len(names))
	for i, name := range names {
		col := columnIndex(schema, name)
		if col < 0 {
			return nil, fmt.Errorf("%w: table %s has no column %s", ErrBadStatement, schema.Name, name)
		}
		indices[i] = col
	}
	return indices, nil
}

// makePKey turns a WHERE clause into a row with its key cells filled and the
// rest left empty, which is what the table layer wants for a lookup.
//
// The clause has to be the whole primary key and nothing else. A key that is
// only half given does not name one row but a range of them, and a WHERE on a
// column outside the key is a filter — both need a way to walk rows that does
// not exist yet, so both are refused here rather than answered wrongly.
//
// Only the names are checked. Whether an int64 was written where the column
// holds bytes is for the table layer, which is where the types live.
func makePKey(schema *table.Schema, keys []NamedCell) (table.Row, error) {
	for _, key := range keys {
		col := columnIndex(schema, key.column)
		if col < 0 {
			return nil, fmt.Errorf("%w: table %s has no column %s", ErrBadStatement, schema.Name, key.column)
		}
		if !isKeyColumn(schema, col) {
			return nil, fmt.Errorf("%w: %s is not part of the primary key of %s, and only whole keys can be looked up",
				ErrBadStatement, key.column, schema.Name)
		}
	}
	if len(keys) != len(schema.PK) {
		return nil, fmt.Errorf("%w: the primary key of %s is %d columns, the where clause gives %d",
			ErrBadStatement, schema.Name, len(schema.PK), len(keys))
	}

	// Driven by the schema rather than by the clause, so every key column has to
	// be named — which is also what catches the same column named twice, since
	// the count above already matched.
	row := make(table.Row, len(schema.Cols))
	for _, col := range schema.PK {
		value, named := findKey(keys, schema.Cols[col])
		if !named {
			return nil, fmt.Errorf("%w: the where clause does not give key column %s of %s",
				ErrBadStatement, schema.Cols[col], schema.Name)
		}
		row[col] = value
	}
	return row, nil
}

// subsetRow picks the columns at indices out of row, in that order.
func subsetRow(row table.Row, indices []int) table.Row {
	out := make(table.Row, len(indices))
	for i, col := range indices {
		out[i] = row[col]
	}
	return out
}

// columnIndex returns the position of the named column, or -1.
func columnIndex(schema *table.Schema, name string) int {
	for i, col := range schema.Cols {
		if col == name {
			return i
		}
	}
	return -1
}

// isKeyColumn reports whether column col is part of the primary key.
func isKeyColumn(schema *table.Schema, col int) bool {
	for _, key := range schema.PK {
		if key == col {
			return true
		}
	}
	return false
}

// findKey returns the value written against the named column in a WHERE clause.
func findKey(keys []NamedCell, name string) (table.Cell, bool) {
	for _, key := range keys {
		if key.column == name {
			return key.value, true
		}
	}
	return table.Cell{}, false
}

// affected turns the "did anything change" the table layer reports into the row
// count a statement result carries. One row is all that can change today.
func affected(changed bool) int {
	if changed {
		return 1
	}
	return 0
}
