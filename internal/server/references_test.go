package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestReferences_AccountWithDirective_IncludeDeclaration(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food

2024-01-15 grocery
    expenses:food  $50
    assets:cash

2024-01-16 restaurant
    expenses:food  $30
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 3, Character: 6},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	result, err := srv.References(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 3)

	assert.Equal(t, uint32(0), result[0].Range.Start.Line)
	assert.Equal(t, uint32(3), result[1].Range.Start.Line)
	assert.Equal(t, uint32(7), result[2].Range.Start.Line)
}

func TestReferences_AccountWithDirective_ExcludeDeclaration(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food

2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 3, Character: 6},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: false},
	}

	result, err := srv.References(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, uint32(3), result[0].Range.Start.Line)
}

func TestReferences_AccountWithoutDirective(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash

2024-01-16 restaurant
    expenses:food  $30
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 1, Character: 6},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	result, err := srv.References(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2)
}

func TestReferences_CommodityWithDirective_IncludeDeclaration(t *testing.T) {
	srv := NewServer()
	content := `commodity $
    format 1,000.00

2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 4, Character: 19},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	result, err := srv.References(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 3)
}

func TestReferences_CommodityOnRight(t *testing.T) {
	srv := NewServer()
	content := `commodity EUR

2024-01-15 grocery
    expenses:food  100.00 EUR
    assets:cash  -100.00 EUR`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 3, Character: 26},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	result, err := srv.References(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 3)
}

func TestReferences_Payee(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Grocery Store
    expenses:food  $30
    assets:cash

2024-01-17 Restaurant
    expenses:dining  $40
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 0, Character: 15},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	result, err := srv.References(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2)
}

func TestReferences_PayeeIncludeDeclarationIgnored(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Grocery Store
    expenses:food  $30
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	paramsInclude := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 0, Character: 15},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	paramsExclude := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 0, Character: 15},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: false},
	}

	resultInclude, _ := srv.References(context.Background(), paramsInclude)
	resultExclude, _ := srv.References(context.Background(), paramsExclude)

	assert.Equal(t, len(resultInclude), len(resultExclude))
}

func TestReferences_UnknownPosition(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 0, Character: 0},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	result, err := srv.References(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestReferences_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///nonexistent.journal"},
			Position:     protocol.Position{Line: 0, Character: 0},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	result, err := srv.References(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestReferences_Deduplication(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food

2024-01-15 grocery
    expenses:food  $50
    expenses:food  $20
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 3, Character: 6},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	result, err := srv.References(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 3) // declaration + 2 unique usages (different lines)
}

func TestReferences_DeterministicOrder(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash

2024-01-14 earlier
    expenses:food  $30
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 1, Character: 6},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	}

	for range 10 {
		result, err := srv.References(context.Background(), params)
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, uint32(1), result[0].Range.Start.Line)
		assert.Equal(t, uint32(5), result[1].Range.Start.Line)
	}
}
