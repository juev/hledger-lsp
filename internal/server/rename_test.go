package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestPrepareRename_Account(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.PrepareRenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 1, Character: 10},
		},
	}

	result, err := srv.PrepareRename(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, uint32(1), result.Start.Line)
	assert.Equal(t, uint32(4), result.Start.Character)
}

func TestPrepareRename_InvalidPosition(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.PrepareRenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 0, Character: 5},
		},
	}

	result, err := srv.PrepareRename(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestRename_Account(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash
2024-01-16 test
    expenses:food  $30
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 1, Character: 10},
		},
		NewName: "expenses:groceries",
	}

	result, err := srv.Rename(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	changes := result.Changes
	require.NotNil(t, changes)
	require.Contains(t, changes, uri)

	edits := changes[uri]
	assert.Len(t, edits, 2)
}

func TestRename_Commodity(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash
2024-01-16 test
    expenses:rent  $100
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 1, Character: 20},
		},
		NewName: "USD",
	}

	result, err := srv.Rename(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	changes := result.Changes
	require.NotNil(t, changes)
	require.Contains(t, changes, uri)

	edits := changes[uri]
	assert.Len(t, edits, 2)
}

func TestRename_InvalidPosition(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 0, Character: 5},
		},
		NewName: "new_name",
	}

	result, err := srv.Rename(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestRename_Payee(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery store
    expenses:food  $50
    assets:cash
2024-01-16 grocery store
    expenses:food  $30
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 0, Character: 15},
		},
		NewName: "supermarket",
	}

	result, err := srv.Rename(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	changes := result.Changes
	require.NotNil(t, changes)
	require.Contains(t, changes, uri)

	edits := changes[uri]
	assert.Len(t, edits, 2)
}

func TestRename_IncludesDeclaration(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food

2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 3, Character: 10},
		},
		NewName: "expenses:groceries",
	}

	result, err := srv.Rename(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	changes := result.Changes
	require.NotNil(t, changes)
	require.Contains(t, changes, uri)

	edits := changes[uri]
	assert.Len(t, edits, 2)
}
