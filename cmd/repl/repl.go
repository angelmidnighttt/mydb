package repl

import (
	"bufio"
	"os"
	"fmt"
)

type REPL interface {
	StartREPL()
}

type replStruct struct {
	// TODO: add fields
}

func New() REPL {
	return &replStruct{}
}

func (r *replStruct) StartREPL() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(">> ")
		input, err := reader.ReadString('\n')
		if (err != nil) {
			fmt.Println(err)
			continue
		}
		input = input[:len(input)-2]
		fmt.Println(input)
		if input == "exit"{
			fmt.Print("Goodbye!")
			break
		}

		r.evaluate(input)
	}
}

func (r *replStruct) evaluate(input string) {
	fmt.Println(input)
}