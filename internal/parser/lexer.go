package parser

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Lexer struct {
	input   string
	pos     int
	line    int
	column  int
	atStart bool
}

func NewLexer(input string) *Lexer {
	return &Lexer{
		input:   input,
		pos:     0,
		line:    1,
		column:  1,
		atStart: true,
	}
}

func (l *Lexer) Next() Token {
	if l.pos >= len(l.input) {
		return l.makeToken(TokenEOF, "")
	}

	if l.atStart && l.column == 1 {
		return l.scanLineStart()
	}

	return l.scanInLine()
}

func (l *Lexer) scanLineStart() Token {
	l.atStart = false

	if l.peek() == ';' {
		return l.scanComment()
	}

	if l.isWhitespace(l.peek()) && l.peek() != '\n' {
		return l.scanIndent()
	}

	if l.isDigit(l.peek()) {
		return l.scanDate()
	}

	if l.isLetter(l.peek()) {
		return l.scanDirectiveOrAccount()
	}

	return l.scanInLine()
}

func (l *Lexer) scanInLine() Token {
	l.skipSpaces()

	if l.pos >= len(l.input) {
		return l.makeToken(TokenEOF, "")
	}

	ch := l.peek()
	r := l.peekRune()

	switch {
	case ch == '\n':
		return l.scanNewline()
	case ch == ';':
		return l.scanComment()
	case ch == '(':
		if l.looksLikeVirtualAccount() {
			l.advance()
			return l.makeToken(TokenLParen, "(")
		}
		return l.scanCode()
	case ch == ')':
		l.advance()
		return l.makeToken(TokenRParen, ")")
	case ch == '[':
		l.advance()
		return l.makeToken(TokenLBracket, "[")
	case ch == ']':
		l.advance()
		return l.makeToken(TokenRBracket, "]")
	case ch == '|':
		l.advance()
		return l.makeToken(TokenPipe, "|")
	case ch == '@':
		return l.scanAt()
	case ch == '=':
		return l.scanEquals()
	case ch == '*' || ch == '!':
		return l.scanStatus()
	case l.isCurrencySymbol(r):
		return l.scanCurrencySymbol()
	case ch == '"':
		return l.scanQuotedCommodity()
	case ch == '-' || l.isDigit(ch):
		if l.isDigit(ch) && l.looksLikeDate() {
			return l.scanDate()
		}
		return l.scanNumber()
	case l.isAccountStart(ch):
		if l.looksLikeAccount() {
			return l.scanAccount()
		}
		return l.scanCommodityOrText()
	default:
		return l.scanText()
	}
}

