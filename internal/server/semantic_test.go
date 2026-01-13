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
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenKeyword)
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenNamespace)
	assert.Contains(t, legend.TokenTypes, protocol.SemanticTokenNumber)
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
