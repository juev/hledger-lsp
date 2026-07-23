package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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

func TestLSPWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverPipe, clientPipe := net.Pipe()
	srv := server.NewServer()
	diagnostics := make(chan protocol.PublishDiagnosticsParams, 1)
	_, serverConn, _ := newServerConnection(ctx, serverPipe, srv)
	_, clientConn, clientServer := protocol.NewClient(ctx, &testClient{diagnostics: diagnostics}, jsonrpc2.NewStream(clientPipe))
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
		require.NoError(t, serverConn.Close())
		<-clientConn.Done()
		<-serverConn.Done()
	})

	initializeResult, err := clientServer.Initialize(ctx, &protocol.InitializeParams{})
	require.NoError(t, err)
	require.NotNil(t, initializeResult.ServerInfo)
	require.Equal(t, "hledger-lsp", initializeResult.ServerInfo.Name)

	require.NoError(t, clientServer.Initialized(ctx, &protocol.InitializedParams{}))

	documentURI := uri.URI("file:///wire.journal")
	require.NoError(t, clientServer.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     documentURI,
			Version: 1,
			Text: `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00`,
		},
	}))

	require.NoError(t, clientServer.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI},
			Version:                2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangePartial{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 4},
			},
			Text: "2025",
		}},
	}))

	require.NoError(t, clientServer.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI},
			Version:                3,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{
			Text: `2025-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00

2025-01-20 lunch
`,
		}},
	}))

	completion, err := clientServer.Completion(ctx, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: 1, Character: 17},
		},
	})
	require.NoError(t, err)
	require.IsType(t, &protocol.CompletionList{}, completion)

	inlineCompletion, err := clientServer.InlineCompletion(ctx, &protocol.InlineCompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: 5, Character: 0},
		},
		Context: protocol.InlineCompletionContext{TriggerKind: protocol.InlineCompletionTriggerKindAutomatic},
	})
	require.NoError(t, err)
	inlineList, ok := inlineCompletion.(*protocol.InlineCompletionList)
	require.True(t, ok)
	require.Len(t, inlineList.Items, 1)
	_, ok = inlineList.Items[0].InsertText.(protocol.String)
	require.True(t, ok)

	codeActions, err := clientServer.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 10},
		},
		Context: protocol.CodeActionContext{},
	})
	require.NoError(t, err)
	require.NotEmpty(t, codeActions)
	require.IsType(t, &protocol.CodeAction{}, codeActions[0])

	documentSymbols, err := clientServer.DocumentSymbol(ctx, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
	})
	require.NoError(t, err)
	require.IsType(t, protocol.DocumentSymbolSlice{}, documentSymbols)

	select {
	case published := <-diagnostics:
		assert.Equal(t, documentURI, published.URI)
		version, ok := published.Version.Get()
		require.True(t, ok)
		assert.Positive(t, version)
	case <-ctx.Done():
		t.Fatal("timed out waiting for publishDiagnostics")
	}
}

type testClient struct {
	protocol.UnimplementedClient
	diagnostics chan<- protocol.PublishDiagnosticsParams
}

func (c *testClient) Configuration(context.Context, *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	return []protocol.LSPAny{}, nil
}

func (c *testClient) RegisterCapability(context.Context, *protocol.RegistrationParams) error {
	return nil
}

func (c *testClient) PublishDiagnostics(_ context.Context, params *protocol.PublishDiagnosticsParams) error {
	c.diagnostics <- *params
	return nil
}

func TestServerDispatcher_CodeAction_DelegatesToServer(t *testing.T) {
	srv := server.NewServer()
	dispatcher := newServerDispatcher(srv)

	documentURI := uri.URI("file:///test.journal")
	srv.StoreDocument(documentURI, `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00`)

	actions, err := dispatcher.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 10},
		},
		Context: protocol.CodeActionContext{},
	})
	require.NoError(t, err)
	require.NotEmpty(t, actions)

	foundQuickFix := false
	for _, action := range actions {
		codeAction, ok := action.(*protocol.CodeAction)
		if ok && codeAction.Kind != nil && *codeAction.Kind == protocol.CodeActionKindQuickFix {
			foundQuickFix = true
			break
		}
	}
	assert.True(t, foundQuickFix, "expected quickfix action in delegated response")
}

func TestServerDispatcher_ExecuteCommand_DelegatesToServer(t *testing.T) {
	srv := server.NewServer()
	dispatcher := newServerDispatcher(srv)

	_, err := dispatcher.ExecuteCommand(context.Background(), &protocol.ExecuteCommandParams{
		Command: "unknown.command",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestServerDispatcher_Request_PayeeAccountHistoryUsesLSPAny(t *testing.T) {
	srv := server.NewServer()
	dispatcher := newServerDispatcher(srv)

	documentURI := uri.URI("file:///payee-history.journal")
	srv.StoreDocument(documentURI, `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash`)

	result, err := dispatcher.Request(context.Background(), "hledger/payeeAccountHistory", protocol.LSPAny(`{"textDocument":{"uri":"file:///payee-history.journal"}}`))
	require.NoError(t, err)

	history, ok := result.(*server.PayeeAccountHistoryResult)
	require.True(t, ok)
	assert.Contains(t, history.PayeeAccounts, "lunch")
}

func TestExitCodeForConnErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "nil",
			err:  nil,
			want: 0,
		},
		{
			name: "closed stdin EOF",
			err:  fmt.Errorf("failed reading header line: %w", io.EOF),
			want: 0,
		},
		{
			name: "protocol error",
			err:  errors.New("missing Content-Length header"),
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exitCodeForConnErr(tt.err))
		})
	}
}
