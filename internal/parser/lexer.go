package parser

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Lexer struct {
	input          string
	pos            int
	line           int
	column         int
	atStart        bool
	afterIndent    bool
	inTransaction  bool
	inDirective    bool
	directiveName  string
	inSubdirective bool
	onPostingLine  bool
	afterSign      bool
	afterNumber    bool
	virtualCloser  rune
	insideBraces   int
}

func NewLexer(input string) *Lexer {
	return &Lexer{
		input:         input,
		pos:           0,
		line:          1,
		column:        1,
		atStart:       true,
		afterIndent:   false,
		inTransaction: false,
		inDirective:   false,
		onPostingLine: false,
		afterSign:     false,
		afterNumber:   false,
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

	if l.peek() == ';' || l.peek() == '#' {
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

	if l.peek() == '~' {
		l.advance()
		return l.makeToken(TokenTilde, "~")
	}

	if l.peek() == '=' {
		l.advance()
		return l.makeToken(TokenAutoRule, "=")
	}

	return l.scanInLine()
}

func (l *Lexer) scanInLine() Token {
	l.skipSpaces()

	if l.pos >= len(l.input) {
		return l.makeToken(TokenEOF, "")
	}

	if l.inSubdirective {
		l.inSubdirective = false
		return l.scanSubdirectiveValue()
	}

	ch := l.peek()
	r := l.peekRune()

	switch {
	case ch == '\n':
		return l.scanNewline()
	case ch == ';':
		return l.scanComment()
	case ch == '#' && l.afterIndent:
		return l.scanComment()
	case ch == '(':
		if l.looksLikeVirtualAccount() {
			l.virtualCloser = ')'
			l.advance()
			return l.makeToken(TokenLParen, "(")
		}
		return l.scanCode()
	case ch == ')':
		l.advance()
		return l.makeToken(TokenRParen, ")")
	case ch == '[':
		if l.onPostingLine {
			l.virtualCloser = ']'
		}
		l.advance()
		return l.makeToken(TokenLBracket, "[")
	case ch == ']':
		l.advance()
		return l.makeToken(TokenRBracket, "]")
	case ch == '|':
		l.advance()
		return l.makeToken(TokenPipe, "|")
	case ch == '{':
		return l.scanLBrace()
	case ch == '}':
		return l.scanRBrace()
	case ch == '@':
		return l.scanAt()
	case ch == '=':
		return l.scanEquals()
	case ch == '*' || ch == '!':
		return l.scanStatus()
	case l.isCurrencySymbol(r):
		return l.scanCurrencySymbol()
	case ch == '"':
		// Header line: quotes are part of description
		if l.inTransaction && !l.onPostingLine && !l.afterIndent {
			return l.scanText()
		}
		return l.scanQuotedCommodity()
	case ch == '-' || ch == '+':
		if l.nextIsCurrencySymbol() || l.nextIsLetterCommodity() || l.nextIsDigit() {
			return l.scanSign()
		}
		return l.scanText()
	case l.isDigit(ch):
		if l.looksLikeDate() {
			return l.scanDate()
		}
		// Header line: digits are part of description
		if l.inTransaction && !l.onPostingLine && !l.afterIndent {
			return l.scanText()
		}
		return l.scanNumber()
	case l.isAccountStart(ch) || l.isAccountStartRune(r):
		// Header line: letters are part of description
		if l.inTransaction && !l.onPostingLine && !l.afterIndent {
			return l.scanText()
		}
		// afterIndent = true → subdirective line or posting line
		if l.afterIndent {
			if l.inDirective {
				return l.scanSubdirectiveContent() // Subdirective line
			}
			return l.scanAccount() // Posting line
		}
		// After sign or number → Commodity (amount context)
		if l.afterSign || l.afterNumber {
			// Check for multi-char currency (AU$, CA$, etc.)
			if l.looksLikeMultiCharCurrency() {
				return l.scanMultiCharCurrency()
			}
			return l.scanCommodityOrText()
		}
		// Posting line after account → Commodity (even if not in transaction)
		if l.onPostingLine {
			// Check for multi-char currency (AU$, CA$, etc.)
			if l.looksLikeMultiCharCurrency() {
				return l.scanMultiCharCurrency()
			}
			return l.scanCommodityOrText()
		}
		// In directive context → check if it's an account, otherwise single-word argument
		if l.inDirective {
			if l.directiveName == "account" || l.directiveName == "bucket" || l.directiveName == "A" {
				return l.scanAccount()
			}
			if l.looksLikeAccount() {
				return l.scanAccount()
			}
			return l.scanDirectiveArg()
		}
		// Not in transaction and not on posting line → could be account (directive line) or text
		if !l.inTransaction {
			if l.looksLikeAccount() {
				return l.scanAccount()
			}
			return l.scanText()
		}
		// Header line → Text
		return l.scanText()
	case ch == ',' && l.insideBraces > 0:
		l.advance()
		return l.makeToken(TokenText, ",")
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
	// Only set inTransaction if date is at column 1 (transaction header, not directive)
	if startPos.Column == 1 {
		l.inTransaction = true
	}
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
	// Indent indicates start of posting line (account + amount) or subdirective
	l.afterIndent = true
	l.afterNumber = false
	l.afterSign = false
	if !l.inDirective {
		l.onPostingLine = true // Only for transactions
	}
	return Token{Type: TokenIndent, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanNewline() Token {
	startPos := l.position()
	l.advance()
	l.line++
	l.column = 1
	l.atStart = true
	l.afterIndent = false
	l.onPostingLine = false
	l.afterNumber = false
	l.afterSign = false
	l.virtualCloser = 0
	l.insideBraces = 0
	// Check if next line starts at column 1 (not indented) - this ends the transaction or directive
	if l.pos < len(l.input) && !l.isWhitespace(l.peek()) {
		l.inTransaction = false
		l.inDirective = false
		l.directiveName = ""
	}
	return Token{Type: TokenNewline, Value: "\n", Pos: startPos, End: l.position()}
}

func (l *Lexer) scanAccount() Token {
	start := l.pos
	startPos := l.position()
	lastNonSpace := start
	lastNonSpaceColumn := l.column

	for l.pos < len(l.input) {
		r, size := utf8.DecodeRuneInString(l.input[l.pos:])

		if r == ' ' {
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == ' ' {
				break
			}
			l.pos += size
			l.column++
			continue
		}

		if l.virtualCloser != 0 && r == l.virtualCloser {
			l.virtualCloser = 0
			break
		}

		if isAccountTerminator(r) {
			break
		}

		l.pos += size
		l.column++
		lastNonSpace = l.pos
		lastNonSpaceColumn = l.column
	}

	value := l.input[start:lastNonSpace]
	endPos := Position{Line: startPos.Line, Column: lastNonSpaceColumn, Offset: lastNonSpace}
	l.afterIndent = false
	return Token{Type: TokenAccount, Value: value, Pos: startPos, End: endPos}
}

// isAccountTerminator returns true for characters that end account names in hledger format.
// Per hledger manual: account names end only at double-space (2+ spaces), tab, or newline.
// The double-space check is handled in scanAccount() before this function is called.
func isAccountTerminator(r rune) bool {
	switch r {
	case '\t', '\n', '\r':
		return true
	}
	return false
}

func (l *Lexer) scanNumber() Token {
	start := l.pos
	startPos := l.position()

	hasDigits := false

	for l.pos < len(l.input) {
		ch := l.peek()
		switch {
		case l.isDigit(ch):
			hasDigits = true
			l.advance()
		case ch == '.' || ch == ',':
			l.advance()
		case ch == ' ' && l.pos+1 < len(l.input) && l.isDigit(l.input[l.pos+1]):
			l.advance()
		case (ch == 'E' || ch == 'e') && hasDigits:
			nextPos := l.pos + 1
			if nextPos < len(l.input) && (l.input[nextPos] == '+' || l.input[nextPos] == '-') {
				nextPos++
			}
			if nextPos >= len(l.input) || !l.isDigit(l.input[nextPos]) {
				goto done
			}
			l.advance()
			if l.pos < len(l.input) && (l.peek() == '+' || l.peek() == '-') {
				l.advance()
			}
		default:
			goto done
		}
	}
done:

	value := l.input[start:l.pos]
	l.afterNumber = true // Mark that we just scanned a number (amount context continues)
	return Token{Type: TokenNumber, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanCurrencySymbol() Token {
	startPos := l.position()
	r, size := utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += size
	l.column++
	return Token{Type: TokenCommodity, Value: string(r), Pos: startPos, End: l.position()}
}

func (l *Lexer) scanMultiCharCurrency() Token {
	startPos := l.position()

	// Scan 2 uppercase letters
	prefix := l.input[l.pos : l.pos+2]
	l.pos += 2
	l.column += 2

	// Scan the currency symbol
	r, size := utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += size
	l.column++

	value := prefix + string(r)
	return Token{Type: TokenCommodity, Value: value, Pos: startPos, End: l.position()}
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

	return Token{Type: TokenQuotedCommodity, Value: value, Pos: startPos, End: l.position()}
}

func (l *Lexer) scanLBrace() Token {
	startPos := l.position()
	l.advance()

	if l.pos < len(l.input) && l.peek() == '{' {
		l.advance()
		l.insideBraces++
		return Token{Type: TokenDoubleLBrace, Value: "{{", Pos: startPos, End: l.position()}
	}

	l.insideBraces++
	return Token{Type: TokenLBrace, Value: "{", Pos: startPos, End: l.position()}
}

func (l *Lexer) scanRBrace() Token {
	startPos := l.position()
	l.advance()

	if l.pos < len(l.input) && l.peek() == '}' {
		l.advance()
		if l.insideBraces > 0 {
			l.insideBraces--
		}
		return Token{Type: TokenDoubleRBrace, Value: "}}", Pos: startPos, End: l.position()}
	}

	if l.insideBraces > 0 {
		l.insideBraces--
	}
	return Token{Type: TokenRBrace, Value: "}", Pos: startPos, End: l.position()}
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
		if l.pos < len(l.input) && l.peek() == '*' {
			l.advance()
			return Token{Type: TokenDoubleEqualsStar, Value: "==*", Pos: startPos, End: l.position()}
		}
		return Token{Type: TokenDoubleEquals, Value: "==", Pos: startPos, End: l.position()}
	}

	if l.pos < len(l.input) && l.peek() == '*' {
		l.advance()
		return Token{Type: TokenEqualsStar, Value: "=*", Pos: startPos, End: l.position()}
	}

	return Token{Type: TokenEquals, Value: "=", Pos: startPos, End: l.position()}
}

func (l *Lexer) scanDirectiveOrAccount() Token {
	start := l.pos
	startPos := l.position()

	// First, scan only letters (for directives like Y, P, D)
	for l.pos < len(l.input) && l.isLetter(l.peek()) {
		l.advance()
	}

	word := l.input[start:l.pos]

	// Check for single-letter directives first (Y, P, D)
	if isDirective(word) {
		l.afterIndent = false
		l.inDirective = true
		l.directiveName = word
		return Token{Type: TokenDirective, Value: word, Pos: startPos, End: l.position()}
	}

	// Check for multi-word directives with hyphens (e.g., "decimal-mark")
	if l.peek() == '-' {
		tempPos := l.pos
		tempCol := l.column
		l.advance() // skip hyphen

		// Scan the rest of the word
		for l.pos < len(l.input) && l.isLetter(l.peek()) {
			l.advance()
		}

		potentialDirective := l.input[start:l.pos]
		if isDirective(potentialDirective) {
			l.afterIndent = false
			l.inDirective = true
			l.directiveName = potentialDirective
			return Token{Type: TokenDirective, Value: potentialDirective, Pos: startPos, End: l.position()}
		}

		// Not a directive, reset position
		l.pos = tempPos
		l.column = tempCol
	}

	// If not a directive, continue scanning with digits for potential account
	for l.pos < len(l.input) && l.isDigit(l.peek()) {
		l.advance()
	}

	// Reset and check if it looks like account
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

	// Header line: everything is Text (description)
	if l.inTransaction && !l.onPostingLine {
		return l.scanText()
	}

	// Posting line or after sign/number (amount context): scan commodity
	// Prefix commodity (no number yet): USD222 → USD + 222 (letters only)
	// Suffix commodity (after number): 10 USD2024 → 10 + USD2024 (letters+digits)
	// After sign without space: -USD222 → - + USD + 222 (letters only)
	if l.afterSign || !l.afterNumber {
		// Scan letters only (prefix commodity or right after sign)
		for l.pos < len(l.input) {
			r := l.peekRune()
			if r == 0 || !unicode.IsLetter(r) {
				break
			}
			l.advance()
		}
	} else {
		// Scan letters and digits together (suffix commodity after number)
		for l.pos < len(l.input) {
			r := l.peekRune()
			if r == 0 {
				break
			}
			// Stop at whitespace or special amount-related characters
			if unicode.IsSpace(r) || r == ';' || r == '@' || r == '=' || r == '(' || r == ')' || r == '[' || r == ']' {
				break
			}
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				break
			}
			l.advance()
		}
	}

	value := l.input[start:l.pos]
	if len(value) > 0 {
		l.afterSign = false   // Reset after consuming commodity
		l.afterNumber = false // Reset after consuming commodity
		return Token{Type: TokenCommodity, Value: value, Pos: startPos, End: l.position()}
	}

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

// scanDirectiveArg scans a single word argument for directives (e.g., "EUR" in "P 2024-01-15 EUR $1.08")
// This allows subsequent tokens (like "$1.08") to be scanned separately
func (l *Lexer) scanDirectiveArg() Token {
	start := l.pos
	startPos := l.position()

	// Scan word (letters, digits, and dots for filenames)
	for l.pos < len(l.input) {
		r := l.peekRune()
		if r == 0 {
			break
		}
		// Stop at whitespace or special characters
		if unicode.IsSpace(r) || r == ';' || r == '|' {
			break
		}
		// For simple words: letters and digits
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '/' && r != '_' && r != '-' {
			break
		}
		l.advance()
	}

	value := l.input[start:l.pos]
	if len(value) > 0 {
		return Token{Type: TokenText, Value: value, Pos: startPos, End: l.position()}
	}
	return l.scanText()
}

// scanSubdirectiveContent scans content of subdirective lines (after indent in directive context)
func (l *Lexer) scanSubdirectiveContent() Token {
	start := l.pos
	startPos := l.position()

	for l.pos < len(l.input) && l.isLetter(l.peek()) {
		l.advance()
	}
	word := l.input[start:l.pos]

	if isSubdirective(word) {
		l.afterIndent = false
		l.inSubdirective = true
		return Token{Type: TokenDirective, Value: word, Pos: startPos, End: l.position()}
	}

	l.pos = start
	l.column = startPos.Column

	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == '\n' || ch == ';' {
			break
		}
		l.advance()
	}

	value := strings.TrimSpace(l.input[start:l.pos])
	l.afterIndent = false
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
	ch := l.peek()
	if ch == ' ' || ch == '\t' {
		// Space/tab after sign means commodity should include digits (like "- USD2024")
		// afterNumber stays true to indicate we're in suffix commodity context
		l.afterSign = false
		for l.pos < len(l.input) {
			ch = l.peek()
			if ch != ' ' && ch != '\t' {
				break
			}
			l.advance()
		}
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

func isUpperASCII(ch byte) bool {
	return ch >= 'A' && ch <= 'Z'
}

func (l *Lexer) isAccountStart(ch byte) bool {
	return l.isLetter(ch)
}

func (l *Lexer) isAccountStartRune(r rune) bool {
	return unicode.IsLetter(r)
}

func (l *Lexer) isCurrencySymbol(r rune) bool {
	return unicode.Is(unicode.Sc, r)
}

func (l *Lexer) looksLikeMultiCharCurrency() bool {
	if l.pos+3 > len(l.input) {
		return false
	}

	// Check for 2 uppercase ASCII letters followed by currency symbol
	if !isUpperASCII(l.input[l.pos]) || !isUpperASCII(l.input[l.pos+1]) {
		return false
	}

	r, _ := utf8.DecodeRuneInString(l.input[l.pos+2:])
	return l.isCurrencySymbol(r)
}

func (l *Lexer) nextIsCurrencySymbol() bool {
	if l.pos+1 >= len(l.input) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(l.input[l.pos+1:])
	return l.isCurrencySymbol(r)
}

func (l *Lexer) nextIsDigit() bool {
	pos := l.pos + 1
	for pos < len(l.input) && l.input[pos] == ' ' {
		pos++
	}
	if pos >= len(l.input) {
		return false
	}
	return l.isDigit(l.input[pos])
}

func (l *Lexer) nextIsLetterCommodity() bool {
	pos := l.pos + 1
	if pos >= len(l.input) {
		return false
	}
	if !l.isLetter(l.input[pos]) {
		return false
	}
	for pos < len(l.input) && l.isLetter(l.input[pos]) {
		pos++
	}
	if pos >= len(l.input) {
		return false
	}
	ch := l.input[pos]
	if l.isDigit(ch) {
		return true
	}
	if (ch == '-' || ch == '+') && pos+1 < len(l.input) && l.isDigit(l.input[pos+1]) {
		return true
	}
	return false
}

func (l *Lexer) scanSign() Token {
	startPos := l.position()
	sign := string(l.peek())
	l.advance()
	l.afterSign = true // Mark that we're right after a sign
	return Token{Type: TokenSign, Value: sign, Pos: startPos, End: l.position()}
}

func (l *Lexer) looksLikeAccount() bool {
	// On transaction header line, text is description, not account
	if l.inTransaction && !l.onPostingLine && !l.afterIndent && !l.atStart {
		return false
	}

	hasColon := false

	for i := l.pos; i < len(l.input); {
		r, size := utf8.DecodeRuneInString(l.input[i:])
		if r == ':' {
			hasColon = true
			i += size
		} else if r == ' ' {
			if i+1 < len(l.input) && l.input[i+1] == ' ' {
				break
			}
			i += size
		} else if isAccountTerminator(r) {
			break
		} else if r == '=' && l.inDirective {
			break
		} else {
			i += size
		}
	}

	// In posting context (after indent), accounts without colons are valid
	if l.afterIndent {
		return true
	}

	return hasColon
}

func (l *Lexer) looksLikeDate() bool {
	// On posting line, numbers are amounts, not dates
	if l.onPostingLine {
		return false
	}

	// Date format: YYYY-MM-DD (minimum 8 chars for YYYY-M-D, typical 10 for YYYY-MM-DD)
	// We require a SECOND separator to distinguish from numbers like 1000.00
	if l.pos+8 > len(l.input) {
		return false
	}

	// Check for 4-digit year
	for i := range 4 {
		if !l.isDigit(l.input[l.pos+i]) {
			return false
		}
	}

	// Check first separator
	sep := l.input[l.pos+4]
	if sep != '-' && sep != '/' && sep != '.' {
		return false
	}

	// Check month digit
	if !l.isDigit(l.input[l.pos+5]) {
		return false
	}

	// Find second separator position (after 1 or 2 digit month)
	secondSepPos := 6
	if l.pos+6 < len(l.input) && l.isDigit(l.input[l.pos+6]) {
		// Two-digit month
		secondSepPos = 7
	}

	// Require second separator to be a date (distinguishes 2024-01-15 from 1000.00)
	if l.pos+secondSepPos >= len(l.input) {
		return false
	}

	return l.input[l.pos+secondSepPos] == sep
}

func (l *Lexer) looksLikeVirtualAccount() bool {
	return l.onPostingLine && l.afterIndent
}

var directiveSet = map[string]struct{}{
	"account": {}, "alias": {}, "apply": {}, "assert": {}, "bucket": {}, "capture": {},
	"check": {}, "comment": {}, "commodity": {}, "D": {}, "decimal-mark": {}, "def": {},
	"define": {}, "end": {}, "eval": {}, "expr": {}, "include": {}, "payee": {}, "P": {},
	"tag": {}, "test": {}, "Y": {}, "year": {},
}

func isDirective(word string) bool {
	_, ok := directiveSet[word]
	return ok
}

var subdirectiveSet = map[string]struct{}{
	"alias": {}, "note": {}, "type": {}, "format": {}, "default": {},
}

func isSubdirective(word string) bool {
	_, ok := subdirectiveSet[word]
	return ok
}

func (l *Lexer) scanSubdirectiveValue() Token {
	start := l.pos
	startPos := l.position()

	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == '\n' || ch == ';' {
			break
		}
		l.advance()
	}

	value := strings.TrimSpace(l.input[start:l.pos])
	return Token{Type: TokenText, Value: value, Pos: startPos, End: l.position()}
}
