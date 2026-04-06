package server

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/rules"
)

const (
	// All indices match the legend order in GetSemanticTokensLegend.
	TokenTypeAccount        = 0  // namespace
	TokenTypeCommodity      = 1  // type
	TokenTypePayee          = 2  // function
	TokenTypeDate           = 3  // number
	TokenTypeAmount         = 4  // number
	TokenTypeTag            = 5  // decorator
	TokenTypeDirective      = 6  // keyword (shared with rules keyword)
	TokenTypeCode           = 7  // string (shared with tagValue)
	TokenTypeStatus         = 8  // operator (shared with standard operator)
	TokenTypeComment        = 9  // comment
	TokenTypeString         = 7  // string (same index as code)
	TokenTypeOperator       = 8  // operator (same index as status)
	TokenTypeTagValue       = 7  // string (same index as code)
	TokenTypeAccountVirtual = 0  // namespace (same as account; distinguished by abstract modifier)
	TokenTypeNote           = 9  // comment (same index as comment)
	TokenTypeRulesKeyword   = 6  // keyword (same index as directive)
	TokenTypeRulesRegexp    = 10 // regexp
	TokenTypeRulesParameter = 11 // parameter
)

const (
	ModifierDeclaration = 0
	ModifierDefinition  = 1
	ModifierAbstract    = 5
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
			protocol.SemanticTokenNamespace,          // 0: account, accountVirtual
			protocol.SemanticTokenType,               // 1: commodity
			protocol.SemanticTokenFunction,           // 2: payee
			protocol.SemanticTokenNumber,             // 3: date (both date and amount use number superType)
			protocol.SemanticTokenNumber,             // 4: amount
			protocol.SemanticTokenTypes("decorator"), // 5: tag
			protocol.SemanticTokenKeyword,            // 6: directive, rules keyword
			protocol.SemanticTokenString,             // 7: code, tagValue, text
			protocol.SemanticTokenOperator,           // 8: status, operator
			protocol.SemanticTokenComment,            // 9: comment, note
			protocol.SemanticTokenRegexp,             // 10: rules regexp
			protocol.SemanticTokenParameter,          // 11: rules parameter
		},
		TokenModifiers: []protocol.SemanticTokenModifiers{
			protocol.SemanticTokenModifierDeclaration, // 0
			protocol.SemanticTokenModifierDefinition,  // 1
			protocol.SemanticTokenModifierReadonly,    // 2
			protocol.SemanticTokenModifierStatic,      // 3
			protocol.SemanticTokenModifierDeprecated,  // 4
			protocol.SemanticTokenModifierAbstract,    // 5: virtual accounts
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

	tokens := tokenizeDoc(params.TextDocument.URI, doc)
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

	allTokens := tokenizeDoc(params.TextDocument.URI, doc)
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

	tokens := tokenizeDoc(params.TextDocument.URI, doc)
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

// tokenizeDoc dispatches to the appropriate tokenizer based on file type.
func tokenizeDoc(uri protocol.DocumentURI, doc string) []semanticToken {
	if filetype.IsRules(string(uri)) {
		return tokenizeRulesForSemantics(doc)
	}
	return tokenizeForSemantics(doc)
}

// tokenizeRulesForSemantics converts rules semantic tokens to the server's token format.
func tokenizeRulesForSemantics(doc string) []semanticToken {
	ruleTokens := rules.SemanticTokens(doc)
	tokens := make([]semanticToken, 0, len(ruleTokens))
	for _, rt := range ruleTokens {
		tokens = append(tokens, semanticToken{
			line:      rt.Line,
			col:       rt.Col,
			length:    rt.Length,
			tokenType: rulesTokenTypeToServer(rt.TokenType),
			modifiers: 0,
		})
	}
	return tokens
}

func rulesTokenTypeToServer(t rules.SemTokenType) uint32 {
	switch t {
	case rules.SemTokenKeyword:
		return TokenTypeRulesKeyword
	case rules.SemTokenDirective:
		return TokenTypeDirective
	case rules.SemTokenRegexp:
		return TokenTypeRulesRegexp
	case rules.SemTokenParameter:
		return TokenTypeRulesParameter
	case rules.SemTokenComment:
		return TokenTypeComment
	case rules.SemTokenString:
		return TokenTypeString
	default:
		return TokenTypeString
	}
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
	inSubdirective := false
	isPayee := false
	currentLine := -1
	inVirtualContext := false
	seenPipe := false
	isTransactionHeader := false
	inCommentBlock := false

	for {
		tok := lexer.Next()
		if tok.Type == parser.TokenEOF {
			break
		}

		// Handle comment block: detect "end comment" to exit
		if inCommentBlock {
			if tok.Type == parser.TokenDirective && tok.Value == "end" {
				// Peek at next token to check for "comment"
				next := lexer.Next()
				if next.Type != parser.TokenEOF &&
					strings.TrimSpace(next.Value) == "comment" {
					// End of comment block — emit both as directive tokens
					inCommentBlock = false
					tokens = append(tokens, semanticToken{
						line:      uint32(tok.Pos.Line - 1),
						col:       uint32(tok.Pos.Column - 1),
						length:    uint32(lsputil.UTF16Len(tok.Value)),
						tokenType: TokenTypeDirective,
						modifiers: 0,
					})
					tokens = append(tokens, semanticToken{
						line:      uint32(next.Pos.Line - 1),
						col:       uint32(next.Pos.Column - 1),
						length:    uint32(lsputil.UTF16Len(next.Value)),
						tokenType: TokenTypeDirective,
						modifiers: 0,
					})
					continue
				}
				// Not "end comment" — emit "end" as comment, then handle next token
				tokens = append(tokens, semanticToken{
					line:      uint32(tok.Pos.Line - 1),
					col:       uint32(tok.Pos.Column - 1),
					length:    uint32(lsputil.UTF16Len(tok.Value)),
					tokenType: TokenTypeComment,
					modifiers: 0,
				})
				// Fall through to handle next token (which is not "comment")
				tok = next
				if tok.Type == parser.TokenEOF {
					break
				}
			}

			// Inside comment block — emit as comment (skip newlines)
			if tok.Type == parser.TokenNewline {
				continue
			}
			length := uint32(lsputil.UTF16Len(tok.Value))
			if tok.Type == parser.TokenComment {
				length++
			}
			tokens = append(tokens, semanticToken{
				line:      uint32(tok.Pos.Line - 1),
				col:       uint32(tok.Pos.Column - 1),
				length:    length,
				tokenType: TokenTypeComment,
				modifiers: 0,
			})
			continue
		}

		if tok.Pos.Line != currentLine {
			currentLine = tok.Pos.Line
			inVirtualContext = false
			seenPipe = false
			isTransactionHeader = false
			inSubdirective = false
			if tok.Type == parser.TokenDirective {
				inDirective = true
				directiveType = tok.Value
				// Enter comment block mode
				if tok.Value == "comment" {
					inCommentBlock = true
					// Emit "comment" directive token
					tokens = append(tokens, semanticToken{
						line:      uint32(tok.Pos.Line - 1),
						col:       uint32(tok.Pos.Column - 1),
						length:    uint32(lsputil.UTF16Len(tok.Value)),
						tokenType: TokenTypeDirective,
						modifiers: 0,
					})
					continue
				}
			} else if tok.Type == parser.TokenDate {
				inDirective = false
				directiveType = ""
				isPayee = true
				isTransactionHeader = true
			} else if tok.Type != parser.TokenIndent && tok.Type != parser.TokenNewline {
				inDirective = false
				directiveType = ""
			}
		}

		// Handle standalone "end comment" (outside block) — both tokens as directive
		if tok.Type == parser.TokenDirective && tok.Value == "end" {
			next := lexer.Next()
			if next.Type != parser.TokenEOF &&
				strings.TrimSpace(next.Value) == "comment" {
				tokens = append(tokens, semanticToken{
					line:      uint32(tok.Pos.Line - 1),
					col:       uint32(tok.Pos.Column - 1),
					length:    uint32(lsputil.UTF16Len(tok.Value)),
					tokenType: TokenTypeDirective,
					modifiers: 0,
				})
				tokens = append(tokens, semanticToken{
					line:      uint32(next.Pos.Line - 1),
					col:       uint32(next.Pos.Column - 1),
					length:    uint32(lsputil.UTF16Len(next.Value)),
					tokenType: TokenTypeDirective,
					modifiers: 0,
				})
				continue
			}
			// Not "end comment" — process "end" normally, then handle next
			semType, ok := mapTokenType(tok.Type)
			if ok {
				tokens = append(tokens, semanticToken{
					line:      uint32(tok.Pos.Line - 1),
					col:       uint32(tok.Pos.Column - 1),
					length:    uint32(lsputil.UTF16Len(tok.Value)),
					tokenType: semType,
					modifiers: 0,
				})
			}
			tok = next
			if tok.Type == parser.TokenEOF {
				break
			}
		}

		if tok.Type == parser.TokenDirective && inDirective && tok.Value != directiveType {
			inSubdirective = true
		}

		// Track virtual account context
		switch tok.Type {
		case parser.TokenLParen, parser.TokenLBracket:
			inVirtualContext = true
		case parser.TokenRParen, parser.TokenRBracket:
			inVirtualContext = false
		}

		// Track pipe separator on transaction header line
		if tok.Type == parser.TokenPipe && isTransactionHeader {
			seenPipe = true
			isPayee = false
		}

		semType, ok := mapTokenType(tok.Type)
		if !ok {
			continue
		}

		modifiers := uint32(0)
		if inDirective && !inSubdirective && (directiveType == "account" || directiveType == "commodity") {
			if tok.Type == parser.TokenAccount || tok.Type == parser.TokenCommodity || tok.Type == parser.TokenQuotedCommodity || tok.Type == parser.TokenText {
				modifiers = 1 << ModifierDeclaration
			}
			if tok.Type == parser.TokenText {
				switch directiveType {
				case "account":
					semType = TokenTypeAccount
				case "commodity":
					semType = TokenTypeCommodity
				}
			}
		}

		// Handle virtual accounts
		if tok.Type == parser.TokenAccount && inVirtualContext {
			modifiers |= 1 << ModifierAbstract
		}

		// Handle transaction header: payee and note
		if tok.Type == parser.TokenText && isTransactionHeader {
			if isPayee && !seenPipe {
				semType = TokenTypePayee
				isPayee = false
			} else if seenPipe {
				semType = TokenTypeNote
			}
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
		if tok.Type == parser.TokenQuotedCommodity {
			length += 2 // surrounding double quotes
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

		tagNameWithColonLen := uint32(lsputil.UTF16Len(name) + 1)
		tagColUTF16 := uint32(lsputil.UTF16Len(commentText[:tagStart]))

		// +1 to baseCol accounts for the semicolon that starts the comment
		tokens = append(tokens, semanticToken{
			line:      baseLine,
			col:       baseCol + 1 + tagColUTF16,
			length:    tagNameWithColonLen,
			tokenType: TokenTypeTag,
			modifiers: 0,
		})

		// Tag value (if present)
		if colonIdx+1 < len(trimmed) {
			value := parser.ExtractTagValue(trimmed[colonIdx+1:])
			if value != "" {
				// Find where the value starts in the original text
				tagNameEnd := tagStart + len(name) + 1
				valueStart := strings.Index(commentText[tagNameEnd:], value)
				if valueStart != -1 {
					valueColUTF16 := uint32(lsputil.UTF16Len(commentText[:tagNameEnd+valueStart]))
					tokens = append(tokens, semanticToken{
						line:      baseLine,
						col:       baseCol + 1 + valueColUTF16,
						length:    uint32(lsputil.UTF16Len(value)),
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
	case parser.TokenCommodity, parser.TokenQuotedCommodity:
		return TokenTypeCommodity, true
	case parser.TokenComment:
		return TokenTypeComment, true
	case parser.TokenAt, parser.TokenAtAt, parser.TokenEquals, parser.TokenDoubleEquals, parser.TokenEqualsStar, parser.TokenDoubleEqualsStar, parser.TokenPipe, parser.TokenLBrace, parser.TokenDoubleLBrace, parser.TokenRBrace, parser.TokenDoubleRBrace:
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
