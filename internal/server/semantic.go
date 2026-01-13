package server

import (
	"context"

	"github.com/juev/hledger-lsp/internal/parser"
	"go.lsp.dev/protocol"
)

const (
	TokenTypeKeyword   = 0
	TokenTypeNamespace = 1
	TokenTypeNumber    = 2
	TokenTypeType      = 3
	TokenTypeComment   = 4
	TokenTypeOperator  = 5
	TokenTypeString    = 6
	TokenTypeModifier  = 7
)

func GetSemanticTokensLegend() protocol.SemanticTokensLegend {
	return protocol.SemanticTokensLegend{
		TokenTypes: []protocol.SemanticTokenTypes{
			protocol.SemanticTokenKeyword,
			protocol.SemanticTokenNamespace,
			protocol.SemanticTokenNumber,
			protocol.SemanticTokenType,
			protocol.SemanticTokenComment,
			protocol.SemanticTokenOperator,
			protocol.SemanticTokenString,
			protocol.SemanticTokenModifier,
		},
		TokenModifiers: []protocol.SemanticTokenModifiers{
			protocol.SemanticTokenModifierDeclaration,
			protocol.SemanticTokenModifierDefinition,
		},
	}
}

type SemanticTokenEncoder struct {
	lastLine uint32
	lastCol  uint32
}

func NewSemanticTokenEncoder() *SemanticTokenEncoder {
	return &SemanticTokenEncoder{}
}

func (e *SemanticTokenEncoder) Encode(line, col, length, tokenType, modifiers uint32) []uint32 {
	deltaLine := line - e.lastLine
	deltaCol := col
	if deltaLine == 0 {
		deltaCol = col - e.lastCol
	}

	e.lastLine = line
	e.lastCol = col

	return []uint32{deltaLine, deltaCol, length, tokenType, modifiers}
}

func (e *SemanticTokenEncoder) Reset() {
	e.lastLine = 0
	e.lastCol = 0
}

func (s *Server) SemanticTokensFull(ctx context.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	if doc == "" {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	tokens := tokenizeForSemantics(doc)
	data := encodeTokens(tokens)

	return &protocol.SemanticTokens{
		Data: data,
	}, nil
}

type semanticToken struct {
	line      uint32
	col       uint32
	length    uint32
	tokenType uint32
	modifiers uint32
}

func tokenizeForSemantics(content string) []semanticToken {
	lexer := parser.NewLexer(content)
	var tokens []semanticToken

	for {
		tok := lexer.Next()
		if tok.Type == parser.TokenEOF {
			break
		}

		semType, ok := mapTokenType(tok.Type)
		if !ok {
			continue
		}

		tokens = append(tokens, semanticToken{
			line:      uint32(tok.Pos.Line - 1),
			col:       uint32(tok.Pos.Column - 1),
			length:    uint32(len(tok.Value)),
			tokenType: semType,
			modifiers: 0,
		})
	}

	return tokens
}

func mapTokenType(t parser.TokenType) (uint32, bool) {
	switch t {
	case parser.TokenDate:
		return TokenTypeKeyword, true
	case parser.TokenAccount:
		return TokenTypeNamespace, true
	case parser.TokenNumber:
		return TokenTypeNumber, true
	case parser.TokenCommodity:
		return TokenTypeType, true
	case parser.TokenComment:
		return TokenTypeComment, true
	case parser.TokenAt, parser.TokenAtAt, parser.TokenEquals, parser.TokenDoubleEquals, parser.TokenPipe:
		return TokenTypeOperator, true
	case parser.TokenText, parser.TokenCode:
		return TokenTypeString, true
	case parser.TokenStatus:
		return TokenTypeModifier, true
	case parser.TokenDirective:
		return TokenTypeKeyword, true
	default:
		return 0, false
	}
}

func encodeTokens(tokens []semanticToken) []uint32 {
	if len(tokens) == 0 {
		return []uint32{}
	}

	encoder := NewSemanticTokenEncoder()
	var data []uint32

	for _, tok := range tokens {
		encoded := encoder.Encode(tok.line, tok.col, tok.length, tok.tokenType, tok.modifiers)
		data = append(data, encoded...)
	}

	return data
}
