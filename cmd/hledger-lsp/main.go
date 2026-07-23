package main

import (
	"context"
	"encoding/json"
	"errors"
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
	srv := server.NewServer()
	_, conn, _ := newServerConnection(ctx, stdrwc{}, srv)
	<-conn.Done()

	if code := exitCodeForConnErr(conn.Err()); code != 0 {
		os.Exit(code)
	}
}

func newServerConnection(ctx context.Context, rwc io.ReadWriteCloser, srv *server.Server) (context.Context, jsonrpc2.Conn, protocol.Client) {
	return newProtocolServerConnection(ctx, rwc, newServerDispatcher(srv))
}

func newProtocolServerConnection(ctx context.Context, rwc io.ReadWriteCloser, srv protocol.Server) (context.Context, jsonrpc2.Conn, protocol.Client) {
	conn := jsonrpc2.NewConn(jsonrpc2.NewStream(rwc), jsonrpc2.WithCodec(lspCodec{}))
	client := protocol.ClientDispatcher(conn)
	ctx = protocol.WithClient(ctx, client)

	serverHandler := protocol.ServerHandler(srv, jsonrpc2.MethodNotFoundHandler)
	handler := protocol.CancelHandler(func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		if req.IsCall() {
			jsonrpc2.Async(ctx)
		}
		return serverHandler(ctx, req)
	})
	conn.Go(ctx, handler)

	return ctx, conn, client
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

func exitCodeForConnErr(err error) int {
	if err == nil || errors.Is(err, io.EOF) {
		return 0
	}
	return 1
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

type serverDispatcher struct {
	protocol.UnimplementedServer
	srv *server.Server
}

func newServerDispatcher(srv *server.Server) protocol.Server {
	return &serverDispatcher{srv: srv}
}

func (d *serverDispatcher) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	if client, ok := protocol.ClientFromContext(ctx); ok {
		d.srv.SetClient(client)
	}
	return d.srv.Initialize(ctx, params)
}

func (d *serverDispatcher) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	return d.srv.Initialized(ctx, params)
}

func (d *serverDispatcher) Shutdown(ctx context.Context) error {
	return d.srv.Shutdown(ctx)
}

func (d *serverDispatcher) Exit(ctx context.Context) error {
	return d.srv.Exit(ctx)
}

func (d *serverDispatcher) WorkDoneProgressCancel(ctx context.Context, params *protocol.WorkDoneProgressCancelParams) error {
	return nil
}

func (d *serverDispatcher) LogTrace(ctx context.Context, params *protocol.LogTraceParams) error {
	return nil
}

func (d *serverDispatcher) SetTrace(ctx context.Context, params *protocol.SetTraceParams) error {
	return nil
}

func (d *serverDispatcher) Progress(ctx context.Context, params *protocol.ProgressParams) error {
	return nil
}

func (d *serverDispatcher) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	return d.srv.CodeAction(ctx, params)
}

func (d *serverDispatcher) CodeLens(ctx context.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	return d.srv.CodeLens(ctx, params)
}

func (d *serverDispatcher) CodeLensResolve(ctx context.Context, params *protocol.CodeLens) (*protocol.CodeLens, error) {
	return d.srv.CodeLensResolve(ctx, params)
}

func (d *serverDispatcher) ColorPresentation(ctx context.Context, params *protocol.ColorPresentationParams) ([]protocol.ColorPresentation, error) {
	return nil, nil
}

func (d *serverDispatcher) Completion(ctx context.Context, params *protocol.CompletionParams) (protocol.CompletionResult, error) {
	result, err := d.srv.Completion(ctx, params)
	return result, err
}

func (d *serverDispatcher) CompletionResolve(ctx context.Context, params *protocol.CompletionItem) (*protocol.CompletionItem, error) {
	return d.srv.CompletionResolve(ctx, params)
}

func (d *serverDispatcher) Declaration(ctx context.Context, params *protocol.DeclarationParams) (protocol.DeclarationResult, error) {
	return nil, nil
}

