package server

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
)

const (
	// Custom hledger-specific semantic token types
	TokenTypeAccount   = 0
	TokenTypeCommodity = 1
	TokenTypePayee     = 2
	TokenTypeDate      = 3
	TokenTypeAmount    = 4
	TokenTypeTag       = 5
	TokenTypeDirective = 6
	TokenTypeCode      = 7
	TokenTypeStatus    = 8

	// Standard LSP types (kept for compatibility)
	TokenTypeComment  = 9
	TokenTypeString   = 10
	TokenTypeOperator = 11

	// Additional custom types
	TokenTypeTagValue = 12
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
			// Custom hledger-specific types (indices 0-8)
			"account",
			"commodity",
			"payee",
			"date",
			"amount",
			"tag",
			"directive",
			"code",
			"status",
			// Standard LSP types (indices 9-11)
			protocol.SemanticTokenComment,
			protocol.SemanticTokenString,
			protocol.SemanticTokenOperator,
			// Additional custom types (index 12+)
			"tagValue",
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
			semType = TokenTypePayee
			isPayee = false
		}

		// Handle comments with tags - extract tag tokens
		if tok.Type == parser.TokenComment {
			tagTokens := extractTagTokensFromComment(tok)
			if len(tagTokens) > 0 {
				tokens = append(tokens, tagTokens...)
				continue
			}
		}

		length := uint32(lsputil.UTF16Len(tok.Value))
		if tok.Type == parser.TokenComment {
			length++
		}

		tokens = append(tokens, semanticToken{
			line:      uint32(tok.Pos.Line - 1),
			col:       uint32(tok.Pos.Column - 1),
			length:    length,
			tokenType: semType,
			modifiers: modifiers,
		})
	}

	return tokens
}

func extractTagTokensFromComment(tok parser.Token) []semanticToken {
	commentText := tok.Value
	if !strings.Contains(commentText, ":") {
		return nil
	}

	var tokens []semanticToken
	baseLine := uint32(tok.Pos.Line - 1)
	baseCol := uint32(tok.Pos.Column - 1)

	parts := strings.Split(commentText, ",")
	searchStart := 0

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			continue
		}

		name := strings.TrimSpace(trimmed[:colonIdx])
		if name == "" || !isValidTagName(name) {
			continue
		}

		// Find the position of this tag in the original comment text
		tagStart := strings.Index(commentText[searchStart:], name+":")
		if tagStart == -1 {
			continue
		}
		tagStart += searchStart

		// Tag name with colon: "name:"
		tagNameWithColonLen := uint32(len(name) + 1)

		// +1 to baseCol accounts for the semicolon that starts the comment
		tokens = append(tokens, semanticToken{
			line:      baseLine,
			col:       baseCol + 1 + uint32(tagStart),
			length:    tagNameWithColonLen,
			tokenType: TokenTypeTag,
			modifiers: 0,
		})

		// Tag value (if present)
		if colonIdx+1 < len(trimmed) {
			value := strings.TrimSpace(trimmed[colonIdx+1:])
			if value != "" {
				// Find where the value starts in the original text
				tagNameEnd := tagStart + len(name) + 1
				valueStart := strings.Index(commentText[tagNameEnd:], value)
				if valueStart != -1 {
					tokens = append(tokens, semanticToken{
						line:      baseLine,
						col:       baseCol + 1 + uint32(tagNameEnd+valueStart),
						length:    uint32(len(value)),
						tokenType: TokenTypeTagValue,
						modifiers: 0,
					})
					searchStart = tagNameEnd + valueStart + len(value)
					continue
				}
			}
		}

		searchStart = tagStart + len(name) + 1
	}

	// If no valid tags found, return nil (comment will be handled normally)
	if len(tokens) == 0 {
		return nil
	}

	return tokens
}

func isValidTagName(name string) bool {
	if len(name) == 0 {
		return false
	}
	for _, r := range name {
		// Allow letters (any script: Latin, Cyrillic, CJK, etc.), digits, underscores, and hyphens
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func mapTokenType(t parser.TokenType) (uint32, bool) {
	switch t {
	case parser.TokenDate:
		return TokenTypeDate, true
	case parser.TokenAccount:
		return TokenTypeAccount, true
	case parser.TokenNumber:
		return TokenTypeAmount, true
	case parser.TokenCommodity:
		return TokenTypeCommodity, true
	case parser.TokenComment:
		return TokenTypeComment, true
	case parser.TokenAt, parser.TokenAtAt, parser.TokenEquals, parser.TokenDoubleEquals, parser.TokenPipe:
		return TokenTypeOperator, true
	case parser.TokenText:
		return TokenTypeString, true
	case parser.TokenCode:
		return TokenTypeCode, true
	case parser.TokenStatus:
		return TokenTypeStatus, true
	case parser.TokenDirective:
		return TokenTypeDirective, true
	case parser.TokenTag:
		return TokenTypeTag, true
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
