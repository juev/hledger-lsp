package server

import (
	"context"

	"go.lsp.dev/protocol"
)

type PayeeAccountHistoryParams struct {
	TextDocument protocol.TextDocumentIdentifier `json:"textDocument"`
}

type PayeeAccountHistoryResult struct {
	PayeeAccounts map[string][]string `json:"payeeAccounts"`
	PairUsage     map[string]int      `json:"pairUsage"`
}

func (s *Server) PayeeAccountHistory(_ context.Context, params *PayeeAccountHistoryParams) (*PayeeAccountHistoryResult, error) {
	content, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return &PayeeAccountHistoryResult{
			PayeeAccounts: make(map[string][]string),
			PairUsage:     make(map[string]int),
		}, nil
	}

	if resolved := s.getWorkspaceResolved(params.TextDocument.URI); resolved != nil {
		result := s.analyzer.AnalyzeResolved(resolved)
		return &PayeeAccountHistoryResult{
			PayeeAccounts: result.PayeeAccounts,
			PairUsage:     result.PayeeAccountPairUsage,
		}, nil
	}

	journal, errs := s.cachedJournal(params.TextDocument.URI, content)
	if len(errs) > 0 || journal == nil {
		return &PayeeAccountHistoryResult{
			PayeeAccounts: make(map[string][]string),
			PairUsage:     make(map[string]int),
		}, nil
	}
	result := s.analyzer.Analyze(journal)

	return &PayeeAccountHistoryResult{
		PayeeAccounts: result.PayeeAccounts,
		PairUsage:     result.PayeeAccountPairUsage,
	}, nil
}
