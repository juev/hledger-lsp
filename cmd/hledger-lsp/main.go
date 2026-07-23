package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/server"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("hledger-lsp %s (commit: %s, built: %s)\n", Version, Commit, Date)
		return
	}

	ctx := context.Background()
	srv := server.NewServerWithVersion(Version)
	_, conn, _, lifecycleState := newServerConnection(ctx, stdrwc{}, srv)
	code := waitForServerExit(conn, lifecycleState)
	if code != 0 {
		os.Exit(code)
	}
}

func newServerConnection(ctx context.Context, rwc io.ReadWriteCloser, srv *server.Server) (context.Context, jsonrpc2.Conn, protocol.Client, *lifecycleState) {
	return newProtocolServerConnection(ctx, rwc, srv)
}

func newProtocolServerConnection(ctx context.Context, rwc io.ReadWriteCloser, srv protocol.Server) (context.Context, jsonrpc2.Conn, protocol.Client, *lifecycleState) {
	conn := jsonrpc2.NewConn(jsonrpc2.NewStream(rwc), jsonrpc2.WithCodec(lspCodec{}))
	client := protocol.ClientDispatcher(conn)
	ctx = protocol.WithClient(ctx, client)

	lifecycleState := newLifecycleState()
	serverHandler := protocol.ServerHandler(srv, jsonrpc2.MethodNotFoundHandler)
	serverAsyncHandler := func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		if req.IsCall() {
			jsonrpc2.Async(ctx)
		}
		return serverHandler(ctx, req)
	}
	handler := protocol.CancelHandler(serverAsyncHandler)
	conn.Go(ctx, lifecycleMiddleware(handler, lifecycleState))

	return ctx, conn, client, lifecycleState
}

type lspCodec struct{}

func (lspCodec) Marshal(v any) ([]byte, error) {
	switch m := v.(type) {
	case jsonrpc2.RawMessage:
		if m == nil {
			return []byte("null"), nil
		}
		return m, nil
	case *jsonrpc2.RawMessage:
		if m == nil || *m == nil {
			return []byte("null"), nil
		}
		return *m, nil
	}
	return protocol.Marshal(v)
}

func (lspCodec) Unmarshal(data []byte, v any) error {
	if p, ok := v.(*jsonrpc2.RawMessage); ok {
		b := make(jsonrpc2.RawMessage, len(data))
		copy(b, data)
		*p = b
		return nil
	}
	return protocol.Unmarshal(data, v)
}

type stdrwc struct{}

func (stdrwc) Read(p []byte) (int, error) {
	return os.Stdin.Read(p)
}

func (stdrwc) Write(p []byte) (int, error) {
	return os.Stdout.Write(p)
}

func (stdrwc) Close() error {
	return nil
}