func (d *serverDispatcher) Definition(ctx context.Context, params *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	result, err := d.srv.Definition(ctx, params)
	return protocol.LocationSlice(result), err
}

func (d *serverDispatcher) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	return d.srv.DidChange(ctx, params)
}

func (d *serverDispatcher) DidChangeConfiguration(ctx context.Context, params *protocol.DidChangeConfigurationParams) error {
	return d.srv.DidChangeConfiguration(ctx, params)
}

func (d *serverDispatcher) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	return d.srv.DidChangeWatchedFiles(ctx, params)
}

func (d *serverDispatcher) DidChangeWorkspaceFolders(ctx context.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
	return nil
}

func (d *serverDispatcher) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	return d.srv.DidClose(ctx, params)
}

func (d *serverDispatcher) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	return d.srv.DidOpen(ctx, params)
}

func (d *serverDispatcher) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	return d.srv.DidSave(ctx, params)
}

func (d *serverDispatcher) DocumentColor(ctx context.Context, params *protocol.DocumentColorParams) ([]protocol.ColorInformation, error) {
	return nil, nil
}

func (d *serverDispatcher) DocumentHighlight(ctx context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	return d.srv.DocumentHighlight(ctx, params)
}

func (d *serverDispatcher) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	return d.srv.DocumentLink(ctx, params)
}

func (d *serverDispatcher) DocumentLinkResolve(ctx context.Context, params *protocol.DocumentLink) (*protocol.DocumentLink, error) {
	return nil, nil
}

func (d *serverDispatcher) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	result, err := d.srv.DocumentSymbol(ctx, params)
	if err != nil {
		return nil, err
	}

	symbols := make(protocol.DocumentSymbolSlice, 0, len(result))
	for _, symbol := range result {
		documentSymbol, ok := symbol.(protocol.DocumentSymbol)
		if !ok {
			return nil, fmt.Errorf("unexpected document symbol type %T", symbol)
		}
		symbols = append(symbols, documentSymbol)
	}
	return symbols, nil
}

func (d *serverDispatcher) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (protocol.LSPAny, error) {
	return d.srv.ExecuteCommand(ctx, params)
}

func (d *serverDispatcher) FoldingRanges(ctx context.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	return d.srv.FoldingRanges(ctx, params)
}

func (d *serverDispatcher) Formatting(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	return d.srv.Format(ctx, params)
}

func (d *serverDispatcher) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	return d.srv.Hover(ctx, params)
}

func (d *serverDispatcher) Implementation(ctx context.Context, params *protocol.ImplementationParams) (protocol.DefinitionResult, error) {
	return nil, nil
}

func (d *serverDispatcher) OnTypeFormatting(ctx context.Context, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	return d.srv.OnTypeFormatting(ctx, params)
}

func (d *serverDispatcher) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (protocol.PrepareRenameResult, error) {
	result, err := d.srv.PrepareRename(ctx, params)
	return result, err
}

func (d *serverDispatcher) RangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	return d.srv.RangeFormat(ctx, params)
}

func (d *serverDispatcher) References(ctx context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	return d.srv.References(ctx, params)
}

func (d *serverDispatcher) Rename(ctx context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	return d.srv.Rename(ctx, params)
}

func (d *serverDispatcher) SignatureHelp(ctx context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	return nil, nil
}

func (d *serverDispatcher) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	result, err := d.srv.WorkspaceSymbol(ctx, params)
	return protocol.SymbolInformationSlice(result), err
}

func (d *serverDispatcher) TypeDefinition(ctx context.Context, params *protocol.TypeDefinitionParams) (protocol.DefinitionResult, error) {
	return nil, nil
}

func (d *serverDispatcher) WillSave(ctx context.Context, params *protocol.WillSaveTextDocumentParams) error {
	return nil
}

func (d *serverDispatcher) WillSaveWaitUntil(ctx context.Context, params *protocol.WillSaveTextDocumentParams) ([]protocol.TextEdit, error) {
	return d.srv.WillSaveWaitUntil(ctx, params)
}

func (d *serverDispatcher) ShowDocument(ctx context.Context, params *protocol.ShowDocumentParams) (*protocol.ShowDocumentResult, error) {
	return nil, nil
}

