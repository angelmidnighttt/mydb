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
