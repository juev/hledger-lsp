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

// refreshingClient turns on running balances and, like a client that redraws
// straight away, asks for the hints again before it answers
// workspace/inlayHint/refresh.
type refreshingClient struct {
	testClient
	server protocol.Server
	hints  chan<- refreshedHints
}

type refreshedHints struct {
	hints []protocol.InlayHint
	err   error
}

func (c *refreshingClient) Configuration(context.Context, *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	return []protocol.LSPAny{protocol.LSPAny(`{"inlayHints":{"runningBalances":true}}`)}, nil
}

func (c *refreshingClient) InlayHintRefresh(ctx context.Context) error {
	// Well under the two seconds a hint request waits for the configuration,
	// so an answer only comes in time if the applied settings released it.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	hints, err := c.server.InlayHint(ctx, &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI("file:///refresh.journal")},
		Range:        protocol.Range{End: protocol.Position{Line: 10}},
	})
	c.hints <- refreshedHints{hints: hints, err: err}
	return nil
}

func TestLSPWire_InlayHintsAnsweredBeforeTheRefreshIs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverPipe, clientPipe := net.Pipe()
	srv := server.NewServer()
	// Stored up front: the refresh may reach the client before a didOpen sent
	// after initialized is handled.
	srv.StoreDocument(uri.URI("file:///refresh.journal"), "2024-01-15 Покупка\n    expenses:еда  $50\n    assets:cash\n")
	_, serverConn, _, _ := newServerConnection(ctx, serverPipe, srv)
	hints := make(chan refreshedHints, 1)
	handler := &refreshingClient{hints: hints}
	_, clientConn, client := protocol.NewClient(ctx, handler, jsonrpc2.NewStream(clientPipe))
	handler.server = client
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
		require.NoError(t, serverConn.Close())
		<-clientConn.Done()
		<-serverConn.Done()
	})

	_, err := client.Initialize(ctx, &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
		Workspace: &protocol.WorkspaceClientCapabilities{
			Configuration: boolValue(true),
			InlayHint:     &protocol.InlayHintWorkspaceClientCapabilities{RefreshSupport: boolValue(true)},
		},
	}})
	require.NoError(t, err)
	require.NoError(t, client.Initialized(ctx, &protocol.InitializedParams{}))

	select {
	case got := <-hints:
		require.NoError(t, got.err, "the hints were held until the refresh was answered")
		var labels []protocol.InlayHintLabel
		for _, hint := range got.hints {
			labels = append(labels, hint.Label)
		}
		assert.Contains(t, labels, protocol.String("balance: 50 $"))
	case <-ctx.Done():
		t.Fatal("the server never asked the client to refresh its hints")
	}
}

func boolValue(value bool) *bool { return &value }
