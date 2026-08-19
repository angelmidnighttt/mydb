package table_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/angelmidnighttt/mydb/internal/table"
)

func Example() {
	cell := table.Cell{Type: table.TypeI64, I64: 1}

	data := cell.Encode(nil)
	fmt.Println(data)

	// The type is not in the bytes — the schema says which column holds what,
	// so the reader sets it before decoding.
	decoded := table.Cell{Type: table.TypeI64}
	if _, err := decoded.Decode(data); err != nil {
		fmt.Println("decode:", err)
		return
	}
	fmt.Println(decoded.I64)

	// Output:
	// [1 0 0 0 0 0 0 0]
	// 1
}

// A string carries its length first, so the reader knows how far it runs.
func ExampleCell_string() {
	cell := table.Cell{Type: table.TypeStr, Str: []byte("bb")}
	fmt.Println(cell.Encode(nil))

	// Output:
	// [2 0 0 0 98 98]
}

// The cells of a row share one buffer: Encode appends to it, and Decode returns
// what is left so the next cell picks up where the last one stopped.
func ExampleCell_row() {
	row := []table.Cell{
		{Type: table.TypeI64, I64: 7},
		{Type: table.TypeStr, Str: []byte("alice")},
	}

	var data []byte
	for i := range row {
		data = row[i].Encode(data)
	}
	fmt.Println(data)

	rest := data
	for i := range row {
		cell := table.Cell{Type: row[i].Type}
		var err error
		if rest, err = cell.Decode(rest); err != nil {
			fmt.Println("decode:", err)
			return
		}
		fmt.Printf("%d: %v %q, %d bytes left\n", i, cell.I64, cell.Str, len(rest))
	}

	// Output:
	// [7 0 0 0 0 0 0 0 5 0 0 0 97 108 105 99 101]
	// 0: 7 "", 9 bytes left
	// 1: 0 "alice", 0 bytes left
}

// A table is a schema plus rows. The schema names the key columns, and every
// operation addresses a row by them.
func Example_crud() {
	dir, err := os.MkdirTemp("", "mydb-example")
	if err != nil {
		fmt.Println("tempdir:", err)
		return
	}
	defer os.RemoveAll(dir)

	// create table users (id int64, name bytes, primary key (id))
	schema := &table.Schema{
		Name:  "users",
		Cols:  []string{"id", "name"},
		Types: []table.CellType{table.TypeI64, table.TypeStr},
		PK:    []int{0},
	}

	db := &table.DB{}
	db.KV.Path = filepath.Join(dir, "example.log")
	if err := db.Open(); err != nil {
		fmt.Println("open:", err)
		return
	}
	defer db.Close()

	row := table.Row{
		{Type: table.TypeI64, I64: 7},
		{Type: table.TypeStr, Str: []byte("alice")},
	}
	inserted, _ := db.Insert(schema, row)
	fmt.Println("inserted:", inserted)

	// The key is taken now, so a second insert is refused.
	inserted, _ = db.Insert(schema, row)
	fmt.Println("inserted again:", inserted)

	// Select is handed a row with only the key filled in, and fills in the rest.
	found := table.Row{{Type: table.TypeI64, I64: 7}, {}}
	ok, _ := db.Select(schema, found)
	fmt.Printf("found: %v, name = %s\n", ok, found[1].Str)

	deleted, _ := db.Delete(schema, table.Row{{Type: table.TypeI64, I64: 7}, {}})
	fmt.Println("deleted:", deleted)

	// Output:
	// inserted: true
	// inserted again: false
	// found: true, name = alice
	// deleted: true
}
