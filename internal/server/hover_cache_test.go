package server

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestHover_UsesCachedBalances(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery\n    expenses:food  $50\n    assets:cash  $-50\n"
	srv.documents.Store(uri, content)

	// Pre-warm the balances cache (non-workspace document → hover takes the
	// non-resolved branch that reads cachedBalances).
	warm := srv.cachedBalances(uri, content)
	require.NotNil(t, warm)

	hoverAt := func() string {
		res, err := srv.Hover(context.Background(), &protocol.HoverParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
				Position:     protocol.Position{Line: 1, Character: 10},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		return res.Contents.Value
	}

	first := hoverAt()
	assert.Contains(t, first, "expenses:food")
	assert.Contains(t, first, "50")

	// Repeated hover is stable, and the balances map is a single cached instance
	// (computed once via sync.Once), not recomputed per request.
	second := hoverAt()
	assert.Equal(t, first, second)

	after := srv.cachedBalances(uri, content)
	assert.Equal(t, reflect.ValueOf(warm).Pointer(), reflect.ValueOf(after).Pointer(),
		"balances must be computed once and reused")
}
