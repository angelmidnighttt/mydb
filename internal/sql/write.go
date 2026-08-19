package sql

import "github.com/angelmidnighttt/mydb/internal/table"

// parseInsert reads an INSERT, from just after the two words that named it:
//
//	insert into <table> values ( <value>, ... )
//
// The values are positional — no column list is written beside them — so how
// many there are and what order they come in is checked against the table when
// the statement is run, not here.
func (p *Parser) parseInsert(out *StmtInsert) error {
	*out = StmtInsert{}

	name, ok := p.tryName()
	if !ok {
		return p.errorf(p.skipSpace(), "expect a table name after insert into")
	}
	out.table = name

	if !p.tryKeyword("values") {
		return p.errorf(p.skipSpace(), "expect values after the table name")
	}
	if !p.tryPunctuation("(") {
		return p.errorf(p.skipSpace(), "expect ( after values")
	}

	for {
		var value table.Cell
		if err := p.parseValue(&value); err != nil {
			return err
		}
		out.value = append(out.value, value)

		if !p.tryPunctuation(",") {
			break
		}
	}

	if !p.tryPunctuation(")") {
		return p.errorf(p.skipSpace(), "expect a comma or ) after the value list")
	}
	return nil
}

// parseUpdate reads an UPDATE, from just after the keyword that named it:
//
//	update <table> set <column> = <value> [, ...] where <column> = <value> [and ...]
//
// Both halves are lists of the same thing, an equality, and both are read by the
// same parseEqual. Only the separator tells them apart — a comma after SET, the
// word AND after WHERE — and that is the whole difference between "write this"
// and "to the row that matches this".
func (p *Parser) parseUpdate(out *StmtUpdate) error {
	*out = StmtUpdate{}

	name, ok := p.tryName()
	if !ok {
		return p.errorf(p.skipSpace(), "expect a table name after update")
	}
	out.table = name

	if !p.tryKeyword("set") {
		return p.errorf(p.skipSpace(), "expect set after the table name")
	}

	for {
		var value NamedCell
		if err := p.parseEqual(&value); err != nil {
			return err
		}
		out.value = append(out.value, value)

		if !p.tryPunctuation(",") {
			break
		}
	}

	return p.parseWhere(&out.keys)
}

// parseDelete reads a DELETE, from just after the two words that named it:
//
//	delete from <table> where <column> = <value> [and ...]
func (p *Parser) parseDelete(out *StmtDelete) error {
	*out = StmtDelete{}

	name, ok := p.tryName()
	if !ok {
		return p.errorf(p.skipSpace(), "expect a table name after delete from")
	}
	out.table = name

	return p.parseWhere(&out.keys)
}
