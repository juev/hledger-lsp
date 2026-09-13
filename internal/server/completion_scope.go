package server

import (
	"context"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// ScopedCompletionParams extends standard completion with a request-local account filter.
type ScopedCompletionParams struct {
	// Do not embed CompletionParams: its generated JSON methods omit added fields.
	TextDocument protocol.TextDocumentIdentifier `json:"textDocument"`
	Position     protocol.Position               `json:"position"`
	Context      protocol.CompletionContext      `json:"context,omitempty"`
	AccountScope string                          `json:"accountScope"`
}

// ScopedCompletionResult also identifies account input when the list is empty.
type ScopedCompletionResult struct {
	CompletionList *protocol.CompletionList `json:"completionList"`
	AccountRange   *protocol.Range          `json:"accountRange,omitempty"`
}

func (s *Server) scopedCompletionRequest(ctx context.Context, params any) (*ScopedCompletionResult, error) {
	raw, ok := params.(protocol.LSPAny)
	if !ok {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "Completion parameters must be a JSON object")
	}
	var decoded ScopedCompletionParams
	if err := protocol.Unmarshal(raw, &decoded); err != nil {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "Invalid completion parameters")
	}
	if decoded.TextDocument.URI == "" {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "Completion requires a document URI")
	}
	if decoded.AccountScope != "nonzero" && decoded.AccountScope != "all" {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "Account scope must be nonzero or all")
	}
	completionParams := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: decoded.TextDocument,
			Position:     decoded.Position,
		},
		Context: decoded.Context,
	}
	return s.completionWithScope(ctx, completionParams, decoded.AccountScope == "all")
}
