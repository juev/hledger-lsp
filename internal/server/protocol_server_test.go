package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestServer_Request_PayeeAccountHistoryUsesTypedDecode(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(uri.URI("file:///payee-history.journal"), `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash`)

	result, err := srv.Request(context.Background(), "hledger/payeeAccountHistory", protocol.LSPAny(`{"textDocument":{"uri":"file:///payee-history.journal"}}`))
	require.NoError(t, err)

	history, ok := result.(*PayeeAccountHistoryResult)
	require.True(t, ok)
	assert.Contains(t, history.PayeeAccounts, "lunch")
}

func TestServer_Request_InvalidPayeeAccountHistoryParams(t *testing.T) {
	srv := NewServer()

	result, err := srv.Request(context.Background(), "hledger/payeeAccountHistory", protocol.LSPAny(`{invalid`))
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestServer_Request_UnknownMethodFallsBackToUnimplemented(t *testing.T) {
	srv := NewServer()

	result, err := srv.Request(context.Background(), "hledger/unknownMethod", protocol.LSPAny(`{}`))
	var rpcErr *jsonrpc2.Error
	require.ErrorAs(t, err, &rpcErr)
	require.Nil(t, result)
	assert.Equal(t, jsonrpc2.Code(protocol.ErrorCodesMethodNotFound), rpcErr.Code)
}

func TestServer_ProtocolInterfaceAssertion(t *testing.T) {
	var _ protocol.Server = (*Server)(nil)
}
