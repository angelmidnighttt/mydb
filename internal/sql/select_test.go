package sql

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/angelmidnighttt/mydb/internal/table"
)

// showCell renders a cell so a whole statement can be compared as one string,
// which says more in a failure than a struct dump does.
func showCell(cell table.Cell) string {
	switch cell.Type {
	case table.TypeI64:
		return fmt.Sprintf("i64:%d", cell.I64)
	case table.TypeStr:
		return fmt.Sprintf("str:%s", cell.Str)
	default:
		return fmt.Sprintf("bad:%v", cell.Type)
	}
}

func showKeys(keys []NamedCell) string {
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = key.column + "=" + showCell(key.value)
	}
	return strings.Join(parts, " and ")
}

func TestTryPunctuation(t *testing.T) {
	tests := []struct {
		buf     string
		punct   string
		want    bool
		wantPos int
	}{
		{",", ",", true, 1},
		{" , ", ",", true, 2},
		{"=1", "=", true, 1},
		{",,", ",", true, 1},  // no separator needed after it
		{"<=", "<=", true, 2}, // more than one byte works
		{"<=", "<", true, 1},  // and a prefix of it matches first
		{"=", ",", false, 0},
		{"", ",", false, 0},
		{"   ", ",", false, 0},
		{"a", ",", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.buf+" "+tt.punct, func(t *testing.T) {
			p := NewParser(tt.buf)

			if got := p.tryPunctuation(tt.punct); got != tt.want {
				t.Fatalf("tryPunctuation(%q) on %q = %v, want %v", tt.punct, tt.buf, got, tt.want)
			}
			if p.pos != tt.wantPos {
				t.Errorf("pos = %d, want %d", p.pos, tt.wantPos)
			}
		})
	}
}

func TestParseEqual(t *testing.T) {
	tests := []struct {
		buf  string
		want string
	}{
		{"c=1", "c=i64:1"},
		{" c = 1 ", "c=i64:1"},
		{"c=-1", "c=i64:-1"},
		{"d='e'", "d=str:e"},
		{`d = "e f"`, "d=str:e f"},
		{"_x9=0", "_x9=i64:0"},
	}

	for _, tt := range tests {
		t.Run(tt.buf, func(t *testing.T) {
			p := NewParser(tt.buf)

			var got NamedCell
			if err := p.parseEqual(&got); err != nil {
				t.Fatalf("parseEqual(%q) error = %v", tt.buf, err)
			}
			if show := showKeys([]NamedCell{got}); show != tt.want {
				t.Errorf("parseEqual(%q) = %s, want %s", tt.buf, show, tt.want)
			}
		})
	}
}

func TestParseEqualErrors(t *testing.T) {
	tests := []struct {
		name string
		buf  string
	}{
		{"empty", ""},
		{"no column", "=1"},
		{"no equals", "c 1"},
		{"no value", "c="},
		{"not a value", "c=d"},
		{"a number for a column", "1=1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.buf)

			var got NamedCell
			if err := p.parseEqual(&got); !errors.Is(err, ErrSyntax) {
				t.Fatalf("parseEqual(%q) error = %v, want ErrSyntax", tt.buf, err)
			}
		})
	}
}

func TestParseSelect(t *testing.T) {
	tests := []struct {
		name      string
		buf       string
		wantTable string
		wantCols  string
		wantKeys  string
	}{
		{
			name:      "the statement from the step",
			buf:       "select a,b from t where c=1 and d='e';",
			wantTable: "t",
			wantCols:  "a,b",
			wantKeys:  "c=i64:1 and d=str:e",
		},
		{
			name:      "one column, one key",
			buf:       "select a from t where b=2",
			wantTable: "t",
			wantCols:  "a",
			wantKeys:  "b=i64:2",
		},
		{
			name:      "keywords are case-insensitive, names are not",
			buf:       "SELECT Ab FROM T WHERE Cd = 1 AND e = 'f'",
			wantTable: "T",
			wantCols:  "Ab",
			wantKeys:  "Cd=i64:1 and e=str:f",
		},
		{
			name:      "spaces and newlines between everything",
			buf:       "select\n  a ,\n  b\nfrom  t\nwhere\n  c = -1\n  and d = 'e'\n",
			wantTable: "t",
			wantCols:  "a,b",
			wantKeys:  "c=i64:-1 and d=str:e",
		},
		{
			// Punctuation and quotes end a token by themselves, so nothing has
			// to be spaced out around them.
			name:      "no spaces where none are needed",
			buf:       "select a,b,c from t where d='x'and e=2",
			wantTable: "t",
			wantCols:  "a,b,c",
			wantKeys:  "d=str:x and e=i64:2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.buf)

			var got StmtSelect
			if err := p.parseSelect(&got); err != nil {
				t.Fatalf("parseSelect(%q) error = %v", tt.buf, err)
			}
			if got.table != tt.wantTable {
				t.Errorf("table = %q, want %q", got.table, tt.wantTable)
			}
			if cols := strings.Join(got.cols, ","); cols != tt.wantCols {
				t.Errorf("cols = %q, want %q", cols, tt.wantCols)
			}
			if keys := showKeys(got.keys); keys != tt.wantKeys {
				t.Errorf("keys = %q, want %q", keys, tt.wantKeys)
			}
		})
	}
}

