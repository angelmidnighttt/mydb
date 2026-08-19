package sql

// The byte classifiers the tokenizer is built out of. SQL keywords and the
// punctuation around them are ASCII, so these work a byte at a time rather than
// decoding runes; anything outside ASCII can only appear inside a quoted string,
// which is read as bytes anyway.

// isSpace reports whether ch is whitespace. The set is the one C and Go both
// call space: tab, newline, vertical tab, form feed, carriage return, blank.
func isSpace(ch byte) bool {
	switch ch {
	case '\t', '\n', '\v', '\f', '\r', ' ':
		return true
	}
	return false
}

// isAlpha reports whether ch is an ASCII letter.
//
// Bit 0x20 is the case bit of an ASCII letter, so ch|32 folds 'A' onto 'a' and
// leaves lowercase alone. No other ASCII byte becomes a letter when that bit is
// set — only 0x41..0x5A and 0x61..0x7A land in 'a'..'z' — which is what makes it
// safe to fold first and range-check once, instead of checking two ranges.
func isAlpha(ch byte) bool {
	return 'a' <= (ch|32) && (ch|32) <= 'z'
}

// isDigit reports whether ch is an ASCII digit.
func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

// isNameStart reports whether ch can begin a name. Digits are left out so that
// 9x is not a name: a token that starts with a digit is a number, and letting
// the two overlap would make the first byte no longer decide which is which.
func isNameStart(ch byte) bool {
	return isAlpha(ch) || ch == '_'
}

// isNameContinue reports whether ch can appear in a name after the first byte.
func isNameContinue(ch byte) bool {
	return isAlpha(ch) || isDigit(ch) || ch == '_'
}

// isSeparator reports whether ch ends a name or a keyword: any ASCII byte that
// cannot appear inside one.
//
// Bytes from 128 up are deliberately not separators. Such a byte is part of a
// character outside ASCII, and no such character ends a word here — without the
// range check, selectá would parse as the keyword select with á left over.
func isSeparator(ch byte) bool {
	return ch < 128 && !isNameContinue(ch)
}
