package main

import (
	"fmt"
	"os"

	"github.com/angelmidnighttt/mydb/internal/kv"
)

const logFile = "mydb.log"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mydb:", err)
		os.Exit(1)
	}
}

func run() error {
	db := &kv.KV{Path: logFile}
	if err := db.Open(); err != nil {
		return err
	}
	defer db.Close()

	fmt.Printf("opened %s with %d key(s): %v\n", logFile, db.Len(), db.Keys())

	updated, err := db.Set([]byte("hello"), []byte("world"))
	if err != nil {
		return err
	}
	fmt.Printf("set hello=world (updated=%v)\n", updated)

	if val, ok := db.Get([]byte("hello")); ok {
		fmt.Printf("hello = %s\n", val)
	}

	fmt.Println("run again — the key is replayed from the log instead of set fresh")
	return nil
}
