package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestInlineCompletion_ReturnsEmpty(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 Grocery Store
    `

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 5, Character: 4},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "inline completion returns empty (templates moved to regular completion)")
}

func TestInlineCompletion_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///nonexistent.journal"},
		Position:     protocol.Position{Line: 0, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "should return empty for nonexistent document")
}

func TestInlineCompletion_InvalidParams(t *testing.T) {
	srv := NewServer()

	result, err := srv.InlineCompletion(context.Background(), []byte("invalid json"))

	assert.Error(t, err)
	assert.Nil(t, result)
}
