package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestCompletionResolve_Account(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food

2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50

2024-01-16 store
    expenses:food  $30
    assets:cash  $-30`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	data := completionResolveData{
		Kind:   "account",
		Label:  "expenses:food",
		DocURI: uri,
	}
	rawData := mustMarshalLSPAny(t, data)

	item := &protocol.CompletionItem{
		Label: "expenses:food",
		Data:  rawData,
	}

	result, err := srv.CompletionResolve(context.Background(), item)
	require.NoError(t, err)
	assert.NotNil(t, result.Documentation)
}

func TestCompletionResolve_Payee(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50

2024-01-16 grocery
    expenses:food  $30
    assets:cash  $-30`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	data := completionResolveData{
		Kind:   "payee",
		Label:  "grocery",
		DocURI: uri,
	}
	rawData := mustMarshalLSPAny(t, data)

	item := &protocol.CompletionItem{
		Label: "grocery",
		Data:  rawData,
	}

	result, err := srv.CompletionResolve(context.Background(), item)
	require.NoError(t, err)
	assert.NotNil(t, result.Documentation)
}

func TestCompletionResolve_Commodity(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	data := completionResolveData{
		Kind:   "commodity",
		Label:  "$",
		DocURI: uri,
	}
	rawData := mustMarshalLSPAny(t, data)

	item := &protocol.CompletionItem{
		Label: "$",
		Data:  rawData,
	}

	result, err := srv.CompletionResolve(context.Background(), item)
	require.NoError(t, err)
	assert.NotNil(t, result.Documentation)
}

func TestCompletionResolve_Tag(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery  ; project:home
    expenses:food  $50  ; project:home
    assets:cash  $-50`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	data := completionResolveData{
		Kind:   "tag",
		Label:  "project",
		DocURI: uri,
	}
	rawData := mustMarshalLSPAny(t, data)

	item := &protocol.CompletionItem{
		Label: "project",
		Data:  rawData,
	}

	result, err := srv.CompletionResolve(context.Background(), item)
	require.NoError(t, err)
	assert.NotNil(t, result.Documentation)
}

func TestCompletionResolve_NilData(t *testing.T) {
	srv := NewServer()

	item := &protocol.CompletionItem{
		Label: "test",
	}

	result, err := srv.CompletionResolve(context.Background(), item)
	require.NoError(t, err)
	assert.Equal(t, item, result)
}

func TestCompletionResolve_InvalidData(t *testing.T) {
	srv := NewServer()

	rawData := protocol.LSPAny(`{"invalid": true}`)
	item := &protocol.CompletionItem{
		Label: "test",
		Data:  rawData,
	}

	result, err := srv.CompletionResolve(context.Background(), item)
	require.NoError(t, err)
	assert.Equal(t, item, result)
}
