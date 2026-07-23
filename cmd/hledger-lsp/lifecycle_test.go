package main

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type lifecycleTestServer struct {
	protocol.UnimplementedServer

	mu                sync.Mutex
	initializeCount   int
	initializedCount  int
	didOpenCount      int
	completionCount   int
	shutdownCount     int
	initializeResult  *protocol.InitializeResult
	initializeErrors  []error
	initializeErrIdx  int
	initializeBlocker chan struct{}
	initializeStarted chan struct{}
	shutdownErr       error
	completionUnblock chan struct{}
	completionErr     error
}

func (s *lifecycleTestServer) initializeNextError() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initializeErrIdx >= len(s.initializeErrors) {
		return nil
	}
	err := s.initializeErrors[s.initializeErrIdx]
	s.initializeErrIdx++
	return err
}

func (s *lifecycleTestServer) Initialize(_ context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	s.mu.Lock()
	s.initializeCount++
	result := s.initializeResult
	s.mu.Unlock()

	if s.initializeStarted != nil {
		select {
		case s.initializeStarted <- struct{}{}:
		default:
		}
	}
	if s.initializeBlocker != nil {
		<-s.initializeBlocker
	}

	if err := s.initializeNextError(); err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}
	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{},
		ServerInfo: protocol.ServerInfo{
			Name:    "test",
			Version: protocol.NewOptional("test"),
		},
	}, nil
}

func (s *lifecycleTestServer) Initialized(context.Context, *protocol.InitializedParams) error {
	s.mu.Lock()
	s.initializedCount++
	s.mu.Unlock()
	return nil
}

func (s *lifecycleTestServer) DidOpen(context.Context, *protocol.DidOpenTextDocumentParams) error {
	s.mu.Lock()
	s.didOpenCount++
	s.mu.Unlock()
	return nil
}

func (s *lifecycleTestServer) Completion(ctx context.Context, _ *protocol.CompletionParams) (protocol.CompletionResult, error) {
	s.mu.Lock()
	s.completionCount++
	s.mu.Unlock()

	if s.completionUnblock != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.completionUnblock:
		}
	}

	if s.completionErr != nil {
		return nil, s.completionErr
	}
	return &protocol.CompletionList{}, nil
}

func (s *lifecycleTestServer) Shutdown(context.Context) error {
	s.mu.Lock()
	s.shutdownCount++
	s.mu.Unlock()
	return s.shutdownErr
}

func (s *lifecycleTestServer) snapshot() (initializeCount, initializedCount, didOpenCount, completionCount, shutdownCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initializeCount, s.initializedCount, s.didOpenCount, s.completionCount, s.shutdownCount
}

func openLifecyclePair(t *testing.T, srv *lifecycleTestServer) (jsonrpc2.Conn, jsonrpc2.Conn, protocol.Server, *lifecycleState, func()) {
	t.Helper()

	serverPipe, clientPipe := net.Pipe()
	serverCtx, serverConn, _, state := newProtocolServerConnection(context.Background(), serverPipe, srv)
	_, clientConn, client := protocol.NewClient(serverCtx, &testClient{}, jsonrpc2.NewStream(clientPipe))
	cleanup := func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		<-serverConn.Done()
	}
	return serverConn, clientConn, client, state, cleanup
}

func assertRPCCode(t *testing.T, err error, code jsonrpc2.Code) {
	t.Helper()
	var rpcErr *jsonrpc2.Error
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, code, rpcErr.Code)
}

func completionParams(uriString string) *protocol.CompletionParams {
	uriValue := uri.URI(uriString)
	return &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uriValue,
			},
			Position: protocol.Position{Line: 0, Character: 0},
		},
	}
}

