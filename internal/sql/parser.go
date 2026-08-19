// Package sql turns SQL text into program data. A statement arrives as a string
// and has to leave as a struct the database can act on:
//
//	select a, b from t where c = 1;
//
// becomes a table name, a list of columns, and a key to look the row up by.
//
// The first half of that trip is tokenizing: cutting the text into the words the
// language is made of. SQL has four kinds — keywords (select, from), names of
// tables and columns, symbols (= , ;) and literals (numbers, strings) — and each
// kind has its own rule, so each gets its own function. The grammar that puts
// tokens together into a statement comes after; this file only splits.
package sql

import (
	"errors"
	"fmt"
	"strings"
)

// Parser is a cursor over one SQL statement: the text, and how far into it the
// parse has got.
//
// Every try method reads from pos and moves it only on a match, so a caller can
// attempt one rule, find it does not fit, and try another with nothing to undo.
// A grammar is mostly a list of alternatives, and that is what makes the
// alternatives cheap to write.
type Parser struct {
	buf string
	pos int
}

// NewParser returns a Parser positioned at the start of s.
func NewParser(s string) Parser {
	return Parser{buf: s, pos: 0}
}

// skipSpace returns the position of the first byte at or after the cursor that
// is not a space. It reports the position instead of moving the cursor: a try
// that fails has to leave the parser exactly where it was, which is easier to
// guarantee when nothing moves until the match is certain.
func (p *Parser) skipSpace() int {
	pos := p.pos
	for pos < len(p.buf) && isSpace(p.buf[pos]) {
		pos++
	}
	return pos
}

// tryName reads a name — of a table, of a column — at the cursor, after any
// spaces. A name starts with a letter or an underscore and carries on with
// letters, digits and underscores.
//
// ok is false when there is no name there, and the cursor has not moved, spaces
// included. Whoever tries next skips them again; a few bytes rescanned is the
// price of a free retry.
//
// A name is only text here — this function knows nothing about keywords, and
// select is a perfectly good name to it. Which of the two a position holds is a
// question for the grammar, and the grammar is the thing calling.
func (p *Parser) tryName() (string, bool) {
	start := p.skipSpace()
	if start >= len(p.buf) || !isNameStart(p.buf[start]) {
		return "", false
	}

	end := start + 1
	for end < len(p.buf) && isNameContinue(p.buf[end]) {
		end++
	}

	p.pos = end
	return p.buf[start:end], true
}

// tryKeyword matches kws at the cursor, one after another, after any spaces.
//
// More than one word is what statements like CREATE TABLE and INSERT INTO are
// named by, and they are matched all or nothing: a text that begins CREATE INDEX
// leaves the cursor on the CREATE, so the next alternative sees the statement
// from its start rather than from the middle of its name.
func (p *Parser) tryKeyword(kws ...string) bool {
	start := p.pos
	for _, kw := range kws {
		if !p.tryOneKeyword(kw) {
			p.pos = start
			return false
		}
	}
	return true
}

// tryOneKeyword matches kw at the cursor, after any spaces, ignoring case:
// SELECT, select and SeLeCt are one word. kw is ASCII, which is all a keyword
// can be.
//
// The match has to end at a separator or at the end of the text. Without that,
// select would be found at the front of selecting, and the parse would carry on
// from ing as though it were the next token.
func (p *Parser) tryOneKeyword(kw string) bool {
	start := p.skipSpace()
	end := start + len(kw)
	if end > len(p.buf) {
		return false
	}
	if !strings.EqualFold(p.buf[start:end], kw) {
		return false
	}
	if end < len(p.buf) && !isSeparator(p.buf[end]) {
		return false
	}

	p.pos = end
	return true
}

// ErrSyntax reports text that is not the SQL the parser expected. Everything
// this package turns away is that one problem, so callers get one sentinel to
// test for, and the message carries the detail: where, and what was expected.
var ErrSyntax = errors.New("sql: syntax error")

// errorf builds a syntax error pointing at pos, a byte offset into the
// statement. An offset is what a caller needs to underline the spot; line and
// column can be counted out of it, and only matter once there is somewhere to
// print them.
func (p *Parser) errorf(pos int, format string, args ...any) error {
	return fmt.Errorf("%w at %d: %s", ErrSyntax, pos, fmt.Sprintf(format, args...))
}

// tryPunctuation matches punct at the cursor, after any spaces.
//
// It needs neither of the two rules tryKeyword needs. Case does not apply, and
// nothing has to follow: punctuation is made of bytes that cannot appear inside
// a name or a number, so it always ends where it ends. 1,2 needs no space to be
// three tokens.
//
// punct is compared as a whole, so an operator of more than one byte works —
// but a shorter one that is a prefix of a longer one will match first, so when
// >= arrives it has to be tried before >.
func (p *Parser) tryPunctuation(punct string) bool {
	start := p.skipSpace()
	end := start + len(punct)
	if end > len(p.buf) || p.buf[start:end] != punct {
		return false
	}

	p.pos = end
	return true
}
