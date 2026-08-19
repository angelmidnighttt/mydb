package table

import (
	"errors"
	"path/filepath"
	"testing"
)

// openDB opens a database in a temporary directory and closes it when the test
// ends.
func openDB(t *testing.T) *DB {
	t.Helper()

	db := &DB{}
	db.KV.Path = filepath.Join(t.TempDir(), "test.log")
	if err := db.Open(); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustInsert(t *testing.T, db *DB, schema *Schema, row Row) bool {
	t.Helper()

	inserted, err := db.Insert(schema, row)
	if err != nil {
		t.Fatalf("Insert(%v) error = %v", row, err)
	}
	return inserted
}

// mustSelect reads the row with primary key b and returns column a with it.
func mustSelect(t *testing.T, db *DB, schema *Schema, b int64) (a int64, ok bool) {
	t.Helper()

	row := testRow(0, b)
	ok, err := db.Select(schema, row)
	if err != nil {
		t.Fatalf("Select(%d) error = %v", b, err)
	}
	return row[0].I64, ok
}

func TestInsertSelect(t *testing.T) {
	db, schema := openDB(t), testSchema()

	if inserted := mustInsert(t, db, schema, testRow(7, 42)); !inserted {
		t.Fatal("Insert(new row) = false, want inserted")
	}

	a, ok := mustSelect(t, db, schema, 42)
	if !ok || a != 7 {
		t.Fatalf("Select(42) = %d, %v; want 7, true", a, ok)
	}
}

// The row handed to Select is left alone when there is nothing to read into it.
func TestSelectMissingLeavesRowAlone(t *testing.T) {
	db, schema := openDB(t), testSchema()

	row := testRow(99, 42)
	ok, err := db.Select(schema, row)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if ok {
		t.Fatal("Select(absent) = true, want false")
	}
	if row[0].I64 != 99 {
		t.Fatalf("row[0] = %d, want 99 — a missing row wrote into the caller cells", row[0].I64)
	}
}

// The three write modes, told apart by what they do to a key that is taken and
// one that is not. wantVal is the value of column a after the call, or -1 when
// the row must not exist at all.
func TestWriteModes(t *testing.T) {
	tests := []struct {
		name    string
		write   func(*DB, *Schema, Row) (bool, error)
		exists  bool
		want    bool
		wantVal int64
	}{
		{"insert/free", (*DB).Insert, false, true, 7},
		{"insert/taken", (*DB).Insert, true, false, 1}, // refused, the old row stands
		{"update/free", (*DB).Update, false, false, -1},
		{"update/taken", (*DB).Update, true, true, 7},
		{"upsert/free", (*DB).Upsert, false, false, 7},
		{"upsert/taken", (*DB).Upsert, true, true, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, schema := openDB(t), testSchema()
			if tt.exists {
				mustInsert(t, db, schema, testRow(1, 42))
			}

			got, err := tt.write(db, schema, testRow(7, 42))
			if err != nil {
				t.Fatalf("write() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("write() = %v, want %v", got, tt.want)
			}

			a, ok := mustSelect(t, db, schema, 42)
			switch {
			case tt.wantVal < 0:
				if ok {
					t.Errorf("Select() found a row = %d, want none", a)
				}
			case !ok || a != tt.wantVal:
				t.Errorf("Select() = %d, %v; want %d, true", a, ok, tt.wantVal)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	db, schema := openDB(t), testSchema()
	mustInsert(t, db, schema, testRow(7, 42))

	// Only the key cell is read, so the rest of the row is free to be wrong.
	deleted, err := db.Delete(schema, testRow(999, 42))
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !deleted {
		t.Fatal("Delete(existing) = false, want true")
	}
	if _, ok := mustSelect(t, db, schema, 42); ok {
		t.Fatal("row still readable after Delete")
	}

	deleted, err = db.Delete(schema, testRow(0, 42))
	if err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if deleted {
		t.Fatal("Delete(absent) = true, want false")
	}
}

// Rows go through the same log as everything else, so they come back on their
// own after a restart.
func TestRowsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	schema := testSchema()

	db := &DB{}
	db.KV.Path = path
	if err := db.Open(); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	mustInsert(t, db, schema, testRow(7, 42))
	mustInsert(t, db, schema, testRow(8, 43))
	if _, err := db.Delete(schema, testRow(0, 43)); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	db.Close()

	reopened := &DB{}
	reopened.KV.Path = path
	if err := reopened.Open(); err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer reopened.Close()

	if a, ok := mustSelect(t, reopened, schema, 42); !ok || a != 7 {
		t.Errorf("Select(42) after restart = %d, %v; want 7, true", a, ok)
	}
	if _, ok := mustSelect(t, reopened, schema, 43); ok {
		t.Error("a deleted row came back after restart")
	}
}

// Two tables in one keyspace, same primary key value. Neither may see the row
// belonging to the other.
func TestTablesDoNotCollide(t *testing.T) {
	db := openDB(t)
	first, second := testSchema(), testSchema()
	second.Name = "u"

	mustInsert(t, db, first, testRow(7, 42))
	mustInsert(t, db, second, testRow(8, 42))

	if a, ok := mustSelect(t, db, first, 42); !ok || a != 7 {
		t.Errorf("Select(t, 42) = %d, %v; want 7, true", a, ok)
	}
	if a, ok := mustSelect(t, db, second, 42); !ok || a != 8 {
		t.Errorf("Select(u, 42) = %d, %v; want 8, true", a, ok)
	}
}

// A string key and a two-column key: the pieces are length-prefixed, so a key
// cannot be read as another key with the pieces split in a different place.
func TestCompositeAndStringKeys(t *testing.T) {
	schema := &Schema{
		Name:  "events",
		Cols:  []string{"user", "day", "note"},
		Types: []CellType{TypeStr, TypeI64, TypeStr},
		PK:    []int{0, 1},
	}
	row := func(user string, day int64, note string) Row {
		return Row{
			{Type: TypeStr, Str: []byte(user)},
			{Type: TypeI64, I64: day},
			{Type: TypeStr, Str: []byte(note)},
		}
	}

	db := openDB(t)
	mustInsert(t, db, schema, row("ab", 1, "first"))
	mustInsert(t, db, schema, row("a", 1, "second"))

	got := row("ab", 1, "")
	ok, err := db.Select(schema, got)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if !ok || string(got[2].Str) != "first" {
		t.Fatalf("Select(ab, 1) = %q, %v; want first, true", got[2].Str, ok)
	}
}

func TestRejectsRowsThatDoNotFit(t *testing.T) {
	schema := testSchema()

	tests := []struct {
		name string
		row  Row
	}{
		{"too short", Row{{Type: TypeI64, I64: 42}}},
		{"too long", append(testRow(7, 42), Cell{Type: TypeI64})},
		{"wrong type in a key column", Row{{Type: TypeI64}, {Type: TypeStr, Str: []byte("42")}}},
		{"wrong type outside the key", Row{{Type: TypeStr, Str: []byte("7")}, {Type: TypeI64, I64: 42}}},
		{"cell with no type", Row{{}, {Type: TypeI64, I64: 42}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openDB(t)
			if _, err := db.Insert(schema, tt.row); !errors.Is(err, ErrBadRow) {
				t.Errorf("Insert() error = %v, want ErrBadRow", err)
			}
		})
	}
}

// A row whose key cells are right is addressable even when the rest is not, so
// Select and Delete accept what Insert would turn away.
func TestKeyOnlyOperationsIgnoreTheOtherCells(t *testing.T) {
	db, schema := openDB(t), testSchema()
	mustInsert(t, db, schema, testRow(7, 42))

	row := Row{{Type: TypeStr, Str: []byte("nonsense")}, {Type: TypeI64, I64: 42}}
	if _, err := db.Select(schema, row); err != nil {
		t.Errorf("Select() error = %v, want the key cells to be enough", err)
	}
	if _, err := db.Delete(schema, row); err != nil {
		t.Errorf("Delete() error = %v, want the key cells to be enough", err)
	}
}

func TestRejectsBadSchemas(t *testing.T) {
	tests := []struct {
		name   string
		schema *Schema
	}{
		{"no name", &Schema{Cols: []string{"a"}, Types: []CellType{TypeI64}, PK: []int{0}}},
		{"no columns", &Schema{Name: "t", PK: []int{0}}},
		{"no primary key", &Schema{Name: "t", Cols: []string{"a"}, Types: []CellType{TypeI64}}},
		{"types do not match columns", &Schema{Name: "t", Cols: []string{"a", "b"}, Types: []CellType{TypeI64}, PK: []int{0}}},
		{"unknown column type", &Schema{Name: "t", Cols: []string{"a"}, Types: []CellType{0}, PK: []int{0}}},
		{"key column out of range", &Schema{Name: "t", Cols: []string{"a"}, Types: []CellType{TypeI64}, PK: []int{1}}},
		{"key column twice", &Schema{Name: "t", Cols: []string{"a"}, Types: []CellType{TypeI64}, PK: []int{0, 0}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openDB(t)
			// The schema is broken, so no row can fit it. The check has to fire
			// before anything indexes Cols or Types by a position out of PK.
			row := Row{{Type: TypeI64, I64: 1}, {Type: TypeI64, I64: 2}}

			if _, err := db.Insert(tt.schema, row); !errors.Is(err, ErrBadSchema) {
				t.Errorf("Insert() error = %v, want ErrBadSchema", err)
			}
			if _, err := db.Select(tt.schema, row); !errors.Is(err, ErrBadSchema) {
				t.Errorf("Select() error = %v, want ErrBadSchema", err)
			}
			if _, err := db.Delete(tt.schema, row); !errors.Is(err, ErrBadSchema) {
				t.Errorf("Delete() error = %v, want ErrBadSchema", err)
			}
		})
	}
}
