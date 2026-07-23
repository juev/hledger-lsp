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
	runningBalances := chronologicalBalances(journal)
	var occurrenceID include.OccurrenceID
	runningFromResolved := map[postingKey]analyzer.AccountBalances(nil)
	if s.workspace != nil {
		resolved, unique := s.workspace.GetUniqueResolvedForFile(uriToPath(params.TextDocument.URI))
		occurrences := []include.JournalOccurrence(nil)
		if unique && resolved != nil {
			occurrences = resolved.OccurrencesForCanonical(uriToPath(params.TextDocument.URI))
		}
		if len(occurrences) != 1 {
			runningBalances = nil
		} else {
			occurrenceID = occurrences[0].ID
			runningFromResolved = resolvedChronologicalBalances(resolved)
		}
	}
	mapper := lsputil.NewPositionMapper(content)
	hints := make([]protocol.InlayHint, 0)
	for _, effect := range effects {
		transaction := &journal.Transactions[effect.TransactionIndex]
		posting := &transaction.Postings[effect.PostingIndex]
		formats := localCommodityFormatsAt(journal, posting.Range.Start.Offset)
		if occurrenceID != 0 {
			if resolved, ok := s.workspace.GetUniqueResolvedForFile(uriToPath(params.TextDocument.URI)); ok {
				formats = resolved.FormatsAt(occurrenceID, posting.Range.Start.Offset)
			}
		}
		if settings.InlayHints.InferredAmounts && len(effect.InferredAmounts) > 0 {
			hints = appendHintInRange(hints, params.Range, protocol.InlayHint{
				Position:    mapper.ByteToLSP(posting.Account.Range.End.Offset),
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
		if settings.InlayHints.RunningBalances && !transactionHasPostingDateOverride(transaction) {
			account := posting.Account.GetResolvedName()
			balances := runningBalances
			if runningFromResolved != nil {
				balances = runningFromResolved
			}
			if amounts := balances[postingKey{effect.TransactionIndex, effect.PostingIndex}][account]; len(amounts) > 0 {
				hints = appendHintInRange(hints, params.Range, protocol.InlayHint{
					Position:    mapper.ByteToLSP(posting.Range.End.Offset),
					Label:       protocol.String("balance: " + formatAmounts(amounts, formats)),
					Kind:        protocol.InlayHintKindType,
					PaddingLeft: boolPtr(true),
				})
			}
		}
	}
	return hints, nil
}

func resolvedChronologicalBalances(resolved *include.ResolvedJournal) map[postingKey]analyzer.AccountBalances {
	type ref struct {
		id          include.OccurrenceID
		index       int
		transaction ast.Transaction
	}
	refs := make([]ref, 0)
	for _, item := range resolved.Items {
		if item.Kind != include.ResolvedItemTransaction {
			continue
		}
		occurrence := resolved.Occurrence(item.OccurrenceID)
		if occurrence == nil || occurrence.Journal == nil || item.Index < 0 || item.Index >= len(occurrence.Journal.Transactions) {
			continue
		}
		refs = append(refs, ref{item.OccurrenceID, item.Index, occurrence.Journal.Transactions[item.Index]})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		left, right := refs[i].transaction.Date, refs[j].transaction.Date
		if left.Year != right.Year {
			return left.Year < right.Year
		}
		if left.Month != right.Month {
			return left.Month < right.Month
		}
		return left.Day < right.Day
	})
	transactions := make([]ast.Transaction, len(refs))
	for i := range refs {
		transactions[i] = refs[i].transaction
	}
	effects := analyzer.CalculatePostingEffects(&ast.Journal{Transactions: transactions})
	balances := make(map[postingKey]analyzer.AccountBalances, len(effects))
	for _, effect := range effects {
		balances[postingKey{refs[effect.TransactionIndex].index, effect.PostingIndex}] = effect.BalanceAfter
	}
	return balances
}

func transactionHasPostingDateOverride(transaction *ast.Transaction) bool {
	for i := range transaction.Postings {
		for _, tag := range transaction.Postings[i].Tags {
			if tag.Name == "date" || tag.Name == "date2" {
				return true
			}
		}
	}
	return false
}

type postingKey struct {
	transactionIndex int
	postingIndex     int
}

func chronologicalBalances(journal *ast.Journal) map[postingKey]analyzer.AccountBalances {
	indices := make([]int, len(journal.Transactions))
	for i := range indices {
		indices[i] = i
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left := journal.Transactions[indices[i]].Date
		right := journal.Transactions[indices[j]].Date
		if left.Year != right.Year {
			return left.Year < right.Year
		}
		if left.Month != right.Month {
			return left.Month < right.Month
		}
		return left.Day < right.Day
	})
	transactions := make([]ast.Transaction, len(indices))
	for i, index := range indices {
		transactions[i] = journal.Transactions[index]
	}
	effects := analyzer.CalculatePostingEffects(&ast.Journal{Transactions: transactions})
	balances := make(map[postingKey]analyzer.AccountBalances, len(effects))
	for _, effect := range effects {
		balances[postingKey{indices[effect.TransactionIndex], effect.PostingIndex}] = effect.BalanceAfter
	}
	return balances
}

func appendHintInRange(hints []protocol.InlayHint, requestRange protocol.Range, hint protocol.InlayHint) []protocol.InlayHint {
	if positionInProtocolRange(hint.Position, requestRange) {
		return append(hints, hint)
	}
	return hints
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
