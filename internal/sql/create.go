package sql

import "github.com/angelmidnighttt/mydb/internal/table"

// parseCreateTable reads a CREATE TABLE, from just after the two words that
// named it:
//
//	create table <name> ( <column> <type> [, ...], primary key ( <column>, ... ) )
//
// Inside the brackets is one list, and each item of it is either a column or the
// primary key. Reading it that way rather than "columns, then the key" costs
// nothing and lets the key be written anywhere in the list, which is what SQL
// allows. Exactly one key clause is required: a table with no primary key cannot
// be addressed at all, since the key is what a row is stored under.
//
// Whether the key names columns that were actually declared is not checked here.
// A parser reports what the text says; the names become positions when the
// statement is run, and a name that matches nothing is caught there.
func (p *Parser) parseCreateTable(out *StmtCreateTable) error {
	*out = StmtCreateTable{}

	name, ok := p.tryName()
	if !ok {
		return p.errorf(p.skipSpace(), "expect a table name after create table")
	}
	out.table = name

	if !p.tryPunctuation("(") {
		return p.errorf(p.skipSpace(), "expect ( after the table name")
	}

	for {
		if p.tryKeyword("primary", "key") {
			if len(out.pkey) > 0 {
				return p.errorf(p.skipSpace(), "table %s already has a primary key", out.table)
			}
			if err := p.parseNameList(&out.pkey); err != nil {
				return err
			}
		} else {
			var col Column
			if err := p.parseColumn(&col); err != nil {
				return err
			}
			out.cols = append(out.cols, col)
		}

		if !p.tryPunctuation(",") {
			break
		}
	}

	if !p.tryPunctuation(")") {
		return p.errorf(p.skipSpace(), "expect a comma or ) after the column list")
	}
	if len(out.cols) == 0 {
		return p.errorf(p.skipSpace(), "expect at least one column in table %s", out.table)
	}
	if len(out.pkey) == 0 {
		return p.errorf(p.skipSpace(), "expect a primary key in table %s", out.table)
	}
	return nil
}

// parseColumn reads one column definition: a name and a type.
func (p *Parser) parseColumn(out *Column) error {
	name, ok := p.tryName()
	if !ok {
		return p.errorf(p.skipSpace(), "expect a column name")
	}

	switch {
	case p.tryKeyword("int64"):
		out.typ = table.TypeI64
	case p.tryKeyword("string"):
		out.typ = table.TypeStr
	default:
		return p.errorf(p.skipSpace(), "expect int64 or string for column %s", name)
	}

	out.name = name
	return nil
}

// parseNameList reads a bracketed list of names: ( a, b, c ). It is the shape
// the primary key is written in, and the shape a column list of an INSERT would
// take when one exists.
func (p *Parser) parseNameList(out *[]string) error {
	if !p.tryPunctuation("(") {
		return p.errorf(p.skipSpace(), "expect ( before a list of column names")
	}

	for {
		name, ok := p.tryName()
		if !ok {
			return p.errorf(p.skipSpace(), "expect a column name")
		}
		*out = append(*out, name)

		if !p.tryPunctuation(",") {
			break
		}
	}

	if !p.tryPunctuation(")") {
		return p.errorf(p.skipSpace(), "expect a comma or ) after a list of column names")
	}
	return nil
}
