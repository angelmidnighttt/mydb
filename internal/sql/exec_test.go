package sql

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angelmidnighttt/mydb/internal/table"
)

// openDB opens a database in a temporary directory and closes it when the test
// ends.
func openDB(t *testing.T) *table.DB {
	t.Helper()

	db := &table.DB{}
	db.KV.Path = filepath.Join(t.TempDir(), "test.log")
	if err := db.Open(); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// run parses one statement and runs it, failing the test if either step does.
// Going through the parser rather than building a struct is the point: this is
// the first test in the project that exercises the whole way down, from text to
// the log on disk.
func run(t *testing.T, db *table.DB, text string) Result {
	t.Helper()

	p := NewParser(text)
	stmt, err := p.parseStmt()
	if err != nil {
		t.Fatalf("parse(%q) error = %v", text, err)
	}

	res, err := Exec(db, stmt)
	if err != nil {
		t.Fatalf("exec(%q) error = %v", text, err)
	}
	return res
}

// runErr is run for statements that are expected to be refused. A parse error
// fails the test — the statement has to be good SQL for the check to be about
// execution at all.
func runErr(t *testing.T, db *table.DB, text string) error {
	t.Helper()

	p := NewParser(text)
	stmt, err := p.parseStmt()
	if err != nil {
		t.Fatalf("parse(%q) error = %v — this test is about execution", text, err)
	}

	res, err := Exec(db, stmt)
	if err == nil {
		t.Fatalf("exec(%q) = %+v, want an error", text, res)
	}
	return err
}

// showRows renders the answer to a SELECT as one string per row.
func showRows(rows []table.Row) string {
	out := make([]string, len(rows))
	for i, row := range rows {
		cells := make([]string, len(row))
		for j, cell := range row {
			cells[j] = showCell(cell)
		}
		out[i] = strings.Join(cells, ",")
	}
	return strings.Join(out, " | ")
}

// createT is the table used by most of these tests: a key of two columns that
// are not the leading ones, so nothing can pass by assuming the key comes first.
const createT = "create table t (a int64, b string, c string, primary key (b, c))"

func TestCreateInsertSelect(t *testing.T) {
	db := openDB(t)
	run(t, db, createT)

	if res := run(t, db, "insert into t values (1, 'x', 'y')"); res.Updated != 1 {
		t.Fatalf("insert updated %d rows, want 1", res.Updated)
	}

	res := run(t, db, "select a from t where b = 'x' and c = 'y'")
	if got := strings.Join(res.Header, ","); got != "a" {
		t.Errorf("header = %q, want %q", got, "a")
	}
	if got := showRows(res.Values); got != "i64:1" {
		t.Errorf("values = %q, want %q", got, "i64:1")
	}
}

// The answer comes back in the order the statement asked for, not in the order
// the table declares.
func TestSelectColumnOrder(t *testing.T) {
	db := openDB(t)
	run(t, db, createT)
	run(t, db, "insert into t values (1, 'x', 'y')")

	res := run(t, db, "select c,a,b from t where b='x' and c='y'")
	if got := strings.Join(res.Header, ","); got != "c,a,b" {
		t.Errorf("header = %q, want %q", got, "c,a,b")
	}
	if got := showRows(res.Values); got != "str:y,i64:1,str:x" {
		t.Errorf("values = %q, want %q", got, "str:y,i64:1,str:x")
	}
}

// A key column can be selected even though it is not in the stored value: the
// row handed to the table layer already carries it, because the statement is
// what put it there.
func TestSelectKeyColumns(t *testing.T) {
	db := openDB(t)
	run(t, db, createT)
	run(t, db, "insert into t values (1, 'x', 'y')")

	res := run(t, db, "select b from t where b='x' and c='y'")
	if got := showRows(res.Values); got != "str:x" {
		t.Errorf("values = %q, want %q", got, "str:x")
	}
}

// A SELECT that matches nothing is not an error. It found nothing, and says so.
func TestSelectMissingRow(t *testing.T) {
	db := openDB(t)
	run(t, db, createT)

	res := run(t, db, "select a from t where b='x' and c='y'")
	if len(res.Values) != 0 {
		t.Errorf("values = %v, want none", res.Values)
	}
}

func TestUpdate(t *testing.T) {
	db := openDB(t)
	run(t, db, createT)
	run(t, db, "insert into t values (1, 'x', 'y')")

	if res := run(t, db, "update t set a = 2 where b='x' and c='y'"); res.Updated != 1 {
		t.Fatalf("update updated %d rows, want 1", res.Updated)
	}
	res := run(t, db, "select a,b,c from t where b='x' and c='y'")
	if got := showRows(res.Values); got != "i64:2,str:x,str:y" {
		t.Errorf("values = %q, want %q", got, "i64:2,str:x,str:y")
	}

	// Nothing to update is not an error, it is nothing updated.
	if res := run(t, db, "update t set a = 3 where b='x' and c='z'"); res.Updated != 0 {
		t.Errorf("update of an absent row updated %d rows, want 0", res.Updated)
	}
}

// An UPDATE writes a whole row, so the columns it says nothing about have to
// survive it. They only can because the row is read first.
func TestUpdateKeepsTheColumnsItDoesNotName(t *testing.T) {
	db := openDB(t)
	run(t, db, "create table u (id int64, a int64, b string, primary key (id))")
	run(t, db, "insert into u values (1, 7, 'keep me')")

	run(t, db, "update u set a = 8 where id = 1")

	res := run(t, db, "select a,b from u where id = 1")
	if got := showRows(res.Values); got != "i64:8,str:keep me" {
		t.Errorf("values = %q, want %q", got, "i64:8,str:keep me")
	}
}

func TestDelete(t *testing.T) {
	db := openDB(t)
	run(t, db, createT)
	run(t, db, "insert into t values (1, 'x', 'y')")

	if res := run(t, db, "delete from t where b='x' and c='y'"); res.Updated != 1 {
		t.Fatalf("delete updated %d rows, want 1", res.Updated)
	}
	if res := run(t, db, "select a from t where b='x' and c='y'"); len(res.Values) != 0 {
		t.Error("the row is still there after delete")
	}
	if res := run(t, db, "delete from t where b='x' and c='y'"); res.Updated != 0 {
		t.Errorf("deleting it again updated %d rows, want 0", res.Updated)
	}
}

// insert is INSERT, not upsert: a key that is taken is refused, and the row that
// is there is left alone.
func TestInsertRefusesADuplicateKey(t *testing.T) {
	db := openDB(t)
	run(t, db, createT)
	run(t, db, "insert into t values (1, 'x', 'y')")

	if res := run(t, db, "insert into t values (2, 'x', 'y')"); res.Updated != 0 {
		t.Fatalf("the second insert updated %d rows, want 0", res.Updated)
	}
	res := run(t, db, "select a from t where b='x' and c='y'")
	if got := showRows(res.Values); got != "i64:1" {
		t.Errorf("values = %q, want %q — the refused insert overwrote the row", got, "i64:1")
	}
}

// The catalog goes through the same log as the rows, so a table is still a table
// after a restart — and the schema is read back out of the store rather than out
// of a cache that did not survive.
func TestTablesSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")

	db := &table.DB{}
	db.KV.Path = path
	if err := db.Open(); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	run(t, db, createT)
	run(t, db, "insert into t values (1, 'x', 'y')")
	db.Close()

	reopened := &table.DB{}
	reopened.KV.Path = path
	if err := reopened.Open(); err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer reopened.Close()

	res := run(t, reopened, "select a from t where b='x' and c='y'")
	if got := showRows(res.Values); got != "i64:1" {
		t.Errorf("values after restart = %q, want %q", got, "i64:1")
	}
}

