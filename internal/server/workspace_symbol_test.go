package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/workspace"
)

func TestWorkspaceSymbol_Accounts(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food
account expenses:rent
account assets:cash

2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "expenses",
	}

	result, err := srv.workspaceSymbols(context.Background(), params)
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

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "USD",
	}

	result, err := srv.workspaceSymbols(context.Background(), params)
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

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "grocery",
	}

	result, err := srv.workspaceSymbols(context.Background(), params)
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

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "",
	}

	result, err := srv.workspaceSymbols(context.Background(), params)
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestWorkspaceSymbol_NoMatches(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WorkspaceSymbolParams{
		Query: "nonexistent_symbol_xyz",
	}

	result, err := srv.workspaceSymbols(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func setupWorkspaceServer(t *testing.T, files map[string]string) *Server {
	t.Helper()
	tmpDir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0o644))
	}
	srv := NewServer()
	srv.loader = include.NewLoader()
	srv.workspace = workspace.NewWorkspace(tmpDir, srv.loader)
	require.NoError(t, srv.workspace.Initialize())
	return srv
}

func TestWorkspaceSymbol_NonOpenIncludeTreeFile(t *testing.T) {
	srv := setupWorkspaceServer(t, map[string]string{
		"main.journal":  "include child.journal\n\naccount assets:cash\n\n2024-01-01 Main Payee\n    assets:cash  $1\n    expenses:x\n",
		"child.journal": "account expenses:food\ncommodity EUR\n\n2024-02-01 Child Payee\n    expenses:food  10 EUR\n    assets:cash\n",
	})

	result, err := srv.workspaceSymbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "child"})
	require.NoError(t, err)

	var names []string
	for _, s := range result {
		names = append(names, s.Name)
	}
	assert.Contains(t, names, "Child Payee", "payee from non-open included file")

	result, err = srv.workspaceSymbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "expenses:food"})
	require.NoError(t, err)
	require.NotEmpty(t, result, "account from non-open included file")
	assert.Equal(t, protocol.SymbolKindClass, result[0].Kind)

	result, err = srv.workspaceSymbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "EUR"})
	require.NoError(t, err)
	require.NotEmpty(t, result, "commodity from non-open included file")
	assert.Equal(t, protocol.SymbolKindEnum, result[0].Kind)
}

func TestWorkspaceSymbol_DeduplicatesRepeatedIncludes(t *testing.T) {
	srv := setupWorkspaceServer(t, map[string]string{
		"main.journal":   "include shared.journal\ninclude shared.journal\n\n2024-01-01 Root\n    assets:cash  $1\n    expenses:x\n",
		"shared.journal": "account expenses:shared\n\n2024-03-01 Shared Payee\n    expenses:shared  $5\n    assets:cash\n",
	})

	result, err := srv.workspaceSymbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "Shared Payee"})
	require.NoError(t, err)

	count := 0
	for _, s := range result {
		if s.Name == "Shared Payee" {
			count++
		}
	}
	assert.Equal(t, 1, count, "repeated include must not duplicate symbols")
}

func TestWorkspaceSymbol_FallbackWithoutWorkspace(t *testing.T) {
	srv := NewServer()
	content := "account expenses:food\n\n2024-01-15 test\n    expenses:food  $50\n    assets:cash"
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	result, err := srv.workspaceSymbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "expenses"})
	require.NoError(t, err)
	require.NotEmpty(t, result)
	assert.Equal(t, "expenses:food", result[0].Name)
}

func TestWorkspaceSymbol_OpenFileOutsideTree(t *testing.T) {
	srv := setupWorkspaceServer(t, map[string]string{
		"main.journal": "account assets:cash\n",
	})

	outsideURI := uri.URI("file:///tmp/outside.journal")
	srv.documents.Store(outsideURI, "account expenses:outside\n\n2024-01-01 Outside Payee\n    expenses:outside  $1\n    assets:x\n")

	result, err := srv.workspaceSymbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "outside"})
	require.NoError(t, err)

	var names []string
	for _, s := range result {
		names = append(names, s.Name)
	}
	assert.Contains(t, names, "expenses:outside", "account from open file outside workspace tree")
	assert.Contains(t, names, "Outside Payee", "payee from open file outside workspace tree")
}

func TestWorkspaceSymbol_NonOpenFile_CRLF_Unicode(t *testing.T) {
	srv := setupWorkspaceServer(t, map[string]string{
		"main.journal":  "include child.journal\n\naccount assets:cash\n",
		"child.journal": "account расходы:еда\r\n\r\n2024-01-01 Магазин\r\n    расходы:еда  100 RUB\r\n    assets:cash\r\n",
	})

	result, err := srv.workspaceSymbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "расходы"})
	require.NoError(t, err)
	require.NotEmpty(t, result, "CJK/Cyrillic account from non-open CRLF file")
	assert.Equal(t, "расходы:еда", result[0].Name)

	result, err = srv.workspaceSymbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "Магазин"})
	require.NoError(t, err)
	require.NotEmpty(t, result, "Cyrillic payee from non-open CRLF file")
}
