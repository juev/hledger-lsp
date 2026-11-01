package server

import (
	"context"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

// InlayHint returns amount inference and optional balance and cost facts for a
// journal document. Semantic facts are cached per exact document content; LSP
// ranges and settings are applied for each request.
func (s *Server) InlayHint(_ context.Context, params *protocol.InlayHintParams) ([]protocol.InlayHint, error) {
	if params == nil {
		return nil, nil
	}
	settings := s.getSettings()
	if !settings.Features.InlayHints {
		return nil, nil
	}
	content, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, content)
	effects := s.cachedPostingEffects(params.TextDocument.URI, content)
	var resolved *include.ResolvedJournal
	var occurrenceID include.OccurrenceID
	if s.workspace != nil {
		tree, unique := s.workspace.GetUniqueResolvedForFile(uriToPath(params.TextDocument.URI))
		occurrences := []include.JournalOccurrence(nil)
		if unique && tree != nil {
			occurrences = tree.OccurrencesForCanonical(uriToPath(params.TextDocument.URI))
		}
		if len(occurrences) == 1 {
			resolved, occurrenceID = tree, occurrences[0].ID
		}
	}
	// Replaying the balances walks the whole include tree, so it runs only when
	// the result will be read.
	var runningBalances map[analyzer.PostingRef]map[string]decimal.Decimal
	if settings.InlayHints.RunningBalances {
		switch {
		case resolved != nil:
			runningBalances = analyzer.RunningBalances(resolved.TransactionsWithSource(), occurrenceID)
		case s.workspace == nil:
			runningBalances = analyzer.RunningBalances(journalTransactions(journal), 0)
		}
	}
	mapper := lsputil.NewPositionMapper(content)
	hints := make([]protocol.InlayHint, 0)
	for _, effect := range effects {
		transaction := &journal.Transactions[effect.TransactionIndex]
		posting := &transaction.Postings[effect.PostingIndex]
		// Commodity formats below are rebuilt per posting, so a posting the client
		// did not ask about must not reach them.
		if !postingTouchesRange(mapper, posting, params.Range) {
			continue
		}
		formats := localCommodityFormatsAt(journal, posting.Range.Start.Offset)
		if resolved != nil {
			formats = resolved.FormatsAt(occurrenceID, posting.Range.Start.Offset)
		}
		if settings.InlayHints.InferredAmounts && len(effect.InferredAmounts) > 0 {
			// Anchored past the end of the line rather than after the account
			// name: an inlay hint takes up room in the rendered line, so a hint
			// placed mid-line pushes the alignment padding — and the cursor
			// waiting at the amount column — to the right.
			accountEnd := mapper.ByteToLSP(posting.Account.Range.End.Offset)
			hints = appendHintInRange(hints, params.Range, protocol.InlayHint{
				Position:    mapper.LineEndPosition(int(accountEnd.Line)),
				Label:       protocol.String("= " + formatAmounts(effect.InferredAmounts, formats)),
				Kind:        protocol.InlayHintKindType,
				PaddingLeft: boolPtr(true),
			})
		}
		if settings.InlayHints.CostExpansion && len(effect.CostContribution) > 0 && posting.Cost != nil {
			hints = appendHintInRange(hints, params.Range, protocol.InlayHint{
				Position:    mapper.ByteToLSP(posting.Cost.Range.End.Offset),
				Label:       protocol.String("cost: " + formatAmounts(effect.CostContribution, formats)),
				Kind:        protocol.InlayHintKindType,
				PaddingLeft: boolPtr(true),
			})
		}
		if amounts := runningBalances[analyzer.PostingRef{TransactionIndex: effect.TransactionIndex, PostingIndex: effect.PostingIndex}]; len(amounts) > 0 {
			hints = appendHintInRange(hints, params.Range, protocol.InlayHint{
				Position:    mapper.ByteToLSP(posting.Range.End.Offset),
				Label:       protocol.String("balance: " + formatAmounts(amounts, formats)),
				Kind:        protocol.InlayHintKindType,
				PaddingLeft: boolPtr(true),
			})
		}
	}
	return hints, nil
}

// journalTransactions presents a lone journal the way a resolved one would.
func journalTransactions(journal *ast.Journal) []include.SourcedTransaction {
	sourced := make([]include.SourcedTransaction, len(journal.Transactions))
	for i := range journal.Transactions {
		sourced[i] = include.SourcedTransaction{Index: i, Transaction: journal.Transactions[i]}
	}
	return sourced
}

func appendHintInRange(hints []protocol.InlayHint, requestRange protocol.Range, hint protocol.InlayHint) []protocol.InlayHint {
	if positionInProtocolRange(hint.Position, requestRange) {
		return append(hints, hint)
	}
	return hints
}

// postingTouchesRange reports whether any line of posting lies in rng. Every hint
// for a posting sits on one of its lines, so this only ever discards postings
// that could not produce a visible hint.
func postingTouchesRange(mapper *lsputil.PositionMapper, posting *ast.Posting, rng protocol.Range) bool {
	first := mapper.ByteToLSP(posting.Range.Start.Offset).Line
	last := mapper.ByteToLSP(posting.Range.End.Offset).Line
	return last >= rng.Start.Line && first <= rng.End.Line
}

func positionInProtocolRange(position protocol.Position, rng protocol.Range) bool {
	if position.Line < rng.Start.Line || position.Line > rng.End.Line {
		return false
	}
	if position.Line == rng.Start.Line && position.Character < rng.Start.Character {
		return false
	}
	return position.Line != rng.End.Line || position.Character < rng.End.Character
}

func localCommodityFormatsAt(journal *ast.Journal, offset int) map[string]formatter.CommodityFormat {
	directives := make([]ast.Directive, 0, len(journal.Directives))
	for _, directive := range journal.Directives {
		if directive.GetRange().Start.Offset <= offset {
			directives = append(directives, directive)
		}
	}
	return formatter.ExtractCommodityFormats(directives)
}

func formatAmounts(amounts map[string]decimal.Decimal, formats map[string]formatter.CommodityFormat) string {
	commodities := make([]string, 0, len(amounts))
	for commodity := range amounts {
		commodities = append(commodities, commodity)
	}
	sort.Strings(commodities)
	formatted := make([]string, 0, len(commodities))
	for _, commodity := range commodities {
		formatted = append(formatted, formatter.FormatBalance(amounts[commodity], commodity, formats))
	}
	return strings.Join(formatted, ", ")
}
