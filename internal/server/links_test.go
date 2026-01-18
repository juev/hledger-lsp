package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestDocumentLink_IncludeDirective(t *testing.T) {
	srv := NewServer()
	content := `include accounts.journal
include data/transactions.journal

2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///home/user/main.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DocumentLinkParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := srv.DocumentLink(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2)

	assert.Equal(t, uint32(0), result[0].Range.Start.Line)
	assert.Contains(t, string(result[0].Target), "accounts.journal")

	assert.Equal(t, uint32(1), result[1].Range.Start.Line)
	assert.Contains(t, string(result[1].Target), "data/transactions.journal")
}

func TestDocumentLink_NoIncludes(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.DocumentLinkParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := srv.DocumentLink(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestDocumentLink_EmptyDocument(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, "")

	params := &protocol.DocumentLinkParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := srv.DocumentLink(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestDocumentLink_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.DocumentLinkParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///nonexistent.journal"},
	}

	result, err := srv.DocumentLink(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}
