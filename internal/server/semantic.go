package server

import (
	"context"
	"strconv"
	"sync"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
)

const (
	TokenTypeNamespace = 0
	TokenTypeNumber    = 1
	TokenTypeType      = 2
	TokenTypeComment   = 3
	TokenTypeOperator  = 4
	TokenTypeString    = 5
	TokenTypeFunction  = 6
	TokenTypeProperty  = 7
	TokenTypeMacro     = 8
	TokenTypeVariable  = 9
)

const (
	ModifierDeclaration = 0
	ModifierDefinition  = 1
)

type SemanticTokensFullOptions struct {
	Delta bool `json:"delta,omitempty"`
}

type SemanticTokensServerCapabilities struct {
	Legend protocol.SemanticTokensLegend `json:"legend"`
	Range  bool                          `json:"range,omitempty"`
	Full   *SemanticTokensFullOptions    `json:"full,omitempty"`
}

func GetSemanticTokensLegend() protocol.SemanticTokensLegend {
	return protocol.SemanticTokensLegend{
		TokenTypes: []protocol.SemanticTokenTypes{
			protocol.SemanticTokenNamespace,
			protocol.SemanticTokenNumber,
			protocol.SemanticTokenType,
			protocol.SemanticTokenComment,
			protocol.SemanticTokenOperator,
			protocol.SemanticTokenString,
			protocol.SemanticTokenFunction,
			protocol.SemanticTokenProperty,
			protocol.SemanticTokenMacro,
			protocol.SemanticTokenVariable,
		},
		TokenModifiers: []protocol.SemanticTokenModifiers{
			protocol.SemanticTokenModifierDeclaration,
			protocol.SemanticTokenModifierDefinition,
		},
	}
}

func GetSemanticTokensCapabilities() *SemanticTokensServerCapabilities {
	return &SemanticTokensServerCapabilities{
		Legend: GetSemanticTokensLegend(),
		Range:  true,
		Full:   &SemanticTokensFullOptions{Delta: true},
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

type semanticTokensCache struct {
	mu       sync.RWMutex
	cache    map[protocol.DocumentURI]*cachedSemanticTokens
	resultID uint64
}

type cachedSemanticTokens struct {
	resultID string
	tokens   []semanticToken
	data     []uint32
}

var tokenCache = &semanticTokensCache{
	cache: make(map[protocol.DocumentURI]*cachedSemanticTokens),
}

func (c *semanticTokensCache) get(uri protocol.DocumentURI) (*cachedSemanticTokens, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cached, ok := c.cache[uri]
	return cached, ok
}

func (c *semanticTokensCache) set(uri protocol.DocumentURI, tokens []semanticToken, data []uint32) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resultID++
	resultID := strconv.FormatUint(c.resultID, 10)
	c.cache[uri] = &cachedSemanticTokens{
		resultID: resultID,
		tokens:   tokens,
		data:     data,
	}
	return resultID
}

func (c *semanticTokensCache) delete(uri protocol.DocumentURI) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, uri)
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
	resultID := tokenCache.set(params.TextDocument.URI, tokens, data)

	return &protocol.SemanticTokens{
		ResultID: resultID,
		Data:     data,
	}, nil
}

func (s *Server) SemanticTokensRange(ctx context.Context, params *protocol.SemanticTokensRangeParams) (*protocol.SemanticTokens, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	if doc == "" {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	allTokens := tokenizeForSemantics(doc)
	filteredTokens := filterTokensByRange(allTokens, params.Range)
	data := encodeTokens(filteredTokens)

	return &protocol.SemanticTokens{
		Data: data,
	}, nil
}

func (s *Server) SemanticTokensFullDelta(ctx context.Context, params *protocol.SemanticTokensDeltaParams) (any, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	if doc == "" {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	tokens := tokenizeForSemantics(doc)
	newData := encodeTokens(tokens)

	cached, ok := tokenCache.get(params.TextDocument.URI)
	if !ok || cached.resultID != params.PreviousResultID {
		resultID := tokenCache.set(params.TextDocument.URI, tokens, newData)
		return &protocol.SemanticTokens{
			ResultID: resultID,
			Data:     newData,
		}, nil
	}

	edits := computeSemanticTokensEdits(cached.data, newData)
	resultID := tokenCache.set(params.TextDocument.URI, tokens, newData)

	return &protocol.SemanticTokensDelta{
		ResultID: resultID,
		Edits:    edits,
	}, nil
}

func filterTokensByRange(tokens []semanticToken, r protocol.Range) []semanticToken {
	var filtered []semanticToken
	for _, tok := range tokens {
		if tok.line >= r.Start.Line && tok.line <= r.End.Line {
			filtered = append(filtered, tok)
		}
	}
	return filtered
}

func computeSemanticTokensEdits(oldData, newData []uint32) []protocol.SemanticTokensEdit {
	if len(oldData) == len(newData) {
		same := true
		for i := range oldData {
			if oldData[i] != newData[i] {
				same = false
				break
			}
		}
		if same {
			return []protocol.SemanticTokensEdit{}
		}
	}

	return []protocol.SemanticTokensEdit{
		{
			Start:       0,
			DeleteCount: uint32(len(oldData)),
			Data:        newData,
		},
	}
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

	inDirective := false
	directiveType := ""
	isPayee := false
	currentLine := -1

	for {
		tok := lexer.Next()
		if tok.Type == parser.TokenEOF {
			break
		}

		if tok.Pos.Line != currentLine {
			currentLine = tok.Pos.Line
			if tok.Type == parser.TokenDirective {
				inDirective = true
				directiveType = tok.Value
			} else if tok.Type == parser.TokenDate {
				inDirective = false
				directiveType = ""
				isPayee = true
			} else if tok.Type != parser.TokenIndent && tok.Type != parser.TokenNewline {
				inDirective = false
				directiveType = ""
			}
		}

		semType, ok := mapTokenType(tok.Type)
		if !ok {
			continue
		}

		modifiers := uint32(0)
		if inDirective && (directiveType == "account" || directiveType == "commodity") {
			if tok.Type == parser.TokenAccount || tok.Type == parser.TokenCommodity || tok.Type == parser.TokenText {
				modifiers = 1 << ModifierDeclaration
			}
		}

		if tok.Type == parser.TokenText && isPayee {
			semType = TokenTypeFunction
			isPayee = false
		}

		tokens = append(tokens, semanticToken{
			line:      uint32(tok.Pos.Line - 1),
			col:       uint32(tok.Pos.Column - 1),
			length:    uint32(lsputil.UTF16Len(tok.Value)),
			tokenType: semType,
			modifiers: modifiers,
		})
	}

	return tokens
}

func mapTokenType(t parser.TokenType) (uint32, bool) {
	switch t {
	case parser.TokenDate:
		return TokenTypeNumber, true
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
	case parser.TokenText:
		return TokenTypeString, true
	case parser.TokenCode:
		return TokenTypeVariable, true
	case parser.TokenStatus:
		return TokenTypeOperator, true
	case parser.TokenDirective:
		return TokenTypeMacro, true
	case parser.TokenTag:
		return TokenTypeProperty, true
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
