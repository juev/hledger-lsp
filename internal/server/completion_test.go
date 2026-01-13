package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestCompletion_Accounts(t *testing.T) {
	srv := NewServer()
	content := `account assets:cash
account expenses:food

2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 5, Character: 4},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "assets:cash")
	assert.Contains(t, labels, "expenses:food")
}

func TestCompletion_AccountsByPrefix(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food:groceries  $30
    expenses:food:restaurant  $20
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 14},
		},
		Context: &protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: ":",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "expenses:food:groceries")
	assert.Contains(t, labels, "expenses:food:restaurant")
}

func TestCompletion_Payees(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Coffee Shop
    expenses:food  $5
    assets:cash

2024-01-17 `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 8, Character: 11},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "Grocery Store")
	assert.Contains(t, labels, "Coffee Shop")
}

func TestCompletion_Commodities(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    expenses:rent  EUR 100
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 20},
		},
		Context: &protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: "@",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "$")
	assert.Contains(t, labels, "EUR")
}

func TestCompletion_EmptyDocument(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), "")

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Items)
}

func TestCompletion_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///nonexistent.journal",
			},
			Position: protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Items)
}

func extractLabels(items []protocol.CompletionItem) []string {
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}
	return labels
}
