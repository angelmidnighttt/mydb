package store_test

import (
	"fmt"

	"github.com/angelmidnighttt/mydb/internal/store"
)

func Example() {
	s := store.New()

	s.Set("hello", []byte("world"))

	if v, ok := s.Get("hello"); ok {
		fmt.Printf("hello = %s\n", v)
	}

	if _, ok := s.Get("missing"); !ok {
		fmt.Println("missing is not set")
	}

	fmt.Println("deleted:", s.Delete("hello"))
	fmt.Println("len:", s.Len())

	// Output:
	// hello = world
	// missing is not set
	// deleted: true
	// len: 0
}

// Values are copied on the way in and on the way out, so neither the caller nor
// the store can mutate the other's bytes by accident.
func ExampleStore_Set_copiesValue() {
	s := store.New()

	value := []byte("original")
	s.Set("k", value)
	value[0] = 'X' // does not affect what is stored

	stored, _ := s.Get("k")
	fmt.Printf("%s\n", stored)

	// Output:
	// original
}
