package server

import (
	"context"
	"fmt"

	"go.lsp.dev/protocol"
)

var _ protocol.Server = (*Server)(nil)

// featureEnabled reports whether a feature flag is currently on. Feature flags are
// read at request time: a client that toggles a setting expects the behaviour to
// change without restarting the server. (The advertised capability set is fixed
// during initialize, which is an LSP limitation.)
func (s *Server) featureEnabled(feature func(featureSettings) bool) bool {
	return feature(s.getSettings().Features)
}

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (protocol.CompletionResult, error) {
	if !s.featureEnabled(func(f featureSettings) bool { return f.Completion }) {
		return nil, nil
	}
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
	if !s.clientCapabilities.supportsHierarchicalDocumentSymbols {
		return protocol.SymbolInformationSlice(flattenDocumentSymbols(params.TextDocument.URI, symbols)), nil
	}
	return protocol.DocumentSymbolSlice(symbols), nil
}

func (s *Server) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (protocol.PrepareRenameResult, error) {
	return s.prepareRename(ctx, params)
}

func (s *Server) Formatting(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	if !s.featureEnabled(func(f featureSettings) bool { return f.Formatting }) {
		return nil, nil
	}
	return s.Format(ctx, params)
}

func (s *Server) RangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	if !s.featureEnabled(func(f featureSettings) bool { return f.Formatting }) {
		return nil, nil
	}
	return s.rangeFormatting(ctx, params)
}

func (s *Server) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	if !s.featureEnabled(func(f featureSettings) bool { return f.WorkspaceSymbol }) {
		return nil, nil
	}
	symbols, err := s.workspaceSymbols(ctx, params)
	if err != nil || len(symbols) == 0 {
		return nil, err
	}
	return protocol.SymbolInformationSlice(symbols), nil
}

func (s *Server) PrepareTypeHierarchy(ctx context.Context, params *protocol.TypeHierarchyPrepareParams) ([]protocol.TypeHierarchyItem, error) {
	return s.prepareTypeHierarchy(ctx, params)
}

func (s *Server) Supertypes(ctx context.Context, params *protocol.TypeHierarchySupertypesParams) ([]protocol.TypeHierarchyItem, error) {
	return s.typeHierarchySupertypes(ctx, params)
}

func (s *Server) Subtypes(ctx context.Context, params *protocol.TypeHierarchySubtypesParams) ([]protocol.TypeHierarchyItem, error) {
	return s.typeHierarchySubtypes(ctx, params)
}

func (s *Server) Request(ctx context.Context, method string, params any) (any, error) {
	if method == "hledger/completion" {
		result, err := s.scopedCompletionRequest(ctx, params)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
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
