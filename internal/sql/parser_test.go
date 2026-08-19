package sql

import (
	"testing"

	"github.com/angelmidnighttt/mydb/internal/table"
)

func TestTryName(t *testing.T) {
	tests := []struct {
		buf     string
		want    string
		wantPos int
	}{
		{" hi ", "hi", 3}, // the cursor stops after the name, not after the space
		{"hi", "hi", 2},
		{"_x9", "_x9", 3},
		{"\t\n a1_b ", "a1_b", 7},
		{"a-b", "a", 1}, // stops at the first byte that cannot continue a name
		{"a,b", "a", 1},
		{"a b", "a", 1},
		{"select", "select", 6}, // a keyword is a name too, as far as this knows
	}

	for _, tt := range tests {
		t.Run(tt.buf, func(t *testing.T) {
			p := NewParser(tt.buf)

			got, ok := p.tryName()
			if !ok {
				t.Fatalf("tryName(%q) = _, false; want %q", tt.buf, tt.want)
			}
			if got != tt.want {
				t.Errorf("tryName(%q) = %q, want %q", tt.buf, got, tt.want)
			}
			if p.pos != tt.wantPos {
				t.Errorf("pos = %d, want %d", p.pos, tt.wantPos)
			}
		})
	}
}

// A try that does not match must leave the cursor alone — including the spaces
// it skipped to look ahead. Otherwise the next alternative in the grammar starts
// from a position the failed one chose.
func TestTryNameFailureKeepsPos(t *testing.T) {
	for _, buf := range []string{"", "   ", "9x", " 9x", ",a", " ,a", "é", "-"} {
		p := NewParser(buf)

		if got, ok := p.tryName(); ok {
			t.Errorf("tryName(%q) = %q, true; want no name", buf, got)
		}
		if p.pos != 0 {
			t.Errorf("tryName(%q) left pos = %d, want 0", buf, p.pos)
		}
	}
}

func TestTryKeyword(t *testing.T) {
	tests := []struct {
		name    string
		buf     string
		kw      string
		want    bool
		wantPos int
	}{
		{"plain", "select a", "select", true, 6},
		{"leading space", "   select a", "select", true, 9},
		{"upper in the text", " SELECT a", "select", true, 7},
		{"mixed on both sides", "SeLeCt a", "SELECT", true, 6},
		{"end of text ends it", "select", "select", true, 6},
		{"comma ends it", "select,", "select", true, 6},
		{"paren ends it", "select(", "select", true, 6},
		{"equals ends it", "where c=1", "where", true, 5},

		{"prefix of a longer word", "selecting", "select", false, 0},
		{"underscore continues it", "select_", "select", false, 0},
		{"digit continues it", "select1", "select", false, 0},
		{"non-ascii continues it", "selectá", "select", false, 0},
		{"text shorter than kw", "sele", "select", false, 0},
		{"empty", "", "select", false, 0},
		{"a different word", "from t", "select", false, 0},
		{"only spaces", "   ", "select", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.buf)

			if got := p.tryKeyword(tt.kw); got != tt.want {
				t.Fatalf("tryKeyword(%q) on %q = %v, want %v", tt.kw, tt.buf, got, tt.want)
			}
			if p.pos != tt.wantPos {
				t.Errorf("pos = %d, want %d", p.pos, tt.wantPos)
			}
		})
	}
}

// One statement read token by token, by hand. Which reader is called where is
// the grammar's decision — this is what parseSelect does, with the decisions
// written out instead of made by the shape of the code.
func TestReadsTheTokensOfAStatement(t *testing.T) {
	p := NewParser("select a,b from t where c=1;")

	if !p.tryKeyword("select") {
		t.Fatal("tryKeyword(select) = false")
	}
	if got, ok := p.tryName(); !ok || got != "a" {
		t.Fatalf("tryName() = %q, %v; want \"a\", true", got, ok)
	}

	// The comma is next, and a word reader cannot take it: both tries fail and
	// the cursor stays put, which is how a caller knows a symbol is what is due.
	if got, ok := p.tryName(); ok {
		t.Fatalf("tryName() at a comma = %q, want no name", got)
	}
	if p.tryKeyword("from") {
		t.Fatal("tryKeyword(from) at a comma = true, want false")
	}
	if p.buf[p.pos] != ',' {
		t.Fatalf("cursor sits on %q, want a comma", p.buf[p.pos])
	}

	if !p.tryPunctuation(",") {
		t.Fatal("tryPunctuation(,) = false")
	}
	if got, ok := p.tryName(); !ok || got != "b" {
		t.Fatalf("tryName() = %q, %v; want \"b\", true", got, ok)
	}
	if !p.tryKeyword("from") {
		t.Fatal("tryKeyword(from) = false")
	}
	if got, ok := p.tryName(); !ok || got != "t" {
		t.Fatalf("tryName() = %q, %v; want \"t\", true", got, ok)
	}
	if !p.tryKeyword("where") {
		t.Fatal("tryKeyword(where) = false")
	}
	if got, ok := p.tryName(); !ok || got != "c" {
		t.Fatalf("tryName() = %q, %v; want \"c\", true", got, ok)
	}
	if !p.tryPunctuation("=") {
		t.Fatal("tryPunctuation(=) = false")
	}

	var value table.Cell
	if err := p.parseValue(&value); err != nil {
		t.Fatalf("parseValue() error = %v", err)
	}
	if value.Type != table.TypeI64 || value.I64 != 1 {
		t.Fatalf("parseValue() = %v %d, want int64 1", value.Type, value.I64)
	}

	// Every token of the statement is read; only the semicolon is left, and
	// nothing at this level claims it.
	if p.buf[p.pos:] != ";" {
		t.Fatalf("%q left over, want only the semicolon", p.buf[p.pos:])
	}
}

func TestCharClasses(t *testing.T) {
	// isSeparator is the one classifier with a rule that is not obvious: a byte
	// outside ASCII does not end a word.
	for _, ch := range []byte{' ', '\t', ',', ';', '=', '(', ')', '*', '-'} {
		if !isSeparator(ch) {
			t.Errorf("isSeparator(%q) = false, want true", ch)
		}
	}
	for _, ch := range []byte{'a', 'Z', '0', '_', 0x80, 0xC3, 0xFF} {
		if isSeparator(ch) {
			t.Errorf("isSeparator(%#x) = true, want false", ch)
		}
	}

	// The case-bit fold in isAlpha must not turn punctuation into a letter.
	// '@'|32 is '`' and '['|32 is '{', the two bytes that sit right outside the
	// lowercase range on either side.
	for _, ch := range []byte{'@', '[', '`', '{', '0', '9', ' '} {
		if isAlpha(ch) {
			t.Errorf("isAlpha(%q) = true, want false", ch)
		}
	}
	for _, ch := range []byte{'a', 'z', 'A', 'Z', 'q'} {
		if !isAlpha(ch) {
			t.Errorf("isAlpha(%q) = false, want true", ch)
		}
	}
}