func TestParseSelectErrors(t *testing.T) {
	tests := []struct {
		name string
		buf  string
	}{
		{"empty", ""},
		{"no select", "a from t where c=1"},
		{"select is only a prefix", "selecting a from t where c=1"},
		{"no columns", "select from t where c=1"},
		{"missing comma", "select a b from t where c=1"},
		{"nothing after select", "select"},
		{"nothing after from", "select a from"},
		{"no where", "select a from t"},
		{"where with nothing in it", "select a from t where"},
		{"key with no equals", "select a from t where c"},
		{"key with no value", "select a from t where c="},
		{"trailing and", "select a from t where c=1 and"},
		{"and with no equals", "select a from t where c=1 and d"},
		{"a number for a column", "select 1 from t where c=1"},
		{"a number for a table", "select a from 1 where c=1"},

		// A number ends at a separator, and a letter is not one — so the and
		// that a reader sees glued to the 1 is not a token boundary.
		{"a number running into and", "select a from t where c=1and d=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.buf)

			var got StmtSelect
			if err := p.parseSelect(&got); !errors.Is(err, ErrSyntax) {
				t.Fatalf("parseSelect(%q) error = %v, want ErrSyntax", tt.buf, err)
			}
		})
	}
}

// Parsing into a struct that has been used before must not leave the columns of
// the earlier statement in it.
func TestParseSelectDoesNotAccumulate(t *testing.T) {
	var stmt StmtSelect

	p := NewParser("select a,b from t where c=1")
	if err := p.parseSelect(&stmt); err != nil {
		t.Fatalf("first parseSelect() error = %v", err)
	}

	p = NewParser("select z from u where y=2")
	if err := p.parseSelect(&stmt); err != nil {
		t.Fatalf("second parseSelect() error = %v", err)
	}

	if cols := strings.Join(stmt.cols, ","); cols != "z" {
		t.Errorf("cols = %q, want %q", cols, "z")
	}
	if keys := showKeys(stmt.keys); keys != "y=i64:2" {
		t.Errorf("keys = %q, want %q", keys, "y=i64:2")
	}
}

// Where the parser stands today: it reads the statement and stops on the
// semicolon, which no rule consumes. Whatever comes after it is not looked at,
// so the statement-level entry point that comes later is what will have to
// insist the text ends here.
func TestParseSelectStopsAtTheSemicolon(t *testing.T) {
	p := NewParser("select a from t where b=2; and then some junk")

	var stmt StmtSelect
	if err := p.parseSelect(&stmt); err != nil {
		t.Fatalf("parseSelect() error = %v", err)
	}
	if p.buf[p.pos] != ';' {
		t.Fatalf("cursor sits on %q, want the semicolon", p.buf[p.pos])
	}
}

// A keyword is a name too, so from and where can be read as one when they turn
// up where a name belongs. The statement is still refused, but by the rule after
// the one that was really broken.
func TestKeywordsAreStillNames(t *testing.T) {
	p := NewParser("select a from where c=1")

	var stmt StmtSelect
	err := p.parseSelect(&stmt)
	if !errors.Is(err, ErrSyntax) {
		t.Fatalf("parseSelect() error = %v, want ErrSyntax", err)
	}
	if stmt.table != "where" {
		t.Errorf("table = %q, want %q — where was read as the table name", stmt.table, "where")
	}
}

func TestParseSelectErrorPointsAtTheTrouble(t *testing.T) {
	tests := []struct {
		name string
		buf  string
		want string
	}{
		{"the second column name", "select a, 1 from t where c=1", "at 10"},
		{"the missing comma", "select a b from t where c=1", "at 9"},
		{"the empty column list", "select from t where c=1", "at 7"},
		{"the missing where", "select a from t", "at 15"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.buf)

			var stmt StmtSelect
			err := p.parseSelect(&stmt)
			if err == nil {
				t.Fatalf("parseSelect(%q) = nil, want an error", tt.buf)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to say %q", err, tt.want)
			}
		})
	}
}
