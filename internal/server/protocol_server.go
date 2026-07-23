package server

import (
	"context"
	"fmt"

	"go.lsp.dev/protocol"
)

var _ protocol.Server = (*Server)(nil)

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (protocol.CompletionResult, error) {
	return s.completion(ctx, params)
}

func (s *Server) Definition(ctx context.Context, params *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	locations, err := s.definition(ctx, params)
	if err != nil || len(locations) == 0 {
		return nil, err
	}
	return protocol.LocationSlice(locations), nil
}

func (s *Server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	symbols := s.documentSymbols(ctx, params)
	if len(symbols) == 0 {
		return nil, nil
	}
	return protocol.DocumentSymbolSlice(symbols), nil
}

func (s *Server) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (protocol.PrepareRenameResult, error) {
	return s.prepareRename(ctx, params)
}

func (s *Server) RangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	return s.rangeFormatting(ctx, params)
}

func (s *Server) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	symbols, err := s.workspaceSymbols(ctx, params)
	if err != nil || len(symbols) == 0 {
		return nil, err
	}
	return protocol.SymbolInformationSlice(symbols), nil
}

func (s *Server) Request(ctx context.Context, method string, params any) (any, error) {
	if method != "hledger/payeeAccountHistory" {
		return s.UnimplementedServer.Request(ctx, method, params)
	}

	raw, ok := params.(protocol.LSPAny)
	if !ok {
		return nil, fmt.Errorf("hledger/payeeAccountHistory params type = %T, want protocol.LSPAny", params)
	}

	var decoded PayeeAccountHistoryParams
	if err := protocol.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return s.PayeeAccountHistory(ctx, &decoded)
}

func (s *Server) rangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	return s.RangeFormat(ctx, params)
}

func (s *Server) workspaceSymbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) ([]protocol.SymbolInformation, error) {
	return s.WorkspaceSymbol(ctx, params)
}
