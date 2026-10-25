package rules

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenNewline
	TokenIndent
	TokenComment
	TokenDirective
	TokenIfKeyword
	TokenEndKeyword
	TokenFieldName
	TokenText
	TokenRegex
	TokenSeparator
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
		"EOF", "Newline", "Indent", "Comment", "Directive",
		"IfKeyword", "EndKeyword", "FieldName", "Text", "Regex", "Separator",
	}
	if int(t) < len(names) {
		return names[t]
	}
	return "Unknown"
}