func (l *Lexer) scanDate() Token {
	start := l.pos
	startPos := l.position()

	for l.pos < len(l.input) {
		ch := l.peek()
		if l.isDigit(ch) || ch == '-' || ch == '/' || ch == '.' {
			l.advance()
		} else {
			break
		}
	}

	value := l.input[start:l.pos]
	return Token{Type: TokenDate, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanStatus() Token {
	startPos := l.position()
	ch := l.peek()
	l.advance()
	return Token{Type: TokenStatus, Value: string(ch), Pos: startPos, End: l.position()}
}

func (l *Lexer) scanCode() Token {
	startPos := l.position()
	l.advance()

	start := l.pos
	for l.pos < len(l.input) && l.peek() != ')' && l.peek() != '\n' {
		l.advance()
	}
	value := l.input[start:l.pos]

	if l.pos < len(l.input) && l.peek() == ')' {
		l.advance()
	}

	return Token{Type: TokenCode, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanComment() Token {
	startPos := l.position()
	l.advance()

	start := l.pos
	for l.pos < len(l.input) && l.peek() != '\n' {
		l.advance()
	}

	value := l.input[start:l.pos]
	return Token{Type: TokenComment, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanIndent() Token {
	start := l.pos
	startPos := l.position()

	for l.pos < len(l.input) && l.isWhitespace(l.peek()) && l.peek() != '\n' {
		l.advance()
	}

	value := l.input[start:l.pos]
	return Token{Type: TokenIndent, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanNewline() Token {
	startPos := l.position()
	l.advance()
	l.line++
	l.column = 1
	l.atStart = true
	return Token{Type: TokenNewline, Value: "\n", Pos: startPos, End: l.position()}
}

func (l *Lexer) scanAccount() Token {
	start := l.pos
	startPos := l.position()
	lastNonSpace := start

	for l.pos < len(l.input) {
		ch := l.peek()

		if ch == ' ' {
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == ' ' {
				break
			}
			l.advance()
			continue
		}

		if !l.isLetter(ch) && !l.isDigit(ch) && ch != ':' && ch != '-' && ch != '_' {
			break
		}

		l.advance()
		lastNonSpace = l.pos
	}

	value := l.input[start:lastNonSpace]
	return Token{Type: TokenAccount, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanNumber() Token {
	start := l.pos
	startPos := l.position()

	if l.peek() == '-' {
		l.advance()
	}

	for l.pos < len(l.input) && (l.isDigit(l.peek()) || l.peek() == '.' || l.peek() == ',') {
		l.advance()
	}

	value := l.input[start:l.pos]
	return Token{Type: TokenNumber, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanCurrencySymbol() Token {
	startPos := l.position()
	r, size := utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += size
	l.column++
	return Token{Type: TokenCommodity, Value: string(r), Pos: startPos, End: l.position()}
}

func (l *Lexer) scanQuotedCommodity() Token {
	startPos := l.position()
	l.advance()

	start := l.pos
	for l.pos < len(l.input) && l.peek() != '"' && l.peek() != '\n' {
		l.advance()
	}
	value := l.input[start:l.pos]

	if l.pos < len(l.input) && l.peek() == '"' {
		l.advance()
	}

	return Token{Type: TokenCommodity, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanAt() Token {
	startPos := l.position()
	l.advance()

	if l.pos < len(l.input) && l.peek() == '@' {
		l.advance()
		return Token{Type: TokenAtAt, Value: "@@", Pos: startPos, End: l.position()}
	}

	return Token{Type: TokenAt, Value: "@", Pos: startPos, End: l.position()}
}

func (l *Lexer) scanEquals() Token {
	startPos := l.position()
	l.advance()

	if l.pos < len(l.input) && l.peek() == '=' {
		l.advance()
		return Token{Type: TokenDoubleEquals, Value: "==", Pos: startPos, End: l.position()}
	}

	return Token{Type: TokenEquals, Value: "=", Pos: startPos, End: l.position()}
}

func (l *Lexer) scanDirectiveOrAccount() Token {
	start := l.pos
	startPos := l.position()

	for l.pos < len(l.input) && (l.isLetter(l.peek()) || l.isDigit(l.peek())) {
		l.advance()
	}

	word := l.input[start:l.pos]

	if isDirective(word) {
		return Token{Type: TokenDirective, Value: word, Pos: startPos, End: l.position()}
	}

	l.pos = start
	l.column = startPos.Column

	if l.looksLikeAccount() {
		return l.scanAccount()
	}

	return l.scanText()
}

func (l *Lexer) scanCommodityOrText() Token {
	start := l.pos
	startPos := l.position()

	for l.pos < len(l.input) {
		ch := l.peek()
		if l.isLetter(ch) || l.isDigit(ch) {
			l.advance()
		} else {
			break
		}
	}

	value := l.input[start:l.pos]

	if l.looksLikeCommodity(value) {
		return Token{Type: TokenCommodity, Value: value, Pos: startPos, End: l.position()}
	}

	l.pos = start
	l.column = startPos.Column
	return l.scanText()
}

func (l *Lexer) scanText() Token {
	start := l.pos
	startPos := l.position()

	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == '\n' || ch == ';' || ch == '|' {
			break
		}
		l.advance()
	}

	value := strings.TrimSpace(l.input[start:l.pos])
	return Token{Type: TokenText, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) peek() byte {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *Lexer) peekRune() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.input[l.pos:])
	return r
}

func (l *Lexer) advance() {
	if l.pos < len(l.input) {
		_, size := utf8.DecodeRuneInString(l.input[l.pos:])
		l.pos += size
		l.column++
	}
}

func (l *Lexer) skipSpaces() {
	for l.pos < len(l.input) && l.input[l.pos] == ' ' {
		l.advance()
	}
}

func (l *Lexer) position() Position {
	return Position{Line: l.line, Column: l.column, Offset: l.pos}
}

func (l *Lexer) makeToken(typ TokenType, value string) Token {
	pos := l.position()
	return Token{Type: typ, Value: value, Pos: pos, End: pos}
}

func (l *Lexer) isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func (l *Lexer) isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func (l *Lexer) isLetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func (l *Lexer) isAccountStart(ch byte) bool {
	return l.isLetter(ch)
}

func (l *Lexer) isCurrencySymbol(r rune) bool {
	return r == '$' || r == '€' || r == '£' || r == '¥' || r == '₽' || r == '₴'
}

func (l *Lexer) looksLikeAccount() bool {
	savedPos := l.pos
	hasColon := false

	for i := l.pos; i < len(l.input); i++ {
		ch := l.input[i]
		if ch == ':' {
			hasColon = true
		} else if ch == ' ' {
			if i+1 < len(l.input) && l.input[i+1] == ' ' {
				break
			}
			continue
		} else if !l.isLetter(ch) && !l.isDigit(ch) && ch != '-' && ch != '_' {
			break
		}
	}

	l.pos = savedPos
	return hasColon
}

func (l *Lexer) looksLikeCommodity(value string) bool {
	if len(value) == 0 {
		return false
	}
	for _, r := range value {
		if !unicode.IsUpper(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func (l *Lexer) looksLikeDate() bool {
	if l.pos+8 > len(l.input) {
		return false
	}

	for i := 0; i < 4; i++ {
		if !l.isDigit(l.input[l.pos+i]) {
			return false
		}
	}

	sep := l.input[l.pos+4]
	if sep != '-' && sep != '/' && sep != '.' {
		return false
	}

	if !l.isDigit(l.input[l.pos+5]) {
		return false
	}

	return true
}

func (l *Lexer) looksLikeVirtualAccount() bool {
	for i := l.pos + 1; i < len(l.input); i++ {
		ch := l.input[i]
		if ch == ')' || ch == '\n' {
			return false
		}
		if ch == ':' {
			return true
		}
	}
	return false
}

func isDirective(word string) bool {
	directives := []string{
		"account", "alias", "apply", "assert", "bucket", "capture",
		"check", "comment", "commodity", "D", "decimal-mark", "def",
		"define", "end", "eval", "expr", "include", "payee", "P",
		"tag", "test", "Y", "year",
	}
	for _, d := range directives {
		if word == d {
			return true
		}
	}
	return false
}
