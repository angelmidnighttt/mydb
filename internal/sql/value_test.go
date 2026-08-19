package sql

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/angelmidnighttt/mydb/internal/table"
)

func TestParseInt(t *testing.T) {
	tests := []struct {
		buf     string
		want    int64
		wantPos int
	}{
		{"1", 1, 1},
		{"0", 0, 1},
		{"-1", -1, 2},
		{"+1", 1, 2},
		{"007", 7, 3},
		{" 42 ", 42, 3}, // the cursor stops after the digits, not after the space
		{"\t-7", -7, 3},
		{"1;", 1, 1},
		{"1,2", 1, 1},
		{"1)", 1, 1},
		{"9223372036854775807", 1<<63 - 1, 19},
		{"-9223372036854775808", -1 << 63, 20},
	}

	for _, tt := range tests {
		t.Run(tt.buf, func(t *testing.T) {
			p := NewParser(tt.buf)

			var got table.Cell
			if err := p.parseInt(&got); err != nil {
				t.Fatalf("parseInt(%q) error = %v", tt.buf, err)
			}
			if got.Type != table.TypeI64 || got.I64 != tt.want {
				t.Errorf("parseInt(%q) = %v %d, want int64 %d", tt.buf, got.Type, got.I64, tt.want)
			}
			if p.pos != tt.wantPos {
				t.Errorf("pos = %d, want %d", p.pos, tt.wantPos)
			}
		})
	}
}

func TestParseIntErrors(t *testing.T) {
	tests := []struct {
		name string
		buf  string
	}{
		{"empty", ""},
		{"only spaces", "   "},
		{"sign with no digits", "-"},
		{"space between sign and digits", "- 1"},
		{"not a number", "abc"},
		{"runs into a name", "1a"},
		{"runs into an underscore", "1_"},
		{"runs into a non-ascii byte", "1á"},
		{"a decimal point", "1.5"},
		{"above the int64 range", "9223372036854775808"},
		{"below the int64 range", "-9223372036854775809"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.buf)

			var got table.Cell
			err := p.parseInt(&got)
			if !errors.Is(err, ErrSyntax) {
				t.Fatalf("parseInt(%q) error = %v, want ErrSyntax", tt.buf, err)
			}
			if p.pos != 0 {
				t.Errorf("parseInt(%q) left pos = %d, want 0", tt.buf, p.pos)
			}
		})
	}
}

