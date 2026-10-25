package rules

import (
	"strings"

	"github.com/juev/hledger-lsp/internal/lsputil"
)

// SemTokenType represents semantic token type indices for rules files.
// These are remapped via rulesTokenTypeToServer() to the server's token type legend.
type SemTokenType uint32

const (
	SemTokenKeyword   SemTokenType = 0 // if, end
	SemTokenRegexp    SemTokenType = 1 // regex patterns in if blocks
	SemTokenParameter SemTokenType = 2 // field names
	SemTokenComment   SemTokenType = 3 // comment lines
	SemTokenString    SemTokenType = 4 // field values / text
	SemTokenDirective SemTokenType = 5 // directives (skip, fields, date-format, ...)
)

// RulesSemanticToken is a single semantic token for a rules file.
type RulesSemanticToken struct {
	Line      uint32
	Col       uint32
	Length    uint32
	TokenType SemTokenType
}

// SemanticTokens returns semantic tokens for a rules file content.
func SemanticTokens(content string) []RulesSemanticToken {
	lexer := NewLexer(content)
	lines := strings.Split(content, "\n")
	var tokens []RulesSemanticToken

	for {
		tok := lexer.Next()
		if tok.Type == TokenEOF {
			break
		}

		line := uint32(tok.Pos.Line - 1)
		byteCol := tok.Pos.Column - 1
		lineText := ""
		if int(line) < len(lines) {
			lineText = strings.TrimRight(lines[line], "\r")
		}
		col := uint32(lsputil.ByteOffsetToUTF16(lineText, byteCol))
		length := uint32(lsputil.UTF16Len(tok.Value))

		switch tok.Type {
		case TokenComment:
			tokens = append(tokens, RulesSemanticToken{
				Line:      line,
				Col:       col,
				Length:    length,
				TokenType: SemTokenComment,
			})

		case TokenDirective:
			tokens = append(tokens, RulesSemanticToken{
				Line:      line,
				Col:       col,
				Length:    length,
				TokenType: SemTokenDirective,
			})

		case TokenIfKeyword, TokenEndKeyword:
			tokens = append(tokens, RulesSemanticToken{
				Line:      line,
				Col:       col,
				Length:    length,
				TokenType: SemTokenKeyword,
			})

		case TokenFieldName:
			tokens = append(tokens, RulesSemanticToken{
				Line:      line,
				Col:       col,
				Length:    length,
				TokenType: SemTokenParameter,
			})

		case TokenRegex:
			tokens = append(tokens, RulesSemanticToken{
				Line:      line,
				Col:       col,
				Length:    length,
				TokenType: SemTokenRegexp,
			})

		case TokenText:
			tokens = append(tokens, RulesSemanticToken{
				Line:      line,
				Col:       col,
				Length:    length,
				TokenType: SemTokenString,
			})

		case TokenNewline, TokenIndent, TokenSeparator:
			// intentionally skipped: structural tokens carry no semantic meaning
		}
	}

	return tokens
}
