package sql

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/angelmidnighttt/mydb/internal/table"
)

// showCell renders a cell so a whole statement can be compared as one string,
// which says more in a failure than a struct dump does.
func showCell(cell table.Cell) string {
	switch cell.Type {
	case table.TypeI64:
		return fmt.Sprintf("i64:%d", cell.I64)
	case table.TypeStr:
		return fmt.Sprintf("str:%s", cell.Str)
	default:
		return fmt.Sprintf("bad:%v", cell.Type)
	}
}

func showKeys(keys []NamedCell) string {
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = key.column + "=" + showCell(key.value)
	}
	return strings.Join(parts, " and ")
}

func showValues(values []table.Cell) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = showCell(value)
	}
	return strings.Join(parts, ", ")
}

// showCols renders a column list. The type comes out as CellType names it, so a
// string column reads back as bytes — the name the storage layer uses for it.
func showCols(cols []Column) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = col.name + " " + col.typ.String()
	}
	return strings.Join(parts, ", ")
}

// parseOne parses one statement, failing the test if it will not parse.
func parseOne(t *testing.T, buf string) any {
	t.Helper()

	p := NewParser(buf)
	stmt, err := p.parseStmt()
	if err != nil {
		t.Fatalf("parseStmt(%q) error = %v", buf, err)
	}
	return stmt
}

// parseOneErr parses one statement, failing the test if it parses.
func parseOneErr(t *testing.T, buf string) error {
	t.Helper()

	p := NewParser(buf)
	stmt, err := p.parseStmt()
	if !errors.Is(err, ErrSyntax) {
		t.Fatalf("parseStmt(%q) = %#v, %v; want ErrSyntax", buf, stmt, err)
	}
	return err
}

// The first word or two is all it takes to tell the statements apart.
func TestParseStmtDispatch(t *testing.T) {
	tests := []struct {
		buf  string
		want string
	}{
		{"select a from t where b=1", "*sql.StmtSelect"},
		{"create table t (a int64, primary key (a))", "*sql.StmtCreateTable"},
		{"insert into t values (1)", "*sql.StmtInsert"},
		{"update t set a=1 where b=2", "*sql.StmtUpdate"},
		{"delete from t where a=1", "*sql.StmtDelete"},

		// The names of statements are keywords like any other: case does not
		// matter, and the words may be spaced out any way.
		{"INSERT   INTO t VALUES (1)", "*sql.StmtInsert"},
		{"\n delete\nfrom t where a=1", "*sql.StmtDelete"},
	}

	for _, tt := range tests {
		t.Run(tt.buf, func(t *testing.T) {
			if got := fmt.Sprintf("%T", parseOne(t, tt.buf)); got != tt.want {
				t.Errorf("parseStmt(%q) = %s, want %s", tt.buf, got, tt.want)
			}
		})
	}
}

func TestParseStmtRejectsUnknownStatements(t *testing.T) {
	for _, buf := range []string{
		"", "   ", "a from t", "selecting a from t where b=1",
		"create index i on t (a)", // create, but not create table
		"insert t values (1)",     // insert, but not insert into
		"delete t where a=1",      // delete, but not delete from
		"drop table t",
	} {
		parseOneErr(t, buf)
	}
}

// A statement name that is two words is matched all or nothing. create index is
// not a create table, and the cursor has to come back to the create so the next
// alternative sees the statement from its start.
func TestParseStmtLeavesUnmatchedNamesAlone(t *testing.T) {
	err := parseOneErr(t, "create index i on t (a)")

	if !strings.Contains(err.Error(), "at 0") {
		t.Errorf("error = %q, want it to point at 0 — the create was taken and not given back", err)
	}
}

func TestParseCreateTable(t *testing.T) {
	tests := []struct {
		name      string
		buf       string
		wantTable string
		wantCols  string
		wantPkey  string
	}{
		{
			name: "the statement from the step",
			buf: `create table t (
				a int64,
				b string,
				c string,
				primary key (b, c)
			)`,
			wantTable: "t",
			wantCols:  "a int64, b bytes, c bytes",
			wantPkey:  "b,c",
		},
		{
			name:      "one column, which is the key",
			buf:       "create table t (a int64, primary key (a))",
			wantTable: "t",
			wantCols:  "a int64",
			wantPkey:  "a",
		},
		{
			// SQL lets the key clause sit anywhere in the list, and reading the
			// brackets as one list of items rather than two sections is what
			// makes that fall out for free.
			name:      "the key written first",
			buf:       "create table t (primary key (b), a int64, b string)",
			wantTable: "t",
			wantCols:  "a int64, b bytes",
			wantPkey:  "b",
		},
		{
			name:      "no spaces anywhere they are not needed",
			buf:       "create table t(a int64,b string,primary key(b))",
			wantTable: "t",
			wantCols:  "a int64, b bytes",
			wantPkey:  "b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, ok := parseOne(t, tt.buf).(*StmtCreateTable)
			if !ok {
				t.Fatalf("parseStmt(%q) is not a create table", tt.buf)
			}

			if stmt.table != tt.wantTable {
				t.Errorf("table = %q, want %q", stmt.table, tt.wantTable)
			}
			if cols := showCols(stmt.cols); cols != tt.wantCols {
				t.Errorf("cols = %q, want %q", cols, tt.wantCols)
			}
			if pkey := strings.Join(stmt.pkey, ","); pkey != tt.wantPkey {
				t.Errorf("pkey = %q, want %q", pkey, tt.wantPkey)
			}
		})
	}
}

