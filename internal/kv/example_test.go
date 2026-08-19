package kv_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/angelmidnighttt/mydb/internal/kv"
)

func Example() {
	dir, err := os.MkdirTemp("", "mydb-example")
	if err != nil {
		fmt.Println("tempdir:", err)
		return
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "example.log")

	db := &kv.KV{Path: path}
	if err := db.Open(); err != nil {
		fmt.Println("open:", err)
		return
	}

	updated, _ := db.Set([]byte("name"), []byte("mydb"))
	fmt.Println("updated:", updated)

	updated, _ = db.Set([]byte("name"), []byte("mydb v2"))
	fmt.Println("updated:", updated)

	db.Close()

	// Everything written above is in the log, so a fresh KV over the same file
	// comes back with the same state.
	reopened := &kv.KV{Path: path}
	if err := reopened.Open(); err != nil {
		fmt.Println("reopen:", err)
		return
	}
	defer reopened.Close()

	val, ok := reopened.Get([]byte("name"))
	fmt.Printf("name = %s (%v)\n", val, ok)

	deleted, _ := reopened.Del([]byte("name"))
	fmt.Println("deleted:", deleted, "len:", reopened.Len())

	// Output:
	// updated: false
	// updated: true
	// name = mydb v2 (true)
	// deleted: true len: 0
}

// SetEx is the same write with a condition attached: insert a new key only,
// overwrite an existing one only, or — as Set does — either way.
func ExampleKV_SetEx() {
	dir, err := os.MkdirTemp("", "mydb-example")
	if err != nil {
		fmt.Println("tempdir:", err)
		return
	}
	defer os.RemoveAll(dir)

	db := &kv.KV{Path: filepath.Join(dir, "modes.log")}
	if err := db.Open(); err != nil {
		fmt.Println("open:", err)
		return
	}
	defer db.Close()

	inserted, _ := db.SetEx([]byte("user:1"), []byte("alice"), kv.ModeInsert)
	fmt.Println("inserted:", inserted)

	// The key is taken now, so a second insert leaves it as it was.
	inserted, _ = db.SetEx([]byte("user:1"), []byte("bob"), kv.ModeInsert)
	val, _ := db.Get([]byte("user:1"))
	fmt.Printf("inserted: %v, user:1 = %s\n", inserted, val)

	// An update refuses to create the key it cannot find.
	updated, _ := db.SetEx([]byte("user:2"), []byte("carol"), kv.ModeUpdate)
	_, exists := db.Get([]byte("user:2"))
	fmt.Printf("updated: %v, user:2 exists: %v\n", updated, exists)

	// Output:
	// inserted: true
	// inserted: false, user:1 = alice
	// updated: false, user:2 exists: false
}
