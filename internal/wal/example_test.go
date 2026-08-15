package wal_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/angelmidnighttt/mydb/internal/wal"
)

func Example() {
	ent := wal.Entry{Key: []byte("a"), Val: []byte("bb")}

	data := ent.Encode()
	fmt.Println(data)

	var decoded wal.Entry
	if err := decoded.Decode(bytes.NewReader(data)); err != nil {
		fmt.Println("decode:", err)
		return
	}
	fmt.Printf("%s = %s\n", decoded.Key, decoded.Val)

	// Output:
	// [1 0 0 0 2 0 0 0 97 98 98]
	// a = bb
}

// Entries sit back to back in the log, so replaying it is a loop that decodes
// until io.EOF. Any other error means the log is damaged.
func Example_replay() {
	var log bytes.Buffer
	for _, ent := range []wal.Entry{
		{Key: []byte("name"), Val: []byte("mydb")},
		{Key: []byte("lang"), Val: []byte("go")},
	} {
		log.Write(ent.Encode())
	}

	for {
		var ent wal.Entry
		err := ent.Decode(&log)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fmt.Println("corrupt log:", err)
			break
		}
		fmt.Printf("%s = %s\n", ent.Key, ent.Val)
	}

	// Output:
	// name = mydb
	// lang = go
}
