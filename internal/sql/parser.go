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

import "strings"

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

// tryKeyword matches kw at the cursor, after any spaces, ignoring case: SELECT,
// select and SeLeCt are one word. kw is ASCII, which is all a keyword can be.
//
// The match has to end at a separator or at the end of the text. Without that,
// select would be found at the front of selecting, and the parse would carry on
// from ing as though it were the next token.
func (p *Parser) tryKeyword(kw string) bool {
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
