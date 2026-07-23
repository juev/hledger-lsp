package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestDocumentHighlight_Account(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food

2024-01-15 grocery
    expenses:food  $50
    assets:cash

2024-01-16 another
    expenses:food  $30
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 3, Character: 6},
		},
	}

	result, err := srv.DocumentHighlight(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 3) // directive + 2 postings
}

func TestDocumentHighlight_Commodity(t *testing.T) {
	srv := NewServer()
	content := `commodity $

2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 3, Character: 19},
		},
	}

	result, err := srv.DocumentHighlight(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 3) // directive + 2 amounts
}

func TestDocumentHighlight_Payee(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash

2024-01-16 grocery
    expenses:food  $30
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 0, Character: 13},
		},
	}

	result, err := srv.DocumentHighlight(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2) // 2 transactions
}

func TestDocumentHighlight_EmptyPosition(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := srv.DocumentHighlight(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestDocumentHighlight_UnknownDocument(t *testing.T) {
	srv := NewServer()

	params := &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///nonexistent.journal"},
			Position:     protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := srv.DocumentHighlight(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}
