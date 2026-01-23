package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestSemanticTokens_Legend(t *testing.T) {
	legend := GetSemanticTokensLegend()

	assert.NotEmpty(t, legend.TokenTypes)
	// Custom hledger types
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenTypes("account"))
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenTypes("commodity"))
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenTypes("payee"))
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenTypes("date"))
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenTypes("amount"))
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenTypes("tag"))
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenTypes("directive"))
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenTypes("code"))
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenTypes("status"))
	// Standard LSP types
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenComment)
}

func TestSemanticTokens_Encode(t *testing.T) {
	encoder := NewSemanticTokenEncoder()

	data := encoder.Encode(0, 0, 10, 0, 0)
	assert.Equal(t, []uint32{0, 0, 10, 0, 0}, data)

	data = encoder.Encode(0, 11, 1, 1, 0)
	assert.Equal(t, []uint32{0, 11, 1, 1, 0}, data)

	data = encoder.Encode(1, 4, 13, 1, 0)
	assert.Equal(t, []uint32{1, 4, 13, 1, 0}, data)
}

func TestSemanticTokens_SimpleTransaction(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.SemanticTokensFull(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Data)
	assert.Equal(t, 0, len(result.Data)%5)
}

func TestSemanticTokens_EmptyDocument(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), "")

	params := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.SemanticTokensFull(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Data)
}

func TestSemanticTokens_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///nonexistent.journal",
		},
	}

	result, err := srv.SemanticTokensFull(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Data)
}

func TestSemanticTokens_CapabilityRegistration(t *testing.T) {
	srv := NewServer()
	params := &protocol.InitializeParams{}

	result, err := srv.Initialize(context.Background(), params)
	require.NoError(t, err)

	opts, ok := result.Capabilities.SemanticTokensProvider.(*SemanticTokensServerCapabilities)
	require.True(t, ok, "SemanticTokensProvider should be *SemanticTokensServerCapabilities")

	assert.NotEmpty(t, opts.Legend.TokenTypes)
	assert.NotEmpty(t, opts.Legend.TokenModifiers)
	assert.NotNil(t, opts.Full)
	assert.True(t, opts.Range)
}

func TestSemanticTokens_TokenTypes(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantType uint32
	}{
		{
			name:     "directive uses directive type",
			content:  "account expenses:food",
			wantType: TokenTypeDirective,
		},
		{
			name:     "date uses date type",
			content:  "2024-01-15 test",
			wantType: TokenTypeDate,
		},
		{
			name:     "payee uses payee type",
			content:  "2024-01-15 grocery store",
			wantType: TokenTypePayee,
		},
		{
			name:     "code uses code type",
			content:  "2024-01-15 (123) test",
			wantType: TokenTypeCode,
		},
		{
			name:     "account uses account type",
			content:  "2024-01-15 test\n    expenses:food  $50",
			wantType: TokenTypeAccount,
		},
		{
			name:     "amount uses amount type",
			content:  "2024-01-15 test\n    expenses:food  50",
			wantType: TokenTypeAmount,
		},
		{
			name:     "commodity uses commodity type",
			content:  "2024-01-15 test\n    expenses:food  $50",
			wantType: TokenTypeCommodity,
		},
		{
			name:     "status uses status type",
			content:  "2024-01-15 * test",
			wantType: TokenTypeStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			found := false
			for _, tok := range tokens {
				if tok.tokenType == tt.wantType {
					found = true
					break
				}
			}
			assert.True(t, found, "expected token type %d not found in tokens", tt.wantType)
		})
	}
}

func TestSemanticTokens_DeclarationModifier(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "account directive marks account as declaration",
			content: "account expenses:food",
		},
		{
			name:    "commodity directive marks commodity as declaration",
			content: "commodity USD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			foundDeclaration := false
			for _, tok := range tokens {
				if tok.modifiers&(1<<ModifierDeclaration) != 0 {
					foundDeclaration = true
					break
				}
			}
			assert.True(t, foundDeclaration, "expected declaration modifier not found")
		})
	}
}

func TestSemanticTokens_Range(t *testing.T) {
	srv := NewServer()
	content := `2024-01-01 tx1
    expenses:food  $10
2024-01-02 tx2
    expenses:rent  $100
2024-01-03 tx3
    assets:cash  $50`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: 2, Character: 0},
			End:   protocol.Position{Line: 3, Character: 100},
		},
	}

	result, err := srv.SemanticTokensRange(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Data)

	fullParams := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	fullResult, err := srv.SemanticTokensFull(context.Background(), fullParams)
	require.NoError(t, err)

	assert.Less(t, len(result.Data), len(fullResult.Data),
		"range result should have fewer tokens than full result")
}

func TestSemanticTokens_Delta(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")

	content1 := `2024-01-01 test
    expenses:food  $10`
	srv.documents.Store(uri, content1)

	fullParams := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	fullResult, err := srv.SemanticTokensFull(context.Background(), fullParams)
	require.NoError(t, err)
	require.NotEmpty(t, fullResult.ResultID)

	content2 := `2024-01-01 test
    expenses:food  $20`
	srv.documents.Store(uri, content2)

	deltaParams := &protocol.SemanticTokensDeltaParams{
		TextDocument:     protocol.TextDocumentIdentifier{URI: uri},
		PreviousResultID: fullResult.ResultID,
	}

	deltaResult, err := srv.SemanticTokensFullDelta(context.Background(), deltaParams)
	require.NoError(t, err)
	require.NotNil(t, deltaResult)

	switch result := deltaResult.(type) {
	case *protocol.SemanticTokens:
		assert.NotEmpty(t, result.Data)
	case *protocol.SemanticTokensDelta:
		assert.NotNil(t, result.Edits)
	default:
		t.Fatalf("unexpected result type: %T", deltaResult)
	}
}

