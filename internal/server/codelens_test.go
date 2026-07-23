package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/parser"
)

func TestCodeLens_BalancedTransaction(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Features.CodeLens = true
	srv.setSettings(settings)

	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := srv.CodeLens(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Contains(t, result[0].Command.Title, "balanced")
	assert.Empty(t, result[0].Command.Command)
}

func TestCodeLens_UnbalancedTransaction(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Features.CodeLens = true
	srv.setSettings(settings)

	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-40`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := srv.CodeLens(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Contains(t, result[0].Command.Title, "unbalanced")
	assert.Equal(t, "hledger.fixUnbalanced", result[0].Command.Command)
	journal, _ := parser.Parse(content)
	require.Len(t, result[0].Command.Arguments, 2)
	commandURI, err := unmarshalLSPAny[string](result[0].Command.Arguments[0])
	require.NoError(t, err)
	assert.Equal(t, string(uri), commandURI)
	commandRange, err := unmarshalLSPAny[protocol.Range](result[0].Command.Arguments[1])
	require.NoError(t, err)
	assert.Equal(t, *astRangeToProtocol(journal.Transactions[0].Range), commandRange)
}

func TestCodeLens_EmptyDocument(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, "")

	params := &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := srv.CodeLens(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestCodeLens_FeatureDisabled(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Features.CodeLens = false
	srv.setSettings(settings)

	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := srv.CodeLens(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestCodeLens_CostRounding_LocalPrecision(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Features.CodeLens = true
	srv.setSettings(settings)

	// 3 * 33.337 = 100.011; diff = 0.011
	// hledger 1.50+: posting $ precision 0 → tolerance 0.5; |0.011| < 0.5 → balanced
	content := `2024-01-15 buy
    assets:stock  3 AAPL @ $33.337
    assets:cash  -$100`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := srv.CodeLens(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Contains(t, result[0].Command.Title, "balanced",
		"CodeLens uses local posting precision only (hledger 1.50+)")
}

func TestCodeLens_OneLensPerTransaction(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Features.CodeLens = true
	srv.setSettings(settings)

	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50

2024-01-16 store
    expenses:clothing  $30
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := srv.CodeLens(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2)
}
