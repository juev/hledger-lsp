package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestDefinition_AccountDirective(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food

2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{Line: 3, Character: 6}, // on "expenses:food" in posting
		},
	}

	result, err := srv.Definition(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	assert.Equal(t, uri, result[0].URI)
	assert.Equal(t, uint32(0), result[0].Range.Start.Line) // account directive is on line 0
}

func TestDefinition_AccountFallback(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash

2024-01-16 another
    expenses:food  $30
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{Line: 5, Character: 6}, // second usage of expenses:food
		},
	}

	result, err := srv.Definition(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	assert.Equal(t, uri, result[0].URI)
	assert.Equal(t, uint32(1), result[0].Range.Start.Line) // first usage on line 1
}

func TestDefinition_CommodityDirective(t *testing.T) {
	srv := NewServer()
	content := `commodity $
    format 1,000.00

2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{Line: 4, Character: 19}, // on "$" in posting amount
		},
	}

	result, err := srv.Definition(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	assert.Equal(t, uri, result[0].URI)
	assert.Equal(t, uint32(0), result[0].Range.Start.Line) // commodity directive is on line 0
}

func TestDefinition_Payee(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Grocery Store
    expenses:food  $30
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{Line: 4, Character: 15}, // on second "Grocery Store"
		},
	}

	result, err := srv.Definition(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	assert.Equal(t, uri, result[0].URI)
	assert.Equal(t, uint32(0), result[0].Range.Start.Line) // first transaction on line 0
}

func TestDefinition_UnknownPosition(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{Line: 0, Character: 0}, // on date - not a navigable element
		},
	}

	result, err := srv.Definition(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result) // empty response, no error
}

func TestDefinition_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///nonexistent.journal",
			},
			Position: protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := srv.Definition(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}
