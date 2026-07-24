package main

import (
	"context"
	"errors"
	"io"
	"sync"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

type lifecyclePhase int

const (
	lifecyclePhasePreInitialize lifecyclePhase = iota
	lifecyclePhaseInitializing
	lifecyclePhaseAwaitingInitialized
	lifecyclePhaseReady
	lifecyclePhaseShutdown
	lifecyclePhaseRetryableInitializeFailed
	lifecyclePhaseInitializeFailed
)

type lifecycleState struct {
	mu            sync.Mutex
	phase         lifecyclePhase
	exitCodeValue int
	exitSignal    bool
	exitCh        chan struct{}
}

func newLifecycleState() *lifecycleState {
	return &lifecycleState{
		phase:         lifecyclePhasePreInitialize,
		exitCodeValue: 1,
		exitCh:        make(chan struct{}, 1),
	}
}

func (s *lifecycleState) lifecycleMiddleware(next jsonrpc2.Handler) jsonrpc2.Handler {
	return func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		method := req.Method()

		if method == protocol.MethodExit {
			s.signalExit()
			return nil, nil
		}

		if method == protocol.MethodInitialize {
			return s.handleInitialize(next, ctx, req)
		}

		if method == protocol.MethodInitialized {
			if req.IsCall() {
				return nil, nil
			}
			if s.tryEnterReady() {
				return next(ctx, req)
			}
			return nil, nil
		}

		if method == protocol.MethodCancelRequest && !req.IsCall() {
			if !s.isReady() {
				return nil, nil
			}
		}

		if err := s.checkRequest(method, req.IsCall()); err != nil {
			return nil, err
		}

		if method == protocol.MethodShutdown && req.IsCall() {
			if !s.isReady() {
				return nil, nil
			}
			s.markShutdown()
			_, err := next(ctx, req)
			return nil, err
		}

		if !s.isReadyForRequest(method, req.IsCall()) {
			return nil, nil
		}

		return next(ctx, req)
	}
}

func (s *lifecycleState) handleInitialize(next jsonrpc2.Handler, ctx context.Context, req *jsonrpc2.Request) (any, error) {
	if !req.IsCall() {
		return nil, nil
	}

	if err := s.startInitialize(); err != nil {
		return nil, err
	}

	result, err := next(ctx, req)
	s.finishInitialize(err)
	return result, err
}

func lifecycleMiddleware(next jsonrpc2.Handler, state *lifecycleState) jsonrpc2.Handler {
	return state.lifecycleMiddleware(next)
}

func (s *lifecycleState) signalExit() {
	s.mu.Lock()
	defer s.mu.Unlock()

	code := 1
	if s.phase == lifecyclePhaseShutdown {
		code = 0
	}
	s.exitSignal = true
	s.exitCodeValue = code
	select {
	case s.exitCh <- struct{}{}:
	default:
	}
}

func (s *lifecycleState) exitSignalAndCode() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.exitSignal {
		return 0, false
	}
	return s.exitCodeValue, true
}

func (s *lifecycleState) markShutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.phase = lifecyclePhaseShutdown
	s.exitSignal = true
	s.exitCodeValue = 0
}

func (s *lifecycleState) setIfTransition(from, to lifecyclePhase) bool {
	if s.phase != from {
		return false
	}
	s.phase = to
	return true
}

func (s *lifecycleState) tryEnterReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.setIfTransition(lifecyclePhaseAwaitingInitialized, lifecyclePhaseReady)
}

func (s *lifecycleState) startInitialize() *jsonrpc2.Error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.phase {
	case lifecyclePhasePreInitialize, lifecyclePhaseRetryableInitializeFailed:
		s.phase = lifecyclePhaseInitializing
		return nil
	case lifecyclePhaseInitializing:
		return jsonrpc2.NewError(jsonrpc2.InvalidRequest, "initialize already in progress")
	case lifecyclePhaseAwaitingInitialized:
		return jsonrpc2.NewError(jsonrpc2.InvalidRequest, "already initialized")
	case lifecyclePhaseReady:
		return jsonrpc2.NewError(jsonrpc2.InvalidRequest, "server already initialized")
	case lifecyclePhaseShutdown:
		return jsonrpc2.NewError(jsonrpc2.InvalidRequest, "server is shutting down")
	case lifecyclePhaseInitializeFailed:
		return jsonrpc2.NewError(jsonrpc2.InvalidRequest, "initialize already failed")
	default:
		return jsonrpc2.NewError(jsonrpc2.Code(protocol.ErrorCodesInvalidRequest), "initialize is unavailable")
	}
}

func (s *lifecycleState) finishInitialize(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.phase != lifecyclePhaseInitializing {
		return
	}

	if err == nil {
		s.phase = lifecyclePhaseAwaitingInitialized
		return
	}

	if isRetryableInitializeError(err) {
		s.phase = lifecyclePhaseRetryableInitializeFailed
		return
	}

	s.phase = lifecyclePhaseInitializeFailed
}

func (s *lifecycleState) checkRequest(method string, isCall bool) *jsonrpc2.Error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.phase {
	case lifecyclePhasePreInitialize, lifecyclePhaseInitializing, lifecyclePhaseRetryableInitializeFailed, lifecyclePhaseInitializeFailed:
		if isCall {
			if method == protocol.MethodInitialized {
				return nil
			}
			return jsonrpc2.NewError(jsonrpc2.Code(protocol.ErrorCodesServerNotInitialized), "server not initialized")
		}
		return nil

	case lifecyclePhaseAwaitingInitialized:
		if !isCall {
			return nil
		}
		return jsonrpc2.NewError(jsonrpc2.InvalidRequest, "server not initialized")

	case lifecyclePhaseShutdown:
		if isCall {
			return jsonrpc2.NewError(jsonrpc2.InvalidRequest, "server is shutting down")
		}
		return nil
	case lifecyclePhaseReady:
		return nil
	}

	return nil
}

func (s *lifecycleState) isReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phase == lifecyclePhaseReady
}

func (s *lifecycleState) isReadyForRequest(method string, isCall bool) bool {
	if isCall {
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.phase {
	case lifecyclePhaseReady:
		return true
	case lifecyclePhaseAwaitingInitialized:
		return method == protocol.MethodInitialized
	case lifecyclePhasePreInitialize, lifecyclePhaseInitializing, lifecyclePhaseRetryableInitializeFailed, lifecyclePhaseInitializeFailed, lifecyclePhaseShutdown:
		return false
	default:
		return false
	}
}

func isRetryableInitializeError(err error) bool {
	var rpcErr *jsonrpc2.Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	var initErr protocol.InitializeError
	if err := protocol.Unmarshal(rpcErr.Data, &initErr); err != nil {
		return false
	}
	return initErr.Retry
}

func waitForServerExit(conn jsonrpc2.Conn, state *lifecycleState) int {
	for {
		select {
		case <-state.exitCh:
			if code, ok := state.exitSignalAndCode(); ok {
				return code
			}
		case <-conn.Done():
			if code, ok := state.exitSignalAndCode(); ok {
				return code
			}
			return exitCodeForConnErr(conn.Err())
		}
	}
}

func exitCodeForConnErr(err error) int {
	if err == nil || errors.Is(err, io.EOF) {
		return 0
	}
	return 1
}
