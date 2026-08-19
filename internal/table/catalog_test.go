package table

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func mustCreate(t *testing.T, db *DB, schema *Schema) {
	t.Helper()

	if err := db.CreateTable(schema); err != nil {
		t.Fatalf("CreateTable(%s) error = %v", schema.Name, err)
	}
}

func TestCreateTableThenGetSchema(t *testing.T) {
	db := openDB(t)
	mustCreate(t, db, testSchema())

	got, err := db.GetSchema("t")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}

	if got.Name != "t" {
		t.Errorf("Name = %q, want %q", got.Name, "t")
	}
	if cols := strings.Join(got.Cols, ","); cols != "a,b" {
		t.Errorf("Cols = %q, want %q", cols, "a,b")
	}
	if len(got.Types) != 2 || got.Types[0] != TypeI64 || got.Types[1] != TypeI64 {
		t.Errorf("Types = %v, want two int64", got.Types)
	}
	if len(got.PK) != 1 || got.PK[0] != 1 {
		t.Errorf("PK = %v, want [1]", got.PK)
	}
}

func TestGetSchemaOfAnUnknownTable(t *testing.T) {
	db := openDB(t)

	if _, err := db.GetSchema("nope"); !errors.Is(err, ErrNoTable) {
		t.Fatalf("GetSchema() error = %v, want ErrNoTable", err)
	}
}

// Creating a table that is already there must not overwrite its definition:
// every row already written under the old one would then be read back under the
// new one, which does not fail, it just reads nonsense.
func TestCreateTableRefusesADuplicate(t *testing.T) {
	db := openDB(t)
	mustCreate(t, db, testSchema())

	changed := testSchema()
	changed.Types = []CellType{TypeStr, TypeI64}

	if err := db.CreateTable(changed); !errors.Is(err, ErrTableExists) {
		t.Fatalf("CreateTable() error = %v, want ErrTableExists", err)
	}

	got, err := db.GetSchema("t")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if got.Types[0] != TypeI64 {
		t.Error("the refused CreateTable changed the definition anyway")
	}
}

func TestCreateTableRefusesABadSchema(t *testing.T) {
	db := openDB(t)

	broken := testSchema()
	broken.PK = []int{7}

	if err := db.CreateTable(broken); !errors.Is(err, ErrBadSchema) {
		t.Fatalf("CreateTable() error = %v, want ErrBadSchema", err)
	}
	if _, err := db.GetSchema("t"); !errors.Is(err, ErrNoTable) {
		t.Error("a schema that does not describe a table was stored anyway")
	}
}

// The catalog is written through the same log as the rows, so it comes back on
// its own — and the cache that made the first read fast is gone, which means
// this reads the definition out of the store.
func TestCatalogSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")

	db := &DB{}
	db.KV.Path = path
	if err := db.Open(); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	mustCreate(t, db, testSchema())
	mustInsert(t, db, testSchema(), testRow(7, 42))
	db.Close()

	reopened := &DB{}
	reopened.KV.Path = path
	if err := reopened.Open(); err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer reopened.Close()

	schema, err := reopened.GetSchema("t")
	if err != nil {
		t.Fatalf("GetSchema() after restart error = %v", err)
	}

	// And the definition that came back has to still address the rows written
	// under the original.
	row := testRow(0, 42)
	if ok, err := reopened.Select(schema, row); err != nil || !ok {
		t.Fatalf("Select() after restart = %v, %v; want true, nil", ok, err)
	}
	if row[0].I64 != 7 {
		t.Errorf("row[0] = %d, want 7", row[0].I64)
	}
}

// The second read comes out of the cache, which is the same schema and not a
// second copy of it.
func TestGetSchemaCaches(t *testing.T) {
	db := openDB(t)
	mustCreate(t, db, testSchema())

	first, err := db.GetSchema("t")
	if err != nil {
		t.Fatalf("first GetSchema() error = %v", err)
	}
	second, err := db.GetSchema("t")
	if err != nil {
		t.Fatalf("second GetSchema() error = %v", err)
	}
	if first != second {
		t.Error("the second GetSchema() decoded the definition again")
	}
}

// Nothing checks a definition between the store and here, so what comes back is
// checked as if it were new. Otherwise a damaged one is found later, by a panic,
// while indexing Cols with a position out of PK.
func TestGetSchemaRejectsADamagedDefinition(t *testing.T) {
	tests := []struct {
		name string
		val  string
	}{
		{"not json at all", "{"},
		{"json of the wrong shape", `{"Name": 7}`},
		{"a key column out of range", `{"Name":"t","Cols":["a"],"Types":[1],"PK":[9]}`},
		{"a column type that does not exist", `{"Name":"t","Cols":["a"],"Types":[9],"PK":[0]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openDB(t)
			if _, err := db.KV.Set(schemaKey("t"), []byte(tt.val)); err != nil {
				t.Fatalf("Set() error = %v", err)
			}

			if _, err := db.GetSchema("t"); err == nil {
				t.Fatal("GetSchema() = nil error, want the damage reported")
			}
		})
	}
}

// The catalog and the rows share one keyspace. A row of the table has to stay
// out of the key its definition is under, and the other way around.
func TestCatalogKeepsOutOfTheRows(t *testing.T) {
	db := openDB(t)
	schema := testSchema()
	mustCreate(t, db, schema)
	mustInsert(t, db, schema, testRow(7, 42))

	if db.KV.Len() != 2 {
		t.Fatalf("Len() = %d, want 2 — one definition and one row", db.KV.Len())
	}
	if _, err := db.GetSchema("t"); err != nil {
		t.Errorf("GetSchema() error = %v — the row landed on the definition", err)
	}
}
