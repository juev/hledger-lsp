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

// heldConfigClient hands every workspace/configuration request to the test,
// which answers them in any order, and reports every
// workspace/inlayHint/refresh.
type heldConfigClient struct {
	testClient
	calls     chan chan protocol.LSPAny
	refreshes chan struct{}
}

func (c *heldConfigClient) Configuration(ctx context.Context, _ *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	reply := make(chan protocol.LSPAny, 1)
	select {
	case c.calls <- reply:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case settings := <-reply:
		return []protocol.LSPAny{settings}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *heldConfigClient) InlayHintRefresh(ctx context.Context) error {
	select {
	case c.refreshes <- struct{}{}:
	case <-ctx.Done():
	}
	return nil
}

// The reply to the fetch made after initialized reaches the server after the
// reply to the fetch made for a later didChangeConfiguration. The older reply
// must not bring back the settings the user has since changed.
func TestLSPWire_StaleConfigurationReplyKeepsTheNewerSettings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serverPipe, clientPipe := net.Pipe()
	srv := server.NewServer()
	docURI := uri.URI("file:///order.journal")
	srv.StoreDocument(docURI, "2024-01-15 Покупка\n    expenses:еда  $50\n    assets:cash\n")
	_, serverConn, _, _ := newServerConnection(ctx, serverPipe, srv)
	handler := &heldConfigClient{calls: make(chan chan protocol.LSPAny), refreshes: make(chan struct{}, 4)}
	_, clientConn, client := protocol.NewClient(ctx, handler, jsonrpc2.NewStream(clientPipe))
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
		require.NoError(t, serverConn.Close())
		<-clientConn.Done()
		<-serverConn.Done()
	})
	nextCall := func() chan protocol.LSPAny {
		t.Helper()
		select {
		case reply := <-handler.calls:
			return reply
		case <-ctx.Done():
			t.Fatal("the server did not ask for the configuration")
			return nil
		}
	}

	_, err := client.Initialize(ctx, &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
		Workspace: &protocol.WorkspaceClientCapabilities{
			Configuration: boolValue(true),
			InlayHint:     &protocol.InlayHintWorkspaceClientCapabilities{RefreshSupport: boolValue(true)},
		},
	}})
	require.NoError(t, err)
	require.NoError(t, client.Initialized(ctx, &protocol.InitializedParams{}))
	startup := nextCall()
	require.NoError(t, client.DidChangeConfiguration(ctx, &protocol.DidChangeConfigurationParams{}))
	changed := nextCall()

	changed <- protocol.LSPAny(`{"inlayHints":{"inferredAmounts":false,"runningBalances":false}}`)
	select {
	case <-handler.refreshes:
	case <-ctx.Done():
		t.Fatal("the newer settings did not ask the client to refresh its hints")
	}

	// Asked while the startup fetch is still out, the hints wait for it, so
	// they show the settings left once its reply has been dealt with.
	type answer struct {
		hints []protocol.InlayHint
		err   error
	}
	answered := make(chan answer, 1)
	go func() {
		hints, err := client.InlayHint(ctx, &protocol.InlayHintParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Range:        protocol.Range{End: protocol.Position{Line: 10}},
		})
		answered <- answer{hints: hints, err: err}
	}()
	startup <- protocol.LSPAny(`{"inlayHints":{"inferredAmounts":true,"runningBalances":true}}`)

	select {
	case got := <-answered:
		require.NoError(t, got.err)
		var labels []protocol.InlayHintLabel
		for _, hint := range got.hints {
			labels = append(labels, hint.Label)
		}
		assert.Empty(t, labels, "the startup reply restored the older inlay hint settings")
	case <-ctx.Done():
		t.Fatal("the inlay hints were never answered")
	}
}
