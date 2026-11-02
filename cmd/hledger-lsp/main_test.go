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
	_, serverConn, _, _ := newServerConnection(ctx, serverPipe, srv)
	_, clientConn, clientServer := protocol.NewClient(ctx, &testClient{diagnostics: diagnostics}, jsonrpc2.NewStream(clientPipe))
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
		require.NoError(t, serverConn.Close())
		<-clientConn.Done()
		<-serverConn.Done()
	})

	initializeResult, err := clientServer.Initialize(ctx, &protocol.InitializeParams{
		Capabilities: codeActionLiteralClientCapabilities(),
	})
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

	_, err = clientServer.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
	})
	require.NoError(t, err)

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

func codeActionLiteralClientCapabilities() protocol.ClientCapabilities {
	hierarchicalDocumentSymbols := true

	return protocol.ClientCapabilities{
		TextDocument: &protocol.TextDocumentClientCapabilities{
			CodeAction: &protocol.CodeActionClientCapabilities{
				CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{
					CodeActionKind: protocol.ClientCodeActionKindOptions{
						ValueSet: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix},
					},
				},
			},
			DocumentSymbol: &protocol.DocumentSymbolClientCapabilities{
				HierarchicalDocumentSymbolSupport: &hierarchicalDocumentSymbols,
			},
		},
	}
}

func TestLSPWire_InitializeReportsRuntimeVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverPipe, clientPipe := net.Pipe()
	srv := server.NewServerWithVersion("wire-test-version")
	_, serverConn, _, _ := newServerConnection(ctx, serverPipe, srv)
	_, clientConn, clientServer := protocol.NewClient(ctx, &testClient{}, jsonrpc2.NewStream(clientPipe))
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
		require.NoError(t, serverConn.Close())
		<-clientConn.Done()
		<-serverConn.Done()
	})

	result, err := clientServer.Initialize(ctx, &protocol.InitializeParams{})

	require.NoError(t, err)
	require.NotNil(t, result.ServerInfo)
	version, ok := result.ServerInfo.Version.Get()
	require.True(t, ok)
	require.Equal(t, "wire-test-version", version)
}

func TestLSPWire_CodeActionResultArmsMatchCapabilities(t *testing.T) {
	tests := []struct {
		name               string
		capabilities       protocol.ClientCapabilities
		wantCodeActionArms bool
	}{
		{
			name:               "literal client",
			capabilities:       codeActionLiteralClientCapabilities(),
			wantCodeActionArms: true,
		},
		{
			name: "command-only client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			serverPipe, clientPipe := net.Pipe()
			srv := server.NewServer()
			_, serverConn, _, _ := newServerConnection(ctx, serverPipe, srv)
			_, clientConn, clientServer := protocol.NewClient(ctx, &testClient{}, jsonrpc2.NewStream(clientPipe))
			t.Cleanup(func() {
				require.NoError(t, clientConn.Close())
				require.NoError(t, serverConn.Close())
				<-clientConn.Done()
				<-serverConn.Done()
			})

			initializeResult, err := clientServer.Initialize(ctx, &protocol.InitializeParams{Capabilities: tt.capabilities})
			require.NoError(t, err)
			if tt.wantCodeActionArms {
				require.IsType(t, &protocol.CodeActionOptions{}, initializeResult.Capabilities.CodeActionProvider)
			} else {
				assert.Equal(t, protocol.Boolean(true), initializeResult.Capabilities.CodeActionProvider)
			}
			require.NoError(t, clientServer.Initialized(ctx, &protocol.InitializedParams{}))

			documentURI := uri.URI("file:///wire-code-action.journal")
			require.NoError(t, clientServer.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI: documentURI,
					Text: `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00`,
				},
			}))

			actions, err := clientServer.CodeAction(ctx, &protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Context: protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{{
					Code: protocol.String("UNBALANCED"),
				}}},
			})
			require.NoError(t, err)
			if tt.wantCodeActionArms {
				require.NotEmpty(t, actions)
			}
			for _, action := range actions {
				_, isCodeAction := action.(*protocol.CodeAction)
				assert.Equal(t, tt.wantCodeActionArms, isCodeAction, "received %T", action)
			}
		})
	}
}

