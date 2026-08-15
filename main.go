package main

import (
	"fmt"

	"github.com/angelmidnighttt/mydb/internal/store"
)

func main() {
	s := store.New()

	s.Set("hello", []byte("world"))
	if v, ok := s.Get("hello"); ok {
		fmt.Printf("hello = %s\n", v)
	}

	fmt.Printf("keys = %v, len = %d\n", s.Keys(), s.Len())
}
