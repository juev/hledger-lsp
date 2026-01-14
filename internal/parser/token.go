package parser

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenNewline
	TokenIndent
	TokenDate
	TokenStatus
	TokenCode
	TokenText
	TokenAccount
	TokenNumber
	TokenCommodity
	TokenComment
	TokenDirective
	TokenTag
	TokenAt
	TokenAtAt
	TokenEquals
	TokenDoubleEquals
	TokenLParen
	TokenRParen
	TokenLBracket
	TokenRBracket
	TokenPipe
	TokenColon
	TokenSemicolon
	TokenSign // + or - before commodity symbol
)

type Position struct {
	Line   int
	Column int
	Offset int
}

type Token struct {
	Type  TokenType
	Value string
	Pos   Position
	End   Position
}

func (t TokenType) String() string {
	names := []string{
		"EOF", "Newline", "Indent", "Date", "Status", "Code",
		"Text", "Account", "Number", "Commodity", "Comment",
		"Directive", "Tag", "At", "AtAt", "Equals", "DoubleEquals",
		"LParen", "RParen", "LBracket", "RBracket", "Pipe", "Colon", "Semicolon",
		"Sign",
	}
	if int(t) < len(names) {
		return names[t]
	}
	return "Unknown"
}