func (d *serverDispatcher) WillCreateFiles(ctx context.Context, params *protocol.CreateFilesParams) (*protocol.WorkspaceEdit, error) {
	return nil, nil
}

func (d *serverDispatcher) DidCreateFiles(ctx context.Context, params *protocol.CreateFilesParams) error {
	return nil
}

func (d *serverDispatcher) WillRenameFiles(ctx context.Context, params *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) {
	return nil, nil
}

func (d *serverDispatcher) DidRenameFiles(ctx context.Context, params *protocol.RenameFilesParams) error {
	return nil
}

func (d *serverDispatcher) WillDeleteFiles(ctx context.Context, params *protocol.DeleteFilesParams) (*protocol.WorkspaceEdit, error) {
	return nil, nil
}

func (d *serverDispatcher) DidDeleteFiles(ctx context.Context, params *protocol.DeleteFilesParams) error {
	return nil
}

func (d *serverDispatcher) SemanticTokensFull(ctx context.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	return d.srv.SemanticTokensFull(ctx, params)
}

func (d *serverDispatcher) SemanticTokensFullDelta(ctx context.Context, params *protocol.SemanticTokensDeltaParams) (protocol.SemanticTokensDeltaResult, error) {
	// Delta is not advertised (Delta: false); clients should not call this.
	// Fall back to a full response for safety.
	result, err := d.srv.SemanticTokensFull(ctx, &protocol.SemanticTokensParams{
		TextDocument: params.TextDocument,
	})
	return result, err
}

func (d *serverDispatcher) SemanticTokensRange(ctx context.Context, params *protocol.SemanticTokensRangeParams) (*protocol.SemanticTokens, error) {
	return d.srv.SemanticTokensRange(ctx, params)
}

func (d *serverDispatcher) SemanticTokensRefresh(ctx context.Context) error {
	return nil
}

func (d *serverDispatcher) LinkedEditingRange(ctx context.Context, params *protocol.LinkedEditingRangeParams) (*protocol.LinkedEditingRanges, error) {
	return nil, nil
}

func (d *serverDispatcher) Moniker(ctx context.Context, params *protocol.MonikerParams) ([]protocol.Moniker, error) {
	return nil, nil
}

func (d *serverDispatcher) PrepareCallHierarchy(ctx context.Context, params *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error) {
	return nil, nil
}

func (d *serverDispatcher) IncomingCalls(ctx context.Context, params *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error) {
	return nil, nil
}

func (d *serverDispatcher) OutgoingCalls(ctx context.Context, params *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error) {
	return nil, nil
}

func (d *serverDispatcher) InlineCompletion(ctx context.Context, params *protocol.InlineCompletionParams) (protocol.InlineCompletionResult, error) {
	return d.srv.InlineCompletion(ctx, params)
}

func (d *serverDispatcher) handlePayeeAccountHistory(ctx context.Context, params any) (any, error) {
	rawParams, ok := params.(protocol.LSPAny)
	if !ok {
		return nil, fmt.Errorf("hledger/payeeAccountHistory params type = %T, want protocol.LSPAny", params)
	}

	var decoded server.PayeeAccountHistoryParams
	if err := protocol.Unmarshal(rawParams, &decoded); err != nil {
		return nil, err
	}

	return d.srv.PayeeAccountHistory(ctx, json.RawMessage(rawParams))
}

func (d *serverDispatcher) CodeLensRefresh(ctx context.Context) error {
	return nil
}

func (d *serverDispatcher) SelectionRange(ctx context.Context, params *protocol.SelectionRangeParams) ([]protocol.SelectionRange, error) {
	return d.srv.SelectionRange(ctx, params)
}

func (d *serverDispatcher) Request(ctx context.Context, method string, params any) (any, error) {
	fmt.Fprintf(os.Stderr, "[LSP DEBUG] Request called: method=%s\n", method)
	if method == "hledger/payeeAccountHistory" {
		return d.handlePayeeAccountHistory(ctx, params)
	}
	return nil, nil
}
