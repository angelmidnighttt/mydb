package sql

import (
	"strconv"

	"github.com/angelmidnighttt/mydb/internal/table"
)

// parseValue reads one literal at the cursor and fills out with it.
//
// A single byte decides which kind it is: a quote opens a string, a digit or a
// sign opens a number, and nothing else starts a value at all. That one byte of
// lookahead is the whole dispatch — no backtracking, no trying one kind and
// falling back to the other. Most of a language can be read this way, and where
// it can, the parser stays this simple.
//
// Unlike the try methods, a parse method says what went wrong instead of just
// failing: by the time the grammar asks for a value, a value is the only thing
// that belongs here, so anything else is an error the user has to be told about
// rather than an alternative to try next.
//
// The cursor does not move when an error is returned, so the error and the
// parser agree on where the trouble is.
func (p *Parser) parseValue(out *table.Cell) error {
	pos := p.skipSpace()
	if pos >= len(p.buf) {
		return p.errorf(pos, "expect a value, the statement ends here")
	}

	switch ch := p.buf[pos]; {
	case ch == '"' || ch == '\'':
		return p.parseString(out)
	case isDigit(ch) || ch == '-' || ch == '+':
		return p.parseInt(out)
	default:
		return p.errorf(pos, "expect a value, found %q", ch)
	}
}

// parseInt reads an int64: an optional + or -, then digits.
//
// The digits have to end where a name would end. Without that rule 1a reads as
// the number 1 with a left over, and a typo turns into two tokens that happen to
// parse. A decimal point is turned away by name: it is a separator, so 1.5 would
// otherwise scan as 1 and leave .5 behind — a value quietly losing its fraction
// is worse than a value that is refused.
func (p *Parser) parseInt(out *table.Cell) error {
	start := p.skipSpace()

	end := start
	if end < len(p.buf) && (p.buf[end] == '-' || p.buf[end] == '+') {
		end++
	}
	digits := end
	for end < len(p.buf) && isDigit(p.buf[end]) {
		end++
	}
	if end == digits {
		return p.errorf(start, "expect a number")
	}

	if end < len(p.buf) && p.buf[end] == '.' {
		return p.errorf(end, "expect an int64, there are no floating point numbers yet")
	}
	if end < len(p.buf) && !isSeparator(p.buf[end]) {
		return p.errorf(end, "a number cannot run straight into %q", p.buf[end])
	}

	// The text is known to be a sign and digits, so the only way this fails is
	// a number too big for 64 bits.
	val, err := strconv.ParseInt(p.buf[start:end], 10, 64)
	if err != nil {
		return p.errorf(start, "%s does not fit in an int64", p.buf[start:end])
	}

	// Both fields are written, the unused one back to its zero value, so a cell
	// handed in twice cannot carry a leftover from the first time.
	out.Type = table.TypeI64
	out.I64 = val
	out.Str = nil

	p.pos = end
	return nil
}

// parseString reads a quoted string. Single and double quotes both open one, and
// the one that opened it is the one that closes it, so the other kind needs no
// escaping inside: 'say "hi"' and "isn't" are both plain text.
//
// A backslash escapes the byte after it, and only a quote or another backslash
// may follow. Anything else is refused rather than passed through as itself.
// That refusal is what keeps the door open: if \n stood for the letter n today,
// giving it its real meaning tomorrow would silently change what old statements
// mean. Refusing it now makes every escape added later a pure addition.
func (p *Parser) parseString(out *table.Cell) error {
	start := p.skipSpace()
	if start >= len(p.buf) {
		return p.errorf(start, "expect a string, the statement ends here")
	}
	quote := p.buf[start]
	if quote != '"' && quote != '\'' {
		return p.errorf(start, "expect a string, found %q", quote)
	}

	// Escapes make the result shorter than the text it is read from, never
	// longer, but by how much is not known until it has been read.
	str := []byte{}

	for pos := start + 1; pos < len(p.buf); {
		switch ch := p.buf[pos]; ch {
		case quote:
			out.Type = table.TypeStr
			out.Str = str
			out.I64 = 0

			p.pos = pos + 1
			return nil

		case '\\':
			if pos+1 >= len(p.buf) {
				return p.errorf(pos, "the statement ends inside an escape")
			}
			esc := p.buf[pos+1]
			if esc != '\\' && esc != '"' && esc != '\'' {
				return p.errorf(pos, "unknown escape \\%c, only \\\\ \\\" and \\' are understood", esc)
			}
			str = append(str, esc)
			pos += 2

		default:
			str = append(str, ch)
			pos++
		}
	}

	// The offset points at the quote that was never answered, not at the end of
	// the text: that is where the reader has to look to fix it.
	return p.errorf(start, "the string opened here is never closed")
}