func TestLSPWire_DidChangeCompletesBeforeInlineCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverPipe, clientPipe := net.Pipe()
	server := &blockingDidChangeServer{
		didChangeStarted: make(chan struct{}),
		unblockDidChange: make(chan struct{}),
		didChangeDone:    make(chan struct{}),
		inlineStarted:    make(chan struct{}),
	}
	_, serverConn, _, _ := newProtocolServerConnection(ctx, serverPipe, server)
	_, clientConn, clientServer := protocol.NewClient(ctx, &testClient{}, jsonrpc2.NewStream(clientPipe))
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
		require.NoError(t, serverConn.Close())
		<-clientConn.Done()
		<-serverConn.Done()
	})

	_, err := clientServer.Initialize(ctx, &protocol.InitializeParams{})
	require.NoError(t, err)
	require.NoError(t, clientServer.Initialized(ctx, &protocol.InitializedParams{}))

	documentURI := uri.URI("file:///ordering.journal")
	require.NoError(t, clientServer.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI},
			Version:                1,
		},
	}))
	require.Eventually(t, func() bool {
		select {
		case <-server.didChangeStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	inlineResult := make(chan error, 1)
	go func() {
		_, err := clientServer.InlineCompletion(ctx, &protocol.InlineCompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			},
		})
		inlineResult <- err
	}()

	require.Never(t, func() bool {
		select {
		case <-server.inlineStarted:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, time.Millisecond)

	close(server.unblockDidChange)
	require.Eventually(t, func() bool {
		select {
		case <-server.didChangeDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	require.NoError(t, <-inlineResult)
}

type blockingDidChangeServer struct {
	protocol.UnimplementedServer
	didChangeStarted chan struct{}
	unblockDidChange chan struct{}
	didChangeDone    chan struct{}
	inlineStarted    chan struct{}
}

func (s *blockingDidChangeServer) DidChange(_ context.Context, _ *protocol.DidChangeTextDocumentParams) error {
	close(s.didChangeStarted)
	<-s.unblockDidChange
	close(s.didChangeDone)
	return nil
}

func (s *blockingDidChangeServer) Initialize(_ context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	return &protocol.InitializeResult{}, nil
}

func (s *blockingDidChangeServer) Initialized(_ context.Context, _ *protocol.InitializedParams) error {
	return nil
}

func (s *blockingDidChangeServer) InlineCompletion(_ context.Context, _ *protocol.InlineCompletionParams) (protocol.InlineCompletionResult, error) {
	close(s.inlineStarted)
	return &protocol.InlineCompletionList{}, nil
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

// PublishDiagnostics drops diagnostics when the test does not observe them and
// otherwise waits for the test to read them, unless the connection context ends
// first. A notification handler error fails the connection, so an abandoned
// delivery returns nil.
func (c *testClient) PublishDiagnostics(ctx context.Context, params *protocol.PublishDiagnosticsParams) error {
	if c.diagnostics == nil {
		return nil
	}
	select {
	case c.diagnostics <- *params:
	case <-ctx.Done():
	}
	return nil
}

// harnessFailureBound only limits how long a broken harness may hang before the
// test reports it; a passing run never waits for it.
const harnessFailureBound = 5 * time.Second

func publishDiagnosticsAsync(ctx context.Context, c *testClient, params protocol.PublishDiagnosticsParams) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- c.PublishDiagnostics(ctx, &params)
	}()
	return done
}

func requireReturned(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(harnessFailureBound):
		t.Fatal("PublishDiagnostics did not return")
	}
}

func TestTestClientPublishDiagnostics(t *testing.T) {
	params := protocol.PublishDiagnosticsParams{URI: uri.URI("file:///sink.journal")}
	stale := protocol.PublishDiagnosticsParams{URI: uri.URI("file:///stale.journal")}

	t.Run("unconfigured sink returns", func(t *testing.T) {
		requireReturned(t, publishDiagnosticsAsync(context.Background(), &testClient{}, params))
	})

	t.Run("configured sink receives the params", func(t *testing.T) {
		sink := make(chan protocol.PublishDiagnosticsParams, 1)

		requireReturned(t, publishDiagnosticsAsync(context.Background(), &testClient{diagnostics: sink}, params))

		require.Len(t, sink, 1)
		assert.Equal(t, params, <-sink)
	})

	t.Run("full sink delivers once a reader frees it", func(t *testing.T) {
		sink := make(chan protocol.PublishDiagnosticsParams, 1)
		sink <- stale

		done := publishDiagnosticsAsync(context.Background(), &testClient{diagnostics: sink}, params)

		assert.Equal(t, stale, <-sink)
		requireReturned(t, done)
		require.Len(t, sink, 1)
		assert.Equal(t, params, <-sink)
	})

	t.Run("full sink returns when the context is already canceled", func(t *testing.T) {
		sink := make(chan protocol.PublishDiagnosticsParams, 1)
		sink <- stale
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		requireReturned(t, publishDiagnosticsAsync(ctx, &testClient{diagnostics: sink}, params))

		require.Len(t, sink, 1)
		assert.Equal(t, stale, <-sink)
	})

	t.Run("full sink returns when the context is canceled during delivery", func(t *testing.T) {
		sink := make(chan protocol.PublishDiagnosticsParams)
		ctx, cancel := context.WithCancel(context.Background())

		done := publishDiagnosticsAsync(ctx, &testClient{diagnostics: sink}, params)
		cancel()

		requireReturned(t, done)
		select {
		case published := <-sink:
			t.Fatalf("canceled delivery reached the sink: %v", published.URI)
		default:
		}
	})
}

// publishingServer publishes one diagnostics notification per URI before it
// answers initialize. The client reads frames in wire order, so every
// publishDiagnostics handler has started by the time Initialize returns.
type publishingServer struct {
	protocol.UnimplementedServer
	uris []uri.URI
}

func (s *publishingServer) Initialize(ctx context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	client, ok := protocol.ClientFromContext(ctx)
	if !ok {
		return nil, errors.New("no client in server context")
	}
	for _, documentURI := range s.uris {
		if err := client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{URI: documentURI}); err != nil {
			return nil, err
		}
	}
	return &protocol.InitializeResult{}, nil
}

// requireWireClosed closes both ends and fails instead of hanging when a
// handler still in flight keeps Close waiting.
func requireWireClosed(t *testing.T, clientConn, serverConn jsonrpc2.Conn) {
	t.Helper()
	closed := make(chan error, 1)
	go func() {
		err := errors.Join(clientConn.Close(), serverConn.Close())
		<-clientConn.Done()
		<-serverConn.Done()
		closed <- err
	}()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(harnessFailureBound):
		t.Fatal("wire teardown blocked on an in-flight client handler")
	}
}

func TestLSPWire_UnconfiguredDiagnosticsSinkDoesNotBlockTeardown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), harnessFailureBound)
	defer cancel()

	serverPipe, clientPipe := net.Pipe()
	srv := &publishingServer{uris: []uri.URI{"file:///unobserved.journal"}}
	_, serverConn, _, _ := newProtocolServerConnection(ctx, serverPipe, srv)
	_, clientConn, clientServer := protocol.NewClient(ctx, &testClient{}, jsonrpc2.NewStream(clientPipe))

	_, err := clientServer.Initialize(ctx, &protocol.InitializeParams{})
	require.NoError(t, err)

	// ctx stays live: an unconfigured sink must not depend on cancellation.
	requireWireClosed(t, clientConn, serverConn)
}

