package sql

import "github.com/angelmidnighttt/mydb/internal/table"

// StmtCreateTable is a parsed CREATE TABLE.
//
//	create table t (a int64, b string, c string, primary key (b, c));
//
// pkey names the key columns rather than pointing at them, because a name is
// what the text gives. Turning those names into the positions Schema.PK wants is
// for whoever runs the statement — and it is where a key naming a column that
// was never declared is caught, since nothing here looks.
type StmtCreateTable struct {
	table string
	cols  []Column
	pkey  []string
}

// Column is one column of a CREATE TABLE: what it is called and what it holds.
type Column struct {
	name string
	typ  table.CellType
}

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

// StmtInsert is a parsed INSERT.
//
//	insert into t values (1, 'x', 'y');
//
// value is positional: one cell per column of the table, in the order the table
// was declared. There is no column list to write beside it, so a row cannot be
// inserted with some columns left out — which suits a table where every column
// is required anyway.
type StmtInsert struct {
	table string
	value []table.Cell
}

// StmtUpdate is a parsed UPDATE.
//
//	update t set a = 1 where b = 'x' and c = 'y';
//
// Both halves are lists of equalities, and they are the same shape written with
// different separators — commas after SET, AND after WHERE — but they mean
// opposite things: value is what to write, keys is which row to write it to.
type StmtUpdate struct {
	table string
	keys  []NamedCell
	value []NamedCell
}

// StmtDelete is a parsed DELETE.
//
//	delete from t where b = 'x' and c = 'y';
type StmtDelete struct {
	table string
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

// parseStmt reads one statement of any kind and returns it.
//
// The first word or two is all it takes to know which: select, create table,
// insert into, update, delete from. Nothing further ahead has to be read, and
// nothing has to be undone — the same one-token lookahead that lets parseValue
// tell a number from a string, at the scale of a whole statement.
//
// That is the point worth taking from this file. SQL, and most languages people
// write by hand, are shaped so that a parser this plain is enough; the machinery
// of compiler theory is for grammars that are not.
//
// The return is any, because Go has no way to say "one of these five types". The
// cost lands on the caller, who has to type-switch with nothing checking that
// every case is covered. An interface with an unexported marker method would at
// least keep other packages out of the set.
func (p *Parser) parseStmt() (any, error) {
	var (
		stmt any
		err  error
	)

	switch {
	case p.tryKeyword("select"):
		out := &StmtSelect{}
		err = p.parseSelect(out)
		stmt = out

	case p.tryKeyword("create", "table"):
		out := &StmtCreateTable{}
		err = p.parseCreateTable(out)
		stmt = out

	case p.tryKeyword("insert", "into"):
		out := &StmtInsert{}
		err = p.parseInsert(out)
		stmt = out

	case p.tryKeyword("update"):
		out := &StmtUpdate{}
		err = p.parseUpdate(out)
		stmt = out

	case p.tryKeyword("delete", "from"):
		out := &StmtDelete{}
		err = p.parseDelete(out)
		stmt = out

	default:
		return nil, p.errorf(p.skipSpace(),
			"expect a statement: select, create table, insert into, update or delete from")
	}

	if err != nil {
		return nil, err
	}
	return stmt, nil
}