func TestCreateTableRefusesTheSameTableTwice(t *testing.T) {
	db := openDB(t)
	run(t, db, createT)

	if err := runErr(t, db, createT); !errors.Is(err, table.ErrTableExists) {
		t.Fatalf("error = %v, want ErrTableExists", err)
	}
}

// Statements that read fine but do not fit the database. Each is refused before
// anything is written.
func TestStatementsThatDoNotFit(t *testing.T) {
	tests := []struct {
		name string
		text string
		want error
	}{
		{"a table that does not exist", "select a from u where b='x' and c='y'", table.ErrNoTable},
		{"insert into a table that does not exist", "insert into u values (1)", table.ErrNoTable},

		{"a column that does not exist", "select z from t where b='x' and c='y'", ErrBadStatement},
		{"a where on a column that does not exist", "select a from t where z='x' and c='y'", ErrBadStatement},
		{"half of the primary key", "select a from t where b='x'", ErrBadStatement},
		{"a where outside the primary key", "select a from t where a=1 and b='x'", ErrBadStatement},
		{"a key column named twice", "select a from t where b='x' and b='y'", ErrBadStatement},

		{"too few values", "insert into t values (1, 'x')", ErrBadStatement},
		{"too many values", "insert into t values (1, 'x', 'y', 'z')", ErrBadStatement},

		{"setting a key column", "update t set b='z' where b='x' and c='y'", ErrBadStatement},
		{"setting a column that does not exist", "update t set z=1 where b='x' and c='y'", ErrBadStatement},

		// The types belong to the schema, so the table layer is what turns these
		// away — the same check that guards a row written by any other caller.
		{"a string where an int64 goes", "insert into t values ('one', 'x', 'y')", table.ErrBadRow},
		{"an int64 where a string goes", "select a from t where b=1 and c='y'", table.ErrBadRow},
		{"a string set into an int64 column", "update t set a='two' where b='x' and c='y'", table.ErrBadRow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openDB(t)
			run(t, db, createT)
			run(t, db, "insert into t values (1, 'x', 'y')")

			if err := runErr(t, db, tt.text); !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

// A CREATE TABLE whose key names a column it never declared. The parser lets it
// through on purpose — both halves are right there in the statement, but names
// become positions here, and this is where the name turns out to match nothing.
func TestCreateTableKeyMustNameItsOwnColumns(t *testing.T) {
	db := openDB(t)

	err := runErr(t, db, "create table t (a int64, primary key (b))")
	if !errors.Is(err, ErrBadStatement) {
		t.Fatalf("error = %v, want ErrBadStatement", err)
	}
	if _, err := db.GetSchema("t"); !errors.Is(err, table.ErrNoTable) {
		t.Error("the table was created anyway")
	}
}

// Two columns of one name cannot both be addressed, so the schema layer refuses
// the table outright.
func TestCreateTableRefusesRepeatedColumns(t *testing.T) {
	db := openDB(t)

	err := runErr(t, db, "create table t (a int64, a string, primary key (a))")
	if !errors.Is(err, table.ErrBadSchema) {
		t.Fatalf("error = %v, want ErrBadSchema", err)
	}
}

func TestExecRejectsSomethingThatIsNotAStatement(t *testing.T) {
	db := openDB(t)

	if _, err := Exec(db, "select a from t"); !errors.Is(err, ErrBadStatement) {
		t.Fatalf("Exec(string) error = %v, want ErrBadStatement", err)
	}
	if _, err := Exec(db, nil); !errors.Is(err, ErrBadStatement) {
		t.Fatalf("Exec(nil) error = %v, want ErrBadStatement", err)
	}
}
