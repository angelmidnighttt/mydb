package store_test

import (
	"fmt"

	"github.com/angelmidnighttt/mydb/internal/store"
)

func Example() {
	s := store.New()

	s.Set([]byte("hello"), []byte("world"))

	if v, ok := s.Get([]byte("hello")); ok {
		fmt.Printf("hello = %s\n", v)
	}

	if _, ok := s.Get([]byte("missing")); !ok {
		fmt.Println("missing is not set")
	}

	fmt.Println("deleted:", s.Delete([]byte("hello")))
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
	s.Set([]byte("k"), value)
	value[0] = 'X' // does not affect what is stored

	stored, _ := s.Get([]byte("k"))
	fmt.Printf("%s\n", stored)

	// Output:
	// original
}

// Keys are held in order, whatever order they were written in. This is the whole
// reason the map underneath became a sorted array: a map can say what is under a
// key, and nothing else.
func ExampleStore_Keys() {
	s := store.New()

	for _, key := range []string{"orders", "users", "items"} {
		s.Set([]byte(key), nil)
	}
	fmt.Println(s.Keys())

	// Output:
	// [items orders users]
}
