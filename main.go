package main

import (
	"fmt"
	"os"
	"github.com/angelmidnighttt/mydb/cmd/repl"
)

func main() {
	cmd := repl.New()
	cmd.StartREPL()

	fmt.Println("Goodbye!")
	os.Exit(0)
}