func TestLSPWire_LifecycleBasics(t *testing.T) {
	ctx := context.Background()
	srv := &lifecycleTestServer{}
	serverConn, _, client, state, cleanup := openLifecyclePair(t, srv)
	defer cleanup()

	exitCode := make(chan int, 1)
	go func() {
		exitCode <- waitForServerExit(serverConn, state)
	}()

	_, err := client.Completion(ctx, completionParams("file:///wire-before-init.journal"))
	assertRPCCode(t, err, jsonrpc2.Code(protocol.ErrorCodesServerNotInitialized))

	require.NoError(t, client.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        uri.URI("file:///wire-before-init.journal"),
			Version:    1,
			Text:       "",
			LanguageID: "hledger",
		},
	}))
	require.Eventually(t, func() bool {
		_, _, didOpen, _, _ := srv.snapshot()
		return didOpen == 0
	}, time.Second, time.Millisecond)

	_, err = client.Initialize(ctx, &protocol.InitializeParams{})
	require.NoError(t, err)
	_, err = client.Completion(ctx, completionParams("file:///wire-before-init.journal"))
	assertRPCCode(t, err, jsonrpc2.InvalidRequest)

	require.NoError(t, client.Initialized(ctx, &protocol.InitializedParams{}))
	_, err = client.Completion(ctx, completionParams("file:///wire-before-init.journal"))
	require.NoError(t, err)
	require.NoError(t, client.Initialized(ctx, &protocol.InitializedParams{}))

	initializes, initialized, _, completions, shutdowns := srv.snapshot()
	require.Equal(t, 1, initializes)
	require.Equal(t, 1, initialized)
	require.Equal(t, 1, completions)
	require.Equal(t, 0, shutdowns)

	require.NoError(t, client.Shutdown(ctx))
	_, err = client.Completion(ctx, completionParams("file:///wire-before-init.journal"))
	assertRPCCode(t, err, jsonrpc2.InvalidRequest)
	err = client.Shutdown(ctx)
	require.Error(t, err)
	assertRPCCode(t, err, jsonrpc2.InvalidRequest)

	require.NoError(t, client.Exit(ctx))
	require.Equal(t, 0, <-exitCode)
}

func TestLSPWire_InitializeInProgress(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{}, 1)
	block := make(chan struct{})
	srv := &lifecycleTestServer{
		initializeStarted: started,
		initializeBlocker: block,
	}
	serverConn, _, client, _, cleanup := openLifecyclePair(t, srv)
	defer cleanup()

	firstResult := make(chan error, 1)
	go func() {
		_, err := client.Initialize(ctx, &protocol.InitializeParams{})
		firstResult <- err
	}()

	require.Eventually(t, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	secondResult := make(chan error, 1)
	go func() {
		_, err := client.Initialize(ctx, &protocol.InitializeParams{})
		secondResult <- err
	}()

	requestResult := make(chan error, 1)
	go func() {
		_, err := client.Completion(ctx, completionParams("file:///wire-before-init.journal"))
		requestResult <- err
	}()

	assertRPCCode(t, <-secondResult, jsonrpc2.Code(protocol.ErrorCodesInvalidRequest))
	assertRPCCode(t, <-requestResult, jsonrpc2.Code(protocol.ErrorCodesServerNotInitialized))

	close(block)
	require.NoError(t, <-firstResult)
	require.NoError(t, serverConn.Close())
}

func TestLSPWire_InitializeRetryPolicy(t *testing.T) {
	ctx := context.Background()

	t.Run("non-retry initialize error blocks retries", func(t *testing.T) {
		srv := &lifecycleTestServer{
			initializeErrors: []error{errors.New("fatal")},
		}
		serverConn, _, client, state, cleanup := openLifecyclePair(t, srv)
		defer cleanup()

		_, err := client.Initialize(ctx, &protocol.InitializeParams{})
		require.Error(t, err)
		_, err = client.Initialize(ctx, &protocol.InitializeParams{})
		assertRPCCode(t, err, jsonrpc2.Code(protocol.ErrorCodesInvalidRequest))

		exitCode := make(chan int, 1)
		go func() {
			exitCode <- waitForServerExit(serverConn, state)
		}()
		require.NoError(t, client.Exit(ctx))
		require.Equal(t, 1, <-exitCode)
	})

	t.Run("retryable initialize error allows second attempt", func(t *testing.T) {
		retryRaw, marshalErr := protocol.Marshal(protocol.InitializeError{Retry: true})
		require.NoError(t, marshalErr)
		retryErr := jsonrpc2.NewError(jsonrpc2.InternalError, "retryable")
		retryErr.Data = jsonrpc2.RawMessage(retryRaw)

		srv := &lifecycleTestServer{
			initializeErrors: []error{retryErr},
		}
		serverConn, _, client, state, cleanup := openLifecyclePair(t, srv)
		defer cleanup()

		_, firstInitErr := client.Initialize(ctx, &protocol.InitializeParams{})
		require.Error(t, firstInitErr)
		_, err := client.Initialize(ctx, &protocol.InitializeParams{})
		require.NoError(t, err)

		exitCode := make(chan int, 1)
		go func() {
			exitCode <- waitForServerExit(serverConn, state)
		}()
		require.NoError(t, client.Exit(ctx))
		require.Equal(t, 1, <-exitCode)
	})
}

