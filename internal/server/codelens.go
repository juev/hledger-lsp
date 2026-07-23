package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
)

func (s *Server) CodeLens(_ context.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	settings := s.getSettings()
	if !settings.Features.CodeLens {
		return nil, nil
	}

	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
	if len(journal.Transactions) == 0 {
		return nil, nil
	}

	lenses := make([]protocol.CodeLens, 0, len(journal.Transactions))

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		result := analyzer.CheckBalance(tx, decimal.NewFromFloat(settings.Diagnostics.BalanceTolerance))

		title := buildCodeLensTitle(result, len(tx.Postings))
		lens := protocol.CodeLens{
			Range:   *astRangeToProtocol(tx.Date.Range),
			Command: &protocol.Command{Title: title},
		}
		if !result.Balanced {
			// Carry the document URI and the transaction range so
			// ExecuteCommand can re-resolve the exact transaction and apply the
			// balance quickfix via workspace/applyEdit.
			lens.Command.Command = "hledger.fixUnbalanced"
			lens.Command.Arguments = []any{string(params.TextDocument.URI), *astRangeToProtocol(tx.Range)}
		}
		lenses = append(lenses, lens)
	}

	return lenses, nil
}

func (s *Server) CodeLensResolve(_ context.Context, params *protocol.CodeLens) (*protocol.CodeLens, error) {
	return params, nil
}

func buildCodeLensTitle(result *analyzer.BalanceResult, postingCount int) string {
	if result.Balanced {
		return fmt.Sprintf("\u2713 balanced | %d postings", postingCount)
	}

	commodities := make([]string, 0, len(result.Differences))
	for c := range result.Differences {
		commodities = append(commodities, c)
	}
	sort.Strings(commodities)

	var diffs []string
	for _, c := range commodities {
		diffs = append(diffs, fmt.Sprintf("%s off by %s", c, result.Differences[c].String()))
	}

	return fmt.Sprintf("\u2717 unbalanced: %s", strings.Join(diffs, ", "))
}
