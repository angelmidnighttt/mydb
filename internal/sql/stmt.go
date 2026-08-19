package sql

import "github.com/angelmidnighttt/mydb/internal/table"

// StmtSelect is a parsed SELECT. Only one shape of statement is supported so
// far — one row, found by its whole primary key:
//
//	select a, b from t where c = 1 and d = 'e';
//
// which is why keys is a list of equalities rather than a condition of any kind,
// and why there is nowhere to put an ORDER BY, a LIMIT or a join. Each of those
// waits on something below this layer: they all need to walk rows in order, and
// the store under the table layer is a map.
type StmtSelect struct {
	table string
	cols  []string
	keys  []NamedCell
}

// NamedCell is one side of c = 1: a value together with the column it was
// written against.
//
// The column is a name, not a position. Nothing in this package knows what
// tables exist, so it cannot say whether column c is there, whether it is part
// of the primary key, or whether an int64 is what it holds. A parser reports
// what the text says; matching that against a schema is for whoever has one.
type NamedCell struct {
	column string
	value  table.Cell
}
