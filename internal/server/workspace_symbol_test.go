package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestWorkspaceSymbol_Accounts(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food
account expenses:rent
account assets:cash

2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "expenses",
	}

	result, err := srv.WorkspaceSymbol(context.Background(), params)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result), 2)

	var accountSymbols []protocol.SymbolInformation
	for _, s := range result {
		if s.Kind == protocol.SymbolKindClass {
			accountSymbols = append(accountSymbols, s)
		}
	}
	assert.GreaterOrEqual(t, len(accountSymbols), 2)
}

func TestWorkspaceSymbol_Commodities(t *testing.T) {
	srv := NewServer()
	content := `commodity USD
commodity EUR

2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "USD",
	}

	result, err := srv.WorkspaceSymbol(context.Background(), params)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	found := false
	for _, s := range result {
		if s.Kind == protocol.SymbolKindEnum && s.Name == "USD" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected to find USD commodity symbol")
}

func TestWorkspaceSymbol_Payees(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery store
    expenses:food  $50
    assets:cash
2024-01-16 grocery store
    expenses:food  $30
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "grocery",
	}

	result, err := srv.WorkspaceSymbol(context.Background(), params)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	found := false
	for _, s := range result {
		if s.Kind == protocol.SymbolKindFunction && s.Name == "grocery store" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected to find payee symbol")
}

func TestWorkspaceSymbol_EmptyQuery(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food

2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "",
	}

	result, err := srv.WorkspaceSymbol(context.Background(), params)
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestWorkspaceSymbol_NoMatches(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "nonexistent_symbol_xyz",
	}

	result, err := srv.WorkspaceSymbol(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}
