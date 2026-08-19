package sql

// parseSelect reads a whole SELECT statement into out. The one shape it accepts:
//
//	select <column> [, <column>]... from <table> where <column> = <value> [and ...]
//
// Reading it is three lists in a row, and each list is read the same way: a part,
// then a separator, then another part, until the separator stops coming. That is
// the whole trick of a hand-written parser — a grammar that looks big is a small
// number of shapes, one calling the next.
//
// The leading keyword is what tells statements apart. Only SELECT exists today,
// so it does no work yet; it is the byte of lookahead that INSERT and DELETE
// will be told apart by, in the same way parseValue tells a string from a number.
//
// out is emptied first, so a struct parsed into twice does not keep the columns
// of the first go.
//
// On error the cursor is left where the parse stopped, not put back. Unlike a
// try, this has no caller waiting to attempt something else: once the statement
// began with select, a SELECT is the only thing it can be, and where the reading
// stopped is where the reader has to look.
func (p *Parser) parseSelect(out *StmtSelect) error {
	*out = StmtSelect{}

	if !p.tryKeyword("select") {
		return p.errorf(p.skipSpace(), "expect select")
	}

	// The column list ends at from, so the keyword is what closes the loop. It
	// has to be tried before the name: from is a name as far as tryName knows.
	colsAt := p.skipSpace()
	for !p.tryKeyword("from") {
		if len(out.cols) > 0 && !p.tryPunctuation(",") {
			return p.errorf(p.skipSpace(), "expect a comma or from after the column list")
		}
		name, ok := p.tryName()
		if !ok {
			return p.errorf(p.skipSpace(), "expect a column name")
		}
		out.cols = append(out.cols, name)
	}
	if len(out.cols) == 0 {
		return p.errorf(colsAt, "expect at least one column between select and from")
	}

	name, ok := p.tryName()
	if !ok {
		return p.errorf(p.skipSpace(), "expect a table name after from")
	}
	out.table = name

	return p.parseWhere(&out.keys)
}

// parseWhere reads the WHERE clause: equalities joined by AND, and nothing else.
//
//	where c = 1 and d = 'e'
//
// It is the column list of parseSelect with two words changed — and in place of
// the comma, an equality in place of a name — because both are the same shape:
// one or more parts with a separator between them.
//
// The clause is required. A SELECT without one asks for every row of the table,
// and there is no way to walk a table yet; refusing it here says so at the point
// where it was written, instead of at some later point where the reason is
// harder to see.
func (p *Parser) parseWhere(out *[]NamedCell) error {
	if !p.tryKeyword("where") {
		return p.errorf(p.skipSpace(), "expect where")
	}

	for {
		var key NamedCell
		if err := p.parseEqual(&key); err != nil {
			return err
		}
		*out = append(*out, key)

		if !p.tryKeyword("and") {
			return nil
		}
	}
}

// parseEqual reads one equality — a column name, an equals sign, a value — into
// out.
//
// This is where the three token readers meet: tryName, tryPunctuation and
// parseValue, in the order the text has them. Nothing here decides anything; the
// shape of the grammar decides, and the function just walks it.
func (p *Parser) parseEqual(out *NamedCell) error {
	column, ok := p.tryName()
	if !ok {
		return p.errorf(p.skipSpace(), "expect a column name")
	}
	if !p.tryPunctuation("=") {
		return p.errorf(p.skipSpace(), "expect = after column %s", column)
	}

	out.column = column
	return p.parseValue(&out.value)
}