func TestSemanticTokens_CommentLength(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantLength uint32
	}{
		{
			name:       "single semicolon comment",
			content:    "; test",
			wantLength: 6,
		},
		{
			name:       "double semicolon comment",
			content:    ";; test",
			wantLength: 7,
		},
		{
			name:       "triple semicolon comment",
			content:    ";;; test",
			wantLength: 8,
		},
		{
			name:       "double semicolon with date",
			content:    ";;  01-12",
			wantLength: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			var commentToken *semanticToken
			for i := range tokens {
				if tokens[i].tokenType == TokenTypeComment {
					commentToken = &tokens[i]
					break
				}
			}
			require.NotNil(t, commentToken, "comment token not found")
			assert.Equal(t, tt.wantLength, commentToken.length,
				"comment length mismatch for %q", tt.content)
		})
	}
}

func TestSemanticTokens_TagsInComments(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantTags   int
		wantTagPos []struct {
			col    uint32
			length uint32
		}
	}{
		{
			name:     "single tag with value",
			content:  "; client:acme",
			wantTags: 1,
			wantTagPos: []struct {
				col    uint32
				length uint32
			}{
				{col: 2, length: 7}, // "client:" = 6+1 = 7 (value is now separate token)
			},
		},
		{
			name:     "multiple tags",
			content:  "; client:acme, project:alpha",
			wantTags: 2,
			wantTagPos: []struct {
				col    uint32
				length uint32
			}{
				{col: 2, length: 7},  // "client:" = 6+1 = 7
				{col: 15, length: 8}, // "project:" = 7+1 = 8
			},
		},
		{
			name:     "tag without value",
			content:  "; billable:",
			wantTags: 1,
			wantTagPos: []struct {
				col    uint32
				length uint32
			}{
				{col: 2, length: 9}, // "billable:" = 8+1 = 9
			},
		},
		{
			name:     "tag in transaction comment",
			content:  "2024-01-15 test  ; date:2024-01-20",
			wantTags: 1,
		},
		{
			name:     "unicode tag name (cyrillic)",
			content:  "; клиент:acme",
			wantTags: 1,
		},
		{
			name:     "unicode tag name (chinese)",
			content:  "; 项目:测试",
			wantTags: 1,
		},
		{
			name:     "date tag with space after colon",
			content:  "; date: 2024-01-20",
			wantTags: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens, "expected tokens")

			var tagTokens []semanticToken
			for _, tok := range tokens {
				if tok.tokenType == TokenTypeTag {
					tagTokens = append(tagTokens, tok)
				}
			}

			assert.Len(t, tagTokens, tt.wantTags, "expected %d tag tokens, got %d", tt.wantTags, len(tagTokens))

			if tt.wantTagPos != nil {
				for i, pos := range tt.wantTagPos {
					if i < len(tagTokens) {
						assert.Equal(t, pos.col, tagTokens[i].col, "tag %d column mismatch", i)
						assert.Equal(t, pos.length, tagTokens[i].length, "tag %d length mismatch", i)
					}
				}
			}
		})
	}
}

func TestSemanticTokens_TagNameAndValueSeparate(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		wantTagTokens   int
		wantValueTokens int
		wantPositions   []struct {
			tokenType uint32
			col       uint32
			length    uint32
		}
	}{
		{
			name:            "tag with value split into two tokens",
			content:         "; client:acme",
			wantTagTokens:   1,
			wantValueTokens: 1,
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 7},      // "client:" = 7
				{tokenType: TokenTypeTagValue, col: 9, length: 4}, // "acme" = 4
			},
		},
		{
			name:            "tag without value has no value token",
			content:         "; billable:",
			wantTagTokens:   1,
			wantValueTokens: 0,
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 9}, // "billable:" = 9
			},
		},
		{
			name:            "multiple tags with values",
			content:         "; client:acme, project:alpha",
			wantTagTokens:   2,
			wantValueTokens: 2,
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 7},       // "client:" = 7
				{tokenType: TokenTypeTagValue, col: 9, length: 4},  // "acme" = 4
				{tokenType: TokenTypeTag, col: 15, length: 8},      // "project:" = 8
				{tokenType: TokenTypeTagValue, col: 23, length: 5}, // "alpha" = 5
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens, "expected tokens")

			var tagTokens, valueTokens []semanticToken
			for _, tok := range tokens {
				if tok.tokenType == TokenTypeTag {
					tagTokens = append(tagTokens, tok)
				}
				if tok.tokenType == TokenTypeTagValue {
					valueTokens = append(valueTokens, tok)
				}
			}

			assert.Len(t, tagTokens, tt.wantTagTokens, "tag token count mismatch")
			assert.Len(t, valueTokens, tt.wantValueTokens, "value token count mismatch")

			if tt.wantPositions != nil {
				allTagTokens := make([]semanticToken, 0)
				for _, tok := range tokens {
					if tok.tokenType == TokenTypeTag || tok.tokenType == TokenTypeTagValue {
						allTagTokens = append(allTagTokens, tok)
					}
				}

				for i, pos := range tt.wantPositions {
					if i < len(allTagTokens) {
						assert.Equal(t, pos.tokenType, allTagTokens[i].tokenType, "token %d type mismatch", i)
						assert.Equal(t, pos.col, allTagTokens[i].col, "token %d column mismatch", i)
						assert.Equal(t, pos.length, allTagTokens[i].length, "token %d length mismatch", i)
					}
				}
			}
		})
	}
}
