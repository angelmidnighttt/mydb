package compiler

type TokenType int

const (
	TokenError TokenType = iota
	TokenEOF
	TokenIdentifier
	TokenKeyword
	TokenSymbol
	TokenWhiteSpace
)

type Token struct {
	Type TokenType
	Value string
}

type Lexer interface {
	NextToken() Token
}

type LexerSimple struct{
	input string
	position int
}

func NewLexex(input string) Lexer {
	return &LexerSimple{input: input, position: 0}
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNumber(c byte) bool {
	return (c >= '0' && c <= '9')
}

func (l *LexerSimple) consumeWhiteSpace() {
	for l.position < len(l.input) && (l.input[l.position] == ' ' || l.input[l.position] == '\t' || l.input[l.position] == '\n' || l.input[l.position] == '\r') {
		l.position++
	}
}

func (l *LexerSimple) consumeIdentifier() Token {
	start := l.position
	for l.position < len(l.input) && (isLetter(l.input[l.position]) || isNumber(l.input[l.position])) {
		l.position++
	}
	return Token{Type: TokenIdentifier, Value: l.input[start:l.position]}
}

func (l *LexerSimple) NextToken() Token {
	if l.position >= len(l.input) {
		return Token{Type: TokenEOF, Value:""}
	}
	c := l.input[l.position]
	switch {
	case c == ' ' ||c == '\t' || c == '\n' || c ==  '\r':
		l.consumeWhiteSpace()
		return l.NextToken()
	case c == ',' || c == ';' || c == '(' || c == ')' || c == '{' || c == '}' || c == '[' || c == ']' || c == '=' || c == '!' || c == '<' || c == '>' || c == '^' || c == '|' || c == '~' || c == '?' || c == ':':
		l.position++
		return Token{Type: TokenSymbol, Value: string(c)}
	case isLetter(c) || isNumber(c):
		return l.consumeIdentifier()
	default:
		return Token{Type: TokenError, Value: string(c)}
	}
}	