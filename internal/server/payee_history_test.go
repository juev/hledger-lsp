package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestPayeeAccountHistory_EmptyJournal(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
	content := ``

	srv.documents.Store(uri, content)

	params := PayeeAccountHistoryParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.PayeeAccountHistory(context.Background(), paramsJSON)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.PayeeAccounts)
	assert.Empty(t, result.PairUsage)
}

func TestPayeeAccountHistory_SinglePayee(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri, content)

	params := PayeeAccountHistoryParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.PayeeAccountHistory(context.Background(), paramsJSON)

	require.NoError(t, err)
	require.NotNil(t, result)

	require.Contains(t, result.PayeeAccounts, "Grocery Store")
	accounts := result.PayeeAccounts["Grocery Store"]
	assert.Contains(t, accounts, "expenses:food")
	assert.Contains(t, accounts, "assets:cash")

	assert.Equal(t, 1, result.PairUsage["Grocery Store::expenses:food"])
	assert.Equal(t, 1, result.PairUsage["Grocery Store::assets:cash"])
}

func TestPayeeAccountHistory_MultiplePayees(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Coffee Shop
    expenses:food  $5
    assets:bank`

	srv.documents.Store(uri, content)

	params := PayeeAccountHistoryParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.PayeeAccountHistory(context.Background(), paramsJSON)

	require.NoError(t, err)
	require.NotNil(t, result)

	require.Contains(t, result.PayeeAccounts, "Grocery Store")
	require.Contains(t, result.PayeeAccounts, "Coffee Shop")

	assert.Equal(t, 1, result.PairUsage["Grocery Store::expenses:food"])
	assert.Equal(t, 1, result.PairUsage["Coffee Shop::expenses:food"])
}

func TestPayeeAccountHistory_FrequentAccounts(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Grocery Store
    expenses:food  $30
    assets:cash

2024-01-17 Grocery Store
    expenses:food  $20
    assets:bank`

	srv.documents.Store(uri, content)

	params := PayeeAccountHistoryParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.PayeeAccountHistory(context.Background(), paramsJSON)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 3, result.PairUsage["Grocery Store::expenses:food"])
	assert.Equal(t, 2, result.PairUsage["Grocery Store::assets:cash"])
	assert.Equal(t, 1, result.PairUsage["Grocery Store::assets:bank"])

	accounts := result.PayeeAccounts["Grocery Store"]
	assert.Contains(t, accounts, "expenses:food")
	assert.Contains(t, accounts, "assets:cash")
	assert.Contains(t, accounts, "assets:bank")
	assert.Len(t, accounts, 3)
}

func TestPayeeAccountHistory_Unicode(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
	content := `2024-01-15 Пятёрочка
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri, content)

	params := PayeeAccountHistoryParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.PayeeAccountHistory(context.Background(), paramsJSON)

	require.NoError(t, err)
	require.NotNil(t, result)

	require.Contains(t, result.PayeeAccounts, "Пятёрочка")
	assert.Equal(t, 1, result.PairUsage["Пятёрочка::expenses:food"])
}

func TestPayeeAccountHistory_InvalidJSON(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	result, err := srv.PayeeAccountHistory(context.Background(), json.RawMessage(`{invalid}`))

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestPayeeAccountHistory_UnknownDocument(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///unknown.journal")
	params := PayeeAccountHistoryParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.PayeeAccountHistory(context.Background(), paramsJSON)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.PayeeAccounts)
	assert.Empty(t, result.PairUsage)
}