func TestParseString(t *testing.T) {
	tests := []struct {
		buf     string
		want    string
		wantPos int
	}{
		{`"abc"`, "abc", 5},
		{`'abc'`, "abc", 5},
		{`""`, "", 2},
		{`''`, "", 2},
		{` "x" `, "x", 4},
		{`"abc" and more`, "abc", 5},

		// The quote that opened the string is the one that closes it, so the
		// other kind is ordinary text inside.
		{`'say "hi"'`, `say "hi"`, 10},
		{`"say 'hi'"`, `say 'hi'`, 10},

		// Both spellings of the example from the step.
		{`"isn\'t"`, "isn't", 8},
		{`'isn\'t'`, "isn't", 8},

		{`"a\"b"`, `a"b`, 6},
		{`'a\'b'`, "a'b", 6},
		{`"a\\b"`, `a\b`, 6},
		{`"\\"`, `\`, 4},
	}

	for _, tt := range tests {
		t.Run(tt.buf, func(t *testing.T) {
			p := NewParser(tt.buf)

			var got table.Cell
			if err := p.parseString(&got); err != nil {
				t.Fatalf("parseString(%q) error = %v", tt.buf, err)
			}
			if got.Type != table.TypeStr || string(got.Str) != tt.want {
				t.Errorf("parseString(%q) = %v %q, want bytes %q", tt.buf, got.Type, got.Str, tt.want)
			}
			if p.pos != tt.wantPos {
				t.Errorf("pos = %d, want %d", p.pos, tt.wantPos)
			}
		})
	}
}

// An empty string is empty, not absent — the same promise Cell.Decode makes.
func TestParseStringEmptyIsNotNil(t *testing.T) {
	p := NewParser(`""`)

	var got table.Cell
	if err := p.parseString(&got); err != nil {
		t.Fatalf("parseString() error = %v", err)
	}
	if got.Str == nil {
		t.Fatal("an empty string decoded to nil, want an empty slice")
	}
}

func TestParseStringErrors(t *testing.T) {
	tests := []struct {
		name string
		buf  string
	}{
		{"empty", ""},
		{"not a string", "abc"},
		{"a number", "1"},
		{"never closed", `"abc`},
		{"closed by the other quote", `'abc"`},
		{"ends inside an escape", `"a\`},
		{"escaped closing quote", `"abc\"`},
		{"unknown escape", `"a\n"`},
		{"escaped letter", `"a\z"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.buf)

			var got table.Cell
			err := p.parseString(&got)
			if !errors.Is(err, ErrSyntax) {
				t.Fatalf("parseString(%q) error = %v, want ErrSyntax", tt.buf, err)
			}
			if p.pos != 0 {
				t.Errorf("parseString(%q) left pos = %d, want 0", tt.buf, p.pos)
			}
		})
	}
}

// One byte decides which kind of value follows.
func TestParseValueDispatch(t *testing.T) {
	tests := []struct {
		buf      string
		wantType table.CellType
	}{
		{"1", table.TypeI64},
		{"-2", table.TypeI64},
		{"+3", table.TypeI64},
		{"  4", table.TypeI64},
		{`"x"`, table.TypeStr},
		{`'x'`, table.TypeStr},
		{`  "x"`, table.TypeStr},
	}

	for _, tt := range tests {
		t.Run(tt.buf, func(t *testing.T) {
			p := NewParser(tt.buf)

			var got table.Cell
			if err := p.parseValue(&got); err != nil {
				t.Fatalf("parseValue(%q) error = %v", tt.buf, err)
			}
			if got.Type != tt.wantType {
				t.Errorf("parseValue(%q) = %v, want %v", tt.buf, got.Type, tt.wantType)
			}
		})
	}
}

func TestParseValueErrors(t *testing.T) {
	for _, buf := range []string{"", "   ", "abc", "_x", ".5", "*", ";"} {
		p := NewParser(buf)

		var got table.Cell
		err := p.parseValue(&got)
		if !errors.Is(err, ErrSyntax) {
			t.Errorf("parseValue(%q) error = %v, want ErrSyntax", buf, err)
		}
		if p.pos != 0 {
			t.Errorf("parseValue(%q) left pos = %d, want 0", buf, p.pos)
		}
	}
}

// A cell carries both fields, and only one of them means anything. Handing the
// same cell in twice must not leave the first value showing through the second.
func TestParseValueClearsTheUnusedField(t *testing.T) {
	cell := table.Cell{Type: table.TypeStr, Str: []byte("old")}
	p := NewParser("7")
	if err := p.parseValue(&cell); err != nil {
		t.Fatalf("parseValue() error = %v", err)
	}
	if cell.Str != nil {
		t.Errorf("Str = %q after parsing a number, want nil", cell.Str)
	}

	cell = table.Cell{Type: table.TypeI64, I64: 9}
	p = NewParser(`"new"`)
	if err := p.parseValue(&cell); err != nil {
		t.Fatalf("parseValue() error = %v", err)
	}
	if cell.I64 != 0 {
		t.Errorf("I64 = %d after parsing a string, want 0", cell.I64)
	}
	if !bytes.Equal(cell.Str, []byte("new")) {
		t.Errorf("Str = %q, want \"new\"", cell.Str)
	}
}

// The offset in the message is what makes the error usable, so it has to point
// at the byte that is wrong — not at the start of the statement, and not at the
// end of it.
func TestErrorPointsAtTheTrouble(t *testing.T) {
	tests := []struct {
		name  string
		buf   string
		parse func(*Parser, *table.Cell) error
		want  string
	}{
		{"the byte a number runs into", "  1a", (*Parser).parseInt, "at 3"},
		{"the quote never closed", `  "abc`, (*Parser).parseString, "at 2"},
		{"the backslash of a bad escape", `"ab\n"`, (*Parser).parseString, "at 3"},
		{"where a value was expected", "  *", (*Parser).parseValue, "at 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.buf)

			var cell table.Cell
			err := tt.parse(&p, &cell)
			if err == nil {
				t.Fatalf("parse(%q) = nil, want an error", tt.buf)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to say %q", err, tt.want)
			}
		})
	}
}