func TestWaitForServerExit(t *testing.T) {
	ctx := context.Background()

	t.Run("exit after shutdown uses 0", func(t *testing.T) {
		srv := &lifecycleTestServer{
			shutdownErr: errors.New("shutdown failed"),
		}
		serverConn, _, client, state, cleanup := openLifecyclePair(t, srv)
		defer cleanup()

		_, err := client.Initialize(ctx, &protocol.InitializeParams{})
		require.NoError(t, err)
		require.NoError(t, client.Initialized(ctx, &protocol.InitializedParams{}))
		err = client.Shutdown(ctx)
		require.Error(t, err)

		exitCode := make(chan int, 1)
		go func() {
			exitCode <- waitForServerExit(serverConn, state)
		}()
		require.NoError(t, client.Exit(ctx))
		require.Equal(t, 0, <-exitCode)
	})

	t.Run("shutdown then peer close keeps code 0", func(t *testing.T) {
		srv := &lifecycleTestServer{}
		serverConn, clientConn, client, state, cleanup := openLifecyclePair(t, srv)
		defer cleanup()

		_, err := client.Initialize(ctx, &protocol.InitializeParams{})
		require.NoError(t, err)
		require.NoError(t, client.Initialized(ctx, &protocol.InitializedParams{}))
		require.NoError(t, client.Shutdown(ctx))
		require.NoError(t, clientConn.Close())

		exitCode := make(chan int, 1)
		go func() {
			exitCode <- waitForServerExit(serverConn, state)
		}()
		require.Equal(t, 0, <-exitCode)
	})

	t.Run("exit with peer close without shutdown gives 1", func(t *testing.T) {
		srv := &lifecycleTestServer{}
		serverConn, clientConn, client, state, cleanup := openLifecyclePair(t, srv)
		defer cleanup()

		_, err := client.Initialize(ctx, &protocol.InitializeParams{})
		require.NoError(t, err)
		require.NoError(t, client.Initialized(ctx, &protocol.InitializedParams{}))

		exitCode := make(chan int, 1)
		go func() {
			exitCode <- waitForServerExit(serverConn, state)
		}()
		require.NoError(t, client.Exit(ctx))
		require.NoError(t, clientConn.Close())
		require.Equal(t, 1, <-exitCode)
	})

	t.Run("peer EOF without exit uses conn fallback code 0", func(t *testing.T) {
		serverConn, clientConn, _, state, cleanup := openLifecyclePair(t, &lifecycleTestServer{})
		defer cleanup()

		require.NoError(t, clientConn.Close())

		exitCode := make(chan int, 1)
		go func() {
			exitCode <- waitForServerExit(serverConn, state)
		}()
		require.Equal(t, 0, <-exitCode)
	})
}

func TestLSPWire_CancelThenNextRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv := &lifecycleTestServer{
		completionUnblock: make(chan struct{}),
	}
	_, _, client, _, cleanup := openLifecyclePair(t, srv)
	defer cleanup()

	_, err := client.Initialize(ctx, &protocol.InitializeParams{})
	require.NoError(t, err)
	require.NoError(t, client.Initialized(ctx, &protocol.InitializedParams{}))

	blockErr := make(chan error, 1)
	blockCtx, blockCancel := context.WithCancel(ctx)
	go func() {
		_, err := client.Completion(blockCtx, completionParams("file:///wire-cancel.journal"))
		blockErr <- err
	}()

	require.Eventually(t, func() bool {
		_, _, _, completionCount, _ := srv.snapshot()
		return completionCount > 0
	}, time.Second, time.Millisecond)

	blockCancel()
	require.ErrorIs(t, <-blockErr, context.Canceled)

	close(srv.completionUnblock)
	_, err = client.Completion(context.Background(), completionParams("file:///wire-cancel.journal"))
	require.NoError(t, err)
}
