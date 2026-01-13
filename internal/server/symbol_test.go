package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestDocumentSymbol_Empty(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), "")

	params := &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.DocumentSymbol(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestDocumentSymbol_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///nonexistent.journal",
		},
	}

	result, err := srv.DocumentSymbol(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestDocumentSymbol_Transactions(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery store
    expenses:food  $50
    assets:cash

2024-01-16 * restaurant
    expenses:food  $30
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.DocumentSymbol(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2)

	symbols := toDocumentSymbols(t, result)

	assert.Equal(t, "2024-01-15 grocery store", symbols[0].Name)
	assert.Equal(t, protocol.SymbolKindFunction, symbols[0].Kind)
	assert.Equal(t, uint32(0), symbols[0].Range.Start.Line)

	assert.Equal(t, "2024-01-16 restaurant", symbols[1].Name)
	assert.Equal(t, protocol.SymbolKindFunction, symbols[1].Kind)
	assert.Equal(t, uint32(4), symbols[1].Range.Start.Line)
}

func TestDocumentSymbol_AccountDirective(t *testing.T) {
	srv := NewServer()
	content := `account assets:bank:checking

account expenses:food`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.DocumentSymbol(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2)

	symbols := toDocumentSymbols(t, result)

	assert.Equal(t, "account assets:bank:checking", symbols[0].Name)
	assert.Equal(t, protocol.SymbolKindClass, symbols[0].Kind)
	assert.Equal(t, uint32(0), symbols[0].Range.Start.Line)

	assert.Equal(t, "account expenses:food", symbols[1].Name)
	assert.Equal(t, protocol.SymbolKindClass, symbols[1].Kind)
	assert.Equal(t, uint32(2), symbols[1].Range.Start.Line)
}

func TestDocumentSymbol_CommodityDirective(t *testing.T) {
	srv := NewServer()
	content := `commodity $

commodity EUR`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.DocumentSymbol(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2)

	symbols := toDocumentSymbols(t, result)

	assert.Equal(t, "commodity $", symbols[0].Name)
	assert.Equal(t, protocol.SymbolKindEnum, symbols[0].Kind)

	assert.Equal(t, "commodity EUR", symbols[1].Name)
	assert.Equal(t, protocol.SymbolKindEnum, symbols[1].Kind)
}

func TestDocumentSymbol_Include(t *testing.T) {
	srv := NewServer()
	content := `include ./accounts.journal

include /path/to/other.journal`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.DocumentSymbol(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2)

	symbols := toDocumentSymbols(t, result)

	assert.Equal(t, "include ./accounts.journal", symbols[0].Name)
	assert.Equal(t, protocol.SymbolKindModule, symbols[0].Kind)

	assert.Equal(t, "include /path/to/other.journal", symbols[1].Name)
	assert.Equal(t, protocol.SymbolKindModule, symbols[1].Kind)
}

func TestDocumentSymbol_Mixed(t *testing.T) {
	srv := NewServer()
	content := `account assets:bank

commodity $

2024-01-15 test transaction
    expenses:food  $50
    assets:bank

include ./other.journal`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.DocumentSymbol(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 4)

	symbols := toDocumentSymbols(t, result)

	kindCounts := make(map[protocol.SymbolKind]int)
	for _, sym := range symbols {
		kindCounts[sym.Kind]++
	}

	assert.Equal(t, 1, kindCounts[protocol.SymbolKindClass])
	assert.Equal(t, 1, kindCounts[protocol.SymbolKindEnum])
	assert.Equal(t, 1, kindCounts[protocol.SymbolKindFunction])
	assert.Equal(t, 1, kindCounts[protocol.SymbolKindModule])
}

func toDocumentSymbols(t *testing.T, result []any) []protocol.DocumentSymbol {
	t.Helper()
	symbols := make([]protocol.DocumentSymbol, 0, len(result))
	for _, item := range result {
		sym, ok := item.(protocol.DocumentSymbol)
		require.True(t, ok, "expected protocol.DocumentSymbol")
		symbols = append(symbols, sym)
	}
	return symbols
}
