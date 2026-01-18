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
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenNamespace)
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenNumber)
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenComment)
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenMacro)
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenFunction)
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenProperty)
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
			name:     "directive uses macro type",
			content:  "account expenses:food",
			wantType: TokenTypeMacro,
		},
		{
			name:     "date uses number type",
			content:  "2024-01-15 test",
			wantType: TokenTypeNumber,
		},
		{
			name:     "payee uses function type",
			content:  "2024-01-15 grocery store",
			wantType: TokenTypeFunction,
		},
		{
			name:     "code uses variable type",
			content:  "2024-01-15 (123) test",
			wantType: TokenTypeVariable,
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
