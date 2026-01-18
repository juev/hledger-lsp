package main

import (
	"context"
	"fmt"
	"os"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.uber.org/zap"

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
	logger := zap.NewNop()

	srv := server.NewServer()
	handler := protocol.ServerHandler(newServerDispatcher(srv), nil)

	stream := jsonrpc2.NewStream(stdrwc{})
	conn := jsonrpc2.NewConn(stream)

	client := protocol.ClientDispatcher(conn, logger)
	srv.SetClient(client)

	conn.Go(ctx, handler)
	<-conn.Done()

	if err := conn.Err(); err != nil {
		os.Exit(1)
	}
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
	srv *server.Server
}

func newServerDispatcher(srv *server.Server) protocol.Server {
	return &serverDispatcher{srv: srv}
}

func (d *serverDispatcher) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
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

func (d *serverDispatcher) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	return nil, nil
}

func (d *serverDispatcher) CodeLens(ctx context.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	return nil, nil
}

func (d *serverDispatcher) CodeLensResolve(ctx context.Context, params *protocol.CodeLens) (*protocol.CodeLens, error) {
	return nil, nil
}

func (d *serverDispatcher) ColorPresentation(ctx context.Context, params *protocol.ColorPresentationParams) ([]protocol.ColorPresentation, error) {
	return nil, nil
}

func (d *serverDispatcher) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	return d.srv.Completion(ctx, params)
}

func (d *serverDispatcher) CompletionResolve(ctx context.Context, params *protocol.CompletionItem) (*protocol.CompletionItem, error) {
	return params, nil
}

func (d *serverDispatcher) Declaration(ctx context.Context, params *protocol.DeclarationParams) ([]protocol.Location, error) {
	return nil, nil
}

func (d *serverDispatcher) Definition(ctx context.Context, params *protocol.DefinitionParams) ([]protocol.Location, error) {
	return d.srv.Definition(ctx, params)
}

func (d *serverDispatcher) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	return d.srv.DidChange(ctx, params)
}

func (d *serverDispatcher) DidChangeConfiguration(ctx context.Context, params *protocol.DidChangeConfigurationParams) error {
	return d.srv.DidChangeConfiguration(ctx, params)
}

func (d *serverDispatcher) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	return nil
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
	return nil, nil
}

func (d *serverDispatcher) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	return d.srv.DocumentLink(ctx, params)
}

func (d *serverDispatcher) DocumentLinkResolve(ctx context.Context, params *protocol.DocumentLink) (*protocol.DocumentLink, error) {
	return nil, nil
}

func (d *serverDispatcher) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) ([]any, error) {
	return d.srv.DocumentSymbol(ctx, params)
}

func (d *serverDispatcher) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (any, error) {
	return nil, nil
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

func (d *serverDispatcher) Implementation(ctx context.Context, params *protocol.ImplementationParams) ([]protocol.Location, error) {
	return nil, nil
}

func (d *serverDispatcher) OnTypeFormatting(ctx context.Context, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	return nil, nil
}

func (d *serverDispatcher) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (*protocol.Range, error) {
	return d.srv.PrepareRename(ctx, params)
}

func (d *serverDispatcher) RangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	return nil, nil
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

func (d *serverDispatcher) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) ([]protocol.SymbolInformation, error) {
	return d.srv.WorkspaceSymbol(ctx, params)
}

func (d *serverDispatcher) TypeDefinition(ctx context.Context, params *protocol.TypeDefinitionParams) ([]protocol.Location, error) {
	return nil, nil
}

func (d *serverDispatcher) WillSave(ctx context.Context, params *protocol.WillSaveTextDocumentParams) error {
	return nil
}

func (d *serverDispatcher) WillSaveWaitUntil(ctx context.Context, params *protocol.WillSaveTextDocumentParams) ([]protocol.TextEdit, error) {
	return nil, nil
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

func (d *serverDispatcher) SemanticTokensFullDelta(ctx context.Context, params *protocol.SemanticTokensDeltaParams) (any, error) {
	return d.srv.SemanticTokensFullDelta(ctx, params)
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

func (d *serverDispatcher) NonstandardRequest(ctx context.Context, method string, params any) (any, error) {
	return nil, nil
}

func (d *serverDispatcher) CodeLensRefresh(ctx context.Context) error {
	return nil
}

func (d *serverDispatcher) SelectionRange(ctx context.Context, params *protocol.SelectionRangeParams) ([]protocol.SelectionRange, error) {
	return nil, nil
}

func (d *serverDispatcher) Request(ctx context.Context, method string, params any) (any, error) {
	return nil, nil
}