func TestParseCreateTableErrors(t *testing.T) {
	tests := []struct {
		name string
		buf  string
	}{
		{"no table name", "create table (a int64, primary key (a))"},
		{"no brackets", "create table t"},
		{"nothing in the brackets", "create table t ()"},
		{"no primary key", "create table t (a int64)"},
		{"two primary keys", "create table t (a int64, primary key (a), primary key (a))"},
		{"no column type", "create table t (a, primary key (a))"},
		{"a type that does not exist", "create table t (a int32, primary key (a))"},
		{"a missing comma", "create table t (a int64 b string, primary key (a))"},
		{"never closed", "create table t (a int64, primary key (a)"},
		{"an empty key", "create table t (a int64, primary key ())"},
		{"a key with no brackets", "create table t (a int64, primary key a)"},
		{"a number for a column name", "create table t (1 int64, primary key (a))"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parseOneErr(t, tt.buf)
		})
	}
}

func TestParseInsert(t *testing.T) {
	tests := []struct {
		name       string
		buf        string
		wantTable  string
		wantValues string
	}{
		{
			name:       "the statement from the step",
			buf:        "insert into t values (1, 'x', 'y');",
			wantTable:  "t",
			wantValues: "i64:1, str:x, str:y",
		},
		{
			name:       "one value",
			buf:        "insert into t values (-1)",
			wantTable:  "t",
			wantValues: "i64:-1",
		},
		{
			name:       "no spaces where none are needed",
			buf:        "insert into t values('x',1)",
			wantTable:  "t",
			wantValues: "str:x, i64:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, ok := parseOne(t, tt.buf).(*StmtInsert)
			if !ok {
				t.Fatalf("parseStmt(%q) is not an insert", tt.buf)
			}

			if stmt.table != tt.wantTable {
				t.Errorf("table = %q, want %q", stmt.table, tt.wantTable)
			}
			if values := showValues(stmt.value); values != tt.wantValues {
				t.Errorf("value = %q, want %q", values, tt.wantValues)
			}
		})
	}
}

func TestParseInsertErrors(t *testing.T) {
	tests := []struct {
		name string
		buf  string
	}{
		{"no table name", "insert into values (1)"},
		{"no values keyword", "insert into t (1)"},
		{"no brackets", "insert into t values 1"},
		{"nothing to insert", "insert into t values ()"},
		{"a missing comma", "insert into t values (1 2)"},
		{"never closed", "insert into t values (1"},
		{"a column name for a value", "insert into t values (a)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parseOneErr(t, tt.buf)
		})
	}
}

func TestParseUpdate(t *testing.T) {
	tests := []struct {
		name      string
		buf       string
		wantTable string
		wantValue string
		wantKeys  string
	}{
		{
			name:      "the statement from the step",
			buf:       "update t set a = 1 where b = 'x' and c = 'y';",
			wantTable: "t",
			wantValue: "a=i64:1",
			wantKeys:  "b=str:x and c=str:y",
		},
		{
			// The SET list and the WHERE list are the same shape with different
			// separators: a comma there, the word and here.
			name:      "several columns set at once",
			buf:       "update t set a=1, b='x' where c=2",
			wantTable: "t",
			wantValue: "a=i64:1 and b=str:x",
			wantKeys:  "c=i64:2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, ok := parseOne(t, tt.buf).(*StmtUpdate)
			if !ok {
				t.Fatalf("parseStmt(%q) is not an update", tt.buf)
			}

			if stmt.table != tt.wantTable {
				t.Errorf("table = %q, want %q", stmt.table, tt.wantTable)
			}
			if value := showKeys(stmt.value); value != tt.wantValue {
				t.Errorf("value = %q, want %q", value, tt.wantValue)
			}
			if keys := showKeys(stmt.keys); keys != tt.wantKeys {
				t.Errorf("keys = %q, want %q", keys, tt.wantKeys)
			}
		})
	}
}

func TestParseUpdateErrors(t *testing.T) {
	tests := []struct {
		name string
		buf  string
	}{
		{"no table name", "update set a=1 where b=2"},
		{"no set", "update t a=1 where b=2"},
		{"nothing to set", "update t set where b=2"},
		{"no where", "update t set a=1"},
		{"a trailing comma in the set list", "update t set a=1, where b=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parseOneErr(t, tt.buf)
		})
	}
}

func TestParseDelete(t *testing.T) {
	stmt, ok := parseOne(t, "delete from t where b = 'x' and c = 'y';").(*StmtDelete)
	if !ok {
		t.Fatal("parseStmt() is not a delete")
	}

	if stmt.table != "t" {
		t.Errorf("table = %q, want %q", stmt.table, "t")
	}
	if keys := showKeys(stmt.keys); keys != "b=str:x and c=str:y" {
		t.Errorf("keys = %q, want %q", keys, "b=str:x and c=str:y")
	}
}

func TestParseDeleteErrors(t *testing.T) {
	for _, buf := range []string{
		"delete from t",
		"delete from where a=1",
		"delete from t where",
		"delete from t where a",
	} {
		parseOneErr(t, buf)
	}
}