func TestLSPWire_ConfiguredDiagnosticsSinkDeliversAndUnblocksOnCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), harnessFailureBound)
	defer cancel()

	serverPipe, clientPipe := net.Pipe()
	published := []uri.URI{"file:///first.journal", "file:///second.journal"}
	srv := &publishingServer{uris: published}
	diagnostics := make(chan protocol.PublishDiagnosticsParams)
	_, serverConn, _, _ := newProtocolServerConnection(ctx, serverPipe, srv)
	_, clientConn, clientServer := protocol.NewClient(ctx, &testClient{diagnostics: diagnostics}, jsonrpc2.NewStream(clientPipe))

	_, err := clientServer.Initialize(ctx, &protocol.InitializeParams{})
	require.NoError(t, err)

	select {
	case got := <-diagnostics:
		assert.Contains(t, published, got.URI)
	case <-ctx.Done():
		t.Fatal("timed out waiting for publishDiagnostics")
	}

	// The other notification still waits on the unread sink; canceling the
	// connection context must release it so teardown completes.
	cancel()
	requireWireClosed(t, clientConn, serverConn)
}

func TestProtocolServer_CodeAction_DelegatesToServer(t *testing.T) {
	srv := server.NewServer()
	_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{
		Capabilities: codeActionLiteralClientCapabilities(),
	})
	require.NoError(t, err)

	documentURI := uri.URI("file:///test.journal")
	srv.StoreDocument(documentURI, `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00`)

	actions, err := srv.CodeAction(context.Background(), &protocol.CodeActionParams{
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

func TestProtocolServer_ExecuteCommand_DelegatesToServer(t *testing.T) {
	srv := server.NewServer()

	_, err := srv.ExecuteCommand(context.Background(), &protocol.ExecuteCommandParams{
		Command: "unknown.command",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestProtocolServer_Request_PayeeAccountHistoryUsesLSPAny(t *testing.T) {
	srv := server.NewServer()

	documentURI := uri.URI("file:///payee-history.journal")
	srv.StoreDocument(documentURI, `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash`)

	result, err := srv.Request(context.Background(), "hledger/payeeAccountHistory", protocol.LSPAny(`{"textDocument":{"uri":"file:///payee-history.journal"}}`))
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
