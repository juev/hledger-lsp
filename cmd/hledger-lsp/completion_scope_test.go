package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/server"
)

func TestLSPWire_ScopedAccountCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverPipe, clientPipe := net.Pipe()
	srv := server.NewServer()
	_, serverConn, _, _ := newServerConnection(ctx, serverPipe, srv)
	_, clientConn, client := protocol.NewClient(ctx, &testClient{}, jsonrpc2.NewStream(clientPipe))
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
		require.NoError(t, serverConn.Close())
		<-clientConn.Done()
		<-serverConn.Done()
	})

	initialize, err := client.Initialize(ctx, &protocol.InitializeParams{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"hledgerCompletion":{"accountScope":true}}`, string(initialize.Capabilities.Experimental))
	require.NoError(t, client.Initialized(ctx, &protocol.InitializedParams{}))
	docURI := uri.URI("file:///staged-completion.journal")
	require.NoError(t, client.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: docURI, Version: 1, LanguageID: "hledger",
			Text: "account assets:unused\r\naccount assets:zero\r\n\r\n2026-09-13 Example\r\n    assets:",
		},
	}))

	for _, scope := range []string{"nonzero", "all", "nonzero"} {
		var result server.ScopedCompletionResult
		_, err = clientConn.Call(ctx, "hledger/completion", &server.ScopedCompletionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 4, Character: 11}, AccountScope: scope,
		}, &result)
		require.NoError(t, err)
		require.NotNil(t, result.CompletionList)
		require.NotNil(t, result.AccountRange)
		assert.Equal(t, protocol.Position{Line: 4, Character: 4}, result.AccountRange.Start)
		assert.Equal(t, protocol.Position{Line: 4, Character: 11}, result.AccountRange.End)
		if scope == "all" {
			var labels []string
			for _, item := range result.CompletionList.Items {
				labels = append(labels, item.Label)
			}
			assert.ElementsMatch(t, []string{"assets:unused", "assets:zero"}, labels)
		} else {
			assert.Empty(t, result.CompletionList.Items)
		}
	}

	var ignored any
	_, err = clientConn.Call(ctx, "hledger/completion", protocol.LSPAny(`{"textDocument":{"uri":"file:///staged-completion.journal"},"position":{"line":4,"character":11},"accountScope":"invalid"}`), &ignored)
	assertRPCCode(t, err, jsonrpc2.InvalidParams)
}
